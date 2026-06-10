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
		t.Skip("mysqldump not installed; run in CI")
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
		Port: int(port.Num()), Database: "shop", User: "u", Password: "pw", RetentionDays: intPtr(7)}

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
