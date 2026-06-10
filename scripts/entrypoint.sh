#!/bin/bash

set -e

CONFIG_FILE="/config/backups.conf"
CRONTAB_FILE="/etc/cron.d/db-backups"
JOBS_DIR="/etc/backup-jobs"

echo "Starting Database Backup Container..."

# Configure the timezone at runtime
if [ -n "$TZ" ] && [ -f "/usr/share/zoneinfo/$TZ" ]; then
    ln -snf "/usr/share/zoneinfo/$TZ" /etc/localtime
    echo "$TZ" > /etc/timezone
    echo "Timezone configured: $TZ"
else
    echo "Timezone: UTC (default)"
fi

# Check that the configuration file exists
if [ ! -f "${CONFIG_FILE}" ]; then
    echo "Error: Configuration file not found at ${CONFIG_FILE}"
    echo "Please mount a configuration file at /config/backups.conf"
    exit 1
fi

# Warn if the configuration file is readable by group or others
CONFIG_PERMS=$(stat -c '%a' "${CONFIG_FILE}")
if (( (8#${CONFIG_PERMS} & 8#044) != 0 )); then
    echo "Warning: ${CONFIG_FILE} is readable by group/other (mode ${CONFIG_PERMS})."
    echo "         It contains database passwords: run 'chmod 600 backups.conf' on the host."
fi

# =============================================================================
# Configuration field validation
# =============================================================================
# Every field interpolated into the crontab or into backup paths is strictly
# validated to prevent command injection in the cron lines (executed as root)
# and directory traversal in /backups.

is_valid_cron() { [[ "$1" =~ ^[A-Za-z0-9*,/@[:space:]-]+$ ]]; }
is_valid_name() { [[ "$1" =~ ^[A-Za-z0-9._-]+$ ]] && [ "$1" != "." ] && [ "$1" != ".." ]; }
is_valid_number() { [[ "$1" =~ ^[0-9]+$ ]]; }

# =============================================================================
# Detect which database clients are needed
# =============================================================================
echo "Detecting required database clients..."

PG_VERSIONS_NEEDED=""
NEED_MARIADB=false
NEED_MONGODB=false

while IFS= read -r line || [ -n "$line" ]; do
    [[ -z "$line" || "$line" =~ ^[[:space:]]*# ]] && continue

    IFS='|' read -r _ TYPE _ _ _ _ _ _ PG_VERSION _ <<< "$line"

    case "$TYPE" in
        postgres)
            PG_VERSION=${PG_VERSION:-18}
            if [[ ! " ${PG_VERSIONS_NEEDED} " =~ " ${PG_VERSION} " ]]; then
                PG_VERSIONS_NEEDED="${PG_VERSIONS_NEEDED} ${PG_VERSION}"
            fi
            ;;
        mariadb|mysql)
            NEED_MARIADB=true
            ;;
        mongodb)
            NEED_MONGODB=true
            ;;
    esac
done < "${CONFIG_FILE}"

# =============================================================================
# Install PostgreSQL clients
# =============================================================================
echo ""
echo "PostgreSQL:"
if [ -n "${PG_VERSIONS_NEEDED}" ]; then
    PG_PACKAGES=""
    for version in ${PG_VERSIONS_NEEDED}; do
        if ! is_valid_number "${version}"; then
            echo "  ✗ Invalid PostgreSQL version '${version}', skipping"
            continue
        fi
        if [ ! -x "/usr/lib/postgresql/${version}/bin/pg_dump" ]; then
            PG_PACKAGES="${PG_PACKAGES} postgresql-client-${version}"
        else
            echo "  ✓ Client version ${version} already installed"
        fi
    done

    if [ -n "${PG_PACKAGES}" ]; then
        echo "  Installing:${PG_PACKAGES}"
        apt-get update -qq
        apt-get install -y -qq ${PG_PACKAGES}
        rm -rf /var/lib/apt/lists/*
        echo "  ✓ PostgreSQL clients installed successfully"
    fi
else
    echo "  ⊘ Not configured, skipping"
fi

# =============================================================================
# Install the MariaDB/MySQL client
# =============================================================================
echo ""
echo "MariaDB/MySQL:"
if [ "${NEED_MARIADB}" = true ]; then
    if command -v mysqldump &> /dev/null; then
        echo "  ✓ Client already installed"
    else
        echo "  Installing mariadb-client..."
        apt-get update -qq
        apt-get install -y -qq mariadb-client
        rm -rf /var/lib/apt/lists/*
        echo "  ✓ MariaDB client installed successfully"
    fi
else
    echo "  ⊘ Not configured, skipping"
fi

# =============================================================================
# Install the MongoDB tools
# =============================================================================
echo ""
echo "MongoDB:"
if [ "${NEED_MONGODB}" = true ]; then
    if command -v mongodump &> /dev/null && command -v mongosh &> /dev/null; then
        echo "  ✓ Tools already installed"
    else
        echo "  Installing MongoDB Database Tools and Shell..."

        ARCH=$(uname -m)
        case "${ARCH}" in
            x86_64)
                MONGO_ARCH="x86_64"
                MONGOSH_ARCH="x64"
                MONGO_TOOLS_SHA256="${MONGO_TOOLS_SHA256_X86_64}"
                MONGOSH_SHA256="${MONGOSH_SHA256_X64}"
                ;;
            aarch64)
                MONGO_ARCH="arm64"
                MONGOSH_ARCH="arm64"
                MONGO_TOOLS_SHA256="${MONGO_TOOLS_SHA256_ARM64}"
                MONGOSH_SHA256="${MONGOSH_SHA256_ARM64}"
                ;;
            *) echo "  ✗ Unsupported architecture: ${ARCH}" && exit 1 ;;
        esac

        # MongoDB Database Tools
        if ! command -v mongodump &> /dev/null; then
            echo "  Installing MongoDB Database Tools ${MONGO_TOOLS_VERSION}..."
            wget -q "https://fastdl.mongodb.org/tools/db/mongodb-database-tools-ubuntu2204-${MONGO_ARCH}-${MONGO_TOOLS_VERSION}.tgz" -O /tmp/mongodb-tools.tgz
            if ! echo "${MONGO_TOOLS_SHA256}  /tmp/mongodb-tools.tgz" | sha256sum -c - > /dev/null 2>&1; then
                echo "  ✗ SHA256 checksum verification failed for MongoDB Database Tools"
                rm -f /tmp/mongodb-tools.tgz
                exit 1
            fi
            tar -xzf /tmp/mongodb-tools.tgz -C /tmp
            cp /tmp/mongodb-database-tools-*/bin/* /usr/local/bin/
            rm -rf /tmp/mongodb-tools.tgz /tmp/mongodb-database-tools-*
        fi

        # MongoDB Shell
        if ! command -v mongosh &> /dev/null; then
            echo "  Installing MongoDB Shell ${MONGOSH_VERSION}..."
            wget -q "https://downloads.mongodb.com/compass/mongosh-${MONGOSH_VERSION}-linux-${MONGOSH_ARCH}.tgz" -O /tmp/mongosh.tgz
            if ! echo "${MONGOSH_SHA256}  /tmp/mongosh.tgz" | sha256sum -c - > /dev/null 2>&1; then
                echo "  ✗ SHA256 checksum verification failed for MongoDB Shell"
                rm -f /tmp/mongosh.tgz
                exit 1
            fi
            tar -xzf /tmp/mongosh.tgz -C /tmp
            cp /tmp/mongosh-${MONGOSH_VERSION}-linux-${MONGOSH_ARCH}/bin/mongosh /usr/local/bin/
            chmod +x /usr/local/bin/mongosh
            rm -rf /tmp/mongosh.tgz /tmp/mongosh-*
        fi

        echo "  ✓ MongoDB tools installed successfully"
    fi
else
    echo "  ⊘ Not configured, skipping"
fi

echo ""

# =============================================================================
# Cron job configuration
# =============================================================================

# Files generated from this point on contain secrets: root-only access
umask 077

# Prepare the job definitions directory (contains the passwords)
rm -rf "${JOBS_DIR}"
install -d -m 700 "${JOBS_DIR}"

# Normalize the optional encryption passphrase into a 600 file
# so the secret is never exposed in the crontab
ENCRYPTION_PASSPHRASE_FILE=""
if [ -n "${BACKUP_ENCRYPTION_PASSPHRASE_FILE}" ]; then
    if [ ! -f "${BACKUP_ENCRYPTION_PASSPHRASE_FILE}" ]; then
        echo "Error: BACKUP_ENCRYPTION_PASSPHRASE_FILE is set but '${BACKUP_ENCRYPTION_PASSPHRASE_FILE}' does not exist"
        exit 1
    fi
    cp "${BACKUP_ENCRYPTION_PASSPHRASE_FILE}" "${JOBS_DIR}/encryption.passphrase"
    ENCRYPTION_PASSPHRASE_FILE="${JOBS_DIR}/encryption.passphrase"
elif [ -n "${BACKUP_ENCRYPTION_PASSPHRASE}" ]; then
    printf '%s' "${BACKUP_ENCRYPTION_PASSPHRASE}" > "${JOBS_DIR}/encryption.passphrase"
    ENCRYPTION_PASSPHRASE_FILE="${JOBS_DIR}/encryption.passphrase"
fi
if [ -n "${ENCRYPTION_PASSPHRASE_FILE}" ]; then
    chmod 600 "${ENCRYPTION_PASSPHRASE_FILE}"
    echo "Backup encryption: enabled (GPG symmetric, AES256)"
fi

# Create the crontab file
echo "# Database backup cron jobs" > "${CRONTAB_FILE}"
echo "SHELL=/bin/bash" >> "${CRONTAB_FILE}"
echo "PATH=/usr/local/sbin:/usr/local/bin:/sbin:/bin:/usr/sbin:/usr/bin" >> "${CRONTAB_FILE}"
# Pass the timezone to cron so that logs and timestamps are correct
if [ -n "$TZ" ]; then
    echo "TZ=${TZ}" >> "${CRONTAB_FILE}"
fi
if [ -n "${ENCRYPTION_PASSPHRASE_FILE}" ]; then
    echo "BACKUP_ENCRYPTION_PASSPHRASE_FILE=${ENCRYPTION_PASSPHRASE_FILE}" >> "${CRONTAB_FILE}"
fi
echo "" >> "${CRONTAB_FILE}"

# Read the configuration file and generate the cron entries
echo "Loading backup configurations from ${CONFIG_FILE}..."

JOB_INDEX=0
while IFS= read -r line || [ -n "$line" ]; do
    # Skip empty lines and comments
    [[ -z "$line" || "$line" =~ ^[[:space:]]*# ]] && continue

    # Parse the configuration line
    # Expected format: CRON_SCHEDULE|TYPE|HOST|PORT|DATABASE|USER|PASSWORD|RETENTION_DAYS|PG_VERSION|TLS
    IFS='|' read -r CRON_SCHEDULE TYPE HOST PORT DATABASE USER PASSWORD RETENTION_DAYS PG_VERSION TLS <<< "$line"

    # Validate the required fields
    # Note: never print the raw line in the logs, it contains the password
    if [ -z "$CRON_SCHEDULE" ] || [ -z "$TYPE" ] || [ -z "$HOST" ] || [ -z "$DATABASE" ]; then
        echo "Warning: Invalid configuration line (missing required field), skipping (type: '${TYPE}', host: '${HOST}', database: '${DATABASE}')"
        continue
    fi

    case "$TYPE" in
        postgres|mariadb|mysql|mongodb) ;;
        *)
            echo "Warning: Unknown database type '${TYPE}', skipping"
            continue
            ;;
    esac

    # For PostgreSQL and MariaDB/MySQL, the user is required
    if [ "$TYPE" != "mongodb" ] && [ -z "$USER" ]; then
        echo "Warning: USER is required for ${TYPE} (host: '${HOST}', database: '${DATABASE}'), skipping"
        continue
    fi

    # Apply default values when not specified
    if [ -z "$PORT" ]; then
        case "$TYPE" in
            postgres)
                PORT="5432"
                ;;
            mariadb|mysql)
                PORT="3306"
                ;;
            mongodb)
                PORT="27017"
                ;;
            *)
                PORT="5432"
                ;;
        esac
    fi
    RETENTION_DAYS=${RETENTION_DAYS:-7}

    # For PostgreSQL, set the default version when not specified
    if [ "$TYPE" = "postgres" ]; then
        PG_VERSION=${PG_VERSION:-18}
    fi

    # Strict validation of every field
    if ! is_valid_cron "$CRON_SCHEDULE"; then
        echo "Warning: Invalid CRON_SCHEDULE '${CRON_SCHEDULE}' (database: '${DATABASE}'), skipping"
        continue
    fi
    if ! is_valid_name "$HOST"; then
        echo "Warning: Invalid HOST '${HOST}' (database: '${DATABASE}'), skipping"
        continue
    fi
    if ! is_valid_number "$PORT"; then
        echo "Warning: Invalid PORT '${PORT}' (database: '${DATABASE}'), skipping"
        continue
    fi
    if ! is_valid_name "$DATABASE"; then
        echo "Warning: Invalid DATABASE name '${DATABASE}', skipping"
        continue
    fi
    if [ -n "$USER" ] && ! is_valid_name "$USER"; then
        echo "Warning: Invalid USER '${USER}' (database: '${DATABASE}'), skipping"
        continue
    fi
    if ! is_valid_number "$RETENTION_DAYS"; then
        echo "Warning: Invalid RETENTION_DAYS '${RETENTION_DAYS}' (database: '${DATABASE}'), skipping"
        continue
    fi
    if [ "$TYPE" = "postgres" ] && ! is_valid_number "$PG_VERSION"; then
        echo "Warning: Invalid PG_VERSION '${PG_VERSION}' (database: '${DATABASE}'), skipping"
        continue
    fi
    if [ -n "$TLS" ] && [ "$TLS" != "true" ]; then
        echo "Warning: Invalid TLS value '${TLS}' (must be empty or 'true', database: '${DATABASE}'), skipping"
        continue
    fi

    # Write the job definition to a root-only file. This way the password never
    # goes through the crontab or backup.sh's command line (argv is visible in
    # /proc/*/cmdline to any unprivileged user on the host).
    JOB_INDEX=$((JOB_INDEX + 1))
    JOB_FILE="${JOBS_DIR}/job-${JOB_INDEX}.env"
    {
        echo "# Generated by entrypoint.sh - do not edit"
        printf 'TYPE=%q\n' "$TYPE"
        printf 'HOST=%q\n' "$HOST"
        printf 'PORT=%q\n' "$PORT"
        printf 'DATABASE=%q\n' "$DATABASE"
        printf 'USER=%q\n' "$USER"
        printf 'PASSWORD=%q\n' "$PASSWORD"
        printf 'RETENTION_DAYS=%q\n' "$RETENTION_DAYS"
        printf 'PG_VERSION=%q\n' "${PG_VERSION}"
        printf 'TLS=%q\n' "${TLS}"
    } > "${JOB_FILE}"
    chmod 600 "${JOB_FILE}"

    # Add the cron entry (no secret nor free-form field in the line)
    echo "${CRON_SCHEDULE} root /scripts/backup.sh ${JOB_FILE} >> /var/log/cron.log 2>&1" >> "${CRONTAB_FILE}"

    if [ "$TYPE" = "postgres" ]; then
        echo "  ✓ Configured ${TYPE} backup for database '${DATABASE}' on ${HOST}:${PORT} (schedule: ${CRON_SCHEDULE}, retention: ${RETENTION_DAYS} days, PG version: ${PG_VERSION})"
    else
        echo "  ✓ Configured ${TYPE} backup for database '${DATABASE}' on ${HOST}:${PORT} (schedule: ${CRON_SCHEDULE}, retention: ${RETENTION_DAYS} days)"
    fi
done < "${CONFIG_FILE}"

# Add an empty line at the end of the crontab (required by cron)
echo "" >> "${CRONTAB_FILE}"

# The crontab no longer contains secrets, but there is no need to expose it: root only
chmod 0600 "${CRONTAB_FILE}"

echo "Cron configuration completed successfully! (${JOB_INDEX} job(s) configured)"
echo ""

# Start cron
echo "Starting cron daemon..."
cron

echo "Database backup service is running. Logs will appear below:"
echo "-----------------------------------------------------------"

# Run the command passed as argument (default: tail -f /var/log/cron.log)
exec "$@"
