package dumper

import (
	"context"
	"fmt"
	"io"

	"github.com/Greite/database-backup/internal/config"
)

type mongodb struct{ job config.Job }

func newMongoDB(j config.Job) mongodb { return mongodb{job: j} }

func (mongodb) Ext() string { return ".tar.gz" }

// Dump is a placeholder; the real implementation lands with the
// MongoDB dumper task.
func (m mongodb) Dump(ctx context.Context, w io.Writer) error {
	return fmt.Errorf("mongodb dumper: not implemented yet")
}
