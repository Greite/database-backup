// Package dumper runs the external database dump tools and streams
// their raw output.
package dumper

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/Greite/database-backup/internal/config"
)

// Dumper streams a raw (uncompressed) dump of one job to w.
type Dumper interface {
	Dump(ctx context.Context, w io.Writer) error
	// Ext is the final filename extension of the compressed pipeline
	// (".sql.gz" or ".tar.gz"), before any encryption extension.
	Ext() string
}

// New returns the Dumper for the job's database type.
func New(job config.Job) (Dumper, error) {
	switch job.Type {
	case "postgres":
		return newPostgres(job), nil
	case "mariadb", "mysql":
		return newMariaDB(job), nil
	case "mongodb":
		return newMongoDB(job), nil
	case "sqlite":
		return newSQLite(job), nil
	}
	return nil, fmt.Errorf("unknown database type %q", job.Type)
}

// runTool execs the tool, streaming stdout to w. Stderr is captured
// and included in the returned error so failures are diagnosable; it
// is also returned for tools that report errors without a non-zero exit.
func runTool(ctx context.Context, w io.Writer, path string, args, env []string) (string, error) {
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Stdout = w
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	cmd.Env = append(os.Environ(), env...)
	err := cmd.Run()
	msg := strings.TrimSpace(stderr.String())
	if err != nil {
		return msg, fmt.Errorf("%s: %w (stderr: %s)", filepath.Base(path), err, msg)
	}
	return msg, nil
}
