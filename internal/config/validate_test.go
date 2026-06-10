package config

import (
	"strings"
	"testing"
)

func validJob() Job {
	return Job{Name: "app", Type: "postgres", Host: "db", Port: 5432,
		Database: "app", User: "u", Password: "p", Schedule: "0 2 * * *",
		RetentionDays: 7, PGVersion: 18}
}

func TestValidateAcceptsValidConfig(t *testing.T) {
	cfg := &Config{Jobs: []Job{validJob()}}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() = %v, want nil", err)
	}
}

func TestValidateRejectsBadInput(t *testing.T) {
	cases := []struct {
		desc   string
		mutate func(*Config)
		want   string // substring expected in the error
	}{
		{"missing name", func(c *Config) { c.Jobs[0].Name = "" }, "name"},
		{"path traversal name", func(c *Config) { c.Jobs[0].Name = ".." }, "name"},
		{"name with slash", func(c *Config) { c.Jobs[0].Name = "a/b" }, "name"},
		{"unknown type", func(c *Config) { c.Jobs[0].Type = "oracle" }, "type"},
		{"missing host", func(c *Config) { c.Jobs[0].Host = "" }, "host"},
		{"missing database", func(c *Config) { c.Jobs[0].Database = "" }, "database"},
		{"missing user non-mongo", func(c *Config) { c.Jobs[0].User = "" }, "user"},
		{"bad schedule", func(c *Config) { c.Jobs[0].Schedule = "every day" }, "schedule"},
		{"password and password_file", func(c *Config) { c.Jobs[0].PasswordFile = "/run/s" }, "password"},
		{"duplicate names", func(c *Config) { c.Jobs = append(c.Jobs, validJob()) }, "duplicate"},
		{"bad encryption method", func(c *Config) {
			c.Encryption = &Encryption{Method: "rot13", Passphrase: "x"}
		}, "method"},
		{"recipients with gpg", func(c *Config) {
			c.Encryption = &Encryption{Method: "gpg", Recipients: []string{"age1x"}}
		}, "recipients"},
		{"encryption without secret", func(c *Config) {
			c.Encryption = &Encryption{Method: "gpg"}
		}, "passphrase"},
		{"passphrase and file", func(c *Config) {
			c.Encryption = &Encryption{Method: "gpg", Passphrase: "a", PassphraseFile: "/f"}
		}, "passphrase"},
		{"negative retention", func(c *Config) { c.Jobs[0].RetentionDays = -1 }, "retention_days"},
	}
	for _, tc := range cases {
		cfg := &Config{Jobs: []Job{validJob()}}
		tc.mutate(cfg)
		err := cfg.Validate()
		if err == nil {
			t.Errorf("%s: want error, got nil", tc.desc)
			continue
		}
		if !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: error %q does not mention %q", tc.desc, err, tc.want)
		}
	}
}

func TestValidateMongoWithoutAuthIsOK(t *testing.T) {
	j := validJob()
	j.Type, j.Port, j.User, j.Password = "mongodb", 27017, "", ""
	cfg := &Config{Jobs: []Job{j}}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() = %v, want nil", err)
	}
}

func TestValidateRequiresAtLeastOneJob(t *testing.T) {
	if err := (&Config{}).Validate(); err == nil {
		t.Fatal("want error for empty jobs, got nil")
	}
}

func TestValidateAcceptsAgeRecipients(t *testing.T) {
	cfg := &Config{
		Encryption: &Encryption{Method: "age", Recipients: []string{"age1ql3z7hjy54pw3hyww5ayyfg7zqgvc7w3j2elw8zmrj2kg5sfn9aqmcac8p"}},
		Jobs:       []Job{validJob()},
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() = %v, want nil", err)
	}
}
