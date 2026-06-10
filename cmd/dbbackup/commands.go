package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/Greite/database-backup/internal/config"
	"github.com/Greite/database-backup/internal/crypto"
	"github.com/Greite/database-backup/internal/dumper"
	"github.com/Greite/database-backup/internal/healthcheck"
	"github.com/Greite/database-backup/internal/migrate"
)

const (
	defaultConfigPath = "/config/backups.yml"
	v1ConfigPath      = "/config/backups.conf"
	backupRoot        = "/backups"
)

func init() {
	commands["run"] = cmdRun
	commands["healthcheck"] = cmdHealthcheck
	commands["backup"] = cmdBackup
	commands["validate"] = cmdValidate
	commands["migrate"] = cmdMigrate
}

// loadConfig parses, validates and resolves secrets, with the v1
// migration guard when the v2 file is missing.
func loadConfig(path string) (*config.Config, error) {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		if _, v1err := os.Stat(v1ConfigPath); v1err == nil {
			return nil, fmt.Errorf(
				"%s not found, but a v1 config exists at %s.\n"+
					"Convert it with:\n"+
					"  docker run --rm -v <appdata>:/config <image> migrate %s > backups.yml\n"+
					"then mount backups.yml at %s",
				path, v1ConfigPath, v1ConfigPath, path)
		}
		return nil, fmt.Errorf("configuration file not found at %s", path)
	}
	warnIfWorldReadable(path)
	cfg, err := config.Load(path)
	if err != nil {
		return nil, err
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if err := cfg.ResolveSecrets(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func warnIfWorldReadable(path string) {
	info, err := os.Stat(path)
	if err == nil && info.Mode().Perm()&0o044 != 0 {
		log.Printf("Warning: %s is readable by group/other (mode %o); it contains credentials, chmod 600 it on the host", path, info.Mode().Perm())
	}
}

func cmdValidate(args []string) int {
	fs := flag.NewFlagSet("validate", flag.ContinueOnError)
	cfgPath := fs.String("config", defaultConfigPath, "config file path")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if _, err := loadConfig(*cfgPath); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Println("configuration is valid")
	return 0
}

func cmdHealthcheck(args []string) int {
	fs := flag.NewFlagSet("healthcheck", flag.ContinueOnError)
	cfgPath := fs.String("config", defaultConfigPath, "config file path")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	cfg, err := loadConfig(*cfgPath)
	if err != nil {
		fmt.Println("UNHEALTHY:", err)
		return 1
	}
	failed := 0
	for _, j := range cfg.Jobs {
		if err := healthcheck.Ping(context.Background(), j); err != nil {
			fmt.Fprintf(os.Stderr, "FAILED: job %q (%s on %s:%d): %v\n", j.Name, j.Type, j.Host, j.Port, err)
			failed++
			continue
		}
		fmt.Printf("OK: job %q (%s on %s:%d)\n", j.Name, j.Type, j.Host, j.Port)
	}
	if failed > 0 {
		fmt.Println("UNHEALTHY: some database connections failed")
		return 1
	}
	fmt.Println("HEALTHY: all database connections successful")
	return 0
}

func cmdBackup(args []string) int {
	fs := flag.NewFlagSet("backup", flag.ContinueOnError)
	cfgPath := fs.String("config", defaultConfigPath, "config file path")
	jobName := fs.String("job", "", "job name to run (required)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *jobName == "" {
		fmt.Fprintln(os.Stderr, "backup: --job <name> is required")
		return 2
	}
	cfg, err := loadConfig(*cfgPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	for _, j := range cfg.Jobs {
		if j.Name != *jobName {
			continue
		}
		if err := runJob(context.Background(), cfg, j); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		return 0
	}
	fmt.Fprintf(os.Stderr, "backup: no job named %q in %s\n", *jobName, *cfgPath)
	return 1
}

func cmdMigrate(args []string) int {
	path := v1ConfigPath
	if len(args) > 0 {
		path = args[0]
	}
	f, err := os.Open(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer f.Close()
	cfg, errs := migrate.Convert(f)
	for _, e := range errs {
		fmt.Fprintln(os.Stderr, "skipped:", e)
	}
	out, err := migrate.ToYAML(cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	os.Stdout.Write(out)
	if len(errs) > 0 {
		return 1
	}
	return 0
}

// runJob builds the dumper and encryptor for one job and executes it.
func runJob(ctx context.Context, cfg *config.Config, j config.Job) error {
	d, err := dumper.New(j)
	if err != nil {
		return err
	}
	enc, err := crypto.New(cfg.Encryption)
	if err != nil {
		return err
	}
	r := dumper.Runner{BackupRoot: backupRoot, Now: time.Now}
	log.Printf("starting backup of %s database %q on %s:%d", j.Type, j.Database, j.Host, j.Port)
	path, err := r.Run(ctx, j, d, enc)
	if err != nil {
		return err
	}
	info, statErr := os.Stat(path)
	size := "?"
	if statErr == nil {
		size = fmt.Sprintf("%.1f MiB", float64(info.Size())/(1024*1024))
	}
	log.Printf("backup completed: %s (%s)", path, size)
	return nil
}
