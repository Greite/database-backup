<p align="center">
  <img src="public/images/lockup-horizontal.svg" alt="database/backup" width="420">
</p>

Lightweight Docker image based on Debian Slim to automate PostgreSQL, MariaDB/MySQL, and MongoDB database backups via cron.

## Features

- PostgreSQL (versions 12 to 18), MariaDB/MySQL, and MongoDB support
- Dynamic client installation at startup (only required tools are installed based on config)
- Lightweight image: no database clients pre-installed
- Flexible configuration via a simple config file
- Cron-based backup scheduling
- Automatic dump compression (gzip)
- Timestamped backup files
- Automatic rotation of old backups
- Multiple simultaneous database support
- Built-in healthcheck verifying database connectivity
- Centralized logging
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
      - ./backups.conf:/config/backups.conf:ro
    networks:
      - db-network
```

### Available Tags

- `latest` — Latest stable version
- `v1.0.0` — Tagged release version (semantic versioning: `v1.0.0`, `1.0`, `1`)

## Project Structure

```
.
├── Dockerfile
├── compose.yml                 # Example with test databases
├── backups.conf                # Backup configuration
├── backups.conf.example        # Configuration template
├── GITHUB_SETUP.md             # GitHub Actions setup guide
├── .github/
│   └── workflows/
│       ├── docker-build.yml    # Build workflow on version tags
│       └── base-image-check.yml # Scheduled rebuild on base image update
├── scripts/
│   ├── backup.sh               # Main backup script
│   ├── entrypoint.sh           # Initialization script
│   └── healthcheck.sh          # Healthcheck script
└── backups/                    # Backup directory (created automatically)
    ├── postgres/
    ├── mariadb/
    └── mongodb/
```

## Configuration

### Config File Format (`backups.conf`)

Each line defines a backup job with pipe-delimited fields:

```
CRON_SCHEDULE|TYPE|HOST|PORT|DATABASE|USER|PASSWORD|RETENTION_DAYS|PG_VERSION
```

**Fields:**

| Field | Description | Required | Default |
|-------|-------------|----------|---------|
| `CRON_SCHEDULE` | Standard cron expression (e.g., `0 2 * * *`) | Yes | — |
| `TYPE` | Database type: `postgres`, `mariadb`, `mysql`, or `mongodb` | Yes | — |
| `HOST` | Database server hostname or IP | Yes | — |
| `PORT` | Connection port | No | 5432 / 3306 / 27017 |
| `DATABASE` | Database name to back up | Yes | — |
| `USER` | Database user | No* | — |
| `PASSWORD` | Database password (special characters supported) | No* | — |
| `RETENTION_DAYS` | Number of days to keep backups | No | 7 |
| `PG_VERSION` | PostgreSQL client version (12–18), PostgreSQL only | No | 18 |

*Required for PostgreSQL/MariaDB; optional for MongoDB without authentication.

**Password handling:**
Passwords with special characters (`!`, `@`, `#`, `$`, `%`, `^`, `&`, `*`, etc.) are fully supported. The system automatically escapes all special characters — no manual quoting needed in the config file.

**Examples:**

```conf
# PostgreSQL 18 daily at 2 AM, keep 14 days
0 2 * * *|postgres|db-server|5432|myapp|backup_user|SecureP@ss|14|18

# PostgreSQL 15 (legacy server) daily at 2:30 AM, keep 14 days
0 2 30 * * *|postgres|pg-old-server|5432|legacy_app|backup_user|SecureP@ss|14|15

# PostgreSQL without specifying version (defaults to v18)
0 3 * * *|postgres|pg-new|5432|modern_app|dbuser|pass123|7

# MariaDB daily at 3 AM, keep 7 days
0 3 * * *|mariadb|mysql-server|3306|wordpress|wp_backup|MyPassword|7

# Every 6 hours, keep 3 days
0 */6 * * *|postgres|localhost|5432|ecommerce|dbuser|pass123|3|18

# Every Sunday at midnight, keep 30 days
0 0 * * 0|mariadb|db.example.com||analytics|readonly|secret|30

# Password with special characters
0 4 * * *|postgres|pg-prod|5432|webapp|admin|ZxirfRuipZPHPc^#V#HFpCpRyrQ!zG5W|14|18

# MongoDB with authentication
0 5 * * *|mongodb|mongo-prod|27017|ecommerce|dbadmin|SecureM0ng0!|14

# MongoDB without authentication (dev/test)
0 5 * * *|mongodb|localhost|27017|test_db|||7
```

