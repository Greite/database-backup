#!/bin/bash

# Healthcheck script verifying database connectivity
# Checks that the container can connect to every configured database

set -e

CONFIG_FILE="/config/backups.conf"
EXIT_CODE=0

# Check that the configuration file exists
if [ ! -f "${CONFIG_FILE}" ]; then
    echo "UNHEALTHY: Configuration file not found at ${CONFIG_FILE}"
    exit 1
fi

# Check that cron is running
if ! pgrep -x "cron" > /dev/null; then
    echo "UNHEALTHY: Cron daemon is not running"
    exit 1
fi

echo "Healthcheck: Testing database connections..."

# Read the configuration file and test the connections
while IFS= read -r line || [ -n "$line" ]; do
    # Skip empty lines and comments
    [[ -z "$line" || "$line" =~ ^[[:space:]]*# ]] && continue

    # Parse the configuration line
    IFS='|' read -r CRON_SCHEDULE TYPE HOST PORT DATABASE USER PASSWORD RETENTION_DAYS PG_VERSION TLS <<< "$line"

    # Validate the required fields
    if [ -z "$TYPE" ] || [ -z "$HOST" ] || [ -z "$DATABASE" ]; then
        continue
    fi

    # Set the default ports when needed
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

    # For PostgreSQL, set the default version
    if [ "$TYPE" = "postgres" ]; then
        PG_VERSION=${PG_VERSION:-18}
    fi

    # Test the connection according to the type
    echo "  Testing ${TYPE} connection to ${HOST}:${PORT} (database: ${DATABASE})..."

    if [ "${TYPE}" = "postgres" ]; then
        export PGPASSWORD="${PASSWORD}"
        if [ "${TLS}" = "true" ]; then
            export PGSSLMODE=require
        fi
        PG_BIN_PATH="/usr/lib/postgresql/${PG_VERSION}/bin"

        if ! timeout 5 "${PG_BIN_PATH}/psql" -h "${HOST}" -p "${PORT}" -U "${USER}" -d "${DATABASE}" -c "SELECT 1" > /dev/null 2>&1; then
            echo "    ✗ FAILED: Cannot connect to PostgreSQL ${PG_VERSION} database ${DATABASE} on ${HOST}:${PORT}"
            EXIT_CODE=1
        else
            echo "    ✓ OK: PostgreSQL ${PG_VERSION} connection successful"
        fi
        unset PGPASSWORD PGSSLMODE

    elif [ "${TYPE}" = "mariadb" ] || [ "${TYPE}" = "mysql" ]; then
        export MYSQL_PWD="${PASSWORD}"
        MYSQL_ARGS=(-h "${HOST}" -P "${PORT}" -u "${USER}")
        if [ "${TLS}" = "true" ]; then
            MYSQL_ARGS+=(--ssl)
        fi

        if ! timeout 5 mysql "${MYSQL_ARGS[@]}" -e "SELECT 1" > /dev/null 2>&1; then
            echo "    ✗ FAILED: Cannot connect to MariaDB/MySQL database on ${HOST}:${PORT}"
            EXIT_CODE=1
        else
            echo "    ✓ OK: MariaDB/MySQL connection successful"
        fi
        unset MYSQL_PWD

    elif [ "${TYPE}" = "mongodb" ]; then
        MONGOSH_ARGS=(--host "${HOST}" --port "${PORT}" --quiet)
        if [ "${TLS}" = "true" ]; then
            MONGOSH_ARGS+=(--tls)
        fi

        # Credentials are passed through the environment and read by the
        # --eval code: they never appear in /proc/*/cmdline
        if [ -n "${USER}" ] && [ -n "${PASSWORD}" ]; then
            MONGOSH_EVAL='db.getSiblingDB("admin").auth(process.env.MONGOSH_USER, process.env.MONGOSH_PWD); db.adminCommand({ping: 1})'
            MONGOSH_OK=true
            MONGOSH_USER="${USER}" MONGOSH_PWD="${PASSWORD}" timeout 5 mongosh "${MONGOSH_ARGS[@]}" --eval "${MONGOSH_EVAL}" > /dev/null 2>&1 || MONGOSH_OK=false
        else
            MONGOSH_OK=true
            timeout 5 mongosh "${MONGOSH_ARGS[@]}" --eval "db.runCommand({ping: 1})" > /dev/null 2>&1 || MONGOSH_OK=false
        fi

        if [ "${MONGOSH_OK}" = "false" ]; then
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
