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
    # Format attendu: CRON_SCHEDULE|TYPE|HOST|PORT|DATABASE|USER|PASSWORD|RETENTION_DAYS
    IFS='|' read -r CRON_SCHEDULE TYPE HOST PORT DATABASE USER PASSWORD RETENTION_DAYS <<< "$line"

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

    # Échapper correctement le mot de passe pour bash
    # printf %q échappe tous les caractères spéciaux
    ESCAPED_PASSWORD=$(printf %q "$PASSWORD")

    # Ajouter l'entrée cron
    echo "${CRON_SCHEDULE} root /scripts/backup.sh \"${TYPE}\" \"${HOST}\" \"${PORT}\" \"${DATABASE}\" \"${USER}\" ${ESCAPED_PASSWORD} \"${RETENTION_DAYS}\" >> /var/log/cron.log 2>&1" >> "${CRONTAB_FILE}"

    echo "  ✓ Configured ${TYPE} backup for database '${DATABASE}' on ${HOST}:${PORT} (schedule: ${CRON_SCHEDULE}, retention: ${RETENTION_DAYS} days)"
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
