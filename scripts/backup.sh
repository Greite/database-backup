#!/bin/bash

# Backup script for PostgreSQL, MariaDB and MongoDB
# Usage: backup.sh <job_file>
#
# <job_file> is a job definition file generated at startup by entrypoint.sh
# in /etc/backup-jobs/ (mode 600). Credentials therefore never go through
# the command line, which is visible in /proc/*/cmdline to any unprivileged
# user on the host.

set -e

# Dumps contain sensitive data: files created as 600, directories as 700
umask 077

JOB_FILE=$1

if [ -z "${JOB_FILE}" ] || [ ! -f "${JOB_FILE}" ]; then
    echo "Usage: backup.sh <job_file>"
    echo "Job files are generated at container startup in /etc/backup-jobs/"
    exit 1
fi

TYPE="" HOST="" PORT="" DATABASE="" USER="" PASSWORD="" RETENTION_DAYS="" PG_VERSION="" TLS=""
# shellcheck disable=SC1090
source "${JOB_FILE}"

RETENTION_DAYS=${RETENTION_DAYS:-7}  # By default, keep 7 days of backups
PG_VERSION=${PG_VERSION:-18}         # By default, use PostgreSQL 18

if [ -z "${TYPE}" ] || [ -z "${HOST}" ] || [ -z "${PORT}" ] || [ -z "${DATABASE}" ]; then
    echo "Error: Invalid job file: ${JOB_FILE}"
    exit 1
fi

BACKUP_DIR="/backups/${TYPE}/${DATABASE}"
TIMESTAMP=$(date +%Y%m%d_%H%M%S)

# For MongoDB, a directory is used instead of a SQL file
if [ "${TYPE}" = "mongodb" ]; then
    BACKUP_FILE="${BACKUP_DIR}/${DATABASE}_${TIMESTAMP}"
    BACKUP_FILE_GZ="${BACKUP_FILE}.tar.gz"
else
    BACKUP_FILE="${BACKUP_DIR}/${DATABASE}_${TIMESTAMP}.sql"
    BACKUP_FILE_GZ="${BACKUP_FILE}.gz"
fi

# Create the backup directory if it does not exist
mkdir -p "${BACKUP_DIR}"

echo "[$(date '+%Y-%m-%d %H:%M:%S')] Starting backup of ${TYPE} database: ${DATABASE} on ${HOST}:${PORT}"

# Run the backup according to the database type
if [ "${TYPE}" = "postgres" ]; then
    export PGPASSWORD="${PASSWORD}"
    if [ "${TLS}" = "true" ]; then
        export PGSSLMODE=require
    fi

    # Use the specific pg_dump version
    PG_DUMP_PATH="/usr/lib/postgresql/${PG_VERSION}/bin/pg_dump"

    # Check that the requested version is installed
    if [ ! -x "${PG_DUMP_PATH}" ]; then
        echo "Error: PostgreSQL ${PG_VERSION} client is not installed"
        echo "Available versions:"
        ls -1 /usr/lib/postgresql/ 2>/dev/null || echo "No PostgreSQL clients found"
        unset PGPASSWORD
        exit 1
    fi

    echo "[$(date '+%Y-%m-%d %H:%M:%S')] Using PostgreSQL client version ${PG_VERSION}"
    "${PG_DUMP_PATH}" -h "${HOST}" -p "${PORT}" -U "${USER}" -d "${DATABASE}" > "${BACKUP_FILE}"
    unset PGPASSWORD

elif [ "${TYPE}" = "mariadb" ] || [ "${TYPE}" = "mysql" ]; then
    # Use MYSQL_PWD to avoid issues with special characters
    export MYSQL_PWD="${PASSWORD}"
    MYSQL_ARGS=(-h "${HOST}" -P "${PORT}" -u "${USER}")
    if [ "${TLS}" = "true" ]; then
        MYSQL_ARGS+=(--ssl)
    fi
    mysqldump "${MYSQL_ARGS[@]}" "${DATABASE}" > "${BACKUP_FILE}"
    unset MYSQL_PWD

