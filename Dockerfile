FROM debian:trixie-slim

# Versions des outils MongoDB (utilisées pour l'installation dynamique)
ENV MONGO_TOOLS_VERSION="100.10.0"
ENV MONGOSH_VERSION="2.3.7"

# Installation des dépendances de base uniquement
# Les clients de bases de données seront installés au démarrage selon la configuration
RUN apt-get update && apt-get install -y \
    cron \
    gzip \
    wget \
    curl \
    ca-certificates \
    gnupg \
    lsb-release \
    procps \
    && rm -rf /var/lib/apt/lists/*

# Configuration du repository PostgreSQL (les clients seront installés au démarrage selon la config)
RUN mkdir -p /etc/apt/keyrings && \
    wget --quiet -O /etc/apt/keyrings/postgresql.asc https://www.postgresql.org/media/keys/ACCC4CF8.asc && \
    sh -c 'echo "deb [signed-by=/etc/apt/keyrings/postgresql.asc] http://apt.postgresql.org/pub/repos/apt $(lsb_release -cs)-pgdg main" > /etc/apt/sources.list.d/pgdg.list'

# Création des répertoires de travail
RUN mkdir -p /backups /scripts /config

# Copie des scripts
COPY scripts/backup.sh /scripts/backup.sh
COPY scripts/entrypoint.sh /scripts/entrypoint.sh
COPY scripts/healthcheck.sh /scripts/healthcheck.sh

# Rendre les scripts exécutables
RUN chmod +x /scripts/backup.sh /scripts/entrypoint.sh /scripts/healthcheck.sh

# Création d'un fichier de log pour cron
RUN touch /var/log/cron.log

# Volume pour les backups
VOLUME ["/backups", "/config"]

# Healthcheck pour vérifier la connectivité aux bases de données
# start-period augmenté pour permettre l'installation des clients PostgreSQL au démarrage
HEALTHCHECK --interval=5m --timeout=30s --start-period=5m --retries=3 \
    CMD ["/scripts/healthcheck.sh"]

# Définir l'entrypoint
ENTRYPOINT ["/scripts/entrypoint.sh"]

# Par défaut, suivre les logs de cron
CMD ["tail", "-f", "/var/log/cron.log"]
