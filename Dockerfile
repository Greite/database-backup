FROM debian:stable-slim

# Installation des dépendances nécessaires
RUN apt-get update && apt-get install -y \
    cron \
    postgresql-client \
    mariadb-client \
    gzip \
    wget \
    curl \
    ca-certificates \
    && rm -rf /var/lib/apt/lists/*

# Installation de MongoDB Database Tools (via tarball pour support multi-arch)
RUN ARCH=$(uname -m) && \
    MONGO_TOOLS_VERSION="100.10.0" && \
    case "${ARCH}" in \
        x86_64) MONGO_ARCH="x86_64" ;; \
        aarch64) MONGO_ARCH="arm64" ;; \
        *) echo "Unsupported architecture: ${ARCH}" && exit 1 ;; \
    esac && \
    echo "Installing MongoDB Database Tools ${MONGO_TOOLS_VERSION} for ${ARCH}..." && \
    wget -q "https://fastdl.mongodb.org/tools/db/mongodb-database-tools-ubuntu2204-${MONGO_ARCH}-${MONGO_TOOLS_VERSION}.tgz" -O /tmp/mongodb-tools.tgz && \
    tar -xzf /tmp/mongodb-tools.tgz -C /tmp && \
    cp /tmp/mongodb-database-tools-*/bin/* /usr/local/bin/ && \
    rm -rf /tmp/mongodb-tools.tgz /tmp/mongodb-database-tools-* && \
    mongodump --version

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
