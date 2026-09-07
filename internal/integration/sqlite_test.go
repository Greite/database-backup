//go:build integration

package integration

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Greite/database-backup/internal/config"
	"github.com/Greite/database-backup/internal/healthcheck"
)

// No container here: SQLite is a file, so this test only needs the CLI.
func TestSQLiteBackupAndHealthcheck(t *testing.T) {
	if _, err := exec.LookPath("sqlite3"); err != nil {
		t.Skip("sqlite3 not installed; run in CI")
	}
	db := filepath.Join(t.TempDir(), "db.sqlite3")
	// WAL mode like Vaultwarden; the side files stay because the seeding
	// connection is closed, so the dump exercises the plain path.
	out, err := exec.Command("sqlite3", db,
		"PRAGMA journal_mode=wal; CREATE TABLE users (id INTEGER PRIMARY KEY, email TEXT); INSERT INTO users VALUES (1, 'alice@example.com');").CombinedOutput()
	if err != nil {
		t.Fatalf("seeding failed: %v (%s)", err, out)
	}

	job := config.Job{Name: "vault", Type: "sqlite", Path: db, RetentionDays: intPtr(7)}

	dump := string(runBackup(t, job))
	for _, want := range []string{"CREATE TABLE users", "alice@example.com", "COMMIT;"} {
		if !strings.Contains(dump, want) {
			t.Errorf("dump missing %q", want)
		}
	}
	if err := healthcheck.Ping(context.Background(), job); err != nil {
		t.Errorf("healthcheck failed: %v", err)
	}
}
