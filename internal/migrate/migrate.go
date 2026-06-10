// Package migrate converts a v1 pipe-delimited backups.conf into the
// v2 YAML configuration.
package migrate

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/Greite/database-backup/internal/config"
	"gopkg.in/yaml.v3"
)

// Convert parses a v1 config. Invalid lines are returned as errors
// (with their line number) and skipped; valid lines are converted.
func Convert(r io.Reader) (*config.Config, []error) {
	cfg := &config.Config{}
	var errs []error
	used := map[string]int{}
	sc := bufio.NewScanner(r)
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		j, err := parseLine(line)
		if err != nil {
			errs = append(errs, fmt.Errorf("line %d: %w", n, err))
			continue
		}
		j.Name = uniqueName(j.Database, used)
		cfg.Jobs = append(cfg.Jobs, j)
	}
	if err := sc.Err(); err != nil {
		errs = append(errs, err)
	}
	return cfg, errs
}

func uniqueName(base string, used map[string]int) string {
	used[base]++
	if used[base] == 1 {
		return base
	}
	return fmt.Sprintf("%s-%d", base, used[base])
}

func parseLine(line string) (config.Job, error) {
	f := strings.Split(line, "|")
	// Pad optional trailing fields up to the 10 v1 columns.
	for len(f) < 10 {
		f = append(f, "")
	}
	if len(f) > 10 {
		return config.Job{}, fmt.Errorf("too many fields (%d, max 10)", len(f))
	}
	j := config.Job{
		Schedule: strings.TrimSpace(f[0]),
		Type:     strings.TrimSpace(f[1]),
		Host:     strings.TrimSpace(f[2]),
		Database: strings.TrimSpace(f[4]),
		User:     strings.TrimSpace(f[5]),
		Password: f[6],
	}
	switch j.Type {
	case "postgres", "mariadb", "mysql", "mongodb":
	default:
		return config.Job{}, fmt.Errorf("unknown type %q", j.Type)
	}
	if j.Schedule == "" || j.Host == "" || j.Database == "" {
		return config.Job{}, fmt.Errorf("missing required field (schedule, host or database)")
	}
	var err error
	if j.Port, err = optInt(f[3], "port"); err != nil {
		return config.Job{}, err
	}
	if j.RetentionDays, err = optInt(f[7], "retention_days"); err != nil {
		return config.Job{}, err
	}
	if j.PGVersion, err = optInt(f[8], "pg_version"); err != nil {
		return config.Job{}, err
	}
	if tls := strings.TrimSpace(f[9]); tls == "true" {
		v := true
		j.TLS = &v
	} else if tls != "" {
		return config.Job{}, fmt.Errorf("TLS must be empty or \"true\", got %q", tls)
	}
	return j, nil
}

func optInt(s, field string) (int, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, nil
	}
	v, err := strconv.Atoi(s)
	if err != nil || v < 0 {
		return 0, fmt.Errorf("%s: %q is not a positive integer", field, s)
	}
	return v, nil
}

// ToYAML renders the converted config as v2 YAML.
func ToYAML(cfg *config.Config) ([]byte, error) {
	return yaml.Marshal(cfg)
}
