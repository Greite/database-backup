package crypto

import (
	"bytes"
	"io"
	"testing"

	"filippo.io/age"

	"github.com/Greite/database-backup/internal/config"
)

func roundTrip(t *testing.T, enc Encryptor, identity age.Identity) {
	t.Helper()
	var out bytes.Buffer
	w, err := enc.Wrap(&out)
	if err != nil {
		t.Fatal(err)
	}
	plaintext := []byte("dump-bytes")
	if _, err := w.Write(plaintext); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	r, err := age.Decrypt(&out, identity)
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, plaintext) {
		t.Errorf("decrypted = %q, want %q", got, plaintext)
	}
}

func TestAgePassphraseRoundTrip(t *testing.T) {
	enc, err := NewAgePassphrase("hunter2")
	if err != nil {
		t.Fatal(err)
	}
	if enc.Ext() != ".age" {
		t.Fatalf("Ext() = %q, want .age", enc.Ext())
	}
	id, err := age.NewScryptIdentity("hunter2")
	if err != nil {
		t.Fatal(err)
	}
	roundTrip(t, enc, id)
}

func TestAgeRecipientsRoundTrip(t *testing.T) {
	id, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatal(err)
	}
	enc, err := NewAgeRecipients([]string{id.Recipient().String()})
	if err != nil {
		t.Fatal(err)
	}
	roundTrip(t, enc, id)
}

func TestNewFactorySelectsImplementation(t *testing.T) {
	cases := []struct {
		enc     *config.Encryption
		wantExt string
	}{
		{nil, ""},
		{&config.Encryption{Method: "gpg", Passphrase: "x"}, ".gpg"},
		{&config.Encryption{Method: "age", Passphrase: "x"}, ".age"},
	}
	for _, tc := range cases {
		e, err := New(tc.enc)
		if err != nil {
			t.Fatal(err)
		}
		if tc.wantExt == "" {
			if e != nil {
				t.Errorf("New(nil) = %v, want nil", e)
			}
			continue
		}
		if e.Ext() != tc.wantExt {
			t.Errorf("Ext() = %q, want %q", e.Ext(), tc.wantExt)
		}
	}
}
