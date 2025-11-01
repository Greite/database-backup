FROM debian:stable-slim

# Installation des dépendances nécessaires
RUN apt-get update && apt-get install -y \
    cron \
    postgresql-client \
    mariadb-client \
    gzip \
    wget \
    gnupg \
    && rm -rf /var/lib/apt/lists/*

# Installation de MongoDB Database Tools
RUN wget -qO - https://www.mongodb.org/static/pgp/server-7.0.asc | apt-key add - \
    && echo "deb http://repo.mongodb.org/apt/debian bookworm/mongodb-org/7.0 main" | tee /etc/apt/sources.list.d/mongodb-org-7.0.list \
    && apt-get update \
    && apt-get install -y mongodb-database-tools \
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
