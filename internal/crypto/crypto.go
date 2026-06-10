// Package crypto provides streaming at-rest encryption for backups.
package crypto

import "io"

// Encryptor wraps an output stream with encryption.
type Encryptor interface {
	// Wrap returns a WriteCloser encrypting everything written to it
	// into out. Close finalizes the stream (it does not close out).
	Wrap(out io.Writer) (io.WriteCloser, error)
	// Ext is the filename extension appended to encrypted backups.
	Ext() string
}
