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
		t.Skip("mongodump not installed; run in CI")
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
		Port: int(port.Num()), Database: "events", RetentionDays: 7}

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
