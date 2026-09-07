package dumper

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
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
	for _, a := range d.args() {
		if strings.Contains(a, "pw") {
			t.Errorf("password leaked into argv: %q", a)
		}
	}
}

func TestNewSelectsImplementation(t *testing.T) {
	for _, typ := range []string{"postgres", "mariadb", "mysql", "mongodb", "sqlite"} {
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

func TestSQLiteCommand(t *testing.T) {
	d := newSQLite(config.Job{Name: "vw", Type: "sqlite", Path: "/sources/vaultwarden/db.sqlite3"})
	if got, want := strings.Join(d.args(), " "), "-readonly /sources/vaultwarden/db.sqlite3 .dump"; got != want {
		t.Errorf("args = %q, want %q", got, want)
	}
	if d.Ext() != ".sql.gz" {
		t.Errorf("Ext = %q, want .sql.gz", d.Ext())
	}
}

func TestTailKeepsLastBytesAcrossWrites(t *testing.T) {
	var out bytes.Buffer
	tw := &tail{w: &out}
	for _, chunk := range []string{"BEGIN TRANSACTION;\n", "INSERT INTO a VALUES(1);\nCOM", "MIT;\n"} {
		if _, err := tw.Write([]byte(chunk)); err != nil {
			t.Fatal(err)
		}
	}
	if !bytes.HasSuffix(tw.last, []byte("COMMIT;\n")) {
		t.Errorf("tail = %q, want suffix COMMIT;", tw.last)
	}
	if out.Len() != len("BEGIN TRANSACTION;\nINSERT INTO a VALUES(1);\nCOMMIT;\n") {
		t.Errorf("underlying writer got %d bytes", out.Len())
	}
}

// sqlite3 exits 0 when .dump fails and ends the output with
// "ROLLBACK; -- due to errors" instead of "COMMIT;". A WAL database
// whose -wal/-shm files are missing and cannot be created (Vaultwarden
// stopped, read-only mount) is the real-world trigger.
func TestSQLiteDumpFailsWhenDumpRollsBack(t *testing.T) {
	if _, err := exec.LookPath("sqlite3"); err != nil {
		t.Skip("sqlite3 not installed")
	}
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	dir := t.TempDir()
	db := filepath.Join(dir, "db.sqlite3")
	if out, err := exec.Command("sqlite3", db, "PRAGMA journal_mode=wal; CREATE TABLE a(x); INSERT INTO a VALUES(1);").CombinedOutput(); err != nil {
		t.Fatalf("seeding: %v (%s)", err, out)
	}
	_ = os.Remove(db + "-wal")
	_ = os.Remove(db + "-shm")
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

	d := newSQLite(config.Job{Name: "vw", Type: "sqlite", Path: db})
	var out bytes.Buffer
	err := d.Dump(context.Background(), &out)
	if err == nil {
		t.Fatalf("Dump succeeded on a rolled-back dump:\n%s", out.String())
	}
	// The exact message differs by platform ("unable to open database
	// file" on macOS, "attempt to write a readonly database" on Linux).
	if !strings.Contains(err.Error(), "sql error") {
		t.Errorf("error %q should carry sqlite3's stderr", err)
	}
}
