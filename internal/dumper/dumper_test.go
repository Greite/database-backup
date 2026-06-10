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
