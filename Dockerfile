FROM debian:stable-slim

# Installation des dépendances nécessaires
RUN apt-get update && apt-get install -y \
    cron \
    postgresql-client \
    mariadb-client \
    gzip \
    && rm -rf /var/lib/apt/lists/*

# Création des répertoires de travail
RUN mkdir -p /backups /scripts /config

# Copie des scripts
COPY scripts/backup.sh /scripts/backup.sh
COPY scripts/entrypoint.sh /scripts/entrypoint.sh

# Rendre les scripts exécutables
RUN chmod +x /scripts/backup.sh /scripts/entrypoint.sh

# Création d'un fichier de log pour cron
RUN touch /var/log/cron.log

# Volume pour les backups
VOLUME ["/backups", "/config"]

# Définir l'entrypoint
ENTRYPOINT ["/scripts/entrypoint.sh"]

# Par défaut, suivre les logs de cron
CMD ["tail", "-f", "/var/log/cron.log"]
