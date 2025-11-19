FROM debian:trixie-slim

# Installation des dépendances nécessaires
RUN apt-get update && apt-get install -y \
    cron \
    mariadb-client \
    gzip \
    wget \
    curl \
    ca-certificates \
    gnupg \
    lsb-release \
    procps \
    && rm -rf /var/lib/apt/lists/*

# Installation de plusieurs versions de PostgreSQL client depuis le repository officiel
RUN mkdir -p /etc/apt/keyrings && \
    wget --quiet -O /etc/apt/keyrings/postgresql.asc https://www.postgresql.org/media/keys/ACCC4CF8.asc && \
    sh -c 'echo "deb [signed-by=/etc/apt/keyrings/postgresql.asc] http://apt.postgresql.org/pub/repos/apt $(lsb_release -cs)-pgdg main" > /etc/apt/sources.list.d/pgdg.list' && \
    apt-get update && \
    apt-get install -y \
        postgresql-client-12 \
        postgresql-client-13 \
        postgresql-client-14 \
        postgresql-client-15 \
        postgresql-client-16 \
        postgresql-client-17 \
        postgresql-client-18 \
    && rm -rf /var/lib/apt/lists/*

# Installation de MongoDB Database Tools et MongoDB Shell (via tarball pour support multi-arch)
RUN ARCH=$(uname -m) && \
    MONGO_TOOLS_VERSION="100.10.0" && \
    MONGOSH_VERSION="2.3.7" && \
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
    mongodump --version && \
    echo "Installing MongoDB Shell ${MONGOSH_VERSION} for ${ARCH}..." && \
    wget -q "https://downloads.mongodb.com/compass/mongosh-${MONGOSH_VERSION}-linux-${MONGO_ARCH}.tgz" -O /tmp/mongosh.tgz && \
    tar -xzf /tmp/mongosh.tgz -C /tmp && \
    cp /tmp/mongosh-*/bin/mongosh /usr/local/bin/ && \
    rm -rf /tmp/mongosh.tgz /tmp/mongosh-* && \
    mongosh --version

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
HEALTHCHECK --interval=5m --timeout=30s --start-period=30s --retries=3 \
    CMD ["/scripts/healthcheck.sh"]

# Définir l'entrypoint
ENTRYPOINT ["/scripts/entrypoint.sh"]

# Par défaut, suivre les logs de cron
CMD ["tail", "-f", "/var/log/cron.log"]
