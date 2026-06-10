package installer

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/Greite/database-backup/internal/config"
)

func TestRequirementsDeduplicatesPGVersions(t *testing.T) {
	cfg := &config.Config{Jobs: []config.Job{
		{Type: "postgres", PGVersion: 18},
		{Type: "postgres", PGVersion: 17},
		{Type: "postgres", PGVersion: 18},
		{Type: "mariadb"},
		{Type: "mongodb"},
	}}
	req := Requirements(cfg)
	if len(req.PGVersions) != 2 || req.PGVersions[0] != 17 || req.PGVersions[1] != 18 {
		t.Errorf("PGVersions = %v, want [17 18]", req.PGVersions)
	}
	if !req.MariaDB || !req.MongoDB {
		t.Errorf("req = %+v, want MariaDB and MongoDB true", req)
	}
}

func TestRequirementsEmptyForMongoOnly(t *testing.T) {
	cfg := &config.Config{Jobs: []config.Job{{Type: "mongodb"}}}
	req := Requirements(cfg)
	if len(req.PGVersions) != 0 || req.MariaDB || !req.MongoDB {
		t.Errorf("req = %+v, want only MongoDB", req)
	}
}

func TestVerifySHA256(t *testing.T) {
	data := []byte("archive-bytes")
	sum := sha256.Sum256(data)
	if err := verifySHA256(bytes.NewReader(data), hex.EncodeToString(sum[:])); err != nil {
		t.Errorf("valid checksum rejected: %v", err)
	}
	if err := verifySHA256(bytes.NewReader(data), "deadbeef"); err == nil {
		t.Error("invalid checksum accepted")
	}
}
