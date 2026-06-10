package dumper

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/Greite/database-backup/internal/config"
)

type postgres struct{ job config.Job }

func newPostgres(j config.Job) postgres { return postgres{job: j} }

func (postgres) Ext() string { return ".sql.gz" }

// path returns the version-specific pg_dump binary, as installed by
// the postgresql-client-<N> Debian packages.
func (p postgres) path() string {
	return fmt.Sprintf("/usr/lib/postgresql/%d/bin/pg_dump", p.job.PGVersion)
}

func (p postgres) args() []string {
	return []string{
		"-h", p.job.Host,
		"-p", fmt.Sprint(p.job.Port),
		"-U", p.job.User,
		"-d", p.job.Database,
	}
}

// env carries the password and TLS mode so they never appear in argv.
func (p postgres) env() []string {
	env := []string{"PGPASSWORD=" + p.job.Password}
	if p.job.IsTLS() {
		env = append(env, "PGSSLMODE=require")
	}
	return env
}

func (p postgres) Dump(ctx context.Context, w io.Writer) error {
	if _, err := os.Stat(p.path()); err != nil {
		return fmt.Errorf("PostgreSQL %d client is not installed (%s)", p.job.PGVersion, p.path())
	}
	return runTool(ctx, w, p.path(), p.args(), p.env())
}
