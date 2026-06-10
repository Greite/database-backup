package healthcheck

import (
	"strings"
	"testing"

	mysql "github.com/go-sql-driver/mysql"

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

	j2 := config.Job{Type: "mariadb", Host: "m", Port: 3307, Database: "wp", User: "u", Password: "p/w@x"}
	dsn := mysqlDSN(j2)
	parsed, err := mysql.ParseDSN(dsn)
	if err != nil {
		t.Fatalf("ParseDSN(%q) = %v", dsn, err)
	}
	if parsed.Passwd != "p/w@x" || parsed.DBName != "wp" {
		t.Errorf("round-trip: passwd=%q db=%q, want p/w@x and wp", parsed.Passwd, parsed.DBName)
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
