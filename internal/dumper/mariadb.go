package dumper

import (
	"context"
	"fmt"
	"io"

	"github.com/Greite/database-backup/internal/config"
)

type mariadb struct{ job config.Job }

func newMariaDB(j config.Job) mariadb { return mariadb{job: j} }

func (mariadb) Ext() string { return ".sql.gz" }

func (m mariadb) args() []string {
	args := []string{
		"-h", m.job.Host,
		"-P", fmt.Sprint(m.job.Port),
		"-u", m.job.User,
	}
	if m.job.IsTLS() {
		args = append(args, "--ssl")
	}
	return append(args, m.job.Database)
}

func (m mariadb) env() []string {
	return []string{"MYSQL_PWD=" + m.job.Password}
}

func (m mariadb) Dump(ctx context.Context, w io.Writer) error {
	return runTool(ctx, w, "mysqldump", m.args(), m.env())
}
