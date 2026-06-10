# database-backup v2 — Go Rewrite Design

**Date:** 2026-06-10
**Status:** Approved design, pending implementation plan

## Context and Motivation

The current container is built from three bash scripts (~700 lines): `entrypoint.sh` parses a pipe-delimited config, installs database clients at startup, generates a crontab and per-job secret files; `backup.sh` runs dumps via external tools, compresses, optionally encrypts with GPG, and rotates old backups; `healthcheck.sh` tests connectivity using the database CLIs.

This works, but has structural limits:

- **Speed:** the cron-based design requires `mongosh` (~200 MB unpacked, downloaded at every container creation) just for the healthcheck; dumps are written uncompressed to disk and then gzipped.
- **Security:** cron requires root for the whole container lifetime; secrets are written to job files on disk; strict field validation is needed only because values are interpolated into a crontab.
- **Tests:** the bash logic is effectively untestable; there are no unit or integration tests.
- **Maintainability:** parsing, validation, scheduling, and error handling are spread across shell scripts with stringly-typed state.

The v2 rewrites the orchestration layer in Go. External dump tools (`pg_dump`, `mariadb-dump`, `mongodump`) are kept: they are the only credible implementations of their formats.

## Goals

- Single static Go binary replacing entrypoint + cron + backup + healthcheck scripts.
- Feature parity with v1: scheduled dumps for PostgreSQL (multi-version), MariaDB/MySQL, MongoDB; gzip compression; optional at-rest encryption; per-job retention/rotation; connectivity healthcheck; dynamic client installation at startup; timezone support.
- New YAML configuration format (breaking change, see Migration).
- Privilege drop: only client installation runs as root; scheduler and backups run as uid 1000.
- Encryption selectable per config: OpenPGP (`gpg`-compatible output) or `age` (passphrase or X25519 recipients).
- Unit + integration test coverage (real databases via testcontainers).
- Smaller base image (~110–130 MB target vs 223 MB today) and faster startup (no `mongosh`).

## Non-Goals (deferred to later versions)

- HTTP status/metrics endpoint (Prometheus).
- Failure notifications (webhook/ntfy/Discord).
- Restore verification jobs.
- Remote storage backends (S3, etc.).
- Pure-Go MongoDB dump (mongo-tools as a library) — `mongodump` binary is kept for now.

## Decisions Summary

| Topic | Decision |
|---|---|
| Compatibility | v2 breaking change; v1 stays available (branch `v1`, existing image tags) |
| Config format | YAML (`/config/backups.yml`), parsed with `gopkg.in/yaml.v3` |
| DB client tools | Installed at container startup based on config (as in v1) |
| Privileges | Start as root for installation, then re-exec as uid/gid 1000 |
| Scope | Feature parity only; no new user-facing features besides `age` encryption and `migrate` |
| Encryption | Optional; `method: gpg` (OpenPGP, `gpg -d` compatible) or `method: age` (passphrase or recipients) |
| Architecture | Single multi-command binary (`run`, `healthcheck`, `backup`, `validate`, `migrate`) |

## Architecture

One binary, `dbbackup`, with subcommands:

- `dbbackup run` — container default: parse config, install missing clients, drop privileges, start the in-process scheduler.
- `dbbackup healthcheck` — Docker `HEALTHCHECK`: native ping of every configured database (no CLI clients involved).
- `dbbackup backup --job <name>` — run one job immediately (manual/debug, via `docker exec`).
- `dbbackup validate` — parse and validate the config file, exit 0/1. Useful in CI and before deployments.
- `dbbackup migrate <v1-file>` — convert a v1 pipe-delimited `backups.conf` to v2 YAML on stdout.

### Project structure

```
database-backup/
├── cmd/dbbackup/main.go        # entry point, subcommand dispatch
├── internal/
│   ├── config/                 # YAML parsing + validation (strong types, defaults)
│   ├── installer/              # detect & install DB clients at boot (apt / tgz+sha256)
│   ├── privileges/             # root → uid 1000 re-exec after installation
│   ├── scheduler/              # robfig/cron/v3 wrapper, job triggering, graceful stop
│   ├── dumper/                 # Dumper interface + postgres.go, mariadb.go, mongodb.go
│   ├── archive/                # streaming gzip (SQL) and tar.gz (MongoDB directory dumps)
│   ├── crypto/                 # Encryptor interface + openpgp.go, age.go
│   ├── rotation/               # purge backups older than retention_days
│   ├── healthcheck/            # native pings (pgx, go-sql-driver/mysql, mongo-driver)
│   └── migrate/                # v1 pipe format → v2 YAML conversion
├── Dockerfile                  # multi-stage: golang build → debian:trixie-slim
└── docs/
```

Each package has a single responsibility and a testable interface. Key interfaces:

```go
type Dumper interface {
    // Dump streams the raw dump to w. Implementations exec the external
    // tool with stdout piped; MongoDB writes a tar stream of the dump dir.
    Dump(ctx context.Context, job config.Job, w io.Writer) error
}

type Encryptor interface {
    // Wrap returns a WriteCloser that encrypts everything written to it
    // into out. Close finalizes the stream.
    Wrap(out io.Writer) (io.WriteCloser, error)
}
```

