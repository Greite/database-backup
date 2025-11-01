#!/bin/bash

# Script de backup pour PostgreSQL et MariaDB
# Usage: backup.sh <type> <host> <port> <database> <user> <password> [retention_days]

set -e

TYPE=$1
HOST=$2
PORT=$3
DATABASE=$4
USER=$5
PASSWORD=$6
RETENTION_DAYS=${7:-7}  # Par défaut, conserver 7 jours de backups

BACKUP_DIR="/backups/${TYPE}/${DATABASE}"
TIMESTAMP=$(date +%Y%m%d_%H%M%S)
BACKUP_FILE="${BACKUP_DIR}/${DATABASE}_${TIMESTAMP}.sql"
BACKUP_FILE_GZ="${BACKUP_FILE}.gz"

# Créer le répertoire de backup s'il n'existe pas
mkdir -p "${BACKUP_DIR}"

echo "[$(date '+%Y-%m-%d %H:%M:%S')] Starting backup of ${TYPE} database: ${DATABASE} on ${HOST}:${PORT}"

# Effectuer le backup selon le type de base de données
if [ "${TYPE}" = "postgres" ]; then
    export PGPASSWORD="${PASSWORD}"
    pg_dump -h "${HOST}" -p "${PORT}" -U "${USER}" -d "${DATABASE}" > "${BACKUP_FILE}"
    unset PGPASSWORD

elif [ "${TYPE}" = "mariadb" ] || [ "${TYPE}" = "mysql" ]; then
    # Utiliser MYSQL_PWD pour éviter les problèmes avec les caractères spéciaux
    export MYSQL_PWD="${PASSWORD}"
    mysqldump -h "${HOST}" -P "${PORT}" -u "${USER}" "${DATABASE}" > "${BACKUP_FILE}"
    unset MYSQL_PWD

else
    echo "Error: Unknown database type: ${TYPE}"
    exit 1
fi

# Vérifier que le dump a réussi
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

BACKUP_SIZE=$(du -h "${BACKUP_FILE_GZ}" | cut -f1)
echo "[$(date '+%Y-%m-%d %H:%M:%S')] Backup completed successfully: ${BACKUP_FILE_GZ} (${BACKUP_SIZE})"

# Rotation des backups - supprimer les backups plus anciens que RETENTION_DAYS
if [ "${RETENTION_DAYS}" -gt 0 ]; then
    echo "[$(date '+%Y-%m-%d %H:%M:%S')] Cleaning up backups older than ${RETENTION_DAYS} days..."
    find "${BACKUP_DIR}" -name "*.sql.gz" -type f -mtime +${RETENTION_DAYS} -delete
    REMAINING_BACKUPS=$(find "${BACKUP_DIR}" -name "*.sql.gz" -type f | wc -l)
    echo "[$(date '+%Y-%m-%d %H:%M:%S')] ${REMAINING_BACKUPS} backup(s) remaining in ${BACKUP_DIR}"
fi

echo "[$(date '+%Y-%m-%d %H:%M:%S')] Backup process finished"
