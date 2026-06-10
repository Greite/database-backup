// Package healthcheck verifies connectivity to every configured
// database using native Go drivers (no CLI clients required).
package healthcheck

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"strings"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/Greite/database-backup/internal/config"
)

const pingTimeout = 5 * time.Second

func postgresDSN(j config.Job) string {
	sslmode := "prefer"
	if j.IsTLS() {
		sslmode = "require"
	}
	// Single-quote the password so spaces and special characters survive keyword/value parsing.
	pw := "'" + strings.ReplaceAll(j.Password, "'", `\'`) + "'"
	return fmt.Sprintf("host=%s port=%d dbname=%s user=%s password=%s sslmode=%s",
		j.Host, j.Port, j.Database, j.User, pw, sslmode)
}

func mysqlDSN(j config.Job) string {
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%d)/%s", j.User, j.Password, j.Host, j.Port, j.Database)
	if j.IsTLS() {
		dsn += "?tls=skip-verify"
	}
	return dsn
}

func mongoURI(j config.Job) string {
	cred := ""
	auth := ""
	if j.User != "" && j.Password != "" {
		// url.UserPassword encodes space as %20, @ as %40, / as %2F — matches the expected format.
		cred = url.UserPassword(j.User, j.Password).String() + "@"
		auth = "&authSource=admin"
	}
	uri := fmt.Sprintf("mongodb://%s%s:%d/?connectTimeoutMS=5000%s", cred, j.Host, j.Port, auth)
	if j.IsTLS() {
		uri += "&tls=true"
	}
	return uri
}

// Ping checks one job's database connectivity.
func Ping(ctx context.Context, j config.Job) error {
	ctx, cancel := context.WithTimeout(ctx, pingTimeout)
	defer cancel()
	switch j.Type {
	case "postgres":
		conn, err := pgx.Connect(ctx, postgresDSN(j))
		if err != nil {
			return err
		}
		defer conn.Close(ctx)
		return conn.Ping(ctx)
	case "mariadb", "mysql":
		db, err := sql.Open("mysql", mysqlDSN(j))
		if err != nil {
			return err
		}
		defer db.Close()
		return db.PingContext(ctx)
	case "mongodb":
		client, err := mongo.Connect(ctx, options.Client().ApplyURI(mongoURI(j)))
		if err != nil {
			return err
		}
		defer client.Disconnect(ctx)
		return client.Ping(ctx, nil)
	}
	return fmt.Errorf("unknown database type %q", j.Type)
}

// Check pings every job and returns one error per failing job.
func Check(ctx context.Context, cfg *config.Config) []error {
	var errs []error
	for _, j := range cfg.Jobs {
		if err := Ping(ctx, j); err != nil {
			errs = append(errs, fmt.Errorf("job %q (%s on %s:%d): %w", j.Name, j.Type, j.Host, j.Port, err))
		}
	}
	return errs
}
