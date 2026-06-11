package archive

import (
	"archive/tar"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

func TestTarDirStreamsAllFiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "events"), 0o700); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"events/data.bson.gz":     "bson-data",
		"events/metadata.json.gz": "meta",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	var buf bytes.Buffer
	if err := TarDir(dir, &buf); err != nil {
		t.Fatal(err)
	}

	tr := tar.NewReader(&buf)
	var got []string
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		b, _ := io.ReadAll(tr)
		if string(b) != files[hdr.Name] {
			t.Errorf("%s content = %q, want %q", hdr.Name, b, files[hdr.Name])
		}
		got = append(got, hdr.Name)
	}
	sort.Strings(got)
	want := []string{"events/data.bson.gz", "events/metadata.json.gz"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("entries = %v, want %v", got, want)
	}
}
