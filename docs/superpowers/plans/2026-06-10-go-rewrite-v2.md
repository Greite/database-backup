# database-backup v2 Go Rewrite — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the bash/cron backup container with a single Go binary (`dbbackup`) providing scheduled PostgreSQL/MariaDB/MongoDB dumps, streaming compression/encryption, rotation, native healthcheck, v1 config migration, and a smaller non-root image.

**Architecture:** One multi-command binary (`run`, `healthcheck`, `backup`, `validate`, `migrate`). `run` parses YAML config, installs missing DB clients as root, re-execs as uid 1000, then schedules jobs in-process (`robfig/cron/v3`). Each job pipes the external dump tool through gzip and an optional encryptor into a temp file renamed on success. Spec: `docs/superpowers/specs/2026-06-10-go-rewrite-v2-design.md`.

**Tech Stack:** Go 1.24, `gopkg.in/yaml.v3`, `robfig/cron/v3`, `ProtonMail/go-crypto`, `filippo.io/age`, `jackc/pgx/v5`, `go-sql-driver/mysql`, `mongo-driver`, `testcontainers-go`, Debian trixie-slim.

**Module path:** `github.com/Greite/database-backup`

**Conventions for every task:** All comments in English (public repo). Commit messages follow `<Type> - #BKP-NoId - <description>`. Run `gofmt -l .` and `go vet ./...` before each commit; both must be clean.

---

## File Structure

```
cmd/dbbackup/main.go            # subcommand dispatch only
internal/config/config.go       # types, Load/Parse, defaults
internal/config/validate.go     # validation rules
internal/config/secrets.go      # password_file / encryption env fallback resolution
internal/migrate/migrate.go     # v1 pipe format → *config.Config → YAML
internal/rotation/rotation.go   # purge old backups
internal/archive/tar.go         # stream a directory as tar (MongoDB)
internal/crypto/crypto.go       # Encryptor interface
internal/crypto/openpgp.go      # gpg-compatible symmetric encryption
internal/crypto/age.go          # age passphrase + recipients
internal/dumper/dumper.go       # Dumper interface + New() factory
internal/dumper/postgres.go     # pg_dump
internal/dumper/mariadb.go      # mariadb-dump/mysqldump
internal/dumper/mongodb.go      # mongodump + tar streaming
internal/dumper/runner.go       # job pipeline: dump→gzip→encrypt→tmp→rename→rotate
internal/healthcheck/healthcheck.go  # native pings
internal/scheduler/scheduler.go # cron wrapper + graceful shutdown
internal/installer/installer.go # client detection + apt / tgz+sha256 install
internal/privileges/privileges.go    # root → uid 1000 re-exec
Dockerfile                      # multi-stage golang → debian:trixie-slim
backups.yml.example             # replaces backups.conf.example
docs/MIGRATION-V2.md            # v1 → v2 guide
.github/workflows/go-ci.yml     # fmt/vet/lint/unit + integration
```

Unit tests live next to each package (`*_test.go`). Integration tests in `internal/integration/` behind the `integration` build tag.

---

### Task 1: Go module bootstrap and subcommand skeleton

**Files:**
- Create: `go.mod` (via `go mod init`)
- Create: `cmd/dbbackup/main.go`
- Create: `cmd/dbbackup/main_test.go`

- [ ] **Step 1: Init the module**

Run: `go mod init github.com/Greite/database-backup && go mod edit -go=1.24`
Expected: `go.mod` created.

- [ ] **Step 2: Write the failing test**

```go
// cmd/dbbackup/main_test.go
package main

import "testing"

func TestDispatchUnknownCommand(t *testing.T) {
	code := dispatch([]string{"bogus"})
	if code != 2 {
		t.Fatalf("dispatch(bogus) = %d, want 2", code)
	}
}

func TestDispatchNoArgsDefaultsToRun(t *testing.T) {
	if got := commandName(nil); got != "run" {
		t.Fatalf("commandName(nil) = %q, want \"run\"", got)
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./cmd/...`
Expected: FAIL (`undefined: dispatch`)

- [ ] **Step 4: Write minimal implementation**

```go
// cmd/dbbackup/main.go
// Command dbbackup schedules and runs database backups.
package main

import (
	"fmt"
	"os"
)

// commands maps a subcommand name to its implementation. Each command
// receives its own args (without the command name) and returns an exit code.
var commands = map[string]func(args []string) int{}

func commandName(args []string) string {
	if len(args) == 0 {
		return "run"
	}
	return args[0]
}

func dispatch(args []string) int {
	name := commandName(args)
	cmd, ok := commands[name]
	if !ok {
		fmt.Fprintf(os.Stderr, "unknown command %q (expected run|healthcheck|backup|validate|migrate)\n", name)
		return 2
	}
	rest := args
	if len(rest) > 0 {
		rest = rest[1:]
	}
	return cmd(rest)
}

func main() {
	os.Exit(dispatch(os.Args[1:]))
}
```

- [ ] **Step 5: Run tests, gofmt, vet**

Run: `go test ./... && gofmt -l . && go vet ./...`
Expected: PASS, no gofmt output.

- [ ] **Step 6: Commit**

```bash
git add go.mod cmd/
git commit -m "Feature - #BKP-NoId - Bootstrap Go module and dbbackup command skeleton"
```

---

### Task 2: Config types, YAML parsing, defaults

**Files:**
- Create: `internal/config/config.go`
- Create: `internal/config/config_test.go`

- [ ] **Step 1: Write the failing tests**

```go
// internal/config/config_test.go
package config

import (
	"strings"
	"testing"
	"time"
)

const minimalYAML = `
jobs:
  - name: myapp
    type: postgres
    host: db
    database: myapp_db
    user: backup
    password: secret
    schedule: "0 2 * * *"
`

func TestParseAppliesDefaults(t *testing.T) {
	cfg, err := Parse(strings.NewReader(minimalYAML))
	if err != nil {
		t.Fatal(err)
	}
	j := cfg.Jobs[0]
	if j.Port != 5432 {
		t.Errorf("Port = %d, want 5432", j.Port)
	}
	if j.RetentionDays != 7 {
		t.Errorf("RetentionDays = %d, want 7", j.RetentionDays)
	}
	if j.PGVersion != 18 {
		t.Errorf("PGVersion = %d, want 18", j.PGVersion)
	}
	if cfg.ShutdownGrace != 5*time.Minute {
		t.Errorf("ShutdownGrace = %v, want 5m", cfg.ShutdownGrace)
	}
}

func TestParseDefaultsBlockOverridesBuiltins(t *testing.T) {
	y := `
defaults:
  retention_days: 30
  tls: true
jobs:
  - name: shop
    type: mariadb
    host: db
    database: shop
    user: u
    password: p
    schedule: "@daily"
`
	cfg, err := Parse(strings.NewReader(y))
	if err != nil {
		t.Fatal(err)
	}
	j := cfg.Jobs[0]
	if j.RetentionDays != 30 || !j.IsTLS() || j.Port != 3306 {
		t.Errorf("got retention=%d tls=%v port=%d, want 30 true 3306", j.RetentionDays, j.IsTLS(), j.Port)
	}
}

func TestParseJobOverridesDefaultsBlock(t *testing.T) {
	y := `
defaults:
  retention_days: 30
jobs:
  - name: m
    type: mongodb
    host: db
    database: m
    schedule: "@daily"
    retention_days: 3
`
	cfg, err := Parse(strings.NewReader(y))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Jobs[0].RetentionDays != 3 || cfg.Jobs[0].Port != 27017 {
		t.Errorf("got retention=%d port=%d, want 3 27017", cfg.Jobs[0].RetentionDays, cfg.Jobs[0].Port)
	}
}

func TestParseRejectsUnknownFields(t *testing.T) {
	if _, err := Parse(strings.NewReader("jobs:\n  - nme: typo\n")); err == nil {
		t.Fatal("want error on unknown field, got nil")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/config/`
Expected: FAIL (`undefined: Parse`)

- [ ] **Step 3: Implement types and Parse**

```go
// internal/config/config.go
// Package config loads and validates the v2 YAML configuration.
package config

import (
	"fmt"
	"io"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	DefaultRetentionDays = 7
	DefaultPGVersion     = 18
	DefaultShutdownGrace = 5 * time.Minute
)

type Config struct {
	Defaults      Defaults      `yaml:"defaults"`
	ShutdownGrace time.Duration `yaml:"shutdown_grace"`
	Encryption    *Encryption   `yaml:"encryption"`
	Jobs          []Job         `yaml:"jobs"`
}

type Defaults struct {
	RetentionDays *int  `yaml:"retention_days"`
	TLS           *bool `yaml:"tls"`
}

type Encryption struct {
	Method         string   `yaml:"method"` // "gpg" or "age"
	Passphrase     string   `yaml:"passphrase"`
	PassphraseFile string   `yaml:"passphrase_file"`
	Recipients     []string `yaml:"recipients"`
}

type Job struct {
	Name          string `yaml:"name"`
	Type          string `yaml:"type"` // postgres|mariadb|mysql|mongodb
	Host          string `yaml:"host"`
	Port          int    `yaml:"port"`
	Database      string `yaml:"database"`
	User          string `yaml:"user"`
	Password      string `yaml:"password"`
	PasswordFile  string `yaml:"password_file"`
	Schedule      string `yaml:"schedule"`
	RetentionDays int    `yaml:"retention_days"`
	PGVersion     int    `yaml:"pg_version"`
	TLS           *bool  `yaml:"tls"`
}

// IsTLS reports whether the job requires an encrypted connection.
func (j Job) IsTLS() bool { return j.TLS != nil && *j.TLS }

func defaultPort(dbType string) int {
	switch dbType {
	case "postgres":
		return 5432
	case "mariadb", "mysql":
		return 3306
	case "mongodb":
		return 27017
	}
	return 0
}

// Parse decodes the YAML, rejects unknown fields, and applies defaults.
// It does NOT validate; call Validate on the result.
func Parse(r io.Reader) (*Config, error) {
	dec := yaml.NewDecoder(r)
	dec.KnownFields(true)
	var cfg Config
	if err := dec.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}
	applyDefaults(&cfg)
	return &cfg, nil
}

func applyDefaults(cfg *Config) {
	if cfg.ShutdownGrace == 0 {
		cfg.ShutdownGrace = DefaultShutdownGrace
	}
	for i := range cfg.Jobs {
		j := &cfg.Jobs[i]
		if j.Port == 0 {
			j.Port = defaultPort(j.Type)
		}
		if j.RetentionDays == 0 {
			if cfg.Defaults.RetentionDays != nil {
				j.RetentionDays = *cfg.Defaults.RetentionDays
			} else {
				j.RetentionDays = DefaultRetentionDays
			}
		}
		if j.TLS == nil {
			j.TLS = cfg.Defaults.TLS
		}
		if j.Type == "postgres" && j.PGVersion == 0 {
			j.PGVersion = DefaultPGVersion
		}
	}
}

// Load reads and parses the config file at path.
func Load(path string) (*Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return Parse(f)
}
```

Run: `go get gopkg.in/yaml.v3`

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/config/ && gofmt -l . && go vet ./...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add go.mod go.sum internal/config/
git commit -m "Feature - #BKP-NoId - Add v2 YAML config types, parsing and defaults"
```

---

### Task 3: Config validation

**Files:**
- Create: `internal/config/validate.go`
- Create: `internal/config/validate_test.go`

- [ ] **Step 1: Write the failing tests**

```go
// internal/config/validate_test.go
package config

import (
	"strings"
	"testing"
)

func validJob() Job {
	return Job{Name: "app", Type: "postgres", Host: "db", Port: 5432,
		Database: "app", User: "u", Password: "p", Schedule: "0 2 * * *",
		RetentionDays: 7, PGVersion: 18}
}

func TestValidateAcceptsValidConfig(t *testing.T) {
	cfg := &Config{Jobs: []Job{validJob()}}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() = %v, want nil", err)
	}
}

func TestValidateRejectsBadInput(t *testing.T) {
	cases := []struct {
		desc   string
		mutate func(*Config)
		want   string // substring expected in the error
	}{
		{"missing name", func(c *Config) { c.Jobs[0].Name = "" }, "name"},
		{"path traversal name", func(c *Config) { c.Jobs[0].Name = ".." }, "name"},
		{"name with slash", func(c *Config) { c.Jobs[0].Name = "a/b" }, "name"},
		{"unknown type", func(c *Config) { c.Jobs[0].Type = "oracle" }, "type"},
		{"missing host", func(c *Config) { c.Jobs[0].Host = "" }, "host"},
		{"missing database", func(c *Config) { c.Jobs[0].Database = "" }, "database"},
		{"missing user non-mongo", func(c *Config) { c.Jobs[0].User = "" }, "user"},
		{"bad schedule", func(c *Config) { c.Jobs[0].Schedule = "every day" }, "schedule"},
		{"password and password_file", func(c *Config) { c.Jobs[0].PasswordFile = "/run/s" }, "password"},
		{"duplicate names", func(c *Config) { c.Jobs = append(c.Jobs, validJob()) }, "duplicate"},
		{"bad encryption method", func(c *Config) {
			c.Encryption = &Encryption{Method: "rot13", Passphrase: "x"}
		}, "method"},
		{"recipients with gpg", func(c *Config) {
			c.Encryption = &Encryption{Method: "gpg", Recipients: []string{"age1x"}}
		}, "recipients"},
		{"encryption without secret", func(c *Config) {
			c.Encryption = &Encryption{Method: "gpg"}
		}, "passphrase"},
		{"passphrase and file", func(c *Config) {
			c.Encryption = &Encryption{Method: "gpg", Passphrase: "a", PassphraseFile: "/f"}
		}, "passphrase"},
	}
	for _, tc := range cases {
		cfg := &Config{Jobs: []Job{validJob()}}
		tc.mutate(cfg)
		err := cfg.Validate()
		if err == nil {
			t.Errorf("%s: want error, got nil", tc.desc)
			continue
		}
		if !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: error %q does not mention %q", tc.desc, err, tc.want)
		}
	}
}

