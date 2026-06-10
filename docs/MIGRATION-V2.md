# Migrating from v1 (bash) to v2 (Go)

v2 replaces the pipe-delimited `backups.conf` with YAML (`backups.yml`)
and the cron daemon with a built-in scheduler. Backups produced by v1
remain readable: same `.sql.gz`/`.tar.gz` formats, same `gpg -d`
decryption for encrypted files.

## Automatic conversion

```bash
docker run --rm -v /path/to/appdata:/config \
  ghcr.io/greite/database-backup:2 migrate /config/backups.conf > backups.yml
```

Review the output, then mount it at `/config/backups.yml`.

## Field mapping

| v1 column | v2 field |
|---|---|
| CRON_SCHEDULE | `schedule` |
| TYPE | `type` |
| HOST | `host` |
| PORT | `port` (optional, defaults by type) |
| DATABASE | `database` (also seeds `name`) |
| USER | `user` |
| PASSWORD | `password` (or switch to `password_file`) |
| RETENTION_DAYS | `retention_days` |
| PG_VERSION | `pg_version` |
| TLS | `tls` |

## Behavior changes

- Invalid configuration is now fatal at startup (v1 skipped bad lines
  with a warning). Run `dbbackup validate` to check before deploying.
- Old backups are no longer rotated when the new backup failed.
- The container drops to uid 1000 after installing database clients;
  the container fixes ownership of the `/backups` mount point itself
  at startup.
- Logs go to container stdout (no `/var/log/cron.log`).
- `mongosh` is no longer downloaded: the healthcheck uses native drivers.
- Passwords may now contain any character, including the pipe `|`.
- `BACKUP_ENCRYPTION_PASSPHRASE` / `BACKUP_ENCRYPTION_PASSPHRASE_FILE`
  keep working unchanged and produce the same gpg-compatible files.
