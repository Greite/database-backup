#!/bin/bash

# Script de backup pour PostgreSQL, MariaDB et MongoDB
# Usage: backup.sh <type> <host> <port> <database> <user> <password> [retention_days] [pg_version]

set -e

TYPE=$1
HOST=$2
PORT=$3
DATABASE=$4
USER=$5
PASSWORD=$6
RETENTION_DAYS=${7:-7}  # Par défaut, conserver 7 jours de backups
PG_VERSION=${8:-18}      # Par défaut, utiliser PostgreSQL 18

BACKUP_DIR="/backups/${TYPE}/${DATABASE}"
TIMESTAMP=$(date +%Y%m%d_%H%M%S)

# Pour MongoDB, on utilise un répertoire au lieu d'un fichier SQL
if [ "${TYPE}" = "mongodb" ]; then
    BACKUP_FILE="${BACKUP_DIR}/${DATABASE}_${TIMESTAMP}"
    BACKUP_FILE_GZ="${BACKUP_FILE}.tar.gz"
else
    BACKUP_FILE="${BACKUP_DIR}/${DATABASE}_${TIMESTAMP}.sql"
    BACKUP_FILE_GZ="${BACKUP_FILE}.gz"
fi

# Créer le répertoire de backup s'il n'existe pas
mkdir -p "${BACKUP_DIR}"

echo "[$(date '+%Y-%m-%d %H:%M:%S')] Starting backup of ${TYPE} database: ${DATABASE} on ${HOST}:${PORT}"

# Effectuer le backup selon le type de base de données
if [ "${TYPE}" = "postgres" ]; then
    export PGPASSWORD="${PASSWORD}"

    # Utiliser la version spécifique de pg_dump
    PG_DUMP_PATH="/usr/lib/postgresql/${PG_VERSION}/bin/pg_dump"

    # Vérifier que la version demandée est installée
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
    # Utiliser MYSQL_PWD pour éviter les problèmes avec les caractères spéciaux
    export MYSQL_PWD="${PASSWORD}"
    mysqldump -h "${HOST}" -P "${PORT}" -u "${USER}" "${DATABASE}" > "${BACKUP_FILE}"
    unset MYSQL_PWD

elif [ "${TYPE}" = "mongodb" ]; then
    # Construire l'URI de connexion MongoDB
    if [ -n "${USER}" ] && [ -n "${PASSWORD}" ]; then
        # Avec authentification
        MONGODB_URI="mongodb://${USER}:${PASSWORD}@${HOST}:${PORT}/${DATABASE}?authSource=admin"
    else
        # Sans authentification
        MONGODB_URI="mongodb://${HOST}:${PORT}/${DATABASE}"
    fi

    # mongodump crée un répertoire, on le dump dans un dossier temporaire
    TEMP_DIR="${BACKUP_FILE}_temp"
    mkdir -p "${TEMP_DIR}"

    mongodump --uri="${MONGODB_URI}" --out="${TEMP_DIR}" --gzip

    # Vérifier que le dump a réussi
    if [ ! -d "${TEMP_DIR}/${DATABASE}" ]; then
        echo "Error: MongoDB dump failed or database directory not created"
        rm -rf "${TEMP_DIR}"
        exit 1
    fi

    # Créer une archive tar.gz du dump
    tar -czf "${BACKUP_FILE_GZ}" -C "${TEMP_DIR}" .

    # Nettoyer le répertoire temporaire
    rm -rf "${TEMP_DIR}"

    # Pour MongoDB, on passe directement à la vérification de l'archive
    if [ ! -f "${BACKUP_FILE_GZ}" ]; then
        echo "Error: Archive creation failed"
        exit 1
    fi

else
    echo "Error: Unknown database type: ${TYPE}"
    exit 1
fi

# Vérifier que le dump a réussi et compresser (sauf MongoDB déjà compressé)
if [ "${TYPE}" != "mongodb" ]; then
    if [ ! -s "${BACKUP_FILE}" ]; then
        echo "Error: Backup file is empty or was not created"
        rm -f "${BACKUP_FILE}"
        exit 1
    fi

    # Compresser le backup
    echo "[$(date '+%Y-%m-%d %H:%M:%S')] Compressing backup..."
    gzip "${BACKUP_FILE}"

    # Vérifier que la compression a réussi
    if [ ! -f "${BACKUP_FILE_GZ}" ]; then
        echo "Error: Compression failed"
        exit 1
    fi
fi

BACKUP_SIZE=$(du -h "${BACKUP_FILE_GZ}" | cut -f1)
echo "[$(date '+%Y-%m-%d %H:%M:%S')] Backup completed successfully: ${BACKUP_FILE_GZ} (${BACKUP_SIZE})"

# Rotation des backups - supprimer les backups plus anciens que RETENTION_DAYS
if [ "${RETENTION_DAYS}" -gt 0 ]; then
    echo "[$(date '+%Y-%m-%d %H:%M:%S')] Cleaning up backups older than ${RETENTION_DAYS} days..."

    if [ "${TYPE}" = "mongodb" ]; then
        # Pour MongoDB, supprimer les fichiers .tar.gz
        find "${BACKUP_DIR}" -name "*.tar.gz" -type f -mtime +${RETENTION_DAYS} -delete
        REMAINING_BACKUPS=$(find "${BACKUP_DIR}" -name "*.tar.gz" -type f | wc -l)
    else
        # Pour PostgreSQL et MariaDB, supprimer les fichiers .sql.gz
        find "${BACKUP_DIR}" -name "*.sql.gz" -type f -mtime +${RETENTION_DAYS} -delete
        REMAINING_BACKUPS=$(find "${BACKUP_DIR}" -name "*.sql.gz" -type f | wc -l)
    fi

    echo "[$(date '+%Y-%m-%d %H:%M:%S')] ${REMAINING_BACKUPS} backup(s) remaining in ${BACKUP_DIR}"
fi

echo "[$(date '+%Y-%m-%d %H:%M:%S')] Backup process finished"
