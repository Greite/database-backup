// internal/dumper/mongodb.go
package dumper

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/Greite/database-backup/internal/archive"
	"github.com/Greite/database-backup/internal/config"
)

type mongodb struct{ job config.Job }

func newMongoDB(j config.Job) mongodb { return mongodb{job: j} }

func (mongodb) Ext() string { return ".tar.gz" }

func (m mongodb) args(outDir, configFile string) []string {
	args := []string{
		"--host", m.job.Host,
		"--port", fmt.Sprint(m.job.Port),
		"--db", m.job.Database,
		"--out", outDir,
		"--gzip",
	}
	if m.job.IsTLS() {
		args = append(args, "--ssl")
	}
	if m.job.User != "" && m.job.Password != "" {
		args = append(args,
			"--username", m.job.User,
			"--authenticationDatabase", "admin",
			"--config", configFile,
		)
	}
	return args
}

// writePasswordConfig writes the password to a 600-mode YAML config
// file inside dir so it never appears in /proc/*/cmdline.
func (m mongodb) writePasswordConfig(dir string) (path string, cleanup func(), err error) {
	f, err := os.CreateTemp(dir, "mongodump-*.yaml")
	if err != nil {
		return "", nil, err
	}
	esc := strings.ReplaceAll(m.job.Password, `\`, `\\`)
	esc = strings.ReplaceAll(esc, `"`, `\"`)
	_, err = fmt.Fprintf(f, "password: \"%s\"\n", esc)
	if err == nil {
		err = f.Chmod(0o600)
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		_ = os.Remove(f.Name())
		return "", nil, err
	}
	return f.Name(), func() { _ = os.Remove(f.Name()) }, nil
}

func (m mongodb) Dump(ctx context.Context, w io.Writer) error {
	tmp, err := os.MkdirTemp("", "mongodump-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(tmp) }()

	configFile := ""
	if m.job.User != "" && m.job.Password != "" {
		path, cleanup, err := m.writePasswordConfig(tmp)
		if err != nil {
			return err
		}
		defer cleanup()
		configFile = path
	}
	outDir := tmp + "/dump"
	if err := runTool(ctx, io.Discard, "mongodump", m.args(outDir, configFile), nil); err != nil {
		return err
	}
	if _, err := os.Stat(filepath.Join(outDir, m.job.Database)); err != nil {
		return fmt.Errorf("mongodump did not produce a %q directory", m.job.Database)
	}
	return archive.TarDir(outDir, w)
}
