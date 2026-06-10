package migrate

import (
	"strings"
	"testing"
)

const v1Sample = `# comment kept out
0 2 * * *|postgres|pg-host|5432|appdb|backup|S3cret!|14|17|true

0 3 * * *|mariadb|maria||shop|wp|pw|7
0 6 * * *|mongodb|mongo|27017|events|||7
`

func TestConvertProducesJobs(t *testing.T) {
	cfg, errs := Convert(strings.NewReader(v1Sample))
	if len(errs) != 0 {
		t.Fatalf("errs = %v, want none", errs)
	}
	if len(cfg.Jobs) != 3 {
		t.Fatalf("got %d jobs, want 3", len(cfg.Jobs))
	}
	j := cfg.Jobs[0]
	if j.Name != "appdb" || j.Type != "postgres" || j.Schedule != "0 2 * * *" ||
		j.Port != 5432 || j.User != "backup" || j.Password != "S3cret!" ||
		j.RetentionDays != 14 || j.PGVersion != 17 || !j.IsTLS() {
		t.Errorf("unexpected first job: %+v", j)
	}
	if cfg.Jobs[1].Port != 0 {
		t.Errorf("empty v1 PORT should stay 0 (filled by defaults), got %d", cfg.Jobs[1].Port)
	}
}

func TestConvertDeduplicatesNames(t *testing.T) {
	in := "0 4 * * *|postgres|h|5432|db1|u|p|7\n0 5 * * *|postgres|h2|5432|db1|u|p|7\n"
	cfg, errs := Convert(strings.NewReader(in))
	if len(errs) != 0 {
		t.Fatalf("errs = %v", errs)
	}
	if cfg.Jobs[0].Name != "db1" || cfg.Jobs[1].Name != "db1-2" {
		t.Errorf("names = %q, %q; want db1, db1-2", cfg.Jobs[0].Name, cfg.Jobs[1].Name)
	}
}

func TestConvertReportsInvalidLinesWithNumbers(t *testing.T) {
	in := "0 2 * * *|postgres|h|5432|db|u|p|7\nnot-a-valid-line\n"
	cfg, errs := Convert(strings.NewReader(in))
	if len(cfg.Jobs) != 1 {
		t.Errorf("got %d jobs, want 1 (valid line still converted)", len(cfg.Jobs))
	}
	if len(errs) != 1 || !strings.Contains(errs[0].Error(), "line 2") {
		t.Errorf("errs = %v, want one error mentioning line 2", errs)
	}
}

func TestToYAMLRoundTrips(t *testing.T) {
	cfg, _ := Convert(strings.NewReader(v1Sample))
	out, err := ToYAML(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "name: appdb") || !strings.Contains(string(out), "schedule: 0 2 * * *") {
		t.Errorf("YAML output missing expected fields:\n%s", out)
	}
}
