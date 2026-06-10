package config

import (
	"strings"
	"testing"
	"time"
)

const minimalYAML = `
jobs:
  - name: myapp
    type: postgres
    host: db
    database: myapp_db
    user: backup
    password: secret
    schedule: "0 2 * * *"
`

func TestParseAppliesDefaults(t *testing.T) {
	cfg, err := Parse(strings.NewReader(minimalYAML))
	if err != nil {
		t.Fatal(err)
	}
	j := cfg.Jobs[0]
	if j.Port != 5432 {
		t.Errorf("Port = %d, want 5432", j.Port)
	}
	if j.RetentionDaysValue() != 7 {
		t.Errorf("RetentionDaysValue() = %d, want 7", j.RetentionDaysValue())
	}
	if j.PGVersion != 18 {
		t.Errorf("PGVersion = %d, want 18", j.PGVersion)
	}
	if cfg.ShutdownGrace != 5*time.Minute {
		t.Errorf("ShutdownGrace = %v, want 5m", cfg.ShutdownGrace)
	}
}

func TestParseDefaultsBlockOverridesBuiltins(t *testing.T) {
	y := `
defaults:
  retention_days: 30
  tls: true
jobs:
  - name: shop
    type: mariadb
    host: db
    database: shop
    user: u
    password: p
    schedule: "@daily"
`
	cfg, err := Parse(strings.NewReader(y))
	if err != nil {
		t.Fatal(err)
	}
	j := cfg.Jobs[0]
	if j.RetentionDaysValue() != 30 || !j.IsTLS() || j.Port != 3306 {
		t.Errorf("got retention=%d tls=%v port=%d, want 30 true 3306", j.RetentionDaysValue(), j.IsTLS(), j.Port)
	}
}

func TestParseJobOverridesDefaultsBlock(t *testing.T) {
	y := `
defaults:
  retention_days: 30
jobs:
  - name: m
    type: mongodb
    host: db
    database: m
    schedule: "@daily"
    retention_days: 3
`
	cfg, err := Parse(strings.NewReader(y))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Jobs[0].RetentionDaysValue() != 3 || cfg.Jobs[0].Port != 27017 {
		t.Errorf("got retention=%d port=%d, want 3 27017", cfg.Jobs[0].RetentionDaysValue(), cfg.Jobs[0].Port)
	}
}

func TestParseRetentionZeroDisablesRotation(t *testing.T) {
	y := `
jobs:
  - name: noop
    type: postgres
    host: db
    database: noop
    user: u
    password: p
    schedule: "@daily"
    retention_days: 0
`
	cfg, err := Parse(strings.NewReader(y))
	if err != nil {
		t.Fatal(err)
	}
	j := cfg.Jobs[0]
	if j.RetentionDays == nil {
		t.Fatal("RetentionDays should be a non-nil pointer for explicit 0")
	}
	if *j.RetentionDays != 0 {
		t.Errorf("*RetentionDays = %d, want 0 (rotation disabled)", *j.RetentionDays)
	}
	if j.RetentionDaysValue() != 0 {
		t.Errorf("RetentionDaysValue() = %d, want 0", j.RetentionDaysValue())
	}
}

func TestParseRejectsUnknownFields(t *testing.T) {
	if _, err := Parse(strings.NewReader("jobs:\n  - nme: typo\n")); err == nil {
		t.Fatal("want error on unknown field, got nil")
	}
}
