# syntax=docker/dockerfile:1

FROM golang:1.25 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/ cmd/
COPY internal/ internal/
# Static binary; tzdata is embedded so the TZ env var works without
# the Debian tzdata package.
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -tags timetzdata \
    -o /dbbackup ./cmd/dbbackup

FROM debian:trixie-slim

# apt stays available: database clients are installed at startup based
# on the configuration, so the base image ships none of them.
# The PostgreSQL APT repository is preconfigured (clients install at startup);
# the release codename comes from os-release at build time.
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    curl \
    && mkdir -p /etc/apt/keyrings \
    && . /etc/os-release \
    && curl -fsSL https://www.postgresql.org/media/keys/ACCC4CF8.asc -o /etc/apt/keyrings/postgresql.asc \
    && echo "deb [signed-by=/etc/apt/keyrings/postgresql.asc] http://apt.postgresql.org/pub/repos/apt ${VERSION_CODENAME}-pgdg main" > /etc/apt/sources.list.d/pgdg.list \
    && apt-get purge -y curl && apt-get autoremove -y \
    && rm -rf /var/lib/apt/lists/*

# Unprivileged user the process drops to after installing clients.
# debian:trixie-slim ships backup:34:34 — reassign to uid/gid 1000.
RUN groupmod -g 1000 backup && usermod -u 1000 -g 1000 backup \
    && mkdir -p /backups /config \
    && chown backup:backup /backups

COPY --from=build /dbbackup /usr/local/bin/dbbackup

VOLUME ["/backups", "/config"]

# start-period allows the startup client installation to finish.
HEALTHCHECK --interval=5m --timeout=30s --start-period=5m --retries=3 \
    CMD ["/usr/local/bin/dbbackup", "healthcheck"]

ENTRYPOINT ["/usr/local/bin/dbbackup"]
CMD ["run"]
