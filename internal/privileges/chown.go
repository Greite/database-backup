package privileges

import (
	"io/fs"
	"os"
	"path/filepath"
)

// This file has no build constraint on purpose: the logic is platform
// agnostic and lives here (rather than in the linux-only privileges.go)
// so the symlink behaviour can be exercised by tests on the dev machine.
// chownTree is only ever called from DropAndReexec, which is linux-only.

// chownTree hands the whole backup tree to the unprivileged user.
// v1 ran as root and left root-owned job directories behind; without
// this, migrated deployments silently stop producing backups.
func chownTree(root string) error {
	return chownTreeAs(root, UID, GID)
}

// chownTreeAs re-owns every entry under root to uid/gid.
//
// It uses os.Lchown rather than os.Chown so a symlink entry has the link
// itself re-owned instead of its target. chownTree runs as root during
// the startup phase, before privileges are dropped, and /backups is a
// persistent volume writable by the unprivileged identity. os.Chown
// follows symlinks (chown(2)), so a symlink planted under the tree would
// let this root-phase pass chown an arbitrary file outside it — an
// arbitrary-chown-as-root primitive that defeats the privilege drop the
// rest of this package enforces. os.Lchown (lchown(2)) never dereferences
// the final path component, so a planted link's target is left untouched.
//
// A swapped parent directory component mid-walk stays theoretically racy,
// but it could only re-own a file to the unprivileged 1000:1000 (not to
// root), so it is not an escalation and is out of scope here; closing it
// would need openat2(RESOLVE_BENEATH), which the standard library's chown
// does not expose.
func chownTreeAs(root string, uid, gid int) error {
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		return os.Lchown(path, uid, gid)
	})
}
