<p align="center">
  <img src="public/images/lockup-horizontal.png" alt="database/backup" width="420">
</p>

Lightweight Docker image based on Debian Slim to automate PostgreSQL, MariaDB/MySQL, and MongoDB database backups with a built-in Go scheduler.

## Features

- PostgreSQL (versions 12 to 18), MariaDB/MySQL, and MongoDB support
- Dynamic client installation at startup (only required tools are installed based on config)
- Lightweight image (~126 MB): no database clients pre-installed
- YAML configuration (`backups.yml`) with full validation at startup
- In-process scheduler — no external cron daemon
- Streaming gzip compression for all dump formats
- Optional at-rest encryption: GPG (symmetric OpenPGP) or age (passphrase or public-key recipients)
- Optional TLS for database connections
- Credentials never exposed on process command lines
- Non-root runtime: container drops to uid 1000 after installing clients
- Built-in healthcheck using native database drivers (no `mongosh` download)
- Migration command to convert v1 `backups.conf` to YAML
- Centralized logging to container stdout
- Automated build via GitHub Actions
- Multi-architecture images (amd64, arm64)
- Automatic rebuild when the base image (`debian:trixie-slim`) is updated (4 times daily)

## Quick Start

### Using the pre-built image from GitHub Container Registry

```bash
docker pull ghcr.io/greite/database-backup:latest
```

Example `compose.yml` using the pre-built image:

```yaml
services:
  db-backup:
    image: ghcr.io/greite/database-backup:latest
    container_name: db-backup
    restart: unless-stopped
    volumes:
      - ./backups:/backups
      - ./backups.yml:/config/backups.yml:ro
    networks:
      - db-network
```

### Available Tags

- `latest` — Latest stable version
- `v2.0.0` — Tagged release version (semantic versioning: `v2.0.0`, `2.0`, `2`)

### Unraid (Community Applications)

