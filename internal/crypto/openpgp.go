package crypto

import (
	"io"

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/packet"
)

// openpgpEncryptor produces symmetric OpenPGP (AES-256) output that
// standard `gpg -d` can decrypt, matching v1's gpg invocation.
type openpgpEncryptor struct {
	passphrase []byte
}

// NewOpenPGP returns a gpg-compatible symmetric Encryptor.
func NewOpenPGP(passphrase string) Encryptor {
	return openpgpEncryptor{passphrase: []byte(passphrase)}
}

func (openpgpEncryptor) Ext() string { return ".gpg" }

func (e openpgpEncryptor) Wrap(out io.Writer) (io.WriteCloser, error) {
	cfg := &packet.Config{DefaultCipher: packet.CipherAES256}
	return openpgp.SymmetricallyEncrypt(out, e.passphrase, nil, cfg)
}
