package dumper

import (
	"os"
	"strings"
	"testing"

	"github.com/Greite/database-backup/internal/config"
)

func TestMongoArgsWithoutAuth(t *testing.T) {
	j := config.Job{Type: "mongodb", Host: "mg", Port: 27018, Database: "ev"}
	m := newMongoDB(j)
	args := strings.Join(m.args("/tmp/out", ""), " ")
	want := "--host mg --port 27018 --db ev --out /tmp/out --gzip"
	if args != want {
		t.Errorf("args = %q, want %q", args, want)
	}
}

func TestMongoArgsWithAuthAndTLS(t *testing.T) {
	tls := true
	j := config.Job{Type: "mongodb", Host: "mg", Port: 27017, Database: "ev",
		User: "admin", Password: "pw", TLS: &tls}
	m := newMongoDB(j)
	args := strings.Join(m.args("/tmp/out", "/tmp/cfg.yaml"), " ")
	for _, want := range []string{"--ssl", "--username admin", "--authenticationDatabase admin", "--config /tmp/cfg.yaml"} {
		if !strings.Contains(args, want) {
			t.Errorf("args %q missing %q", args, want)
		}
	}
	if strings.Contains(args, "pw") {
		t.Errorf("password leaked into argv: %q", args)
	}
}

func TestMongoPasswordConfigFile(t *testing.T) {
	j := config.Job{Type: "mongodb", User: "u", Password: `p"w\x`}
	m := newMongoDB(j)
	path, cleanup, err := m.writePasswordConfig(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// Backslashes and double quotes must be escaped for mongodump's YAML.
	want := `password: "p\"w\\x"` + "\n"
	if string(b) != want {
		t.Errorf("config content = %q, want %q", b, want)
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0o600 {
		t.Errorf("config mode = %o, want 600", info.Mode().Perm())
	}
}
