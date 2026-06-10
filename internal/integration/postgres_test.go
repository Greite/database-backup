//go:build integration

package integration

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/Greite/database-backup/internal/config"
	"github.com/Greite/database-backup/internal/healthcheck"
)

func TestPostgresBackupAndHealthcheck(t *testing.T) {
	if _, err := os.Stat("/usr/lib/postgresql/18/bin/pg_dump"); err != nil {
		t.Skip("pg_dump 18 not installed at the Debian path; run in CI")
	}
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
		Port: int(port.Num()), Database: "appdb", User: "u", Password: "pw",
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
