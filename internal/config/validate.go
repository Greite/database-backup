package config

import (
	"fmt"
	"regexp"

	"github.com/robfig/cron/v3"
)

// nameRe restricts values used as path components under /backups.
var nameRe = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

func validName(s string) bool {
	return nameRe.MatchString(s) && s != "." && s != ".."
}

// Validate checks the whole configuration. Errors include the job name
// (or index) and field so users can fix the file directly.
func (c *Config) Validate() error {
	if len(c.Jobs) == 0 {
		return fmt.Errorf("config: at least one job is required")
	}
	if err := c.Encryption.validate(); err != nil {
		return err
	}
	seen := map[string]bool{}
	for i, j := range c.Jobs {
		if err := j.validate(); err != nil {
			return fmt.Errorf("job %d (%q): %w", i+1, j.Name, err)
		}
		if seen[j.Name] {
			return fmt.Errorf("job %d: duplicate name %q", i+1, j.Name)
		}
		seen[j.Name] = true
	}
	return nil
}

func (j Job) validate() error {
	if !validName(j.Name) {
		return fmt.Errorf("name must match [A-Za-z0-9._-]+ and not be '.' or '..'")
	}
	switch j.Type {
	case "postgres", "mariadb", "mysql", "mongodb":
	default:
		return fmt.Errorf("type %q is not one of postgres|mariadb|mysql|mongodb", j.Type)
	}
	if j.Host == "" {
		return fmt.Errorf("host is required")
	}
	if j.Database == "" {
		return fmt.Errorf("database is required")
	}
	if j.Type != "mongodb" && j.User == "" {
		return fmt.Errorf("user is required for %s", j.Type)
	}
	if j.Password != "" && j.PasswordFile != "" {
		return fmt.Errorf("password and password_file are mutually exclusive")
	}
	if _, err := cron.ParseStandard(j.Schedule); err != nil {
		return fmt.Errorf("schedule %q: %w", j.Schedule, err)
	}
	if j.RetentionDays != nil && *j.RetentionDays < 0 {
		return fmt.Errorf("retention_days must be >= 0")
	}
	return nil
}

func (e *Encryption) validate() error {
	if e == nil {
		return nil
	}
	switch e.Method {
	case "gpg":
		if len(e.Recipients) > 0 {
			return fmt.Errorf("encryption: recipients is only valid with method \"age\"")
		}
		if e.Passphrase == "" && e.PassphraseFile == "" {
			return fmt.Errorf("encryption: method \"gpg\" requires passphrase or passphrase_file")
		}
		if e.Passphrase != "" && e.PassphraseFile != "" {
			return fmt.Errorf("encryption: passphrase and passphrase_file are mutually exclusive")
		}
		return nil
	case "age":
		set := 0
		if e.Passphrase != "" {
			set++
		}
		if e.PassphraseFile != "" {
			set++
		}
		if len(e.Recipients) > 0 {
			set++
		}
		if set == 0 {
			return fmt.Errorf("encryption: method \"age\" requires passphrase, passphrase_file or recipients")
		}
		if set > 1 {
			return fmt.Errorf("encryption: passphrase, passphrase_file and recipients are mutually exclusive")
		}
		return nil
	default:
		return fmt.Errorf("encryption: method %q is not \"gpg\" or \"age\"", e.Method)
	}
}
