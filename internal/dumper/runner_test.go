package dumper

import (
	"compress/gzip"
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