An Unraid template for this image is maintained in the [Greite/unraid-templates](https://github.com/Greite/unraid-templates) repository and is published on the [Community Applications](https://forums.unraid.net/topic/38582-plug-in-community-applications/) store.

**Install via Community Applications:**

1. Open the **Apps** tab in your Unraid web UI
2. Search for `database-backup`
3. Click **Install** and adjust the paths:
   - **Backups Storage** — host path that will receive the dumps (default `/mnt/user/appdata/database-backup/backups`)
   - **Configuration File** — host path to your `backups.yml` (default `/mnt/user/appdata/database-backup/backups.yml`); copy [`backups.yml.example`](backups.yml.example) there first and edit it
   - **Timezone** — IANA timezone name (default `Europe/Paris`)
4. Click **Apply**

**Manual install (without Community Applications):**

In the Unraid web UI go to **Docker → Add Container**, set the **Template URL** to:

```
https://raw.githubusercontent.com/Greite/unraid-templates/main/database-backup/database-backup.xml
```

Then load the template and follow the same steps as above.

## Project Structure

```
.
├── Dockerfile
├── compose.yml                  # Example with test databases
├── backups.yml.example          # Configuration template
├── docs/
│   └── MIGRATION-V2.md          # Migration guide from v1
├── .github/
│   └── workflows/
│       ├── docker-build.yml     # Build workflow on version tags
│       ├── go-ci.yml            # Go lint, unit, and integration tests
│       └── base-image-check.yml # Scheduled rebuild on base image update
├── cmd/
│   └── dbbackup/                # Main binary
└── internal/                    # Internal packages (config, scheduler, crypto…)
```

## Configuration

### Config File (`backups.yml`)

Mount the file at `/config/backups.yml` inside the container. Use `chmod 600` on the host because the file contains credentials (or use `password_file` everywhere).

See [`backups.yml.example`](backups.yml.example) for a ready-to-use template.

#### Schema

**Top-level fields:**

| Field | Required | Default | Description |
|---|---|---|---|
| `defaults.retention_days` | No | `7` | Retention days applied to all jobs unless overridden |
| `defaults.tls` | No | `false` | TLS flag applied to all jobs unless overridden |
| `shutdown_grace` | No | `5m` | Grace period for in-flight backups on container stop. Must be a duration string (`"5m"`, `"300s"`, `"1h"`) — bare integers are rejected |
| `encryption` | No | — | At-rest encryption block (see below) |
| `jobs` | Yes | — | List of backup jobs |

**Per-job fields:**

| Field | Required | Default | Description |
|---|---|---|---|
| `name` | Yes | — | Unique job identifier (used in log output and directory paths) |
| `type` | Yes | — | `postgres`, `mariadb`, `mysql`, or `mongodb` |
| `host` | Yes | — | Database hostname or IP |
| `port` | No | 5432 / 3306 / 27017 | Connection port |
| `database` | Yes | — | Database name to back up |
| `user` | No* | — | Database user |
| `password` | No* | — | Inline password (any character, including `\|`) |
| `password_file` | No* | — | Path to a file containing the password (e.g. a Docker secret) |
| `schedule` | Yes | — | Cron expression (`"0 2 * * *"`) or shorthand (`"@daily"`) |
| `retention_days` | No | from `defaults` or `7` | Number of days to keep backups |
| `pg_version` | No | `18` | PostgreSQL client version (12–18); PostgreSQL only |
| `tls` | No | from `defaults` or `false` | `true` to encrypt the database connection |

*`user` and `password` / `password_file` are required for PostgreSQL and MariaDB. For MongoDB they are optional (omit for unauthenticated dev/test setups).

#### Encryption block

**GPG (symmetric OpenPGP)** — decrypt with `gpg -d file.gpg`:

```yaml
encryption:
  method: gpg
  passphrase_file: /run/secrets/backup_passphrase
  # or: passphrase: my_passphrase  (visible in docker inspect — avoid in production)
```

**age (passphrase)** — decrypt with `age -d`:

```yaml
encryption:
  method: age
  passphrase_file: /run/secrets/backup_passphrase
```

**age (public-key recipients)** — the container never holds the private key:

```yaml
encryption:
  method: age
  recipients:
    - age1ql3z7hjy54pw3hyww5ayyfg7zqgvc7w3j2elw8zmrj2kg5sfn9aqmcac8p
```

The v1 environment variables `BACKUP_ENCRYPTION_PASSPHRASE` and `BACKUP_ENCRYPTION_PASSPHRASE_FILE` remain supported and produce the same GPG-compatible files when no `encryption:` block is present.

#### Docker secrets (`password_file`)

```yaml
services:
  db-backup:
    image: ghcr.io/greite/database-backup:latest
    secrets:
      - db_password
    # In backups.yml: password_file: /run/secrets/db_password

secrets:
  db_password:
    file: ./secrets/db_password.txt
```

## Usage

### Volumes

| Mount point | Purpose |
|---|---|
| `/config/backups.yml` | YAML configuration file (required) |
| `/backups` | Directory where backup files are written |

### Subcommands

The container binary (`dbbackup`) supports the following subcommands. When no subcommand is given, `run` is the default.

| Subcommand | Description |
|---|---|
| `run` | Load config and start the built-in scheduler (default) |
| `validate` | Parse and validate the config, then exit; useful in CI |
| `backup --job <name>` | Run a single named job immediately (use with `docker exec`) |
| `healthcheck` | Check connectivity for all configured databases and exit |
| `migrate [path]` | Convert a v1 pipe-delimited file to YAML and print to stdout |

#### Docker Compose (recommended)

1. Copy and edit the example config:

```bash
cp backups.yml.example backups.yml
chmod 600 backups.yml
# Edit backups.yml with your settings
```

2. Start the services:

```bash
docker compose up -d
```

3. Check the logs:

```bash
docker compose logs -f db-backup
```

#### Docker Run

```bash
docker run -d \
  --name db-backup \
  -v $(pwd)/backups:/backups \
  -v $(pwd)/backups.yml:/config/backups.yml:ro \
  ghcr.io/greite/database-backup:latest
```

#### Validate before deploying

```bash
docker run --rm \
  -v $(pwd)/backups.yml:/config/backups.yml:ro \
  ghcr.io/greite/database-backup:latest validate
```

#### Trigger a manual backup

```bash
docker exec db-backup dbbackup backup --job myapp
```

## Migrating from v1

v2 replaces the pipe-delimited `backups.conf` with YAML (`backups.yml`). An automatic conversion command is provided:

```bash
docker run --rm -v /path/to/appdata:/config \
  ghcr.io/greite/database-backup:2 migrate /config/backups.conf > backups.yml
```

Review the output, then mount it at `/config/backups.yml`.

See [docs/MIGRATION-V2.md](docs/MIGRATION-V2.md) for the full field mapping and list of behavior changes.

## Managing Backups

### Backup file structure

Backups are organized by type and database:

```
backups/
├── postgres/
│   └── myapp_db/
│       ├── myapp_db_20250131_020000.sql.gz
│       ├── myapp_db_20250130_020000.sql.gz
│       └── ...
├── mariadb/
│   └── wordpress/
│       ├── wordpress_20250131_030000.sql.gz
│       └── ...
└── mongodb/
    └── events/
        ├── events_20250131_000000.tar.gz
        └── ...
```

> **Note:** MongoDB backups use `.tar.gz` format (compressed BSON archive), while PostgreSQL and MariaDB use `.sql.gz` (compressed SQL dump). Encrypted backups get an additional `.gpg` or `.age` suffix.

### Restore a backup

> If at-rest encryption is enabled, first decrypt the file:
> ```bash
> # GPG
> gpg --batch --passphrase-file /path/to/passphrase \
>   --decrypt myapp_db_20250131_020000.sql.gz.gpg > myapp_db_20250131_020000.sql.gz
> # age
> age --decrypt -i /path/to/key myapp_db_20250131_020000.sql.gz.age > myapp_db_20250131_020000.sql.gz
> ```

**PostgreSQL:**

```bash
gunzip -c backups/postgres/myapp_db/myapp_db_20250131_020000.sql.gz | \
  psql -h localhost -U postgres -d myapp_db
```

**MariaDB:**

```bash
gunzip -c backups/mariadb/wordpress/wordpress_20250131_030000.sql.gz | \
  mysql -h localhost -u root -p wordpress
```

**MongoDB:**

```bash
mkdir -p /tmp/mongo_restore
tar -xzf backups/mongodb/events/events_20250131_000000.tar.gz -C /tmp/mongo_restore
mongorestore --uri="mongodb://admin:password@localhost:27017/events?authSource=admin" \
  --gzip --drop /tmp/mongo_restore/events
rm -rf /tmp/mongo_restore
```

## Security

### Password handling

All passwords are written at startup to per-job files (root-only, mode 600) and are never passed on command lines — `argv` is world-readable through `/proc/*/cmdline`, so nothing sensitive ever appears in `ps aux` or `docker top`.

Prefer `password_file` over inline `password` in production; Docker secrets mount the file at `/run/secrets/<name>` automatically.

### TLS connections

Set `tls: true` on a job (or under `defaults`) to encrypt the connection: `PGSSLMODE=require` for PostgreSQL, `--ssl` for MariaDB/MySQL, `--ssl`/`--tls` for MongoDB. Recommended when the database is not on the same private Docker network.

### Best practices

1. **File permissions**: `chmod 600 backups.yml` on the host (the container warns at startup if the mounted file is group/other-readable)

2. **Dedicated backup users**: Create users with minimal privileges:

**PostgreSQL:**
```sql
CREATE USER backup_user WITH PASSWORD 'secure_password';
GRANT CONNECT ON DATABASE myapp_db TO backup_user;
GRANT SELECT ON ALL TABLES IN SCHEMA public TO backup_user;
ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT SELECT ON TABLES TO backup_user;
```

**MariaDB:**
```sql
CREATE USER 'backup_user'@'%' IDENTIFIED BY 'secure_password';
GRANT SELECT, LOCK TABLES, SHOW VIEW, EVENT, TRIGGER ON myapp_db.* TO 'backup_user'@'%';
FLUSH PRIVILEGES;
```

**MongoDB:**
```javascript
use admin
db.createUser({
  user: "backup_user",
  pwd: "secure_password",
  roles: [
    { role: "backup", db: "admin" },
    { role: "read", db: "myapp" }
  ]
})
```

3. **Backup storage**: Consider mounting an encrypted volume for `/backups`

4. **Off-site backups**: Regularly sync backups to external storage (S3, NAS, etc.)

## Monitoring

The container includes a healthcheck that verifies all configured databases are reachable using native drivers. It runs every 5 minutes with a 30-second timeout.

```bash
# View healthcheck status
docker inspect --format='{{.State.Health.Status}}' db-backup

# View last healthcheck details
docker inspect --format='{{json .State.Health}}' db-backup | jq

# Run the healthcheck manually
docker exec db-backup dbbackup healthcheck
```

### Dynamic client installation

No database client is pre-installed in the Docker image. At startup the container parses the configuration and installs only the required tools:

- **PostgreSQL**: installs the specific configured versions (12–18)
- **MariaDB/MySQL**: installs `mariadb-client`
- **MongoDB**: installs `mongodump` and `mongorestore`

Internet access is required on first startup. First startup may take an extra 30–90 seconds depending on which clients need to be installed.

## CI/CD with GitHub Actions

### Release Build (`.github/workflows/docker-build.yml`)

Triggered on version tags (`v*.*.*`) or manual dispatch. Builds and pushes the Docker image to GitHub Container Registry with semantic versioning tags.

### Base Image Update Check (`.github/workflows/base-image-check.yml`)

Runs automatically **4 times daily** (at 00:00, 06:00, 12:00, and 18:00 UTC) to check if the `debian:trixie-slim` base image has been updated. If a new version is detected, it automatically rebuilds and pushes the image.

### Creating a release

```bash
git tag -a v2.0.0 -m "Release version 2.0.0"
git push origin v2.0.0
```

This automatically creates the following Docker tags:
- `ghcr.io/greite/database-backup:v2.0.0`
- `ghcr.io/greite/database-backup:2.0`
- `ghcr.io/greite/database-backup:2`
- `ghcr.io/greite/database-backup:latest`

## License

This project is licensed under the MIT License — see the [LICENSE](LICENSE) file for details.
