# database-backup

Single static Go binary (`dbbackup`) in a `debian:trixie-slim` image that schedules PostgreSQL / MariaDB / MongoDB dumps. No shell scripts, no cron daemon. User-facing docs are in README.md; this file only covers what a contributor needs beyond it.

## Commands

```bash
test -z "$(gofmt -l .)" && go vet ./... && go test ./...   # what CI runs on every push; no Docker needed
GOOS=linux golangci-lint run ./...                       # lint like CI (see Gotchas for why GOOS=linux)
go test -tags integration -v ./internal/integration/     # testcontainers; needs Docker + Debian-path clients, skips otherwise
docker compose up -d --build && docker compose logs -f db-backup   # full image against the three sample databases
```

## Layout

- `cmd/dbbackup/` — subcommand dispatch (`run` default, `validate`, `backup --job`, `healthcheck`, `migrate`); register new commands in `commands.go` `init()`.
- `internal/config` — YAML schema, validation, secret resolution (`password_file`, `passphrase_file`).
- `internal/dumper` — one file per engine plus `runner.go`: dump → gzip → optional encrypt → 0600 temp file → rename → `rotation.Purge`.
- `internal/installer` — apt/curl install of clients at container start (root phase); MongoDB tools are version- and SHA256-pinned here.
- `internal/privileges` — Linux-only privilege drop by re-exec; `privileges_other.go` is a stub so the tree builds and tests on macOS.
- `docs/superpowers/` — v2 rewrite spec and plan, historical, not maintained.

## Conventions

- Commit subject: `<Type> - #BKP-NoId - <Summary>`, Type ∈ Feature, Fix, Refactor, Docs, Security, Chore.
- All comments and docs in English (public repo).
- A change to config fields or env vars updates the README tables, `backups.yml.example` and `compose.yml` comments. The Unraid template lives in the separate `Greite/unraid-templates` repo.
- Release: push an annotated `vX.Y.Z` tag (triggers `docker-build.yml`), then `gh release create` with hand-written notes (New / Changes / Upgrading), never `--generate-notes`. Dependabot targets `main` only; the v1 branch is deprecated.

## Gotchas

- `run` starts as root, installs clients, then re-execs itself as `PUID:PGID` with `DBBACKUP_DROPPED=1`. Root-only work goes before `DropAndReexec`. `backup` (used via `docker exec`) also drops but skips the installer.
- On macOS `golangci-lint` reports `SA4023` on the two `DropAndReexec` calls: false positive from the `!linux` stub returning a constant error. `GOOS=linux` gives CI's result (CI uses `version: latest`, no `.golangci.yml`).
- Integration tests skip unless `/usr/lib/postgresql/18/bin/pg_dump` exists; CI runs them only on pull requests, not on push to `main`.
- `sqlite3 .dump` exits 0 even when it fails (it ends the output with `ROLLBACK; -- due to errors`); the sqlite dumper checks for the `COMMIT;` trailer instead of trusting the exit code.
- Credentials never go on argv: `PGPASSWORD` / `MYSQL_PWD` env vars, a 0600 `--config` file for mongodump. Keep it that way when adding client flags.
- Build is `CGO_ENABLED=0` with `-tags timetzdata`: no cgo dependencies, no reliance on system tzdata.