### Dependencies

| Library | Role |
|---|---|
| `gopkg.in/yaml.v3` | config parsing |
| `github.com/robfig/cron/v3` | in-process scheduler (standard 5-field cron expressions) |
| `github.com/ProtonMail/go-crypto` | OpenPGP symmetric encryption (`gpg`-compatible `.gpg` output) |
| `filippo.io/age` | age encryption (passphrase or X25519 recipients, `.age` output) |
| `github.com/jackc/pgx/v5` | PostgreSQL ping (healthcheck only) |
| `github.com/go-sql-driver/mysql` | MariaDB/MySQL ping (healthcheck only) |
| `go.mongodb.org/mongo-driver` | MongoDB ping (healthcheck only) |
| `github.com/testcontainers/testcontainers-go` | integration tests (dev dependency) |

Timezone data is embedded via the standard `time/tzdata` package; the `TZ` environment variable keeps working without the Debian `tzdata` package.

## Configuration Format (v2)

File: `/config/backups.yml`.

```yaml
defaults:            # optional, applies to every job unless overridden
  retention_days: 7
  tls: false

encryption:          # optional; absent = no encryption
  method: gpg        # "gpg" or "age"
  passphrase_file: /run/secrets/backup_pass
  # passphrase: "inline-secret"        # discouraged; passphrase_file preferred
  # recipients:                        # age public-key mode (method: age only)
  #   - age1ql3z7hjy54pw3hyww5ayyfg7zqgvc7w3j2elw8zmrj2kg5sfn9aqmcac8p

jobs:
  - name: myapp                 # required, unique, [A-Za-z0-9._-]
    type: postgres              # postgres | mariadb | mysql | mongodb
    host: postgres-server       # required
    port: 5432                  # optional, defaults: 5432/3306/27017 by type
    database: myapp_db          # required
    user: backup_user           # required except mongodb without auth
    password: SecretPassword    # or password_file (Docker secrets friendly)
    # password_file: /run/secrets/pg_pass
    schedule: "0 2 * * *"       # required, standard cron expression
    retention_days: 14          # optional, default from defaults block
    pg_version: 18              # postgres only, optional, default 18
    tls: false                  # optional
```

Validation rules:

- Config errors are **fatal at startup** with file/line/field context. v1's "warn and skip" behavior is dropped: a misconfigured job must be noticed.
- `name` is the backup directory key: restricted to `[A-Za-z0-9._-]`, not `.` or `..`.
- `host`, `user`, `database`, `password` are free-form (no crontab interpolation anymore, so no charset restriction beyond what the tools accept). The pipe character is no longer special.
- `password` and `password_file` are mutually exclusive; same for encryption `passphrase`/`passphrase_file`/`recipients`.
- `recipients` is only valid with `method: age`.
- Environment variables `BACKUP_ENCRYPTION_PASSPHRASE` / `BACKUP_ENCRYPTION_PASSPHRASE_FILE` are still honored (v1 parity) and are equivalent to `encryption: {method: gpg, passphrase[_file]: …}`; the YAML block takes precedence if both are present.

## Runtime Flow

### Startup (`dbbackup run`)

1. Parse and validate `/config/backups.yml`. Any error is fatal with a precise message.
2. Guard: if `backups.yml` is missing but a v1 `/config/backups.conf` exists, exit with a message showing the exact `migrate` command to run.
3. Warn if the config file is group/other-readable (as in v1).
4. Detect required clients from job types; install missing ones **as root**: `postgresql-client-<N>` via apt (pgdg repo preconfigured in the image), `mariadb-client` via apt, MongoDB Database Tools via tgz download + SHA256 verification (checksums pinned as in v1). `mongosh` is no longer needed.
5. **Privilege drop:** re-exec the binary as uid/gid 1000 (`backup:backup`, created in the image). Everything after — scheduler, dumps, writes to `/backups` — runs unprivileged. `/backups` ownership is fixed up before the drop if needed.
6. Register every job with the scheduler and run forever. On SIGTERM/SIGINT: stop scheduling new runs, let in-flight backups finish within a configurable grace period (default 5 min), then exit.

Logs go to stdout/stderr (standard container logging). No cron log file, no `tail -f`.

### Job execution

```
external tool (stdout pipe) → gzip writer → [encryptor] → temp file → rename
```

- `pg_dump`/`mariadb-dump` stream SQL to stdout; `mongodump` dumps to a temp directory which is streamed as tar. Compression and encryption happen in-process and in-stream: **no intermediate plaintext file ever touches disk**.
- Output: `/backups/<type>/<name>/<name>_<YYYYMMDD_HHMMSS>.sql.gz[.gpg|.age]` (`.tar.gz[.gpg|.age]` for MongoDB), written as `<file>.tmp` then renamed on success — no partial file is ever visible under its final name. Files are mode 600, directories 700.
- Secrets are passed to child processes via environment (`PGPASSWORD`, `MYSQL_PWD`) or a 600 temp config file for `mongodump` — never via argv (visible in `/proc/*/cmdline`).
- TLS per job: `PGSSLMODE=require`, `--ssl`, `--ssl` (mongodump) as in v1.

