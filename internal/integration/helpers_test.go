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
