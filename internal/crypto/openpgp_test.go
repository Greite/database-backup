package crypto

import (
	"bytes"
	"io"
	"testing"

	"github.com/ProtonMail/go-crypto/openpgp"
)

func TestOpenPGPRoundTrip(t *testing.T) {
	enc := NewOpenPGP("hunter2")
	if enc.Ext() != ".gpg" {
		t.Fatalf("Ext() = %q, want .gpg", enc.Ext())
	}

	var out bytes.Buffer
	w, err := enc.Wrap(&out)
	if err != nil {
		t.Fatal(err)
	}
	plaintext := []byte("CREATE TABLE t (id int);")
	if _, err := w.Write(plaintext); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	// Decrypt with the same library to prove gpg compatibility of the format.
	md, err := openpgp.ReadMessage(&out, nil,
		func(keys []openpgp.Key, symmetric bool) ([]byte, error) {
			return []byte("hunter2"), nil
		}, nil)
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(md.UnverifiedBody)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, plaintext) {
		t.Errorf("decrypted = %q, want %q", got, plaintext)
	}
}
