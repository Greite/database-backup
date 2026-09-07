package dumper

import (
	"bytes"
	"context"
	"fmt"
	"io"

	"github.com/Greite/database-backup/internal/config"
)

type sqlite struct{ job config.Job }

func newSQLite(j config.Job) sqlite { return sqlite{job: j} }

func (sqlite) Ext() string { return ".sql.gz" }

// args opens the file read-only: the dump must never checkpoint or
// otherwise write into the owning application's directory.
func (s sqlite) args() []string {
	return []string{"-readonly", s.job.Path, ".dump"}
}

// dumpOK is how sqlite3 ends a successful .dump; on failure it ends
// with "ROLLBACK; -- due to errors" but still exits 0.
const dumpOK = "COMMIT;\n"

func (s sqlite) Dump(ctx context.Context, w io.Writer) error {
	tw := &tail{w: w}
	stderr, err := runTool(ctx, tw, "sqlite3", s.args(), nil)
	if err != nil {
		return err
	}
	if !bytes.HasSuffix(tw.last, []byte(dumpOK)) {
		return fmt.Errorf("sqlite3: dump did not end with COMMIT (stderr: %s)", stderr)
	}
	return nil
}

// tail forwards writes and remembers the last bytes, enough to see how
// the stream ended.
type tail struct {
	w    io.Writer
	last []byte
}

func (t *tail) Write(p []byte) (int, error) {
	n, err := t.w.Write(p)
	t.last = append(t.last, p[:n]...)
	if len(t.last) > 2*len(dumpOK) {
		t.last = t.last[len(t.last)-2*len(dumpOK):]
	}
	return n, err
}
