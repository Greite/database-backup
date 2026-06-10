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
