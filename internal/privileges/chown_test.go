//go:build !windows

package privileges

import (
	"os"
	"path/filepath"
	"testing"
)

// TestChownTreeDoesNotFollowSymlinks guards the privilege-drop boundary.
// chownTree runs as root over the (untrusted) backup volume, so it must
// not follow a symlink planted there. We point a symlink at a non-existent
// target and re-own the tree: with os.Lchown the link itself is re-owned
// and the walk succeeds, whereas the previous os.Chown would dereference
// the dangling link and fail with ENOENT — so reverting the fix makes
// this test fail.
func TestChownTreeDoesNotFollowSymlinks(t *testing.T) {
	root := t.TempDir()

	if err := os.WriteFile(filepath.Join(root, "dump.sql"), []byte("data"), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if err := os.Symlink("/nonexistent/escape-target", filepath.Join(root, "evil")); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	// Re-own to our own uid/gid: a no-op ownership change needs no
	// CAP_CHOWN, so the test runs unprivileged and has no side effects.
	if err := chownTreeAs(root, os.Getuid(), os.Getgid()); err != nil {
		t.Fatalf("chownTreeAs followed a symlink (want lchown semantics): %v", err)
	}
}
