// Package installer installs the database client tools required by
// the configuration at container startup (must run as root).
package installer

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"sort"
	"time"

	"github.com/Greite/database-backup/internal/config"
)

// Pinned MongoDB Database Tools release, verified by SHA256 at download.
const mongoToolsVersion = "100.14.0"

var mongoToolsSHA256 = map[string]string{
	"amd64": "4104998bda784a0cb16fc2e06d9c21645516d72c4fb481c9b103f1e0a8458fc0",
	"arm64": "ef2945973b7e9c0f95d25dc607d420b0b07c486a675937ac9723b32f56ce718d",
}

var mongoToolsArch = map[string]string{"amd64": "x86_64", "arm64": "arm64"}

// Req describes which client tools the configuration requires.
type Req struct {
	PGVersions []int // sorted, deduplicated
	MariaDB    bool
	MongoDB    bool
}

// Requirements computes which client tools the config needs.
func Requirements(cfg *config.Config) Req {
	var req Req
	pg := map[int]bool{}
	for _, j := range cfg.Jobs {
		switch j.Type {
		case "postgres":
			pg[j.PGVersion] = true
		case "mariadb", "mysql":
			req.MariaDB = true
		case "mongodb":
			req.MongoDB = true
		}
	}
	for v := range pg {
		req.PGVersions = append(req.PGVersions, v)
	}
	sort.Ints(req.PGVersions)
	return req
}

func verifySHA256(r io.Reader, want string) error {
	h := sha256.New()
	if _, err := io.Copy(h, r); err != nil {
		return err
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != want {
		return fmt.Errorf("sha256 mismatch: got %s, want %s", got, want)
	}
	return nil
}

// Install installs every missing tool. It must run as root and is a
// no-op for tools already present (preinstalled or previous start).
func Install(req Req) error {
	var aptPkgs []string
	for _, v := range req.PGVersions {
		if _, err := os.Stat(fmt.Sprintf("/usr/lib/postgresql/%d/bin/pg_dump", v)); err != nil {
			aptPkgs = append(aptPkgs, fmt.Sprintf("postgresql-client-%d", v))
		}
	}
	if req.MariaDB {
		if _, err := exec.LookPath("mysqldump"); err != nil {
			aptPkgs = append(aptPkgs, "mariadb-client")
		}
	}
	if len(aptPkgs) > 0 {
		log.Printf("installing packages: %v", aptPkgs)
		if err := aptInstall(aptPkgs); err != nil {
			return err
		}
	}
	if req.MongoDB {
		if _, err := exec.LookPath("mongodump"); err != nil {
			log.Printf("installing MongoDB Database Tools %s", mongoToolsVersion)
			if err := installMongoTools(); err != nil {
				return err
			}
		}
	}
	return nil
}

func aptInstall(pkgs []string) error {
	if out, err := exec.Command("apt-get", "update", "-qq").CombinedOutput(); err != nil {
		return fmt.Errorf("apt-get update: %w (%s)", err, out)
	}
	args := append([]string{"install", "-y", "-qq"}, pkgs...)
	if out, err := exec.Command("apt-get", args...).CombinedOutput(); err != nil {
		return fmt.Errorf("apt-get install: %w (%s)", err, out)
	}
	return os.RemoveAll("/var/lib/apt/lists")
}

func installMongoTools() error {
	arch := runtime.GOARCH
	sum, ok := mongoToolsSHA256[arch]
	if !ok {
		return fmt.Errorf("unsupported architecture %q", arch)
	}
	url := fmt.Sprintf("https://fastdl.mongodb.org/tools/db/mongodb-database-tools-ubuntu2204-%s-%s.tgz",
		mongoToolsArch[arch], mongoToolsVersion)

	tmp, err := os.CreateTemp("", "mongo-tools-*.tgz")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()

	client := &http.Client{Timeout: 5 * time.Minute}
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("downloading %s: HTTP %d", url, resp.StatusCode)
	}
	if _, err := io.Copy(tmp, resp.Body); err != nil {
		return err
	}
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		return err
	}
	if err := verifySHA256(tmp, sum); err != nil {
		return fmt.Errorf("MongoDB tools archive: %w", err)
	}

	// Extract bin/* into /usr/local/bin (strip the top-level directory).
	// Archive layout is mongodb-database-tools-*/bin/<tools>, hence --strip-components=2.
	out, err := exec.Command("tar", "-xzf", tmp.Name(), "-C", "/usr/local/bin",
		"--strip-components=2", "--wildcards", "*/bin/*").CombinedOutput()
	if err != nil {
		return fmt.Errorf("extracting MongoDB tools: %w (%s)", err, out)
	}
	if _, err := exec.LookPath("mongodump"); err != nil {
		return fmt.Errorf("mongodump not found after extraction")
	}
	return nil
}
