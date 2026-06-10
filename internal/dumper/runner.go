package dumper

import (
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/Greite/database-backup/internal/config"
	"github.com/Greite/database-backup/internal/crypto"
	"github.com/Greite/database-backup/internal/rotation"
)

// Runner executes one backup job end to end.
type Runner struct {
	BackupRoot string           // usually /backups
	Now        func() time.Time // injected for tests
}

// Run dumps the job through gzip and optional encryption into
// <BackupRoot>/<type>/<name>/<name>_<timestamp><ext>, writing to a
// .tmp file renamed only on success. Rotation runs only after success.
// It returns the final file path.
func (r Runner) Run(ctx context.Context, job config.Job, d Dumper, enc crypto.Encryptor) (string, error) {
	dir := filepath.Join(r.BackupRoot, job.Type, job.Name)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("job %q: %w", job.Name, err)
	}

	ext := d.Ext()
	if enc != nil {
		ext += enc.Ext()
	}
	ts := r.Now().Format("20060102_150405")
	final := filepath.Join(dir, fmt.Sprintf("%s_%s%s", job.Name, ts, ext))
	tmp := final + ".tmp"

	if err := r.writeBackup(ctx, d, enc, tmp); err != nil {
		_ = os.Remove(tmp)
		return "", fmt.Errorf("job %q: %w", job.Name, err)
	}
	if err := os.Rename(tmp, final); err != nil {
		_ = os.Remove(tmp)
		return "", fmt.Errorf("job %q: %w", job.Name, err)
	}

	// Purge also eventually removes orphan .tmp files from interrupted
	// runs: the suffix match includes them once they age past retention.
	deleted, remaining, err := rotation.Purge(dir, d.Ext(), job.RetentionDaysValue(), r.Now())
	if err != nil {
		// The backup itself succeeded; report rotation problems without failing the job.
		log.Printf("job %q: rotation error: %v", job.Name, err)
	} else if deleted > 0 {
		log.Printf("job %q: rotated %d old backup(s), %d remaining", job.Name, deleted, remaining)
	}
	return final, nil
}

func (r Runner) writeBackup(ctx context.Context, d Dumper, enc crypto.Encryptor, path string) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	// Build the write chain: dump -> gzip -> [encrypt] -> file.
	var sink = io.Writer(f)
	var encW io.WriteCloser
	if enc != nil {
		if encW, err = enc.Wrap(f); err != nil {
			return err
		}
		sink = encW
	}
	zw := gzip.NewWriter(sink)

	if err := d.Dump(ctx, zw); err != nil {
		return err
	}
	if err := zw.Close(); err != nil {
		return err
	}
	if encW != nil {
		if err := encW.Close(); err != nil {
			return err
		}
	}
	return f.Sync()
}