**PostgreSQL version notes:**
- The `PG_VERSION` field is only used for PostgreSQL backups
- If omitted, version 18 is used by default
- This allows backing up different PostgreSQL versions with the same container
- Supported versions: 12, 13, 14, 15, 16, 17, 18

**Dynamic client installation:**
- **No** database client is pre-installed in the Docker image
- At startup, the container parses the configuration and installs only the required tools:
  - **PostgreSQL**: installs the specific configured versions (12–18)
  - **MariaDB/MySQL**: installs `mariadb-client` if configured
  - **MongoDB**: installs `mongodump`, `mongorestore`, and `mongosh` if configured
- This significantly reduces the base image size
- **Prerequisite**: internet access is required on first startup
- First startup may take an extra 30–90 seconds depending on which clients need to be installed

### Common Cron Expressions

```
0 2 * * *      # Every day at 2 AM
0 */6 * * *    # Every 6 hours
0 0 * * 0      # Every Sunday at midnight
30 1 * * *     # Every day at 1:30 AM
0 0 1 * *      # First day of each month at midnight
```

## Usage

### Option 1: Docker Compose (recommended)

1. Create your `backups.conf` file:

```bash
cp backups.conf.example backups.conf
# Edit backups.conf with your settings
```

2. Start the services:

```bash
docker compose up -d
```

3. Check the logs:

```bash
docker compose logs -f db-backup
```

### Option 2: Docker Run

1. Build the image:

```bash
docker build -t db-backup .
```

2. Run the container:

```bash
docker run -d \
  --name db-backup \
  -v $(pwd)/backups:/backups \
  -v $(pwd)/backups.conf:/config/backups.conf:ro \
  db-backup
```

3. Check the logs:

```bash
docker logs -f db-backup
```

## Managing Backups

### View created backups

```bash
ls -lh backups/postgres/myapp_db/
ls -lh backups/mariadb/wordpress/
ls -lh backups/mongodb/ecommerce/
```

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
│       ├── wordpress_20250130_030000.sql.gz
│       └── ...
└── mongodb/
    └── ecommerce/
        ├── ecommerce_20250131_050000.tar.gz
        ├── ecommerce_20250130_050000.tar.gz
        └── ...
```

> **Note:** MongoDB backups use `.tar.gz` format (compressed BSON archive), while PostgreSQL and MariaDB use `.sql.gz` (compressed SQL dump).

### Restore a backup

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
# Extract the archive and restore
mkdir -p /tmp/mongo_restore
tar -xzf backups/mongodb/ecommerce/ecommerce_20250131_050000.tar.gz -C /tmp/mongo_restore

# Restore the database
mongorestore --uri="mongodb://admin:password@localhost:27017/ecommerce?authSource=admin" \
  --gzip \
  --drop \
  /tmp/mongo_restore/ecommerce

# Clean up
rm -rf /tmp/mongo_restore
```

### Trigger a manual backup

You can run a backup manually without waiting for cron:

**PostgreSQL 18:**
```bash
docker exec db-backup /scripts/backup.sh \
  postgres db-server 5432 myapp_db postgres postgres_password 14 18
```

**PostgreSQL 15 (or other version):**
```bash
docker exec db-backup /scripts/backup.sh \
  postgres db-server 5432 myapp_db postgres postgres_password 14 15
```

**MariaDB:**
```bash
docker exec db-backup /scripts/backup.sh \
  mariadb mariadb-db 3306 wordpress wp_user wp_password 7
```

**MongoDB:**
```bash
docker exec db-backup /scripts/backup.sh \
  mongodb mongodb-db 27017 myapp admin mongo_password 7
```

## Security

### Password handling

**Special character support:**
The system automatically handles complex passwords containing all types of special characters: `!@#$%^&*()-_+=`, spaces, and Unicode characters.

