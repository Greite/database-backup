package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveSecretsReadsPasswordFile(t *testing.T) {
	dir := t.TempDir()
	pf := filepath.Join(dir, "pass")
	if err := os.WriteFile(pf, []byte("s3cret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := &Config{Jobs: []Job{{Name: "a", PasswordFile: pf}}}
	if err := cfg.ResolveSecrets(); err != nil {
		t.Fatal(err)
	}
	if cfg.Jobs[0].Password != "s3cret" {
		t.Errorf("Password = %q, want trailing newline trimmed \"s3cret\"", cfg.Jobs[0].Password)
	}
}

func TestResolveSecretsMissingFileFails(t *testing.T) {
	cfg := &Config{Jobs: []Job{{Name: "a", PasswordFile: "/nope"}}}
	if err := cfg.ResolveSecrets(); err == nil {
		t.Fatal("want error for missing password_file, got nil")
	}
}

func TestEncryptionEnvFallback(t *testing.T) {
	t.Setenv("BACKUP_ENCRYPTION_PASSPHRASE", "envpass")
	cfg := &Config{Jobs: []Job{{Name: "a"}}}
	if err := cfg.ResolveSecrets(); err != nil {
		t.Fatal(err)
	}
	if cfg.Encryption == nil || cfg.Encryption.Method != "gpg" || cfg.Encryption.Passphrase != "envpass" {
		t.Errorf("Encryption = %+v, want gpg method with env passphrase", cfg.Encryption)
	}
}

func TestEncryptionYAMLBlockTakesPrecedenceOverEnv(t *testing.T) {
	t.Setenv("BACKUP_ENCRYPTION_PASSPHRASE", "envpass")
	cfg := &Config{
		Encryption: &Encryption{Method: "age", Passphrase: "yamlpass"},
		Jobs:       []Job{{Name: "a"}},
	}
	if err := cfg.ResolveSecrets(); err != nil {
		t.Fatal(err)
	}
	if cfg.Encryption.Method != "age" || cfg.Encryption.Passphrase != "yamlpass" {
		t.Errorf("Encryption = %+v, want YAML block untouched", cfg.Encryption)
	}
}

func TestEncryptionPassphraseFileResolved(t *testing.T) {
	dir := t.TempDir()
	pf := filepath.Join(dir, "ep")
	if err := os.WriteFile(pf, []byte("kk\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := &Config{
		Encryption: &Encryption{Method: "gpg", PassphraseFile: pf},
		Jobs:       []Job{{Name: "a"}},
	}
	if err := cfg.ResolveSecrets(); err != nil {
		t.Fatal(err)
	}
	if cfg.Encryption.Passphrase != "kk" {
		t.Errorf("Passphrase = %q, want \"kk\"", cfg.Encryption.Passphrase)
	}
}
