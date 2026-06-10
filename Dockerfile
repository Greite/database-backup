FROM debian:trixie-slim

# MongoDB tool versions (used for the dynamic installation)
ENV MONGO_TOOLS_VERSION="100.14.0"
ENV MONGOSH_VERSION="2.8.3"

# SHA256 checksums of the MongoDB archives, verified by entrypoint.sh at
# download time (database-tools: official checksums from release.json;
# mongosh: GitHub release asset digests, cross-checked against
# downloads.mongodb.com)
ENV MONGO_TOOLS_SHA256_X86_64="4104998bda784a0cb16fc2e06d9c21645516d72c4fb481c9b103f1e0a8458fc0"
ENV MONGO_TOOLS_SHA256_ARM64="ef2945973b7e9c0f95d25dc607d420b0b07c486a675937ac9723b32f56ce718d"
ENV MONGOSH_SHA256_X64="f3d994c05c889f3c9f72f43cf6b574bc178a2a35f0be9322ab7f7b1aa66efd55"
ENV MONGOSH_SHA256_ARM64="68b4894acac60bf49902d6342d5ef91782473490e55100b4dc5db2ce1ff01fb2"

# Default timezone (can be overridden via environment variable)
ENV TZ=UTC

# Install only the base dependencies
# Database clients are installed at startup based on the configuration
RUN apt-get update && apt-get install -y \
    cron \
    gzip \
    wget \
    curl \
    ca-certificates \
    gnupg \
    lsb-release \
    procps \
    tzdata \
    && rm -rf /var/lib/apt/lists/*

# Configure the timezone (uses the TZ variable)
RUN ln -snf /usr/share/zoneinfo/$TZ /etc/localtime && echo $TZ > /etc/timezone

# Configure the PostgreSQL repository (clients are installed at startup based on the config)
RUN mkdir -p /etc/apt/keyrings && \
    wget --quiet -O /etc/apt/keyrings/postgresql.asc https://www.postgresql.org/media/keys/ACCC4CF8.asc && \
    sh -c 'echo "deb [signed-by=/etc/apt/keyrings/postgresql.asc] http://apt.postgresql.org/pub/repos/apt $(lsb_release -cs)-pgdg main" > /etc/apt/sources.list.d/pgdg.list'

# Create the working directories
RUN mkdir -p /backups /scripts /config

# Copy the scripts
COPY scripts/backup.sh /scripts/backup.sh
COPY scripts/entrypoint.sh /scripts/entrypoint.sh
COPY scripts/healthcheck.sh /scripts/healthcheck.sh

# Make the scripts executable
RUN chmod +x /scripts/backup.sh /scripts/entrypoint.sh /scripts/healthcheck.sh

# Create a log file for cron
RUN touch /var/log/cron.log

# Volume for the backups
VOLUME ["/backups", "/config"]

# Healthcheck verifying database connectivity
# start-period increased to allow the PostgreSQL clients to install at startup
HEALTHCHECK --interval=5m --timeout=30s --start-period=5m --retries=3 \
    CMD ["/scripts/healthcheck.sh"]

# Set the entrypoint
ENTRYPOINT ["/scripts/entrypoint.sh"]

# By default, follow the cron logs
CMD ["tail", "-f", "/var/log/cron.log"]
