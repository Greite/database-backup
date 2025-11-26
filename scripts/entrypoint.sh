#!/bin/bash

set -e

CONFIG_FILE="/config/backups.conf"
CRONTAB_FILE="/etc/cron.d/db-backups"

echo "Starting Database Backup Container..."

# Vérifier si le fichier de configuration existe
if [ ! -f "${CONFIG_FILE}" ]; then
    echo "Error: Configuration file not found at ${CONFIG_FILE}"
    echo "Please mount a configuration file at /config/backups.conf"
    exit 1
fi

# =============================================================================
# Détection des clients de bases de données nécessaires
# =============================================================================
echo "Detecting required database clients..."

PG_VERSIONS_NEEDED=""
NEED_MARIADB=false
NEED_MONGODB=false

while IFS= read -r line || [ -n "$line" ]; do
    [[ -z "$line" || "$line" =~ ^[[:space:]]*# ]] && continue

    IFS='|' read -r _ TYPE _ _ _ _ _ _ PG_VERSION <<< "$line"

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
# Installation des clients PostgreSQL
# =============================================================================
echo ""
echo "PostgreSQL:"
if [ -n "${PG_VERSIONS_NEEDED}" ]; then
    PG_PACKAGES=""
    for version in ${PG_VERSIONS_NEEDED}; do
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
# Installation du client MariaDB/MySQL
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
# Installation des outils MongoDB
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
            x86_64) MONGO_ARCH="x86_64"; MONGOSH_ARCH="x64" ;;
            aarch64) MONGO_ARCH="arm64"; MONGOSH_ARCH="arm64" ;;
            *) echo "  ✗ Unsupported architecture: ${ARCH}" && exit 1 ;;
        esac

        # MongoDB Database Tools
        if ! command -v mongodump &> /dev/null; then
            echo "  Installing MongoDB Database Tools ${MONGO_TOOLS_VERSION}..."
            wget -q "https://fastdl.mongodb.org/tools/db/mongodb-database-tools-ubuntu2204-${MONGO_ARCH}-${MONGO_TOOLS_VERSION}.tgz" -O /tmp/mongodb-tools.tgz
            tar -xzf /tmp/mongodb-tools.tgz -C /tmp
            cp /tmp/mongodb-database-tools-*/bin/* /usr/local/bin/
            rm -rf /tmp/mongodb-tools.tgz /tmp/mongodb-database-tools-*
        fi

        # MongoDB Shell
        if ! command -v mongosh &> /dev/null; then
            echo "  Installing MongoDB Shell ${MONGOSH_VERSION}..."
            wget -q "https://downloads.mongodb.com/compass/mongosh-${MONGOSH_VERSION}-linux-${MONGOSH_ARCH}.tgz" -O /tmp/mongosh.tgz
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
# Configuration des tâches cron
# =============================================================================

# Créer le fichier crontab
echo "# Database backup cron jobs" > "${CRONTAB_FILE}"
echo "SHELL=/bin/bash" >> "${CRONTAB_FILE}"
echo "PATH=/usr/local/sbin:/usr/local/bin:/sbin:/bin:/usr/sbin:/usr/bin" >> "${CRONTAB_FILE}"
echo "" >> "${CRONTAB_FILE}"

# Lire le fichier de configuration et générer les entrées cron
echo "Loading backup configurations from ${CONFIG_FILE}..."

while IFS= read -r line || [ -n "$line" ]; do
    # Ignorer les lignes vides et les commentaires
    [[ -z "$line" || "$line" =~ ^[[:space:]]*# ]] && continue

    # Parser la ligne de configuration
    # Format attendu: CRON_SCHEDULE|TYPE|HOST|PORT|DATABASE|USER|PASSWORD|RETENTION_DAYS|PG_VERSION
    IFS='|' read -r CRON_SCHEDULE TYPE HOST PORT DATABASE USER PASSWORD RETENTION_DAYS PG_VERSION <<< "$line"

    # Valider les champs obligatoires
    if [ -z "$CRON_SCHEDULE" ] || [ -z "$TYPE" ] || [ -z "$HOST" ] || [ -z "$DATABASE" ]; then
        echo "Warning: Invalid configuration line, skipping: $line"
        continue
    fi

    # Pour PostgreSQL et MariaDB/MySQL, l'utilisateur est obligatoire
    if [ "$TYPE" != "mongodb" ] && [ -z "$USER" ]; then
        echo "Warning: USER is required for $TYPE, skipping: $line"
        continue
    fi

    # Utiliser les valeurs par défaut si non spécifiées
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

    # Pour PostgreSQL, définir la version par défaut si non spécifiée
    if [ "$TYPE" = "postgres" ]; then
        PG_VERSION=${PG_VERSION:-18}
    fi

    # Échapper correctement le mot de passe pour bash
    # printf %q échappe tous les caractères spéciaux
    ESCAPED_PASSWORD=$(printf %q "$PASSWORD")

    # Ajouter l'entrée cron
    if [ "$TYPE" = "postgres" ]; then
        echo "${CRON_SCHEDULE} root /scripts/backup.sh \"${TYPE}\" \"${HOST}\" \"${PORT}\" \"${DATABASE}\" \"${USER}\" ${ESCAPED_PASSWORD} \"${RETENTION_DAYS}\" \"${PG_VERSION}\" >> /var/log/cron.log 2>&1" >> "${CRONTAB_FILE}"
        echo "  ✓ Configured ${TYPE} backup for database '${DATABASE}' on ${HOST}:${PORT} (schedule: ${CRON_SCHEDULE}, retention: ${RETENTION_DAYS} days, PG version: ${PG_VERSION})"
    else
        echo "${CRON_SCHEDULE} root /scripts/backup.sh \"${TYPE}\" \"${HOST}\" \"${PORT}\" \"${DATABASE}\" \"${USER}\" ${ESCAPED_PASSWORD} \"${RETENTION_DAYS}\" >> /var/log/cron.log 2>&1" >> "${CRONTAB_FILE}"
        echo "  ✓ Configured ${TYPE} backup for database '${DATABASE}' on ${HOST}:${PORT} (schedule: ${CRON_SCHEDULE}, retention: ${RETENTION_DAYS} days)"
    fi
done < "${CONFIG_FILE}"

# Ajouter une ligne vide à la fin du crontab (requis par cron)
echo "" >> "${CRONTAB_FILE}"

# Définir les bonnes permissions
chmod 0644 "${CRONTAB_FILE}"

echo "Cron configuration completed successfully!"
echo ""

# Démarrer cron
echo "Starting cron daemon..."
cron

echo "Database backup service is running. Logs will appear below:"
echo "-----------------------------------------------------------"

# Exécuter la commande passée en argument (par défaut: tail -f /var/log/cron.log)
exec "$@"
