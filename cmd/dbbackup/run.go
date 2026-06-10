package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/Greite/database-backup/internal/installer"
	"github.com/Greite/database-backup/internal/privileges"
	"github.com/Greite/database-backup/internal/scheduler"
)

func cmdRun(args []string) int {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	cfgPath := fs.String("config", defaultConfigPath, "config file path")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	log.Println("Starting Database Backup Container (v2)...")

	cfg, err := loadConfig(*cfgPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	// Phase 1 (root): install required client tools, then drop privileges.
	if privileges.NeedsDrop() {
		if err := installer.Install(installer.Requirements(cfg)); err != nil {
			fmt.Fprintln(os.Stderr, "installing clients:", err)
			return 1
		}
		log.Printf("dropping privileges to uid %d", privileges.UID)
		if err := privileges.DropAndReexec(backupRoot); err != nil {
			fmt.Fprintln(os.Stderr, "privilege drop:", err)
			return 1
		}
		// DropAndReexec exits the process on success.
	}

	// Phase 2 (unprivileged): schedule and run.
	sched := scheduler.New(cfg.ShutdownGrace)
	for _, j := range cfg.Jobs {
		job := j // capture
		err := sched.Add(job.Schedule, func(ctx context.Context) {
			if err := runJob(ctx, cfg, job); err != nil {
				log.Printf("ERROR: %v", err)
			}
		})
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		log.Printf("scheduled %s backup of %q (%s, retention %d days)",
			job.Type, job.Database, job.Schedule, job.RetentionDays)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	log.Printf("%d job(s) scheduled, running...", len(cfg.Jobs))
	sched.Run(ctx)
	log.Println("shutdown complete")
	return 0
}
