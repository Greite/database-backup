package crypto

import (
	"fmt"
	"io"

	"filippo.io/age"
)

type ageEncryptor struct {
	recipients []age.Recipient
}

// NewAgePassphrase returns an age Encryptor using scrypt passphrase mode.
func NewAgePassphrase(passphrase string) (Encryptor, error) {
	r, err := age.NewScryptRecipient(passphrase)
	if err != nil {
		return nil, err
	}
	return ageEncryptor{recipients: []age.Recipient{r}}, nil
}

// NewAgeRecipients returns an age Encryptor for X25519 public keys.
// The container never holds the private key in this mode.
func NewAgeRecipients(keys []string) (Encryptor, error) {
	var rs []age.Recipient
	for _, k := range keys {
		r, err := age.ParseX25519Recipient(k)
		if err != nil {
			return nil, fmt.Errorf("age recipient %q: %w", k, err)
		}
		rs = append(rs, r)
	}
	return ageEncryptor{recipients: rs}, nil
}

func (ageEncryptor) Ext() string { return ".age" }

func (e ageEncryptor) Wrap(out io.Writer) (io.WriteCloser, error) {
	return age.Encrypt(out, e.recipients...)
}
