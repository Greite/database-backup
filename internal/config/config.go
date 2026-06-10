// Package config loads and validates the v2 YAML configuration.
package config

import (
	"fmt"
	"io"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	DefaultRetentionDays = 7
	DefaultPGVersion     = 18
	DefaultShutdownGrace = 5 * time.Minute
)

type Config struct {
	Defaults      Defaults      `yaml:"defaults"`
	ShutdownGrace time.Duration `yaml:"shutdown_grace"`
	Encryption    *Encryption   `yaml:"encryption"`
	Jobs          []Job         `yaml:"jobs"`
}

type Defaults struct {
	RetentionDays *int  `yaml:"retention_days"`
	TLS           *bool `yaml:"tls"`
}

type Encryption struct {
	Method         string   `yaml:"method"` // "gpg" or "age"
	Passphrase     string   `yaml:"passphrase"`
	PassphraseFile string   `yaml:"passphrase_file"`
	Recipients     []string `yaml:"recipients"`
}

type Job struct {
	Name          string `yaml:"name"`
	Type          string `yaml:"type"` // postgres|mariadb|mysql|mongodb
	Host          string `yaml:"host"`
	Port          int    `yaml:"port"`
	Database      string `yaml:"database"`
	User          string `yaml:"user"`
	Password      string `yaml:"password"`
	PasswordFile  string `yaml:"password_file"`
	Schedule      string `yaml:"schedule"`
	RetentionDays int    `yaml:"retention_days"`
	PGVersion     int    `yaml:"pg_version"`
	TLS           *bool  `yaml:"tls"`
}

// IsTLS reports whether the job requires an encrypted connection.
func (j Job) IsTLS() bool { return j.TLS != nil && *j.TLS }

func defaultPort(dbType string) int {
	switch dbType {
	case "postgres":
		return 5432
	case "mariadb", "mysql":
		return 3306
	case "mongodb":
		return 27017
	}
	return 0
}

// Parse decodes the YAML, rejects unknown fields, and applies defaults.
// It does NOT validate; call Validate on the result.
func Parse(r io.Reader) (*Config, error) {
	dec := yaml.NewDecoder(r)
	dec.KnownFields(true)
	var cfg Config
	if err := dec.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}
	applyDefaults(&cfg)
	return &cfg, nil
}

func applyDefaults(cfg *Config) {
	if cfg.ShutdownGrace == 0 {
		cfg.ShutdownGrace = DefaultShutdownGrace
	}
	for i := range cfg.Jobs {
		j := &cfg.Jobs[i]
		if j.Port == 0 {
			j.Port = defaultPort(j.Type)
		}
		if j.RetentionDays == 0 {
			if cfg.Defaults.RetentionDays != nil {
				j.RetentionDays = *cfg.Defaults.RetentionDays
			} else {
				j.RetentionDays = DefaultRetentionDays
			}
		}
		if j.TLS == nil {
			j.TLS = cfg.Defaults.TLS
		}
		if j.Type == "postgres" && j.PGVersion == 0 {
			j.PGVersion = DefaultPGVersion
		}
	}
}

// Load reads and parses the config file at path.
func Load(path string) (*Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return Parse(f)
}
