// Package privileges re-executes the current binary as an
// unprivileged user once root-only startup work is done.
package privileges

import "os"

const (
	// UID/GID of the `backup` user created in the Dockerfile.
	UID = 1000
	GID = 1000

	droppedEnv = "DBBACKUP_DROPPED"
)

func shouldDrop(uid int, droppedMarker string) bool {
	return uid == 0 && droppedMarker == ""
}

// NeedsDrop reports whether the current process should re-exec
// unprivileged.
func NeedsDrop() bool {
	return shouldDrop(os.Getuid(), os.Getenv(droppedEnv))
}
