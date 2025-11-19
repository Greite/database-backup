#!/bin/bash

# Script de healthcheck pour vérifier la connectivité aux bases de données
# Vérifie que le conteneur peut se connecter à toutes les bases configurées

set -e

CONFIG_FILE="/config/backups.conf"
EXIT_CODE=0

# Vérifier que le fichier de configuration existe
if [ ! -f "${CONFIG_FILE}" ]; then
    echo "UNHEALTHY: Configuration file not found at ${CONFIG_FILE}"
    exit 1
fi

# Vérifier que cron est en cours d'exécution
if ! pgrep -x "cron" > /dev/null; then
    echo "UNHEALTHY: Cron daemon is not running"
    exit 1
fi

echo "Healthcheck: Testing database connections..."

# Lire le fichier de configuration et tester les connexions
while IFS= read -r line || [ -n "$line" ]; do
    # Ignorer les lignes vides et les commentaires
    [[ -z "$line" || "$line" =~ ^[[:space:]]*# ]] && continue

    # Parser la ligne de configuration
    IFS='|' read -r CRON_SCHEDULE TYPE HOST PORT DATABASE USER PASSWORD RETENTION_DAYS PG_VERSION <<< "$line"

    # Valider les champs obligatoires
    if [ -z "$TYPE" ] || [ -z "$HOST" ] || [ -z "$DATABASE" ]; then
        continue
    fi

    # Définir les ports par défaut si nécessaire
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

    # Pour PostgreSQL, définir la version par défaut
    if [ "$TYPE" = "postgres" ]; then
        PG_VERSION=${PG_VERSION:-18}
    fi

    # Tester la connexion selon le type
    echo "  Testing ${TYPE} connection to ${HOST}:${PORT} (database: ${DATABASE})..."

    if [ "${TYPE}" = "postgres" ]; then
        export PGPASSWORD="${PASSWORD}"
        PG_BIN_PATH="/usr/lib/postgresql/${PG_VERSION}/bin"

        if ! timeout 5 "${PG_BIN_PATH}/psql" -h "${HOST}" -p "${PORT}" -U "${USER}" -d "${DATABASE}" -c "SELECT 1" > /dev/null 2>&1; then
            echo "    ✗ FAILED: Cannot connect to PostgreSQL ${PG_VERSION} database ${DATABASE} on ${HOST}:${PORT}"
            EXIT_CODE=1
        else
            echo "    ✓ OK: PostgreSQL ${PG_VERSION} connection successful"
        fi
        unset PGPASSWORD

    elif [ "${TYPE}" = "mariadb" ] || [ "${TYPE}" = "mysql" ]; then
        export MYSQL_PWD="${PASSWORD}"

        if ! timeout 5 mysql -h "${HOST}" -P "${PORT}" -u "${USER}" -e "SELECT 1" > /dev/null 2>&1; then
            echo "    ✗ FAILED: Cannot connect to MariaDB/MySQL database on ${HOST}:${PORT}"
            EXIT_CODE=1
        else
            echo "    ✓ OK: MariaDB/MySQL connection successful"
        fi
        unset MYSQL_PWD

    elif [ "${TYPE}" = "mongodb" ]; then
        # Construire l'URI MongoDB
        if [ -n "${USER}" ] && [ -n "${PASSWORD}" ]; then
            MONGODB_URI="mongodb://${USER}:${PASSWORD}@${HOST}:${PORT}/${DATABASE}?authSource=admin"
        else
            MONGODB_URI="mongodb://${HOST}:${PORT}/${DATABASE}"
        fi

        if ! timeout 5 mongosh "${MONGODB_URI}" --quiet --eval "db.runCommand({ping: 1})" > /dev/null 2>&1; then
            echo "    ✗ FAILED: Cannot connect to MongoDB database ${DATABASE} on ${HOST}:${PORT}"
            EXIT_CODE=1
        else
            echo "    ✓ OK: MongoDB connection successful"
        fi
    fi

done < "${CONFIG_FILE}"

if [ ${EXIT_CODE} -eq 0 ]; then
    echo "HEALTHY: All database connections successful"
else
    echo "UNHEALTHY: Some database connections failed"
fi

exit ${EXIT_CODE}
