// Package privileges re-executes the current binary as an
// unprivileged user once root-only startup work is done.
package privileges

import (
	"fmt"
	"os"
	"strconv"
)

const (
	// defaultID is the uid/gid of the `backup` user created in the Dockerfile.
	defaultID = 1000

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

// IDs returns the uid/gid to drop to: PUID/PGID when set (linuxserver.io
// convention, e.g. 99/100 on Unraid), else the image's backup user.
func IDs() (uid, gid int, err error) {
	return parseIDs(os.Getenv("PUID"), os.Getenv("PGID"))
}

func parseIDs(puid, pgid string) (uid, gid int, err error) {
	if uid, err = parseID("PUID", puid); err != nil {
		return 0, 0, err
	}
	if gid, err = parseID("PGID", pgid); err != nil {
		return 0, 0, err
	}
	return uid, gid, nil
}

// parseID rejects 0: root is never a drop target.
func parseID(name, s string) (int, error) {
	if s == "" {
		return defaultID, nil
	}
	id, err := strconv.Atoi(s)
	if err != nil || id < 1 {
		return 0, fmt.Errorf("%s: want a positive integer, got %q", name, s)
	}
	return id, nil
}