elif [ "${TYPE}" = "mongodb" ]; then
    # mongodump creates a directory, dump into a temporary folder
    TEMP_DIR="${BACKUP_FILE}_temp"
    mkdir -p "${TEMP_DIR}"

    MONGODUMP_ARGS=(--host "${HOST}" --port "${PORT}" --db "${DATABASE}" --out "${TEMP_DIR}" --gzip)
    if [ "${TLS}" = "true" ]; then
        MONGODUMP_ARGS+=(--ssl)
    fi

    # The password is passed through a temporary configuration file (mode 600),
    # never through a URI argument visible in /proc/*/cmdline
    MONGO_CONFIG=""
    if [ -n "${USER}" ] && [ -n "${PASSWORD}" ]; then
        MONGO_CONFIG=$(mktemp /tmp/mongodump.XXXXXX)
        YAML_PASSWORD=${PASSWORD//\\/\\\\}
        YAML_PASSWORD=${YAML_PASSWORD//\"/\\\"}
        printf 'password: "%s"\n' "${YAML_PASSWORD}" > "${MONGO_CONFIG}"
        MONGODUMP_ARGS+=(--username "${USER}" --authenticationDatabase admin --config "${MONGO_CONFIG}")
    fi

    if ! mongodump "${MONGODUMP_ARGS[@]}"; then
        [ -n "${MONGO_CONFIG}" ] && rm -f "${MONGO_CONFIG}"
        rm -rf "${TEMP_DIR}"
        echo "Error: MongoDB dump failed"
        exit 1
    fi
    [ -n "${MONGO_CONFIG}" ] && rm -f "${MONGO_CONFIG}"

    # Check that the dump succeeded
    if [ ! -d "${TEMP_DIR}/${DATABASE}" ]; then
        echo "Error: MongoDB dump failed or database directory not created"
        rm -rf "${TEMP_DIR}"
        exit 1
    fi

    # Create a tar.gz archive of the dump
    tar -czf "${BACKUP_FILE_GZ}" -C "${TEMP_DIR}" .

    # Clean up the temporary directory
    rm -rf "${TEMP_DIR}"

    # For MongoDB, go straight to the archive verification
    if [ ! -f "${BACKUP_FILE_GZ}" ]; then
        echo "Error: Archive creation failed"
        exit 1
    fi

else
    echo "Error: Unknown database type: ${TYPE}"
    exit 1
fi

# Check that the dump succeeded and compress (except MongoDB, already compressed)
if [ "${TYPE}" != "mongodb" ]; then
    if [ ! -s "${BACKUP_FILE}" ]; then
        echo "Error: Backup file is empty or was not created"
        rm -f "${BACKUP_FILE}"
        exit 1
    fi

    # Compress the backup
    echo "[$(date '+%Y-%m-%d %H:%M:%S')] Compressing backup..."
    gzip "${BACKUP_FILE}"

    # Check that the compression succeeded
    if [ ! -f "${BACKUP_FILE_GZ}" ]; then
        echo "Error: Compression failed"
        exit 1
    fi
fi

# Optional at-rest encryption (GPG symmetric, AES256)
# Enabled via BACKUP_ENCRYPTION_PASSPHRASE(_FILE) on the container, normalized
# by entrypoint.sh into /etc/backup-jobs/encryption.passphrase (mode 600)
if [ -n "${BACKUP_ENCRYPTION_PASSPHRASE_FILE}" ] && [ -f "${BACKUP_ENCRYPTION_PASSPHRASE_FILE}" ]; then
    echo "[$(date '+%Y-%m-%d %H:%M:%S')] Encrypting backup..."
    gpg --batch --yes --quiet --pinentry-mode loopback \
        --passphrase-file "${BACKUP_ENCRYPTION_PASSPHRASE_FILE}" \
        --symmetric --cipher-algo AES256 \
        --output "${BACKUP_FILE_GZ}.gpg" "${BACKUP_FILE_GZ}"
    rm -f "${BACKUP_FILE_GZ}"
    BACKUP_FILE_GZ="${BACKUP_FILE_GZ}.gpg"
fi

BACKUP_SIZE=$(du -h "${BACKUP_FILE_GZ}" | cut -f1)
echo "[$(date '+%Y-%m-%d %H:%M:%S')] Backup completed successfully: ${BACKUP_FILE_GZ} (${BACKUP_SIZE})"

# Backup rotation - delete backups older than RETENTION_DAYS
if [ "${RETENTION_DAYS}" -gt 0 ]; then
    echo "[$(date '+%Y-%m-%d %H:%M:%S')] Cleaning up backups older than ${RETENTION_DAYS} days..."

    # The patterns include the encrypted variants (.gpg)
    if [ "${TYPE}" = "mongodb" ]; then
        # For MongoDB, delete the .tar.gz / .tar.gz.gpg archives
        find "${BACKUP_DIR}" -name "*.tar.gz*" -type f -mtime +${RETENTION_DAYS} -delete
        REMAINING_BACKUPS=$(find "${BACKUP_DIR}" -name "*.tar.gz*" -type f | wc -l)
    else
        # For PostgreSQL and MariaDB, delete the .sql.gz / .sql.gz.gpg files
        find "${BACKUP_DIR}" -name "*.sql.gz*" -type f -mtime +${RETENTION_DAYS} -delete
        REMAINING_BACKUPS=$(find "${BACKUP_DIR}" -name "*.sql.gz*" -type f | wc -l)
    fi

    echo "[$(date '+%Y-%m-%d %H:%M:%S')] ${REMAINING_BACKUPS} backup(s) remaining in ${BACKUP_DIR}"
fi

echo "[$(date '+%Y-%m-%d %H:%M:%S')] Backup process finished"