func TestValidateMongoWithoutAuthIsOK(t *testing.T) {
	j := validJob()
	j.Type, j.Port, j.User, j.Password = "mongodb", 27017, "", ""
	cfg := &Config{Jobs: []Job{j}}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() = %v, want nil", err)
	}
}

func TestValidateRequiresAtLeastOneJob(t *testing.T) {
	if err := (&Config{}).Validate(); err == nil {
		t.Fatal("want error for empty jobs, got nil")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/config/`
Expected: FAIL (`undefined: (*Config).Validate`)

- [ ] **Step 3: Implement Validate**

```go
// internal/config/validate.go
package config

import (
	"fmt"
	"regexp"

	"github.com/robfig/cron/v3"
)

// nameRe restricts values used as path components under /backups.
var nameRe = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

func validName(s string) bool {
	return nameRe.MatchString(s) && s != "." && s != ".."
}

// Validate checks the whole configuration. Errors include the job name
// (or index) and field so users can fix the file directly.
func (c *Config) Validate() error {
	if len(c.Jobs) == 0 {
		return fmt.Errorf("config: at least one job is required")
	}
	if err := c.Encryption.validate(); err != nil {
		return err
	}
	seen := map[string]bool{}
	for i, j := range c.Jobs {
		if err := j.validate(); err != nil {
			return fmt.Errorf("job %d (%q): %w", i+1, j.Name, err)
		}
		if seen[j.Name] {
			return fmt.Errorf("job %d: duplicate name %q", i+1, j.Name)
		}
		seen[j.Name] = true
	}
	return nil
}

func (j Job) validate() error {
	if !validName(j.Name) {
		return fmt.Errorf("name must match [A-Za-z0-9._-]+ and not be '.' or '..'")
	}
	switch j.Type {
	case "postgres", "mariadb", "mysql", "mongodb":
	default:
		return fmt.Errorf("type %q is not one of postgres|mariadb|mysql|mongodb", j.Type)
	}
	if j.Host == "" {
		return fmt.Errorf("host is required")
	}
	if j.Database == "" {
		return fmt.Errorf("database is required")
	}
	if j.Type != "mongodb" && j.User == "" {
		return fmt.Errorf("user is required for %s", j.Type)
	}
	if j.Password != "" && j.PasswordFile != "" {
		return fmt.Errorf("password and password_file are mutually exclusive")
	}
	if _, err := cron.ParseStandard(j.Schedule); err != nil {
		return fmt.Errorf("schedule %q: %w", j.Schedule, err)
	}
	if j.RetentionDays < 0 {
		return fmt.Errorf("retention_days must be >= 0")
	}
	return nil
}

func (e *Encryption) validate() error {
	if e == nil {
		return nil
	}
	switch e.Method {
	case "gpg":
		if len(e.Recipients) > 0 {
			return fmt.Errorf("encryption: recipients is only valid with method \"age\"")
		}
	case "age":
	default:
		return fmt.Errorf("encryption: method %q is not \"gpg\" or \"age\"", e.Method)
	}
	set := 0
	if e.Passphrase != "" {
		set++
	}
	if e.PassphraseFile != "" {
		set++
	}
	if len(e.Recipients) > 0 {
		set++
	}
	if set == 0 {
		return fmt.Errorf("encryption: one of passphrase, passphrase_file or recipients is required")
	}
	if set > 1 {
		return fmt.Errorf("encryption: passphrase, passphrase_file and recipients are mutually exclusive")
	}
	return nil
}
```

Run: `go get github.com/robfig/cron/v3`

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/config/ && gofmt -l . && go vet ./...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add go.mod go.sum internal/config/
git commit -m "Feature - #BKP-NoId - Add config validation rules"
```

---

### Task 4: Secret resolution (password_file, encryption env fallback)

**Files:**
- Create: `internal/config/secrets.go`
- Create: `internal/config/secrets_test.go`

- [ ] **Step 1: Write the failing tests**

```go
// internal/config/secrets_test.go
package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveSecretsReadsPasswordFile(t *testing.T) {
	dir := t.TempDir()
	pf := filepath.Join(dir, "pass")
	if err := os.WriteFile(pf, []byte("s3cret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := &Config{Jobs: []Job{{Name: "a", PasswordFile: pf}}}
	if err := cfg.ResolveSecrets(); err != nil {
		t.Fatal(err)
	}
	if cfg.Jobs[0].Password != "s3cret" {
		t.Errorf("Password = %q, want trailing newline trimmed \"s3cret\"", cfg.Jobs[0].Password)
	}
}

func TestResolveSecretsMissingFileFails(t *testing.T) {
	cfg := &Config{Jobs: []Job{{Name: "a", PasswordFile: "/nope"}}}
	if err := cfg.ResolveSecrets(); err == nil {
		t.Fatal("want error for missing password_file, got nil")
	}
}

func TestEncryptionEnvFallback(t *testing.T) {
	t.Setenv("BACKUP_ENCRYPTION_PASSPHRASE", "envpass")
	cfg := &Config{Jobs: []Job{{Name: "a"}}}
	if err := cfg.ResolveSecrets(); err != nil {
		t.Fatal(err)
	}
	if cfg.Encryption == nil || cfg.Encryption.Method != "gpg" || cfg.Encryption.Passphrase != "envpass" {
		t.Errorf("Encryption = %+v, want gpg method with env passphrase", cfg.Encryption)
	}
}

func TestEncryptionYAMLBlockTakesPrecedenceOverEnv(t *testing.T) {
	t.Setenv("BACKUP_ENCRYPTION_PASSPHRASE", "envpass")
	cfg := &Config{
		Encryption: &Encryption{Method: "age", Passphrase: "yamlpass"},
		Jobs:       []Job{{Name: "a"}},
	}
	if err := cfg.ResolveSecrets(); err != nil {
		t.Fatal(err)
	}
	if cfg.Encryption.Method != "age" || cfg.Encryption.Passphrase != "yamlpass" {
		t.Errorf("Encryption = %+v, want YAML block untouched", cfg.Encryption)
	}
}

func TestEncryptionPassphraseFileResolved(t *testing.T) {
	dir := t.TempDir()
	pf := filepath.Join(dir, "ep")
	if err := os.WriteFile(pf, []byte("kk\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := &Config{
		Encryption: &Encryption{Method: "gpg", PassphraseFile: pf},
		Jobs:       []Job{{Name: "a"}},
	}
	if err := cfg.ResolveSecrets(); err != nil {
		t.Fatal(err)
	}
	if cfg.Encryption.Passphrase != "kk" {
		t.Errorf("Passphrase = %q, want \"kk\"", cfg.Encryption.Passphrase)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/config/`
Expected: FAIL (`undefined: (*Config).ResolveSecrets`)

- [ ] **Step 3: Implement ResolveSecrets**

```go
// internal/config/secrets.go
package config

import (
	"fmt"
	"os"
	"strings"
)

func readSecretFile(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("reading secret file: %w", err)
	}
	return strings.TrimRight(string(b), "\r\n"), nil
}

// ResolveSecrets loads password_file contents into Password fields and
// applies the v1 encryption environment variable fallback. Call after
// Validate, before using the config.
func (c *Config) ResolveSecrets() error {
	for i := range c.Jobs {
		j := &c.Jobs[i]
		if j.PasswordFile == "" {
			continue
		}
		p, err := readSecretFile(j.PasswordFile)
		if err != nil {
			return fmt.Errorf("job %q password_file: %w", j.Name, err)
		}
		j.Password = p
	}
	if c.Encryption == nil {
		// v1 parity: env vars enable gpg encryption when no YAML block exists.
		if f := os.Getenv("BACKUP_ENCRYPTION_PASSPHRASE_FILE"); f != "" {
			c.Encryption = &Encryption{Method: "gpg", PassphraseFile: f}
		} else if p := os.Getenv("BACKUP_ENCRYPTION_PASSPHRASE"); p != "" {
			c.Encryption = &Encryption{Method: "gpg", Passphrase: p}
		}
	}
	if c.Encryption != nil && c.Encryption.PassphraseFile != "" {
		p, err := readSecretFile(c.Encryption.PassphraseFile)
		if err != nil {
			return fmt.Errorf("encryption passphrase_file: %w", err)
		}
		c.Encryption.Passphrase = p
	}
	return nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/config/ && gofmt -l . && go vet ./...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/config/
git commit -m "Feature - #BKP-NoId - Resolve password_file secrets and v1 encryption env fallback"
```

---

### Task 5: v1 → v2 config migration

**Files:**
- Create: `internal/migrate/migrate.go`
- Create: `internal/migrate/migrate_test.go`

v1 line format: `CRON_SCHEDULE|TYPE|HOST|PORT|DATABASE|USER|PASSWORD|RETENTION_DAYS|PG_VERSION|TLS` (trailing fields optional, `#` comments and blank lines ignored).

- [ ] **Step 1: Write the failing tests**

```go
// internal/migrate/migrate_test.go
package migrate

import (
	"strings"
	"testing"
)

const v1Sample = `# comment kept out
0 2 * * *|postgres|pg-host|5432|appdb|backup|S3cret!|14|17|true

0 3 * * *|mariadb|maria||shop|wp|pw|7
0 6 * * *|mongodb|mongo|27017|events|||7
`

func TestConvertProducesJobs(t *testing.T) {
	cfg, errs := Convert(strings.NewReader(v1Sample))
	if len(errs) != 0 {
		t.Fatalf("errs = %v, want none", errs)
	}
	if len(cfg.Jobs) != 3 {
		t.Fatalf("got %d jobs, want 3", len(cfg.Jobs))
	}
	j := cfg.Jobs[0]
	if j.Name != "appdb" || j.Type != "postgres" || j.Schedule != "0 2 * * *" ||
		j.Port != 5432 || j.User != "backup" || j.Password != "S3cret!" ||
		j.RetentionDays != 14 || j.PGVersion != 17 || !j.IsTLS() {
		t.Errorf("unexpected first job: %+v", j)
	}
	if cfg.Jobs[1].Port != 0 {
		t.Errorf("empty v1 PORT should stay 0 (filled by defaults), got %d", cfg.Jobs[1].Port)
	}
}

func TestConvertDeduplicatesNames(t *testing.T) {
	in := "0 4 * * *|postgres|h|5432|db1|u|p|7\n0 5 * * *|postgres|h2|5432|db1|u|p|7\n"
	cfg, errs := Convert(strings.NewReader(in))
	if len(errs) != 0 {
		t.Fatalf("errs = %v", errs)
	}
	if cfg.Jobs[0].Name != "db1" || cfg.Jobs[1].Name != "db1-2" {
		t.Errorf("names = %q, %q; want db1, db1-2", cfg.Jobs[0].Name, cfg.Jobs[1].Name)
	}
}

func TestConvertReportsInvalidLinesWithNumbers(t *testing.T) {
	in := "0 2 * * *|postgres|h|5432|db|u|p|7\nnot-a-valid-line\n"
	cfg, errs := Convert(strings.NewReader(in))
	if len(cfg.Jobs) != 1 {
		t.Errorf("got %d jobs, want 1 (valid line still converted)", len(cfg.Jobs))
	}
	if len(errs) != 1 || !strings.Contains(errs[0].Error(), "line 2") {
		t.Errorf("errs = %v, want one error mentioning line 2", errs)
	}
}

func TestToYAMLRoundTrips(t *testing.T) {
	cfg, _ := Convert(strings.NewReader(v1Sample))
	out, err := ToYAML(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "name: appdb") || !strings.Contains(string(out), "schedule: 0 2 * * *") {
		t.Errorf("YAML output missing expected fields:\n%s", out)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/migrate/`
Expected: FAIL (`undefined: Convert`)

- [ ] **Step 3: Implement Convert and ToYAML**

```go
// internal/migrate/migrate.go
// Package migrate converts a v1 pipe-delimited backups.conf into the
// v2 YAML configuration.
package migrate

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/Greite/database-backup/internal/config"
	"gopkg.in/yaml.v3"
)

// Convert parses a v1 config. Invalid lines are returned as errors
// (with their line number) and skipped; valid lines are converted.
func Convert(r io.Reader) (*config.Config, []error) {
	cfg := &config.Config{}
	var errs []error
	used := map[string]int{}
	sc := bufio.NewScanner(r)
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		j, err := parseLine(line)
		if err != nil {
			errs = append(errs, fmt.Errorf("line %d: %w", n, err))
			continue
		}
		j.Name = uniqueName(j.Database, used)
		cfg.Jobs = append(cfg.Jobs, j)
	}
	if err := sc.Err(); err != nil {
		errs = append(errs, err)
	}
	return cfg, errs
}

func uniqueName(base string, used map[string]int) string {
	used[base]++
	if used[base] == 1 {
		return base
	}
	return fmt.Sprintf("%s-%d", base, used[base])
}

func parseLine(line string) (config.Job, error) {
	f := strings.Split(line, "|")
	// Pad optional trailing fields up to the 10 v1 columns.
	for len(f) < 10 {
		f = append(f, "")
	}
	if len(f) > 10 {
		return config.Job{}, fmt.Errorf("too many fields (%d, max 10)", len(f))
	}
	j := config.Job{
		Schedule: strings.TrimSpace(f[0]),
		Type:     strings.TrimSpace(f[1]),
		Host:     strings.TrimSpace(f[2]),
		Database: strings.TrimSpace(f[4]),
		User:     strings.TrimSpace(f[5]),
		Password: f[6],
	}
	switch j.Type {
	case "postgres", "mariadb", "mysql", "mongodb":
	default:
		return config.Job{}, fmt.Errorf("unknown type %q", j.Type)
	}
	if j.Schedule == "" || j.Host == "" || j.Database == "" {
		return config.Job{}, fmt.Errorf("missing required field (schedule, host or database)")
	}
	var err error
	if j.Port, err = optInt(f[3], "port"); err != nil {
		return config.Job{}, err
	}
	if j.RetentionDays, err = optInt(f[7], "retention_days"); err != nil {
		return config.Job{}, err
	}
	if j.PGVersion, err = optInt(f[8], "pg_version"); err != nil {
		return config.Job{}, err
	}
	if tls := strings.TrimSpace(f[9]); tls == "true" {
		v := true
		j.TLS = &v
	} else if tls != "" {
		return config.Job{}, fmt.Errorf("TLS must be empty or \"true\", got %q", tls)
	}
	return j, nil
}

func optInt(s, field string) (int, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, nil
	}
	v, err := strconv.Atoi(s)
	if err != nil || v < 0 {
		return 0, fmt.Errorf("%s: %q is not a positive integer", field, s)
	}
	return v, nil
}

// ToYAML renders the converted config as v2 YAML.
func ToYAML(cfg *config.Config) ([]byte, error) {
	return yaml.Marshal(cfg)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/migrate/ && gofmt -l . && go vet ./...`
Expected: PASS

Also in this step: add `,omitempty` to the yaml tags of `Port`, `User`, `Password`, `PasswordFile`, `Schedule`, `RetentionDays`, `PGVersion`, `TLS`, `Encryption`, `ShutdownGrace` and `Defaults` in `internal/config/config.go` so the marshalled YAML omits zero values (`omitempty` does not affect decoding, existing config tests stay green). Example: `Port int \`yaml:"port,omitempty"\``.

- [ ] **Step 5: Commit**

```bash
git add internal/migrate/ internal/config/
git commit -m "Feature - #BKP-NoId - Add v1 to v2 config migration converter"
```

---

### Task 6: Rotation

**Files:**
- Create: `internal/rotation/rotation.go`
- Create: `internal/rotation/rotation_test.go`

- [ ] **Step 1: Write the failing tests**

```go
// internal/rotation/rotation_test.go
package rotation

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func touch(t *testing.T, dir, name string, age time.Duration, now time.Time) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	mt := now.Add(-age)
	if err := os.Chtimes(p, mt, mt); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestPurgeDeletesOnlyExpiredMatchingFiles(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	old := touch(t, dir, "app_20260101_020000.sql.gz", 10*24*time.Hour, now)
	oldEnc := touch(t, dir, "app_20260102_020000.sql.gz.age", 9*24*time.Hour, now)
	fresh := touch(t, dir, "app_20260609_020000.sql.gz", 24*time.Hour, now)
	other := touch(t, dir, "notes.txt", 30*24*time.Hour, now)

	deleted, remaining, err := Purge(dir, ".sql.gz", 7, now)
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 2 || remaining != 1 {
		t.Errorf("deleted=%d remaining=%d, want 2 and 1", deleted, remaining)
	}
	for _, p := range []string{old, oldEnc} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("%s should be deleted", p)
		}
	}
	for _, p := range []string{fresh, other} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("%s should still exist", p)
		}
	}
}

func TestPurgeZeroRetentionIsNoop(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	touch(t, dir, "a_x.sql.gz", 100*24*time.Hour, now)
	deleted, _, err := Purge(dir, ".sql.gz", 0, now)
	if err != nil || deleted != 0 {
		t.Errorf("deleted=%d err=%v, want 0 and nil", deleted, err)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/rotation/`
Expected: FAIL (`undefined: Purge`)

- [ ] **Step 3: Implement Purge**

```go
// internal/rotation/rotation.go
// Package rotation deletes backups older than the retention window.
package rotation

import (
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Purge deletes files in dir whose name contains suffix (this also
// matches encrypted variants like suffix+".gpg"/".age") and whose
// mtime is older than retentionDays before now. retentionDays == 0
// disables rotation. It returns deleted and remaining match counts.
func Purge(dir, suffix string, retentionDays int, now time.Time) (deleted, remaining int, err error) {
	if retentionDays <= 0 {
		return 0, 0, nil
	}
	cutoff := now.AddDate(0, 0, -retentionDays)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, 0, err
	}
	for _, e := range entries {
		if e.IsDir() || !strings.Contains(e.Name(), suffix) {
			continue
		}
		info, err := e.Info()
		if err != nil {
			return deleted, remaining, err
		}
		if info.ModTime().Before(cutoff) {
			if err := os.Remove(filepath.Join(dir, e.Name())); err != nil {
				return deleted, remaining, err
			}
			deleted++
			continue
		}
		remaining++
	}
	return deleted, remaining, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/rotation/ && gofmt -l . && go vet ./...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/rotation/
git commit -m "Feature - #BKP-NoId - Add backup rotation with retention window"
```

---

### Task 7: Archive — stream a directory as tar (MongoDB dumps)

**Files:**
- Create: `internal/archive/tar.go`
- Create: `internal/archive/tar_test.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/archive/tar_test.go
package archive

import (
	"archive/tar"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

func TestTarDirStreamsAllFiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "events"), 0o700); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"events/data.bson.gz":     "bson-data",
		"events/metadata.json.gz": "meta",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	var buf bytes.Buffer
	if err := TarDir(dir, &buf); err != nil {
		t.Fatal(err)
	}

	tr := tar.NewReader(&buf)
	var got []string
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		b, _ := io.ReadAll(tr)
		if string(b) != files[hdr.Name] {
			t.Errorf("%s content = %q, want %q", hdr.Name, b, files[hdr.Name])
		}
		got = append(got, hdr.Name)
	}
	sort.Strings(got)
	want := []string{"events/data.bson.gz", "events/metadata.json.gz"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("entries = %v, want %v", got, want)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/archive/`
Expected: FAIL (`undefined: TarDir`)

- [ ] **Step 3: Implement TarDir**

```go
// internal/archive/tar.go
// Package archive streams directory trees as tar archives.
package archive

import (
	"archive/tar"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

// TarDir writes the contents of dir (paths relative to dir) as an
// uncompressed tar stream to w. Compression is the caller's concern.
func TarDir(dir string, w io.Writer) error {
	tw := tar.NewWriter(w)
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		hdr, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		hdr.Name = filepath.ToSlash(rel)
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		_, err = io.Copy(tw, f)
		return err
	})
	if err != nil {
		return err
	}
	return tw.Close()
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/archive/ && gofmt -l . && go vet ./...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/archive/
git commit -m "Feature - #BKP-NoId - Add tar directory streaming for MongoDB dumps"
```

---

### Task 8: Crypto — Encryptor interface and OpenPGP implementation

**Files:**
- Create: `internal/crypto/crypto.go`
- Create: `internal/crypto/openpgp.go`
- Create: `internal/crypto/openpgp_test.go`

- [ ] **Step 1: Write the failing tests**

```go
// internal/crypto/openpgp_test.go
package crypto

import (
	"bytes"
	"io"
	"testing"

	"github.com/ProtonMail/go-crypto/openpgp"
)

func TestOpenPGPRoundTrip(t *testing.T) {
	enc := NewOpenPGP("hunter2")
	if enc.Ext() != ".gpg" {
		t.Fatalf("Ext() = %q, want .gpg", enc.Ext())
	}

	var out bytes.Buffer
	w, err := enc.Wrap(&out)
	if err != nil {
		t.Fatal(err)
	}
	plaintext := []byte("CREATE TABLE t (id int);")
	if _, err := w.Write(plaintext); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	// Decrypt with the same library to prove gpg compatibility of the format.
	md, err := openpgp.ReadMessage(&out, nil,
		func(keys []openpgp.Key, symmetric bool) ([]byte, error) {
			return []byte("hunter2"), nil
		}, nil)
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(md.UnverifiedBody)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, plaintext) {
		t.Errorf("decrypted = %q, want %q", got, plaintext)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/crypto/`
Expected: FAIL (`undefined: NewOpenPGP`)

- [ ] **Step 3: Implement the interface and OpenPGP encryptor**

```go
// internal/crypto/crypto.go
// Package crypto provides streaming at-rest encryption for backups.
package crypto

import "io"

// Encryptor wraps an output stream with encryption.
type Encryptor interface {
	// Wrap returns a WriteCloser encrypting everything written to it
	// into out. Close finalizes the stream (it does not close out).
	Wrap(out io.Writer) (io.WriteCloser, error)
	// Ext is the filename extension appended to encrypted backups.
	Ext() string
}
```

```go
// internal/crypto/openpgp.go
package crypto

import (
	"io"

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/packet"
)

// openpgpEncryptor produces symmetric OpenPGP (AES-256) output that
// standard `gpg -d` can decrypt, matching v1's gpg invocation.
type openpgpEncryptor struct {
	passphrase []byte
}

// NewOpenPGP returns a gpg-compatible symmetric Encryptor.
func NewOpenPGP(passphrase string) Encryptor {
	return openpgpEncryptor{passphrase: []byte(passphrase)}
}

func (openpgpEncryptor) Ext() string { return ".gpg" }

func (e openpgpEncryptor) Wrap(out io.Writer) (io.WriteCloser, error) {
	cfg := &packet.Config{DefaultCipher: packet.CipherAES256}
	return openpgp.SymmetricallyEncrypt(out, e.passphrase, nil, cfg)
}
```

Run: `go get github.com/ProtonMail/go-crypto`

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/crypto/ && gofmt -l . && go vet ./...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add go.mod go.sum internal/crypto/
git commit -m "Feature - #BKP-NoId - Add Encryptor interface with gpg-compatible OpenPGP implementation"
```

---

### Task 9: Crypto — age implementation (passphrase and recipients)

**Files:**
- Create: `internal/crypto/age.go`
- Create: `internal/crypto/age_test.go`
- Modify: `internal/crypto/crypto.go` (add the `New` factory)

- [ ] **Step 1: Write the failing tests**

```go
// internal/crypto/age_test.go
package crypto

import (
	"bytes"
	"io"
	"testing"

	"filippo.io/age"

	"github.com/Greite/database-backup/internal/config"
)

func roundTrip(t *testing.T, enc Encryptor, identity age.Identity) {
	t.Helper()
	var out bytes.Buffer
	w, err := enc.Wrap(&out)
	if err != nil {
		t.Fatal(err)
	}
	plaintext := []byte("dump-bytes")
	if _, err := w.Write(plaintext); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	r, err := age.Decrypt(&out, identity)
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, plaintext) {
		t.Errorf("decrypted = %q, want %q", got, plaintext)
	}
}

func TestAgePassphraseRoundTrip(t *testing.T) {
	enc, err := NewAgePassphrase("hunter2")
	if err != nil {
		t.Fatal(err)
	}
	if enc.Ext() != ".age" {
		t.Fatalf("Ext() = %q, want .age", enc.Ext())
	}
	id, err := age.NewScryptIdentity("hunter2")
	if err != nil {
		t.Fatal(err)
	}
	roundTrip(t, enc, id)
}

func TestAgeRecipientsRoundTrip(t *testing.T) {
	id, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatal(err)
	}
	enc, err := NewAgeRecipients([]string{id.Recipient().String()})
	if err != nil {
		t.Fatal(err)
	}
	roundTrip(t, enc, id)
}