**How it works:**
- **PostgreSQL**: uses the `PGPASSWORD` environment variable (recommended secure method)
- **MariaDB/MySQL**: uses the `MYSQL_PWD` environment variable (avoids command-line exposure)
- **MongoDB**: uses the connection URI with built-in authentication (password is automatically URL-encoded)
- Passwords are automatically escaped with `printf %q` for safe passage through the cron system

### Best practices

1. **File permissions**: Ensure `backups.conf` has restrictive permissions since it contains passwords:

```bash
chmod 600 backups.conf
```

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

### Check service status

```bash
docker compose ps
```

The container includes a healthcheck that automatically verifies:
- The cron daemon is running
- All configured databases are reachable
- Connections use the correct PostgreSQL client version

The healthcheck runs every 5 minutes with a 30-second timeout. View healthcheck status:

```bash
docker inspect --format='{{.State.Health.Status}}' db-backup
```

View last healthcheck details:

```bash
docker inspect --format='{{json .State.Health}}' db-backup | jq
```

Run the healthcheck manually:

```bash
docker exec db-backup /scripts/healthcheck.sh
```

### View real-time logs

```bash
docker compose logs -f db-backup
```

### Check recent backups

```bash
find backups -name "*.sql.gz" -type f -mtime -1 -ls
```

## Troubleshooting

### Container won't start

Check that the `backups.conf` file exists:

```bash
docker compose logs db-backup
```

### Backups aren't running

1. Check the cron configuration:

```bash
docker exec db-backup cat /etc/cron.d/db-backups
```

2. Verify cron is running:

```bash
docker exec db-backup ps aux | grep cron
```

3. Test database connectivity:

```bash
# PostgreSQL — check installed versions
docker exec db-backup ls -la /usr/lib/postgresql/

# PostgreSQL 18
docker exec db-backup /usr/lib/postgresql/18/bin/pg_dump --version
docker exec db-backup /usr/lib/postgresql/18/bin/psql -h postgres-db -U postgres -d myapp_db -c "SELECT 1"

# MariaDB
docker exec db-backup mysqldump --version
docker exec db-backup mysql -h mariadb-db -u wp_user -p'wp_password' -e "SELECT 1"

# MongoDB
docker exec db-backup mongodump --version
docker exec db-backup mongosh "mongodb://admin:mongo_password@mongodb-db:27017/myapp?authSource=admin" --eval "db.runCommand({ping: 1})"
```

### Old backups aren't being deleted

Check that `RETENTION_DAYS` is properly set in your configuration and that the value is a positive number.

## Customization

### Change the timezone

Set the `TZ` environment variable:

```yaml
services:
  db-backup:
    image: ghcr.io/greite/database-backup:latest
    environment:
      - TZ=Europe/Paris
```

### Add notifications

Modify `scripts/backup.sh` to send email or webhook notifications on success or failure.

### Change compression format

Replace `gzip` with `bzip2` or `xz` in `scripts/backup.sh` for better compression.

## CI/CD with GitHub Actions

This project includes two GitHub Actions workflows:

### Release Build (`.github/workflows/docker-build.yml`)

Triggered on version tags (`v*.*.*`) or manual dispatch. Builds and pushes the Docker image to GitHub Container Registry with semantic versioning tags.

### Base Image Update Check (`.github/workflows/base-image-check.yml`)

Runs automatically **4 times daily** (at 00:00, 06:00, 12:00, and 18:00 UTC) to check if the `debian:trixie-slim` base image has been updated. If a new version is detected, it automatically rebuilds and pushes the image to keep it up to date with the latest security patches.

### Creating a release

```bash
git tag -a v1.0.0 -m "Release version 1.0.0"
git push origin v1.0.0
```

This automatically creates the following Docker tags:
- `ghcr.io/greite/database-backup:v1.0.0`
- `ghcr.io/greite/database-backup:1.0`
- `ghcr.io/greite/database-backup:1`
- `ghcr.io/greite/database-backup:latest`

### Verify the build

1. Go to the **Actions** tab of your GitHub repository
2. Select the relevant workflow
3. Verify the build succeeded
4. The image will be available in the **Packages** section of your repository

## License

This project is licensed under the MIT License — see the [LICENSE](LICENSE) file for details.
