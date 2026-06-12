//go:build linux

package privileges

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
)

// DropAndReexec chowns the backup root for the target user, then
// re-executes the current binary (same args) as uid/gid 1000. On
// success it never returns: the parent waits for the child and exits
// with its code.
//
// Signal forwarding: SIGTERM and SIGINT received by the parent are
// forwarded to the child process so that docker stop / ctrl-c reach
// the unprivileged child cleanly. Without this the child would be
// orphaned when the parent exits on a signal.
func DropAndReexec(backupRoot string) error {
	// Defence in depth: never re-exec when already dropped.
	if !NeedsDrop() {
		return nil
	}

	if err := chownTree(backupRoot); err != nil {
		return fmt.Errorf("chown %s: %w", backupRoot, err)
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := exec.Command(exe, os.Args[1:]...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	cmd.Env = append(os.Environ(), droppedEnv+"=1")
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Credential: &syscall.Credential{Uid: UID, Gid: GID},
	}
	if err := cmd.Start(); err != nil {
		return err
	}

	// Forward SIGTERM and SIGINT to the child so docker stop / ctrl-c
	// reach the unprivileged process cleanly instead of orphaning it.
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		for s := range sigs {
			if cmd.Process != nil {
				_ = cmd.Process.Signal(s)
			}
		}
	}()

	if err := cmd.Wait(); err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			code := ee.ExitCode()
			if code == -1 {
				// Child was killed by a signal: follow the 128+signum
				// convention so orchestrators interpret it correctly.
				if ws, ok := ee.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
					code = 128 + int(ws.Signal())
				} else {
					code = 255
				}
			}
			os.Exit(code)
		}
		signal.Stop(sigs)
		return err
	}
	os.Exit(0)
	return nil
}
