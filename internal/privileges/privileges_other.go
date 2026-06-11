//go:build !linux

package privileges

import "fmt"

// DropAndReexec is only supported on linux (the container platform).
func DropAndReexec(backupRoot string) error {
	return fmt.Errorf("privilege drop is only supported on linux")
}
