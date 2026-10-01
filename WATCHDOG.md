# Review priorities - database-backup

Reviewer for a single static Go binary (`dbbackup`) that schedules PostgreSQL / MariaDB / MongoDB / SQLite dumps inside a `debian:trixie-slim` image. Contributor context is in `AGENTS.md`. Check the code before asserting; flag first what loses backups, leaks credentials or breaks the container. Pure style stays a `nit`.

## Blocker: silent backup loss

- **A failed dump reported as success.** Every stage of `dumper.Runner.writeBackup` (dump → gzip → optional encryption → file) must propagate its error: `zw.Close()`, the encryptor's `Close()` and `f.Sync()` included. An ignored close error leaves a truncated archive under a final name.
- **Trusting a tool's exit code when it lies.** `sqlite3 .dump` exits 0 on failure; the sqlite dumper checks for the `COMMIT;` trailer. A new engine or flag must state how failure is detected.
- **Final file written in place.** Output goes to an `O_EXCL` 0600 `.tmp` file, renamed only on success, and removed on error. Any change that writes the final name directly, widens permissions, or creates directories looser than 0700 is a blocker.
- **Rotation before success, or too broad.** `rotation.Purge` runs only after the rename and matches by `strings.Contains(name, suffix)` inside `<root>/<type>/<name>/`. Flag a change that purges on failure, purges another job's directory, or loosens the suffix match.
- **Encryption bypassed.** With `encryption` configured, no plaintext dump may touch disk (no intermediate file before `enc.Wrap`).

## Concern: security

- **Credentials on argv.** Passwords go through `PGPASSWORD` / `MYSQL_PWD` or a 0600 `--config` file for mongodump; never as a CLI flag, never in a log line or an error message (stderr is wrapped into errors by `runTool`).
- **Privilege drop.** `run` starts as root, installs clients, then re-execs as `PUID:PGID` (`DropAndReexec`, `DBBACKUP_DROPPED=1`). Root-only work placed after the drop, or dump work placed before it, is wrong. `backup` drops too but skips the installer.
- **Unverified downloads in `internal/installer`.** MongoDB tools are version- and SHA256-pinned; a version bump without a matching checksum, or a new download without one, is a blocker.
- **Secret files.** `password_file` / `passphrase_file` are resolved in `internal/config/secrets.go`; their content must not end up in `validate` or `list` output.
- **Personal data in a public repo.** No hostnames, container names, family names or homelab context in code, docs, comments or commit messages.

## Concern: recurring omissions

- Config field or env var added or renamed without updating the README tables, `backups.yml.example` and `compose.yml` comments (the Unraid template is in the separate `Greite/unraid-templates` repo: remind, don't block).
- New subcommand not registered in `cmd/dbbackup/commands.go` `init()`, or missing from the README subcommand table and the "unknown command" error.
- New dependency that needs cgo or system tzdata: the build is `CGO_ENABLED=0` with `-tags timetzdata`.
- Comments or docs not in English.
- "Fixing" the `SA4023` reported by `golangci-lint` on macOS on the `DropAndReexec` calls: it is a false positive from the `!linux` stub.

## Evidence expected before "done"

- `test -z "$(gofmt -l .)" && go vet ./... && go test ./...` (what CI runs on push).
- `GOOS=linux golangci-lint run ./...` for lint, not a bare macOS run.
- Integration tests skip silently without Docker and `/usr/lib/postgresql/18/bin/pg_dump`: a claimed pass of `go test -tags integration` must show `-v` output with no `SKIP`, otherwise treat it as unverified.
- Container behavior (privilege drop, installer, scheduler) needs a real `docker compose up -d --build` run, not only unit tests.
- Commit subject `<Type> - #BKP-NoId - <Summary>`, Type ∈ Feature, Fix, Refactor, Docs, Security, Chore.
- Release: annotated `vX.Y.Z` tag on the merged `main` commit, `docker-build.yml` run green, `gh release create` with hand-written notes containing New / Changes / Upgrading, never `--generate-notes`.