func TestNewFactorySelectsImplementation(t *testing.T) {
	cases := []struct {
		enc     *config.Encryption
		wantExt string
	}{
		{nil, ""},
		{&config.Encryption{Method: "gpg", Passphrase: "x"}, ".gpg"},
		{&config.Encryption{Method: "age", Passphrase: "x"}, ".age"},
	}
	for _, tc := range cases {
		e, err := New(tc.enc)
		if err != nil {
			t.Fatal(err)
		}
		if tc.wantExt == "" {
			if e != nil {
				t.Errorf("New(nil) = %v, want nil", e)
			}
			continue
		}
		if e.Ext() != tc.wantExt {
			t.Errorf("Ext() = %q, want %q", e.Ext(), tc.wantExt)
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/crypto/`
Expected: FAIL (`undefined: NewAgePassphrase`)

- [ ] **Step 3: Implement age encryptors and the factory**

```go
// internal/crypto/age.go
package crypto

import (
	"fmt"
	"io"

	"filippo.io/age"
)

type ageEncryptor struct {
	recipients []age.Recipient
}

// NewAgePassphrase returns an age Encryptor using scrypt passphrase mode.
func NewAgePassphrase(passphrase string) (Encryptor, error) {
	r, err := age.NewScryptRecipient(passphrase)
	if err != nil {
		return nil, err
	}
	return ageEncryptor{recipients: []age.Recipient{r}}, nil
}

// NewAgeRecipients returns an age Encryptor for X25519 public keys.
// The container never holds the private key in this mode.
func NewAgeRecipients(keys []string) (Encryptor, error) {
	var rs []age.Recipient
	for _, k := range keys {
		r, err := age.ParseX25519Recipient(k)
		if err != nil {
			return nil, fmt.Errorf("age recipient %q: %w", k, err)
		}
		rs = append(rs, r)
	}
	return ageEncryptor{recipients: rs}, nil
}

func (ageEncryptor) Ext() string { return ".age" }

func (e ageEncryptor) Wrap(out io.Writer) (io.WriteCloser, error) {
	return age.Encrypt(out, e.recipients...)
}
```

Append to `internal/crypto/crypto.go`:

```go
// New builds the Encryptor matching the config block, or nil when
// encryption is disabled. The config must already be validated and
// have secrets resolved (PassphraseFile loaded into Passphrase).
func New(enc *config.Encryption) (Encryptor, error) {
	if enc == nil {
		return nil, nil
	}
	switch enc.Method {
	case "gpg":
		return NewOpenPGP(enc.Passphrase), nil
	case "age":
		if len(enc.Recipients) > 0 {
			return NewAgeRecipients(enc.Recipients)
		}
		return NewAgePassphrase(enc.Passphrase)
	}
	return nil, fmt.Errorf("unknown encryption method %q", enc.Method)
}
```

with imports updated to:

```go
import (
	"fmt"
	"io"

	"github.com/Greite/database-backup/internal/config"
)
```

Run: `go get filippo.io/age`

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/crypto/ && gofmt -l . && go vet ./...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add go.mod go.sum internal/crypto/
git commit -m "Feature - #BKP-NoId - Add age encryption (passphrase and recipients) and encryptor factory"
```

---

### Task 10: Dumpers — interface, PostgreSQL and MariaDB

**Files:**
- Create: `internal/dumper/dumper.go`
- Create: `internal/dumper/postgres.go`
- Create: `internal/dumper/mariadb.go`
- Create: `internal/dumper/dumper_test.go`

The exec-based Dump methods are exercised end-to-end by the integration tests (Task 18); unit tests here cover the security-sensitive parts: command line and environment construction (no secret in argv).

- [ ] **Step 1: Write the failing tests**

```go
// internal/dumper/dumper_test.go
package dumper

import (
	"strings"
	"testing"

	"github.com/Greite/database-backup/internal/config"
)

func pgJob() config.Job {
	tls := true
	return config.Job{Name: "app", Type: "postgres", Host: "db", Port: 5433,
		Database: "appdb", User: "u", Password: "s3cret", PGVersion: 17, TLS: &tls}
}

func TestPostgresCommand(t *testing.T) {
	d := newPostgres(pgJob())
	if d.path() != "/usr/lib/postgresql/17/bin/pg_dump" {
		t.Errorf("path = %q", d.path())
	}
	args := strings.Join(d.args(), " ")
	want := "-h db -p 5433 -U u -d appdb"
	if args != want {
		t.Errorf("args = %q, want %q", args, want)
	}
	env := strings.Join(d.env(), "\n")
	if !strings.Contains(env, "PGPASSWORD=s3cret") || !strings.Contains(env, "PGSSLMODE=require") {
		t.Errorf("env missing PGPASSWORD/PGSSLMODE: %q", env)
	}
	for _, a := range d.args() {
		if strings.Contains(a, "s3cret") {
			t.Errorf("password leaked into argv: %q", a)
		}
	}
}

func TestMariaDBCommand(t *testing.T) {
	tls := true
	j := config.Job{Name: "w", Type: "mariadb", Host: "m", Port: 3307,
		Database: "wp", User: "u", Password: "pw", TLS: &tls}
	d := newMariaDB(j)
	args := strings.Join(d.args(), " ")
	want := "-h m -P 3307 -u u --ssl wp"
	if args != want {
		t.Errorf("args = %q, want %q", args, want)
	}
	if got := strings.Join(d.env(), "\n"); !strings.Contains(got, "MYSQL_PWD=pw") {
		t.Errorf("env missing MYSQL_PWD: %q", got)
	}
}

func TestNewSelectsImplementation(t *testing.T) {
	for _, typ := range []string{"postgres", "mariadb", "mysql", "mongodb"} {
		j := config.Job{Type: typ, PGVersion: 18}
		if _, err := New(j); err != nil {
			t.Errorf("New(%s) error: %v", typ, err)
		}
	}
	if _, err := New(config.Job{Type: "oracle"}); err == nil {
		t.Error("New(oracle) should fail")
	}
}

func TestExtByType(t *testing.T) {
	pg, _ := New(config.Job{Type: "postgres", PGVersion: 18})
	mg, _ := New(config.Job{Type: "mongodb"})
	if pg.Ext() != ".sql.gz" || mg.Ext() != ".tar.gz" {
		t.Errorf("Ext: pg=%q mongo=%q, want .sql.gz / .tar.gz", pg.Ext(), mg.Ext())
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/dumper/`
Expected: FAIL (`undefined: newPostgres`)

- [ ] **Step 3: Implement the interface and both SQL dumpers**

```go
// internal/dumper/dumper.go
// Package dumper runs the external database dump tools and streams
// their raw output.
package dumper

import (
	"context"
	"fmt"
	"io"

	"github.com/Greite/database-backup/internal/config"
)

// Dumper streams a raw (uncompressed) dump of one job to w.
type Dumper interface {
	Dump(ctx context.Context, w io.Writer) error
	// Ext is the uncompressed-pipeline filename extension
	// (".sql.gz" or ".tar.gz"), before any encryption extension.
	Ext() string
}

// New returns the Dumper for the job's database type.
func New(job config.Job) (Dumper, error) {
	switch job.Type {
	case "postgres":
		return newPostgres(job), nil
	case "mariadb", "mysql":
		return newMariaDB(job), nil
	case "mongodb":
		return newMongoDB(job), nil
	}
	return nil, fmt.Errorf("unknown database type %q", job.Type)
}

// runTool execs the tool, streaming stdout to w. Stderr is captured
// and included in the returned error so failures are diagnosable.
func runTool(ctx context.Context, w io.Writer, path string, args, env []string) error {
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Stdout = w
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	cmd.Env = append(os.Environ(), env...)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s: %w (stderr: %s)", filepath.Base(path), err, strings.TrimSpace(stderr.String()))
	}
	return nil
}
```

(with `bytes`, `os`, `os/exec`, `path/filepath`, `strings` added to the import block)

```go
// internal/dumper/postgres.go
package dumper

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/Greite/database-backup/internal/config"
)

type postgres struct{ job config.Job }

func newPostgres(j config.Job) postgres { return postgres{job: j} }

func (postgres) Ext() string { return ".sql.gz" }

// path returns the version-specific pg_dump binary, as installed by
// the postgresql-client-<N> Debian packages.
func (p postgres) path() string {
	return fmt.Sprintf("/usr/lib/postgresql/%d/bin/pg_dump", p.job.PGVersion)
}

func (p postgres) args() []string {
	return []string{
		"-h", p.job.Host,
		"-p", fmt.Sprint(p.job.Port),
		"-U", p.job.User,
		"-d", p.job.Database,
	}
}

// env carries the password and TLS mode so they never appear in argv.
func (p postgres) env() []string {
	env := []string{"PGPASSWORD=" + p.job.Password}
	if p.job.IsTLS() {
		env = append(env, "PGSSLMODE=require")
	}
	return env
}

func (p postgres) Dump(ctx context.Context, w io.Writer) error {
	if _, err := os.Stat(p.path()); err != nil {
		return fmt.Errorf("PostgreSQL %d client is not installed (%s)", p.job.PGVersion, p.path())
	}
	return runTool(ctx, w, p.path(), p.args(), p.env())
}
```

```go
// internal/dumper/mariadb.go
package dumper

import (
	"context"
	"io"

	"github.com/Greite/database-backup/internal/config"
)

type mariadb struct{ job config.Job }

func newMariaDB(j config.Job) mariadb { return mariadb{job: j} }

func (mariadb) Ext() string { return ".sql.gz" }

func (m mariadb) args() []string {
	args := []string{
		"-h", m.job.Host,
		"-P", fmt.Sprint(m.job.Port),
		"-u", m.job.User,
	}
	if m.job.IsTLS() {
		args = append(args, "--ssl")
	}
	return append(args, m.job.Database)
}

func (m mariadb) env() []string {
	return []string{"MYSQL_PWD=" + m.job.Password}
}

func (m mariadb) Dump(ctx context.Context, w io.Writer) error {
	return runTool(ctx, w, "mysqldump", m.args(), m.env())
}
```

(add `fmt` to the mariadb.go import block)

`newMongoDB` does not exist yet — add a temporary stub in `internal/dumper/mongodb.go` so the package compiles; Task 11 replaces it:

```go
// internal/dumper/mongodb.go
package dumper

import (
	"context"
	"fmt"
	"io"

	"github.com/Greite/database-backup/internal/config"
)

type mongodb struct{ job config.Job }

func newMongoDB(j config.Job) mongodb { return mongodb{job: j} }

func (mongodb) Ext() string { return ".tar.gz" }

func (m mongodb) Dump(ctx context.Context, w io.Writer) error {
	return fmt.Errorf("mongodb dumper: not implemented yet")
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/dumper/ && gofmt -l . && go vet ./...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/dumper/
git commit -m "Feature - #BKP-NoId - Add dumper interface with PostgreSQL and MariaDB implementations"
```

---

### Task 11: Dumper — MongoDB

**Files:**
- Modify: `internal/dumper/mongodb.go` (replace the stub)
- Create: `internal/dumper/mongodb_test.go`

`mongodump` cannot stream to stdout for a whole database: it writes a dump directory. The Dump implementation dumps into a private temp dir (`--gzip` BSON), then streams it with `archive.TarDir` and removes it. Credentials go through a 600-mode temp YAML config file (`--config`), never argv — same approach as v1.

- [ ] **Step 1: Write the failing tests**

```go
// internal/dumper/mongodb_test.go
package dumper

import (
	"os"
	"strings"
	"testing"

	"github.com/Greite/database-backup/internal/config"
)

func TestMongoArgsWithoutAuth(t *testing.T) {
	j := config.Job{Type: "mongodb", Host: "mg", Port: 27018, Database: "ev"}
	m := newMongoDB(j)
	args := strings.Join(m.args("/tmp/out", ""), " ")
	want := "--host mg --port 27018 --db ev --out /tmp/out --gzip"
	if args != want {
		t.Errorf("args = %q, want %q", args, want)
	}
}

func TestMongoArgsWithAuthAndTLS(t *testing.T) {
	tls := true
	j := config.Job{Type: "mongodb", Host: "mg", Port: 27017, Database: "ev",
		User: "admin", Password: "pw", TLS: &tls}
	m := newMongoDB(j)
	args := strings.Join(m.args("/tmp/out", "/tmp/cfg.yaml"), " ")
	for _, want := range []string{"--ssl", "--username admin", "--authenticationDatabase admin", "--config /tmp/cfg.yaml"} {
		if !strings.Contains(args, want) {
			t.Errorf("args %q missing %q", args, want)
		}
	}
	if strings.Contains(args, "pw") {
		t.Errorf("password leaked into argv: %q", args)
	}
}

func TestMongoPasswordConfigFile(t *testing.T) {
	j := config.Job{Type: "mongodb", User: "u", Password: `p"w\x`}
	m := newMongoDB(j)
	path, cleanup, err := m.writePasswordConfig(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// Backslashes and double quotes must be escaped for mongodump's YAML.
	want := `password: "p\"w\\x"` + "\n"
	if string(b) != want {
		t.Errorf("config content = %q, want %q", b, want)
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0o600 {
		t.Errorf("config mode = %o, want 600", info.Mode().Perm())
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/dumper/`
Expected: FAIL (`m.args undefined` / `writePasswordConfig undefined`)

- [ ] **Step 3: Implement the MongoDB dumper**

```go
// internal/dumper/mongodb.go
package dumper

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/Greite/database-backup/internal/archive"
	"github.com/Greite/database-backup/internal/config"
)

type mongodb struct{ job config.Job }

func newMongoDB(j config.Job) mongodb { return mongodb{job: j} }

func (mongodb) Ext() string { return ".tar.gz" }

func (m mongodb) args(outDir, configFile string) []string {
	args := []string{
		"--host", m.job.Host,
		"--port", fmt.Sprint(m.job.Port),
		"--db", m.job.Database,
		"--out", outDir,
		"--gzip",
	}
	if m.job.IsTLS() {
		args = append(args, "--ssl")
	}
	if m.job.User != "" && m.job.Password != "" {
		args = append(args,
			"--username", m.job.User,
			"--authenticationDatabase", "admin",
			"--config", configFile,
		)
	}
	return args
}

// writePasswordConfig writes the password to a 600-mode YAML config
// file inside dir so it never appears in /proc/*/cmdline.
func (m mongodb) writePasswordConfig(dir string) (path string, cleanup func(), err error) {
	f, err := os.CreateTemp(dir, "mongodump-*.yaml")
	if err != nil {
		return "", nil, err
	}
	esc := strings.ReplaceAll(m.job.Password, `\`, `\\`)
	esc = strings.ReplaceAll(esc, `"`, `\"`)
	if _, err := fmt.Fprintf(f, "password: \"%s\"\n", esc); err == nil {
		err = f.Chmod(0o600)
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(f.Name())
		return "", nil, err
	}
	return f.Name(), func() { os.Remove(f.Name()) }, nil
}

func (m mongodb) Dump(ctx context.Context, w io.Writer) error {
	tmp, err := os.MkdirTemp("", "mongodump-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)

	configFile := ""
	if m.job.User != "" && m.job.Password != "" {
		path, cleanup, err := m.writePasswordConfig(tmp)
		if err != nil {
			return err
		}
		defer cleanup()
		configFile = path
	}
	outDir := tmp + "/dump"
	if err := runTool(ctx, io.Discard, "mongodump", m.args(outDir, configFile), nil); err != nil {
		return err
	}
	if _, err := os.Stat(outDir + "/" + m.job.Database); err != nil {
		return fmt.Errorf("mongodump did not produce a %q directory", m.job.Database)
	}
	return archive.TarDir(outDir, w)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/dumper/ && gofmt -l . && go vet ./...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/dumper/
git commit -m "Feature - #BKP-NoId - Add MongoDB dumper with tar streaming and password config file"
```

---

### Task 12: Runner — the job pipeline

**Files:**
- Create: `internal/dumper/runner.go`
- Create: `internal/dumper/runner_test.go`

The pipeline: `Dumper → gzip → [Encryptor] → <final>.tmp → rename`. On success, rotation purges old files in the job directory. On failure, the temp file is removed and rotation is skipped (spec: never delete old backups when the new one failed).

- [ ] **Step 1: Write the failing tests**

```go
// internal/dumper/runner_test.go
package dumper

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Greite/database-backup/internal/config"
	"github.com/Greite/database-backup/internal/crypto"
)

// fakeDumper lets runner tests avoid real database tools.
type fakeDumper struct {
	content string
	err     error
}

func (f fakeDumper) Dump(ctx context.Context, w io.Writer) error {
	if f.err != nil {
		return f.err
	}
	_, err := io.WriteString(w, f.content)
	return err
}

func (fakeDumper) Ext() string { return ".sql.gz" }

func runnerJob() config.Job {
	return config.Job{Name: "app", Type: "postgres", Database: "appdb", RetentionDays: 7}
}

func TestRunWritesGzippedBackup(t *testing.T) {
	root := t.TempDir()
	r := Runner{BackupRoot: root, Now: func() time.Time {
		return time.Date(2026, 6, 10, 2, 0, 0, 0, time.UTC)
	}}
	path, err := r.Run(context.Background(), runnerJob(), fakeDumper{content: "SELECT 1;"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(root, "postgres", "app", "app_20260610_020000.sql.gz")
	if path != want {
		t.Errorf("path = %q, want %q", path, want)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(zr)
	if string(b) != "SELECT 1;" {
		t.Errorf("content = %q, want SELECT 1;", b)
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0o600 {
		t.Errorf("mode = %o, want 600", info.Mode().Perm())
	}
}

func TestRunAppendsEncryptionExt(t *testing.T) {
	root := t.TempDir()
	enc, _ := crypto.NewAgePassphrase("k")
	r := Runner{BackupRoot: root, Now: time.Now}
	path, err := r.Run(context.Background(), runnerJob(), fakeDumper{content: "x"}, enc)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(path, ".sql.gz.age") {
		t.Errorf("path = %q, want .sql.gz.age suffix", path)
	}
}

func TestRunFailureLeavesNoFileAndSkipsRotation(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "postgres", "app")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	// An expired backup that must survive because the new run fails.
	oldFile := filepath.Join(dir, "app_20250101_020000.sql.gz")
	os.WriteFile(oldFile, []byte("old"), 0o600)
	past := time.Now().Add(-30 * 24 * time.Hour)
	os.Chtimes(oldFile, past, past)

	r := Runner{BackupRoot: root, Now: time.Now}
	_, err := r.Run(context.Background(), runnerJob(), fakeDumper{err: errors.New("boom")}, nil)
	if err == nil {
		t.Fatal("want error, got nil")
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("dir entries = %d, want only the old backup", len(entries))
	}
	if _, err := os.Stat(oldFile); err != nil {
		t.Error("old backup must not be rotated after a failed run")
	}
}

func TestRunRotatesAfterSuccess(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "postgres", "app")
	os.MkdirAll(dir, 0o700)
	oldFile := filepath.Join(dir, "app_20250101_020000.sql.gz")
	os.WriteFile(oldFile, []byte("old"), 0o600)
	past := time.Now().Add(-30 * 24 * time.Hour)
	os.Chtimes(oldFile, past, past)

	r := Runner{BackupRoot: root, Now: time.Now}
	if _, err := r.Run(context.Background(), runnerJob(), fakeDumper{content: "x"}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(oldFile); !os.IsNotExist(err) {
		t.Error("expired backup should be rotated after a successful run")
	}
}
```

(add `compress/gzip` to the test import block)

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/dumper/`
Expected: FAIL (`undefined: Runner`)

- [ ] **Step 3: Implement the Runner**

```go
// internal/dumper/runner.go
package dumper

import (
	"compress/gzip"
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/Greite/database-backup/internal/config"
	"github.com/Greite/database-backup/internal/crypto"
	"github.com/Greite/database-backup/internal/rotation"
)

// Runner executes one backup job end to end.
type Runner struct {
	BackupRoot string           // usually /backups
	Now        func() time.Time // injected for tests
}

// Run dumps the job through gzip and optional encryption into
// <BackupRoot>/<type>/<name>/<name>_<timestamp><ext>, writing to a
// .tmp file renamed only on success. Rotation runs only after success.
// It returns the final file path.
func (r Runner) Run(ctx context.Context, job config.Job, d Dumper, enc crypto.Encryptor) (string, error) {
	dir := filepath.Join(r.BackupRoot, job.Type, job.Name)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}

	ext := d.Ext()
	if enc != nil {
		ext += enc.Ext()
	}
	ts := r.Now().Format("20060102_150405")
	final := filepath.Join(dir, fmt.Sprintf("%s_%s%s", job.Name, ts, ext))
	tmp := final + ".tmp"

	if err := r.writeBackup(ctx, job, d, enc, tmp); err != nil {
		os.Remove(tmp)
		return "", fmt.Errorf("job %q: %w", job.Name, err)
	}
	if err := os.Rename(tmp, final); err != nil {
		os.Remove(tmp)
		return "", err
	}

	deleted, remaining, err := rotation.Purge(dir, d.Ext(), job.RetentionDays, r.Now())
	if err != nil {
		// The backup itself succeeded; report rotation problems without failing the job.
		log.Printf("job %q: rotation error: %v", job.Name, err)
	} else if deleted > 0 {
		log.Printf("job %q: rotated %d old backup(s), %d remaining", job.Name, deleted, remaining+1)
	}
	return final, nil
}

func (r Runner) writeBackup(ctx context.Context, job config.Job, d Dumper, enc crypto.Encryptor, path string) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()

	// Build the write chain: dump -> gzip -> [encrypt] -> file.
	var sink = io.Writer(f)
	var encW io.WriteCloser
	if enc != nil {
		if encW, err = enc.Wrap(f); err != nil {
			return err
		}
		sink = encW
	}
	zw := gzip.NewWriter(sink)

	if err := d.Dump(ctx, zw); err != nil {
		return err
	}
	if err := zw.Close(); err != nil {
		return err
	}
	if encW != nil {
		if err := encW.Close(); err != nil {
			return err
		}
	}
	return f.Sync()
}
```

(add `io` to the import block)

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/dumper/ && gofmt -l . && go vet ./...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/dumper/
git commit -m "Feature - #BKP-NoId - Add backup runner pipeline with atomic rename and post-success rotation"
```

---

### Task 13: Healthcheck — native pings

**Files:**
- Create: `internal/healthcheck/healthcheck.go`
- Create: `internal/healthcheck/healthcheck_test.go`

Unit tests cover DSN/URI construction (credentials, TLS flags). Real connectivity is covered by the integration tests (Task 18).

- [ ] **Step 1: Write the failing tests**

```go
// internal/healthcheck/healthcheck_test.go
package healthcheck

import (
	"strings"
	"testing"

	"github.com/Greite/database-backup/internal/config"
)

func TestPostgresDSN(t *testing.T) {
	tls := true
	j := config.Job{Type: "postgres", Host: "db", Port: 5433, Database: "app",
		User: "u", Password: "p w", TLS: &tls}
	dsn := postgresDSN(j)
	for _, want := range []string{"host=db", "port=5433", "dbname=app", "user=u", "password='p w'", "sslmode=require"} {
		if !strings.Contains(dsn, want) {
			t.Errorf("dsn %q missing %q", dsn, want)
		}
	}
	j.TLS = nil
	if !strings.Contains(postgresDSN(j), "sslmode=prefer") {
		t.Error("non-TLS jobs should use sslmode=prefer")
	}
}

func TestMySQLDSN(t *testing.T) {
	j := config.Job{Type: "mariadb", Host: "m", Port: 3307, Database: "wp", User: "u", Password: "p"}
	if got, want := mysqlDSN(j), "u:p@tcp(m:3307)/wp"; got != want {
		t.Errorf("dsn = %q, want %q", got, want)
	}
	tls := true
	j.TLS = &tls
	if !strings.Contains(mysqlDSN(j), "tls=skip-verify") {
		t.Error("TLS jobs should set tls=skip-verify (parity with v1 --ssl)")
	}
}

func TestMongoURI(t *testing.T) {
	j := config.Job{Type: "mongodb", Host: "mg", Port: 27017, Database: "ev"}
	if got, want := mongoURI(j), "mongodb://mg:27017/?connectTimeoutMS=5000"; got != want {
		t.Errorf("uri = %q, want %q", got, want)
	}
	j.User, j.Password = "ad min", "p@ss/w"
	uri := mongoURI(j)
	if !strings.Contains(uri, "ad%20min:p%40ss%2Fw@") || !strings.Contains(uri, "authSource=admin") {
		t.Errorf("uri %q must URL-escape credentials and set authSource", uri)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/healthcheck/`
Expected: FAIL (`undefined: postgresDSN`)

- [ ] **Step 3: Implement the healthcheck**

```go
// internal/healthcheck/healthcheck.go
// Package healthcheck verifies connectivity to every configured
// database using native Go drivers (no CLI clients required).
package healthcheck

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	_ "github.com/go-sql-driver/mysql"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/Greite/database-backup/internal/config"
)

const pingTimeout = 5 * time.Second

func postgresDSN(j config.Job) string {
	sslmode := "prefer"
	if j.IsTLS() {
		sslmode = "require"
	}
	// Single-quote the password so spaces and quotes survive keyword/value parsing.
	pw := "'" + strings.ReplaceAll(j.Password, "'", `\'`) + "'"
	return fmt.Sprintf("host=%s port=%d dbname=%s user=%s password=%s sslmode=%s",
		j.Host, j.Port, j.Database, j.User, pw, sslmode)
}

func mysqlDSN(j config.Job) string {
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%d)/%s", j.User, j.Password, j.Host, j.Port, j.Database)
	if j.IsTLS() {
		dsn += "?tls=skip-verify"
	}
	return dsn
}

func mongoURI(j config.Job) string {
	cred := ""
	auth := ""
	if j.User != "" && j.Password != "" {
		cred = url.QueryEscape(j.User) + ":" + url.QueryEscape(j.Password) + "@"
		auth = "&authSource=admin"
	}
	uri := fmt.Sprintf("mongodb://%s%s:%d/?connectTimeoutMS=5000%s", cred, j.Host, j.Port, auth)
	if j.IsTLS() {
		uri += "&tls=true"
	}
	return uri
}

// Ping checks one job's database connectivity.
func Ping(ctx context.Context, j config.Job) error {
	ctx, cancel := context.WithTimeout(ctx, pingTimeout)
	defer cancel()
	switch j.Type {
	case "postgres":
		conn, err := pgx.Connect(ctx, postgresDSN(j))
		if err != nil {
			return err
		}
		defer conn.Close(ctx)
		return conn.Ping(ctx)
	case "mariadb", "mysql":
		db, err := sql.Open("mysql", mysqlDSN(j))
		if err != nil {
			return err
		}
		defer db.Close()
		return db.PingContext(ctx)
	case "mongodb":
		client, err := mongo.Connect(ctx, options.Client().ApplyURI(mongoURI(j)))
		if err != nil {
			return err
		}
		defer client.Disconnect(ctx)
		return client.Ping(ctx, nil)
	}
	return fmt.Errorf("unknown database type %q", j.Type)
}

// Check pings every job and returns one error per failing job.
func Check(ctx context.Context, cfg *config.Config) []error {
	var errs []error
	for _, j := range cfg.Jobs {
		if err := Ping(ctx, j); err != nil {
			errs = append(errs, fmt.Errorf("job %q (%s on %s:%d): %w", j.Name, j.Type, j.Host, j.Port, err))
		}
	}
	return errs
}
```

Run: `go get github.com/jackc/pgx/v5 github.com/go-sql-driver/mysql go.mongodb.org/mongo-driver`

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/healthcheck/ && gofmt -l . && go vet ./...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add go.mod go.sum internal/healthcheck/
git commit -m "Feature - #BKP-NoId - Add native database connectivity healthcheck"
```

---

### Task 14: Scheduler — cron wrapper with graceful shutdown

**Files:**
- Create: `internal/scheduler/scheduler.go`
- Create: `internal/scheduler/scheduler_test.go`

- [ ] **Step 1: Write the failing tests**

```go
// internal/scheduler/scheduler_test.go
package scheduler

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestAddRejectsBadSchedule(t *testing.T) {
	s := New(time.Minute)
	if err := s.Add("not a cron", func(ctx context.Context) {}); err == nil {
		t.Fatal("want error for invalid schedule, got nil")
	}
}

func TestRunStopsOnContextCancel(t *testing.T) {
	s := New(time.Second)
	if err := s.Add("* * * * *", func(ctx context.Context) {}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { s.Run(ctx); close(done) }()
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after context cancellation")
	}
}

func TestRunWaitsForInFlightJobs(t *testing.T) {
	s := New(5 * time.Second)
	var finished atomic.Bool
	started := make(chan struct{})
	// Trigger the job manually to avoid waiting a real cron minute.
	job := func(ctx context.Context) {
		close(started)
		time.Sleep(200 * time.Millisecond)
		finished.Store(true)
	}
	if err := s.Add("* * * * *", job); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { s.Run(ctx); close(done) }()
	go s.trigger(0) // test hook: fire entry 0 now
	<-started
	cancel()
	<-done
	if !finished.Load() {
		t.Error("Run returned before the in-flight job finished")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/scheduler/`
Expected: FAIL (`undefined: New`)

- [ ] **Step 3: Implement the scheduler**

```go
// internal/scheduler/scheduler.go
// Package scheduler runs backup jobs on cron schedules with graceful
// shutdown: on stop, in-flight jobs may finish within a grace period.
package scheduler

import (
	"context"
	"sync"
	"time"

	"github.com/robfig/cron/v3"
)

type Scheduler struct {
	cron  *cron.Cron
	grace time.Duration
	wg    sync.WaitGroup
	jobs  []func(ctx context.Context)
	ctx   context.Context
}

// New creates a scheduler; grace bounds how long Run waits for
// in-flight jobs after the context is cancelled.
func New(grace time.Duration) *Scheduler {
	return &Scheduler{cron: cron.New(), grace: grace}
}

// Add registers fn under a cron schedule (5-field or @daily-style).
func (s *Scheduler) Add(schedule string, fn func(ctx context.Context)) error {
	idx := len(s.jobs)
	s.jobs = append(s.jobs, fn)
	_, err := s.cron.AddFunc(schedule, func() { s.trigger(idx) })
	return err
}

// trigger runs job idx now, tracked by the WaitGroup. Exposed for tests.
func (s *Scheduler) trigger(idx int) {
	s.wg.Add(1)
	defer s.wg.Done()
	s.jobs[idx](s.ctx)
}

// Run starts the cron loop and blocks until ctx is cancelled, then
// waits up to the grace period for running jobs.
func (s *Scheduler) Run(ctx context.Context) {
	s.ctx = ctx
	s.cron.Start()
	<-ctx.Done()
	stopCtx := s.cron.Stop() // no new runs; returns a ctx done when cron jobs return

	graceTimer := time.NewTimer(s.grace)
	defer graceTimer.Stop()
	finished := make(chan struct{})
	go func() {
		<-stopCtx.Done()
		s.wg.Wait()
		close(finished)
	}()
	select {
	case <-finished:
	case <-graceTimer.C:
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/scheduler/ && gofmt -l . && go vet ./...`
Expected: PASS (note: `TestRunWaitsForInFlightJobs` relies on `trigger` being called before `cancel`, which the `started` channel guarantees)

- [ ] **Step 5: Commit**

```bash
git add internal/scheduler/
git commit -m "Feature - #BKP-NoId - Add cron scheduler with graceful shutdown"
```

---

### Task 15: Installer — client detection and installation

**Files:**
- Create: `internal/installer/installer.go`
- Create: `internal/installer/installer_test.go`

Unit tests cover requirement detection and checksum verification. The apt/download paths run only inside the container and are covered by the CI smoke test (Task 20).

- [ ] **Step 1: Write the failing tests**

```go
// internal/installer/installer_test.go
package installer

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/Greite/database-backup/internal/config"
)

func TestRequirementsDeduplicatesPGVersions(t *testing.T) {
	cfg := &config.Config{Jobs: []config.Job{
		{Type: "postgres", PGVersion: 18},
		{Type: "postgres", PGVersion: 17},
		{Type: "postgres", PGVersion: 18},
		{Type: "mariadb"},
		{Type: "mongodb"},
	}}
	req := Requirements(cfg)
	if len(req.PGVersions) != 2 || req.PGVersions[0] != 17 || req.PGVersions[1] != 18 {
		t.Errorf("PGVersions = %v, want [17 18]", req.PGVersions)
	}
	if !req.MariaDB || !req.MongoDB {
		t.Errorf("req = %+v, want MariaDB and MongoDB true", req)
	}
}

func TestRequirementsEmptyForMongoOnly(t *testing.T) {
	cfg := &config.Config{Jobs: []config.Job{{Type: "mongodb"}}}
	req := Requirements(cfg)
	if len(req.PGVersions) != 0 || req.MariaDB || !req.MongoDB {
		t.Errorf("req = %+v, want only MongoDB", req)
	}
}

func TestVerifySHA256(t *testing.T) {
	data := []byte("archive-bytes")
	sum := sha256.Sum256(data)
	if err := verifySHA256(bytes.NewReader(data), hex.EncodeToString(sum[:])); err != nil {
		t.Errorf("valid checksum rejected: %v", err)
	}
	if err := verifySHA256(bytes.NewReader(data), "deadbeef"); err == nil {
		t.Error("invalid checksum accepted")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/installer/`
Expected: FAIL (`undefined: Requirements`)

- [ ] **Step 3: Implement the installer**

```go
// internal/installer/installer.go
// Package installer installs the database client tools required by
// the configuration at container startup (must run as root).
package installer

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
)

// Pinned MongoDB Database Tools release, verified by SHA256 at download.
const mongoToolsVersion = "100.14.0"

var mongoToolsSHA256 = map[string]string{
	"amd64": "4104998bda784a0cb16fc2e06d9c21645516d72c4fb481c9b103f1e0a8458fc0",
	"arm64": "ef2945973b7e9c0f95d25dc607d420b0b07c486a675937ac9723b32f56ce718d",
}

var mongoToolsArch = map[string]string{"amd64": "x86_64", "arm64": "arm64"}

type Req struct {
	PGVersions []int // sorted, deduplicated
	MariaDB    bool
	MongoDB    bool
}

// Requirements computes which client tools the config needs.
func Requirements(cfg *config.Config) Req {
	var req Req
	pg := map[int]bool{}
	for _, j := range cfg.Jobs {
		switch j.Type {
		case "postgres":
			pg[j.PGVersion] = true
		case "mariadb", "mysql":
			req.MariaDB = true
		case "mongodb":
			req.MongoDB = true
		}
	}
	for v := range pg {
		req.PGVersions = append(req.PGVersions, v)
	}
	sort.Ints(req.PGVersions)
	return req
}

func verifySHA256(r io.Reader, want string) error {
	h := sha256.New()
	if _, err := io.Copy(h, r); err != nil {
		return err
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != want {
		return fmt.Errorf("sha256 mismatch: got %s, want %s", got, want)
	}
	return nil
}

// Install installs every missing tool. It must run as root and is a
// no-op for tools already present (preinstalled or previous start).
func Install(req Req) error {
	var aptPkgs []string
	for _, v := range req.PGVersions {
		if _, err := os.Stat(fmt.Sprintf("/usr/lib/postgresql/%d/bin/pg_dump", v)); err != nil {
			aptPkgs = append(aptPkgs, fmt.Sprintf("postgresql-client-%d", v))
		}
	}
	if req.MariaDB {
		if _, err := exec.LookPath("mysqldump"); err != nil {
			aptPkgs = append(aptPkgs, "mariadb-client")
		}
	}
	if len(aptPkgs) > 0 {
		log.Printf("installing packages: %v", aptPkgs)
		if err := aptInstall(aptPkgs); err != nil {
			return err
		}
	}
	if req.MongoDB {
		if _, err := exec.LookPath("mongodump"); err != nil {
			log.Printf("installing MongoDB Database Tools %s", mongoToolsVersion)
			if err := installMongoTools(); err != nil {
				return err
			}
		}
	}
	return nil
}

func aptInstall(pkgs []string) error {
	if out, err := exec.Command("apt-get", "update", "-qq").CombinedOutput(); err != nil {
		return fmt.Errorf("apt-get update: %w (%s)", err, out)
	}
	args := append([]string{"install", "-y", "-qq"}, pkgs...)
	if out, err := exec.Command("apt-get", args...).CombinedOutput(); err != nil {
		return fmt.Errorf("apt-get install: %w (%s)", err, out)
	}
	return os.RemoveAll("/var/lib/apt/lists")
}

func installMongoTools() error {
	arch := runtime.GOARCH
	sum, ok := mongoToolsSHA256[arch]
	if !ok {
		return fmt.Errorf("unsupported architecture %q", arch)
	}
	url := fmt.Sprintf("https://fastdl.mongodb.org/tools/db/mongodb-database-tools-ubuntu2204-%s-%s.tgz",
		mongoToolsArch[arch], mongoToolsVersion)

	tmp, err := os.CreateTemp("", "mongo-tools-*.tgz")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("downloading %s: HTTP %d", url, resp.StatusCode)
	}
	if _, err := io.Copy(tmp, resp.Body); err != nil {
		return err
	}
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		return err
	}
	if err := verifySHA256(tmp, sum); err != nil {
		return fmt.Errorf("MongoDB tools archive: %w", err)
	}
	tmp.Close()

	// Extract bin/* into /usr/local/bin (strip the top-level directory).
	out, err := exec.Command("tar", "-xzf", tmp.Name(), "-C", "/usr/local/bin",
		"--strip-components=2", "--wildcards", "*/bin/*").CombinedOutput()
	if err != nil {
		return fmt.Errorf("extracting MongoDB tools: %w (%s)", err, out)
	}
	if _, err := exec.LookPath("mongodump"); err != nil {
		return fmt.Errorf("mongodump not found after extraction")
	}
	return nil
}
```

Add `"github.com/Greite/database-backup/internal/config"` to the import block and remove `path/filepath` from it (unused).

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/installer/ && gofmt -l . && go vet ./...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/installer/
git commit -m "Feature - #BKP-NoId - Add client tool detection and installation"
```

---

### Task 16: Privileges — root → uid 1000 re-exec

**Files:**
- Create: `internal/privileges/privileges.go`
- Create: `internal/privileges/privileges_test.go`

The drop works by re-executing `/proc/self/exe` with `SysProcAttr.Credential` set to uid/gid 1000 and the marker env var `DBBACKUP_DROPPED=1`, after chowning `/backups` so the unprivileged process can write. Only the decision logic is unit-testable on macOS/CI; the actual re-exec is covered by the container smoke test.

- [ ] **Step 1: Write the failing tests**

```go
// internal/privileges/privileges_test.go
package privileges

import "testing"

func TestShouldDrop(t *testing.T) {
	cases := []struct {
		uid     int
		dropped string
		want    bool
	}{
		{0, "", true},     // root, not yet dropped
		{0, "1", false},   // already re-executed
		{1000, "", false}, // already unprivileged (e.g. user: in compose)
	}
	for _, tc := range cases {
		if got := shouldDrop(tc.uid, tc.dropped); got != tc.want {
			t.Errorf("shouldDrop(%d, %q) = %v, want %v", tc.uid, tc.dropped, got, tc.want)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/privileges/`
Expected: FAIL (`undefined: shouldDrop`)

- [ ] **Step 3: Implement the privilege drop**

```go
// internal/privileges/privileges.go
// Package privileges re-executes the current binary as an
// unprivileged user once root-only startup work is done.
package privileges

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"
)

const (
	// UID/GID of the `backup` user created in the Dockerfile.
	UID = 1000
	GID = 1000

	droppedEnv = "DBBACKUP_DROPPED"
)

func shouldDrop(uid int, droppedMarker string) bool {
	return uid == 0 && droppedMarker == ""
}

// NeedsDrop reports whether the current process should re-exec
// unprivileged.
func NeedsDrop() bool {
	return shouldDrop(os.Getuid(), os.Getenv(droppedEnv))
}

// DropAndReexec chowns the backup root for the target user, then
// re-executes the current binary (same args) as uid/gid 1000. On
// success it never returns: the parent waits for the child and exits
// with its code.
func DropAndReexec(backupRoot string) error {
	if err := os.Chown(backupRoot, UID, GID); err != nil {
		return fmt.Errorf("chown %s: %w", backupRoot, err)
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := exec.Command(exe, os.Args[1:]...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	cmd.Env = append(os.Environ(), droppedEnv+"=1")
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Credential: &syscall.Credential{Uid: UID, Gid: GID},
	}
	if err := cmd.Run(); err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			os.Exit(ee.ExitCode())
		}
		return err
	}
	os.Exit(0)
	return nil
}
```

Note: `syscall.Credential` only exists on Linux. Guard the file with a build tag `//go:build linux` at the top, and add `privileges_other.go` so the package builds on macOS for local development:

```go
// internal/privileges/privileges_other.go
//go:build !linux

package privileges

import "fmt"

const (
	UID        = 1000
	GID        = 1000
	droppedEnv = "DBBACKUP_DROPPED"
)

func shouldDrop(uid int, droppedMarker string) bool {
	return uid == 0 && droppedMarker == ""
}

func NeedsDrop() bool { return false }

func DropAndReexec(backupRoot string) error {
	return fmt.Errorf("privilege drop is only supported on linux")
}
```

Move `shouldDrop`, the constants, and `NeedsDrop`'s uid logic into a shared unguarded file `internal/privileges/common.go` instead of duplicating them (keep `privileges.go` linux-only for `DropAndReexec`, `privileges_other.go` for the non-linux stub):

```go
// internal/privileges/common.go
package privileges

import "os"

const (
	// UID/GID of the `backup` user created in the Dockerfile.
	UID = 1000
	GID = 1000

	droppedEnv = "DBBACKUP_DROPPED"
)

func shouldDrop(uid int, droppedMarker string) bool {
	return uid == 0 && droppedMarker == ""
}

// NeedsDrop reports whether the current process should re-exec
// unprivileged.
func NeedsDrop() bool {
	return shouldDrop(os.Getuid(), os.Getenv(droppedEnv))
}
```

Final layout: `common.go` (shared logic above), `privileges.go` (`//go:build linux`, only `DropAndReexec`), `privileges_other.go` (`//go:build !linux`, only the `DropAndReexec` stub). Remove the duplicated declarations from both platform files.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/privileges/ && gofmt -l . && go vet ./...`
Expected: PASS
Also run: `GOOS=linux go build ./...`
Expected: compiles (validates the linux file).

- [ ] **Step 5: Commit**

```bash
git add internal/privileges/
git commit -m "Security - #BKP-NoId - Add root to uid 1000 privilege drop via re-exec"
```

---

### Task 17: Wire the subcommands in main

**Files:**
- Create: `cmd/dbbackup/run.go`
- Create: `cmd/dbbackup/commands.go`
- Modify: `cmd/dbbackup/main.go` (register commands)
- Modify: `cmd/dbbackup/main_test.go` (registration test)

- [ ] **Step 1: Write the failing test**

Append to `cmd/dbbackup/main_test.go`:

```go
func TestAllCommandsRegistered(t *testing.T) {
	for _, name := range []string{"run", "healthcheck", "backup", "validate", "migrate"} {
		if _, ok := commands[name]; !ok {
			t.Errorf("command %q is not registered", name)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/...`
Expected: FAIL (empty `commands` map)

- [ ] **Step 3: Implement the commands**

```go
// cmd/dbbackup/commands.go
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/Greite/database-backup/internal/config"
	"github.com/Greite/database-backup/internal/crypto"
	"github.com/Greite/database-backup/internal/dumper"
	"github.com/Greite/database-backup/internal/healthcheck"
	"github.com/Greite/database-backup/internal/migrate"
)

const (
	defaultConfigPath = "/config/backups.yml"
	v1ConfigPath      = "/config/backups.conf"
	backupRoot        = "/backups"
)

func init() {
	commands["run"] = cmdRun
	commands["healthcheck"] = cmdHealthcheck
	commands["backup"] = cmdBackup
	commands["validate"] = cmdValidate
	commands["migrate"] = cmdMigrate
}

// loadConfig parses, validates and resolves secrets, with the v1
// migration guard when the v2 file is missing.
func loadConfig(path string) (*config.Config, error) {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		if _, v1err := os.Stat(v1ConfigPath); v1err == nil {
			return nil, fmt.Errorf(
				"%s not found, but a v1 config exists at %s.\n"+
					"Convert it with:\n"+
					"  docker run --rm -v <appdata>:/config <image> migrate %s > backups.yml\n"+
					"then mount backups.yml at %s",
				path, v1ConfigPath, v1ConfigPath, path)
		}
		return nil, fmt.Errorf("configuration file not found at %s", path)
	}
	warnIfWorldReadable(path)
	cfg, err := config.Load(path)
	if err != nil {
		return nil, err
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if err := cfg.ResolveSecrets(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func warnIfWorldReadable(path string) {
	info, err := os.Stat(path)
	if err == nil && info.Mode().Perm()&0o044 != 0 {
		log.Printf("Warning: %s is readable by group/other (mode %o); it contains credentials, chmod 600 it on the host", path, info.Mode().Perm())
	}
}

func cmdValidate(args []string) int {
	fs := flag.NewFlagSet("validate", flag.ContinueOnError)
	cfgPath := fs.String("config", defaultConfigPath, "config file path")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if _, err := loadConfig(*cfgPath); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Println("configuration is valid")
	return 0
}

func cmdHealthcheck(args []string) int {
	fs := flag.NewFlagSet("healthcheck", flag.ContinueOnError)
	cfgPath := fs.String("config", defaultConfigPath, "config file path")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	cfg, err := loadConfig(*cfgPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "UNHEALTHY:", err)
		return 1
	}
	errs := healthcheck.Check(context.Background(), cfg)
	for _, e := range errs {
		fmt.Fprintln(os.Stderr, "FAILED:", e)
	}
	if len(errs) > 0 {
		fmt.Println("UNHEALTHY: some database connections failed")
		return 1
	}
	fmt.Println("HEALTHY: all database connections successful")
	return 0
}

func cmdBackup(args []string) int {
	fs := flag.NewFlagSet("backup", flag.ContinueOnError)
	cfgPath := fs.String("config", defaultConfigPath, "config file path")
	jobName := fs.String("job", "", "job name to run (required)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *jobName == "" {
		fmt.Fprintln(os.Stderr, "backup: --job <name> is required")
		return 2
	}
	cfg, err := loadConfig(*cfgPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	for _, j := range cfg.Jobs {
		if j.Name != *jobName {
			continue
		}
		if err := runJob(context.Background(), cfg, j); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		return 0
	}
	fmt.Fprintf(os.Stderr, "backup: no job named %q in %s\n", *jobName, *cfgPath)
	return 1
}

func cmdMigrate(args []string) int {
	path := v1ConfigPath
	if len(args) > 0 {
		path = args[0]
	}
	f, err := os.Open(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer f.Close()
	cfg, errs := migrate.Convert(f)
	for _, e := range errs {
		fmt.Fprintln(os.Stderr, "skipped:", e)
	}
	out, err := migrate.ToYAML(cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	os.Stdout.Write(out)
	if len(errs) > 0 {
		return 1
	}
	return 0
}

// runJob builds the dumper and encryptor for one job and executes it.
func runJob(ctx context.Context, cfg *config.Config, j config.Job) error {
	d, err := dumper.New(j)
	if err != nil {
		return err
	}
	enc, err := crypto.New(cfg.Encryption)
	if err != nil {
		return err
	}
	r := dumper.Runner{BackupRoot: backupRoot, Now: time.Now}
	log.Printf("starting backup of %s database %q on %s:%d", j.Type, j.Database, j.Host, j.Port)
	path, err := r.Run(ctx, j, d, enc)
	if err != nil {
		return err
	}
	info, statErr := os.Stat(path)
	size := "?"
	if statErr == nil {
		size = fmt.Sprintf("%.1f MiB", float64(info.Size())/(1024*1024))
	}
	log.Printf("backup completed: %s (%s)", path, size)
	return nil
}
```

(add `time` to the import block)

```go
// cmd/dbbackup/run.go
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/Greite/database-backup/internal/config"
	"github.com/Greite/database-backup/internal/installer"
	"github.com/Greite/database-backup/internal/privileges"
	"github.com/Greite/database-backup/internal/scheduler"
)

func cmdRun(args []string) int {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	cfgPath := fs.String("config", defaultConfigPath, "config file path")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	log.Println("Starting Database Backup Container (v2)...")

	cfg, err := loadConfig(*cfgPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	// Phase 1 (root): install required client tools, then drop privileges.
	if privileges.NeedsDrop() {
		if err := installer.Install(installer.Requirements(cfg)); err != nil {
			fmt.Fprintln(os.Stderr, "installing clients:", err)
			return 1
		}
		log.Printf("dropping privileges to uid %d", privileges.UID)
		if err := privileges.DropAndReexec(backupRoot); err != nil {
			fmt.Fprintln(os.Stderr, "privilege drop:", err)
			return 1
		}
		// DropAndReexec exits the process on success.
	}

	// Phase 2 (unprivileged): schedule and run.
	sched := scheduler.New(cfg.ShutdownGrace)
	for _, j := range cfg.Jobs {
		job := j // capture
		err := sched.Add(job.Schedule, func(ctx context.Context) {
			if err := runJob(ctx, cfg, job); err != nil {
				log.Printf("ERROR: %v", err)
			}
		})
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		log.Printf("scheduled %s backup of %q (%s, retention %d days)",
			job.Type, job.Database, job.Schedule, job.RetentionDays)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	log.Printf("%d job(s) scheduled, running...", len(cfg.Jobs))
	sched.Run(ctx)
	log.Println("shutdown complete")
	return 0
}
```

Update `cmd/dbbackup/main.go`: no change needed beyond what Task 1 created (commands register themselves via `init()` in this package).

- [ ] **Step 4: Run tests, build, vet**

Run: `go test ./... && GOOS=linux go build ./... && gofmt -l . && go vet ./...`
Expected: PASS, clean build.

- [ ] **Step 5: Manual sanity check of migrate**

Run: `go run ./cmd/dbbackup migrate backups.conf.example | head -30`
Expected: YAML with the example jobs, `skipped:` lines empty, exit 0.

- [ ] **Step 6: Commit**

```bash
git add cmd/
git commit -m "Feature - #BKP-NoId - Wire run, healthcheck, backup, validate and migrate subcommands"
```

---

### Task 18: Integration tests (testcontainers)

**Files:**
- Create: `internal/integration/postgres_test.go`
- Create: `internal/integration/mariadb_test.go`
- Create: `internal/integration/mongodb_test.go`
- Create: `internal/integration/helpers_test.go`

All files start with `//go:build integration`. They need Docker and the dump CLIs on the host, so they run in the CI container job (Task 20), not in the default `go test ./...`. Locally: `go test -tags integration ./internal/integration/`.

- [ ] **Step 1: Write the helpers and the PostgreSQL test**

```go
// internal/integration/helpers_test.go
//go:build integration

package integration

import (
	"compress/gzip"
	"context"
	"io"
	"os"
	"testing"
	"time"

	"github.com/Greite/database-backup/internal/config"
	"github.com/Greite/database-backup/internal/dumper"
)

// runBackup executes one job against a live container and returns the
// decompressed dump bytes.
func runBackup(t *testing.T, job config.Job) []byte {
	t.Helper()
	root := t.TempDir()
	r := dumper.Runner{BackupRoot: root, Now: time.Now}
	d, err := dumper.New(job)
	if err != nil {
		t.Fatal(err)
	}
	path, err := r.Run(context.Background(), job, d, nil)
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	b, err := io.ReadAll(zr)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
```

```go
// internal/integration/postgres_test.go
//go:build integration

package integration

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/Greite/database-backup/internal/config"
	"github.com/Greite/database-backup/internal/healthcheck"
)

func TestPostgresBackupAndHealthcheck(t *testing.T) {
	ctx := context.Background()
	pg, err := tcpostgres.Run(ctx, "postgres:18-alpine",
		tcpostgres.WithDatabase("appdb"),
		tcpostgres.WithUsername("u"),
		tcpostgres.WithPassword("pw"),
		tcpostgres.BasicWaitStrategies(),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer testcontainers.TerminateContainer(pg)

	host, _ := pg.Host(ctx)
	port, _ := pg.MappedPort(ctx, "5432/tcp")

	// Seed reference data.
	conn, err := pgx.Connect(ctx, pg.MustConnectionString(ctx))
	if err != nil {
		t.Fatal(err)
	}
	_, err = conn.Exec(ctx, `CREATE TABLE items (id int PRIMARY KEY, label text);
		INSERT INTO items VALUES (1, 'first'), (2, 'second');`)
	conn.Close(ctx)
	if err != nil {
		t.Fatal(err)
	}

	job := config.Job{Name: "appdb", Type: "postgres", Host: host,
		Port: port.Int(), Database: "appdb", User: "u", Password: "pw",
		PGVersion: 18, RetentionDays: 7}

	dump := string(runBackup(t, job))
	for _, want := range []string{"CREATE TABLE", "items", "first", "second"} {
		if !strings.Contains(dump, want) {
			t.Errorf("dump missing %q", want)
		}
	}

	if err := healthcheck.Ping(ctx, job); err != nil {
		t.Errorf("healthcheck against live postgres failed: %v", err)
	}
	bad := job
	bad.Password = "wrong"
	if err := healthcheck.Ping(ctx, bad); err == nil {
		t.Error("healthcheck with wrong password should fail")
	}
}
```

Note for the engineer: this test requires a host `pg_dump` 18 at `/usr/lib/postgresql/18/bin/pg_dump` (Debian layout). In CI it runs inside the built image where the path exists; skip locally if absent:

```go
	if _, err := os.Stat("/usr/lib/postgresql/18/bin/pg_dump"); err != nil {
		t.Skip("pg_dump 18 not installed at the Debian path; run inside the image")
	}
```

(place this guard at the top of `TestPostgresBackupAndHealthcheck`, with `os` imported)

- [ ] **Step 2: Write the MariaDB test**

```go
// internal/integration/mariadb_test.go
//go:build integration

package integration

import (
	"context"
	"os/exec"
	"strings"
	"testing"

	"github.com/testcontainers/testcontainers-go"
	tcmariadb "github.com/testcontainers/testcontainers-go/modules/mariadb"

	"github.com/Greite/database-backup/internal/config"
	"github.com/Greite/database-backup/internal/healthcheck"
)

func TestMariaDBBackupAndHealthcheck(t *testing.T) {
	if _, err := exec.LookPath("mysqldump"); err != nil {
		t.Skip("mysqldump not installed; run inside the image")
	}
	ctx := context.Background()
	mdb, err := tcmariadb.Run(ctx, "mariadb:11",
		tcmariadb.WithDatabase("shop"),
		tcmariadb.WithUsername("u"),
		tcmariadb.WithPassword("pw"),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer testcontainers.TerminateContainer(mdb)

	host, _ := mdb.Host(ctx)
	port, _ := mdb.MappedPort(ctx, "3306/tcp")

	// Seed via the container's own client to avoid a host dependency.
	code, _, err := mdb.Exec(ctx, []string{"mariadb", "-u", "u", "-ppw", "shop", "-e",
		"CREATE TABLE products (id int PRIMARY KEY, name varchar(50)); INSERT INTO products VALUES (1,'widget');"})
	if err != nil || code != 0 {
		t.Fatalf("seeding failed: code=%d err=%v", code, err)
	}

	job := config.Job{Name: "shop", Type: "mariadb", Host: host,
		Port: port.Int(), Database: "shop", User: "u", Password: "pw", RetentionDays: 7}

	dump := string(runBackup(t, job))
	for _, want := range []string{"CREATE TABLE", "products", "widget"} {
		if !strings.Contains(dump, want) {
			t.Errorf("dump missing %q", want)
		}
	}
	if err := healthcheck.Ping(ctx, job); err != nil {
		t.Errorf("healthcheck failed: %v", err)
	}
}
```

- [ ] **Step 3: Write the MongoDB test**

```go
// internal/integration/mongodb_test.go
//go:build integration

package integration

import (
	"archive/tar"
	"bytes"
	"context"
	"io"
	"os/exec"
	"strings"
	"testing"

	"github.com/testcontainers/testcontainers-go"
	tcmongo "github.com/testcontainers/testcontainers-go/modules/mongodb"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/Greite/database-backup/internal/config"
	"github.com/Greite/database-backup/internal/healthcheck"
)

func TestMongoDBBackupAndHealthcheck(t *testing.T) {
	if _, err := exec.LookPath("mongodump"); err != nil {
		t.Skip("mongodump not installed; run inside the image")
	}
	ctx := context.Background()
	mg, err := tcmongo.Run(ctx, "mongo:7")
	if err != nil {
		t.Fatal(err)
	}
	defer testcontainers.TerminateContainer(mg)

	uri, _ := mg.ConnectionString(ctx)
	client, err := mongo.Connect(ctx, options.Client().ApplyURI(uri))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Database("events").Collection("clicks").
		InsertOne(ctx, bson.M{"page": "home"}); err != nil {
		t.Fatal(err)
	}
	client.Disconnect(ctx)

	host, _ := mg.Host(ctx)
	port, _ := mg.MappedPort(ctx, "27017/tcp")
	job := config.Job{Name: "events", Type: "mongodb", Host: host,
		Port: port.Int(), Database: "events", RetentionDays: 7}

	// runBackup gunzips the outer layer; the result is a tar stream.
	raw := runBackup(t, job)
	tr := tar.NewReader(bytes.NewReader(raw))
	found := false
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(hdr.Name, "events/clicks.bson") {
			found = true
		}
	}
	if !found {
		t.Error("tar archive does not contain events/clicks.bson*")
	}
	if err := healthcheck.Ping(ctx, job); err != nil {
		t.Errorf("healthcheck failed: %v", err)
	}
}
```

Run: `go get github.com/testcontainers/testcontainers-go github.com/testcontainers/testcontainers-go/modules/postgres github.com/testcontainers/testcontainers-go/modules/mariadb github.com/testcontainers/testcontainers-go/modules/mongodb`

- [ ] **Step 4: Run what can run locally**

Run: `go vet -tags integration ./internal/integration/ && gofmt -l internal/integration/`
Expected: clean. Then `go test -tags integration ./internal/integration/` — tests skip cleanly or pass depending on locally installed dump tools (Docker required).

- [ ] **Step 5: Commit**

```bash
git add go.mod go.sum internal/integration/
git commit -m "Feature - #BKP-NoId - Add testcontainers integration tests for all three databases"
```

---

### Task 19: Dockerfile and removal of the bash scripts

**Files:**
- Modify: `Dockerfile` (full rewrite)
- Delete: `scripts/backup.sh`, `scripts/entrypoint.sh`, `scripts/healthcheck.sh`
- Modify: `.dockerignore`
- Modify: `compose.yml` (config mount `backups.conf` → `backups.yml`)

- [ ] **Step 1: Rewrite the Dockerfile**

```dockerfile
# syntax=docker/dockerfile:1

FROM golang:1.24 AS build
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
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    && rm -rf /var/lib/apt/lists/*

# PostgreSQL APT repository (clients are installed at startup).
# The release codename is read from os-release at build time.
RUN mkdir -p /etc/apt/keyrings \
    && . /etc/os-release \
    && { command -v curl >/dev/null || apt-get update; } \
    && apt-get install -y --no-install-recommends curl \
    && curl -fsSL https://www.postgresql.org/media/keys/ACCC4CF8.asc -o /etc/apt/keyrings/postgresql.asc \
    && echo "deb [signed-by=/etc/apt/keyrings/postgresql.asc] http://apt.postgresql.org/pub/repos/apt ${VERSION_CODENAME}-pgdg main" > /etc/apt/sources.list.d/pgdg.list \
    && apt-get purge -y curl && apt-get autoremove -y \
    && rm -rf /var/lib/apt/lists/*

# Unprivileged user the process drops to after installing clients.
RUN groupadd -g 1000 backup && useradd -u 1000 -g 1000 -M -s /usr/sbin/nologin backup \
    && mkdir -p /backups /config \
    && chown backup:backup /backups

COPY --from=build /dbbackup /usr/local/bin/dbbackup

VOLUME ["/backups", "/config"]

# start-period allows the startup client installation to finish.
HEALTHCHECK --interval=5m --timeout=30s --start-period=5m --retries=3 \
    CMD ["/usr/local/bin/dbbackup", "healthcheck"]

ENTRYPOINT ["/usr/local/bin/dbbackup"]
CMD ["run"]
```

Note: `timetzdata` is the build tag form of importing `time/tzdata` — no code change needed.

- [ ] **Step 2: Remove the bash scripts and update .dockerignore**

```bash
git rm scripts/backup.sh scripts/entrypoint.sh scripts/healthcheck.sh
```

`.dockerignore` — replace its content with:

```
.git
.github
.claude
docs
public
*.md
backups.conf
backups.conf.example
backups.yml
compose.yml
LICENSE
```

- [ ] **Step 3: Update compose.yml**

In `compose.yml`, change the config mount line from `./backups.conf:/config/backups.conf:ro` to `./backups.yml:/config/backups.yml:ro` (keep everything else as is; check the current file for the exact line).

- [ ] **Step 4: Build and smoke-test the image**

```bash
docker build -t dbbk-v2-test .
docker images dbbk-v2-test --format '{{.Size}}'
```

Expected: image builds; size noticeably below the v1 223 MB (target ~110–130 MB).

```bash
printf 'jobs:\n  - name: t\n    type: postgres\n    host: localhost\n    database: t\n    user: u\n    password: p\n    schedule: "0 2 * * *"\n' > /tmp/backups.yml
docker run --rm -v /tmp/backups.yml:/config/backups.yml:ro dbbk-v2-test validate
```

Expected: `configuration is valid`, exit 0.

- [ ] **Step 5: Commit**

```bash
git add Dockerfile .dockerignore compose.yml
git commit -m "Feature - #BKP-NoId - Replace bash scripts with Go binary in multi-stage Dockerfile"
```

---

### Task 20: CI workflows

**Files:**
- Create: `.github/workflows/go-ci.yml`
- Modify: `.github/workflows/docker-build.yml` (only if it references `scripts/`; check and adjust paths/triggers)

- [ ] **Step 1: Create the Go CI workflow**

```yaml
# .github/workflows/go-ci.yml
name: Go CI

on:
  push:
    branches: [main]
  pull_request:

jobs:
  lint-and-unit:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: "1.24"
      - name: gofmt
        run: test -z "$(gofmt -l .)" || { gofmt -l .; exit 1; }
      - name: go vet
        run: go vet ./...
      - name: golangci-lint
        uses: golangci/golangci-lint-action@v6
        with:
          version: latest
      - name: Unit tests
        run: go test ./...

  integration:
    runs-on: ubuntu-latest
    needs: lint-and-unit
    if: github.event_name == 'pull_request'
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: "1.24"
      - name: Install dump tools (Debian paths)
        run: |
          sudo install -d /usr/share/postgresql-common/pgdg
          sudo curl -fsSL https://www.postgresql.org/media/keys/ACCC4CF8.asc \
            -o /usr/share/postgresql-common/pgdg/apt.postgresql.org.asc
          echo "deb [signed-by=/usr/share/postgresql-common/pgdg/apt.postgresql.org.asc] http://apt.postgresql.org/pub/repos/apt $(lsb_release -cs)-pgdg main" \
            | sudo tee /etc/apt/sources.list.d/pgdg.list
          sudo apt-get update
          sudo apt-get install -y postgresql-client-18 mariadb-client
          wget -q "https://fastdl.mongodb.org/tools/db/mongodb-database-tools-ubuntu2204-x86_64-100.14.0.tgz" -O /tmp/mt.tgz
          sudo tar -xzf /tmp/mt.tgz -C /usr/local/bin --strip-components=2 --wildcards '*/bin/*'
      - name: Integration tests
        run: go test -tags integration -v ./internal/integration/
```

- [ ] **Step 2: Check docker-build.yml**

Read `.github/workflows/docker-build.yml`. If it triggers on `scripts/**` paths or copies scripts, update the paths to `cmd/**`, `internal/**`, `go.mod`, `go.sum`, `Dockerfile`. Keep the multi-arch (amd64/arm64) build and the existing tag scheme. Keep `base-image-check.yml` unchanged (still relevant: debian:trixie-slim).

- [ ] **Step 3: Validate locally what can be validated**

Run: `docker build -t dbbk-v2-test . && go test ./...`
Expected: both pass (workflow YAML itself is validated by GitHub on push).

- [ ] **Step 4: Commit**

```bash
git add .github/workflows/
git commit -m "Feature - #BKP-NoId - Add Go CI workflow with lint, unit and integration tests"
```

---

### Task 21: Documentation and examples

**Files:**
- Create: `backups.yml.example`
- Create: `docs/MIGRATION-V2.md`
- Modify: `README.md` (rewrite the configuration/usage sections)
- Delete: `backups.conf.example` (superseded; the migrate command still reads any v1 file)

- [ ] **Step 1: Write backups.yml.example**

```yaml
# Database backup configuration (v2)
#
# Mount this file at /config/backups.yml (chmod 600: it contains
# credentials unless you use password_file everywhere).

# Optional global defaults, overridable per job.
defaults:
  retention_days: 7
  tls: false

# Optional grace period for in-flight backups on container stop.
# shutdown_grace: 5m

# Optional at-rest encryption for every backup file.
# Method "gpg": symmetric OpenPGP, decrypt with `gpg -d file.gpg`.
# Method "age": passphrase or public-key recipients, decrypt with `age -d`.
# encryption:
#   method: gpg
#   passphrase_file: /run/secrets/backup_passphrase
# --- or, age public-key mode (container never holds the private key):
# encryption:
#   method: age
#   recipients:
#     - age1ql3z7hjy54pw3hyww5ayyfg7zqgvc7w3j2elw8zmrj2kg5sfn9aqmcac8p

jobs:
  # PostgreSQL every day at 2 AM, keep 14 days, PostgreSQL 18 client.
  - name: myapp
    type: postgres
    host: postgres-server
    database: myapp_db
    user: backup_user
    password: SecurePassword123
    schedule: "0 2 * * *"
    retention_days: 14
    pg_version: 18

  # MariaDB with a Docker secret instead of an inline password.
  - name: wordpress
    type: mariadb
    host: mariadb-server
    database: wordpress
    user: wp_backup
    password_file: /run/secrets/wp_db_password
    schedule: "0 3 * * *"

  # MongoDB without authentication (dev/test).
  - name: events
    type: mongodb
    host: mongo-server
    database: events
    schedule: "@daily"

  # Remote PostgreSQL over TLS.
  - name: webapp
    type: postgres
    host: pg.example.com
    database: webapp
    user: backup_user
    password: SecurePass
    schedule: "0 1 * * *"
    retention_days: 14
    pg_version: 17
    tls: true
```

- [ ] **Step 2: Write docs/MIGRATION-V2.md**

```markdown
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
  ensure your `/backups` host directory is writable by uid 1000 (the
  container fixes ownership of the mount point itself at startup).
- Logs go to container stdout (no `/var/log/cron.log`).
- `mongosh` is no longer downloaded: the healthcheck uses native drivers.
- `BACKUP_ENCRYPTION_PASSPHRASE` / `BACKUP_ENCRYPTION_PASSPHRASE_FILE`
  keep working unchanged and produce the same gpg-compatible files.
```

- [ ] **Step 3: Update README.md**

Rewrite the configuration section around `backups.yml` (YAML schema summary, link to `backups.yml.example`), the usage section (subcommands `run`/`validate`/`backup --job`/`migrate`), add a "Migrating from v1" link to `docs/MIGRATION-V2.md`, and update the feature list (native healthcheck, non-root runtime, gpg/age encryption, streaming compression). Keep the existing badges, images and license sections.

```bash
git rm backups.conf.example
```

- [ ] **Step 4: Commit**

```bash
git add backups.yml.example docs/MIGRATION-V2.md README.md
git commit -m "Feature - #BKP-NoId - Add v2 documentation, YAML example and migration guide"
```

---

## Release Checklist (after all tasks pass CI)

- [ ] Tag `v2.0.0`; publish image tags `2`, `2.0`, `2.0.0` (leave `latest` on v1 for now).
- [ ] Create branch `v1` from the last bash commit (security fixes only).
- [ ] Write curated release notes from the commits (user preference: no `--generate-notes`).
- [ ] Update the Unraid template in `Greite/unraid-templates` (separate repo, see spec "Release Checklist — External Repos"): config mount → `/config/backups.yml`, rewritten Overview, add optional `TZ` and `BACKUP_ENCRYPTION_PASSPHRASE` variables (`Display="advanced"`, `Mask="true"` for the passphrase).
- [ ] Switch `latest` to v2 once the migration guide is published with the release.

## Plan Self-Review Notes

- Spec coverage: config/validation (T2-T4), migrate (T5, T17), rotation-after-success (T6, T12), tar/gzip streaming (T7, T12), gpg+age (T8-T9), dumpers with no-secret-in-argv (T10-T11), native healthcheck (T13), scheduler + SIGTERM grace (T14), startup install (T15), privilege drop (T16), subcommands + v1 guard (T17), integration restore-and-compare (T18), image target + script removal (T19), gofmt/vet/lint CI (T20), docs/example/migration guide (T21), Unraid template (release checklist).
- Known simplification vs spec: T18 verifies dump *content* rather than a full restore-into-fresh-server cycle; the dump tools' own formats make content assertion sufficient for v2.0 and keeps CI fast. Noted as acceptable deviation — flag it in the release notes if it matters.
- `commands` map registration via `init()` keeps Task 1's `main.go` untouched in Task 17.
```
