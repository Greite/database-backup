package config

import (
	"fmt"
	"os"
	"strings"
)

func readSecretFile(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("reading secret file: %w", err)
	}
	return strings.TrimRight(string(b), "\r\n"), nil
}

// ResolveSecrets loads password_file contents into Password fields and
// applies the v1 encryption environment variable fallback. Call after
// Validate, before using the config.
func (c *Config) ResolveSecrets() error {
	for i := range c.Jobs {
		j := &c.Jobs[i]
		if j.PasswordFile == "" {
			continue
		}
		p, err := readSecretFile(j.PasswordFile)
		if err != nil {
			return fmt.Errorf("job %q password_file: %w", j.Name, err)
		}
		j.Password = p
	}
	if c.Encryption == nil {
		// v1 parity: env vars enable gpg encryption when no YAML block exists.
		if f := os.Getenv("BACKUP_ENCRYPTION_PASSPHRASE_FILE"); f != "" {
			c.Encryption = &Encryption{Method: "gpg", PassphraseFile: f}
		} else if p := os.Getenv("BACKUP_ENCRYPTION_PASSPHRASE"); p != "" {
			c.Encryption = &Encryption{Method: "gpg", Passphrase: p}
		}
	}
	if c.Encryption != nil && c.Encryption.PassphraseFile != "" {
		p, err := readSecretFile(c.Encryption.PassphraseFile)
		if err != nil {
			return fmt.Errorf("encryption passphrase_file: %w", err)
		}
		c.Encryption.Passphrase = p
	}
	return nil
}
