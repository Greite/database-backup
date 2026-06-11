// Package crypto provides streaming at-rest encryption for backups.
package crypto

import (
	"fmt"
	"io"

	"github.com/Greite/database-backup/internal/config"
)

// Encryptor wraps an output stream with encryption.
type Encryptor interface {
	// Wrap returns a WriteCloser encrypting everything written to it
	// into out. Close finalizes the stream (it does not close out).
	Wrap(out io.Writer) (io.WriteCloser, error)
	// Ext is the filename extension appended to encrypted backups.
	Ext() string
}

// New builds the Encryptor matching the config block, or nil when
// encryption is disabled. The config must already be validated and
// have secrets resolved (PassphraseFile loaded into Passphrase).
func New(enc *config.Encryption) (Encryptor, error) {
	if enc == nil {
		return nil, nil
	}
	switch enc.Method {
	case "gpg":
		return NewOpenPGP(enc.Passphrase), nil
	case "age":
		if len(enc.Recipients) > 0 {
			return NewAgeRecipients(enc.Recipients)
		}
		return NewAgePassphrase(enc.Passphrase)
	}
	return nil, fmt.Errorf("unknown encryption method %q", enc.Method)
}