### Error handling

- A failing job logs the job name, stage, and the tool's stderr; removes its temp file; and does not affect other jobs.
- **Rotation runs only after a successful backup** (v1 rotated unconditionally; v2 never deletes old backups when the new one failed).
- Rotation deletes files matching the job's own pattern older than `retention_days` (0 disables rotation), then logs the remaining count.
- The last run result per job is kept in memory (informational logging; the Docker healthcheck remains connectivity-based).

### Healthcheck (`dbbackup healthcheck`)

For each configured job: native driver ping with a 5 s timeout (pgx / mysql driver / mongo-driver, honoring TLS and credentials). Exit 0 if all pass, 1 otherwise, with per-job OK/FAILED lines. Dockerfile keeps `HEALTHCHECK --interval=5m --timeout=30s --start-period=5m --retries=3`.

## Security Model

- Root is used only for client installation at boot; the long-running process and all dumps run as uid 1000.
- Secrets live in memory only (no `/etc/backup-jobs` job files). `password_file` support enables Docker secrets.
- No crontab generation → no shell interpolation → no injection surface; field validation is only about path safety (`name`) and tool correctness.
- `age` recipients mode allows a setup where the container holds only a public key: a compromised container cannot decrypt existing backups.
- Download verification: MongoDB tools tgz checked against pinned SHA256, as in v1.

## Migration (v1 → v2)

`dbbackup migrate /config/backups.conf` writes the equivalent YAML to stdout (works with read-only `/config` mounts):

```bash
docker run --rm -v /mnt/user/appdata/database-backup:/config \
  ghcr.io/greite/database-backup:2 migrate /config/backups.conf > backups.yml
```

- Field mapping: `CRON_SCHEDULE→schedule`, `TYPE→type`, `HOST→host`, `PORT→port`, `DATABASE→database`, `USER→user`, `PASSWORD→password`, `RETENTION_DAYS→retention_days`, `PG_VERSION→pg_version`, `TLS→tls`. `name` is derived from the database name (suffixed `-2`, `-3`… on collision).
- Invalid v1 lines are reported on stderr with their line number; valid lines are still converted (exit code 1 if any line was skipped).
- v1 encryption env vars need no migration: they keep working in v2 (GPG output stays decryptable with `gpg -d`).

## Docker Image

```dockerfile
FROM golang:1.24 AS build       # CGO_ENABLED=0, static binary
FROM debian:trixie-slim         # apt needed for startup client installation
# + ca-certificates, pgdg apt repo preconfigured, backup user (uid 1000),
# + /usr/local/bin/dbbackup
ENTRYPOINT ["/usr/local/bin/dbbackup", "run"]
HEALTHCHECK CMD ["/usr/local/bin/dbbackup", "healthcheck"]
```

Removed vs v1: `cron`, `wget`, `curl`, `gnupg`, `lsb-release`, `procps`, `gzip`, `tzdata` (embedded in the binary). The pgdg repo codename is resolved from `/etc/os-release` at build time (no `lsb-release`).

Expected base image: **~110–130 MB** (vs 223 MB measured for v1). Startup no longer downloads `mongosh` for MongoDB configs (~200 MB unpacked saved at runtime).

## Testing Strategy

- **Unit tests** (no external deps): config parsing/validation (valid, invalid, defaults, golden files), rotation logic (synthetic file sets and dates), encryption round-trips for both gpg and age (encrypt in Go, decrypt and compare), dump tool argument construction, migrate conversion (golden v1→v2 pairs).
- **Integration tests** (`testcontainers-go`, run in CI): start real PostgreSQL/MariaDB/MongoDB containers, seed data, run a backup, then **restore the dump and compare the data**. Healthcheck tested against up and down databases.
- **Container smoke test** (CI): build the image, start it with an example config, assert healthcheck passes and a scheduled job produces a file.

## CI and Release

- GitHub Actions: `golangci-lint` + unit tests on every push; integration tests + multi-arch build (amd64, arm64) on PRs and releases. Adapt the existing `docker-build.yml`; keep `base-image-check.yml` (auto-rebuild on debian updates).
- Versioning: v2 ships as `v2.0.0` with image tags `2`, `2.0`, `2.0.0`. The `latest` tag moves to v2 **only when the migration guide is published**. v1 lives on a `v1` branch (security fixes only) with its existing tags untouched.
- Docs: rewritten README (YAML format, subcommands), migration guide with the v1→v2 field table, new `backups.yml.example` replacing `backups.conf.example`.

## Release Checklist — External Repos

- **Unraid template** (`Greite/unraid-templates/database-backup/database-backup.xml`):
  - `Configuration File` mapping: `/config/backups.conf` → `/config/backups.yml`, updated description and example link.
  - Rewritten `Overview` (YAML config, native healthcheck, non-root runtime, gpg/age encryption).
  - Add optional env vars to the template: `TZ` (`Display="advanced"`), `BACKUP_ENCRYPTION_PASSPHRASE` (`Display="advanced"`, `Mask="true"`).
  - Publish the template update together with the `latest` tag switch, not before.
