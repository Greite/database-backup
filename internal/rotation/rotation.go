// Package rotation deletes backups older than the retention window.
package rotation

import (
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Purge deletes files in dir whose name contains suffix (this also
// matches encrypted variants like suffix+".gpg"/".age") and whose
// mtime is older than retentionDays before now. retentionDays == 0
// disables rotation. It returns deleted and remaining match counts.
func Purge(dir, suffix string, retentionDays int, now time.Time) (deleted, remaining int, err error) {
	if retentionDays <= 0 {
		return 0, 0, nil
	}
	cutoff := now.AddDate(0, 0, -retentionDays)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, 0, err
	}
	for _, e := range entries {
		if e.IsDir() || !strings.Contains(e.Name(), suffix) {
			continue
		}
		info, err := e.Info()
		if err != nil {
			return deleted, remaining, err
		}
		if info.ModTime().Before(cutoff) {
			if err := os.Remove(filepath.Join(dir, e.Name())); err != nil {
				return deleted, remaining, err
			}
			deleted++
			continue
		}
		remaining++
	}
	return deleted, remaining, nil
}
