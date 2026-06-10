// Package scheduler runs backup jobs on cron schedules with graceful
// shutdown: on stop, in-flight jobs may finish within a grace period.
package scheduler

import (
	"context"
	"sync"
	"time"

	"github.com/robfig/cron/v3"
)

// Scheduler wraps robfig/cron with graceful-shutdown semantics.
// In-flight jobs are tracked via a WaitGroup; Run blocks until the
// context is cancelled, then waits up to grace for them to finish.
type Scheduler struct {
	cron  *cron.Cron
	grace time.Duration
	wg    sync.WaitGroup
	jobs  []func(ctx context.Context)

	// mu guards ctx so that trigger calls that race with Run.ctx
	// assignment (possible in tests via the test-hook trigger path)
	// are safe under -race.
	mu  sync.RWMutex
	ctx context.Context
}

// New creates a scheduler; grace bounds how long Run waits for
// in-flight jobs after the context is cancelled.
func New(grace time.Duration) *Scheduler {
	return &Scheduler{
		cron:  cron.New(),
		grace: grace,
		ctx:   context.Background(), // safe default until Run assigns one
	}
}

// Add registers fn under a cron schedule (5-field or @daily-style).
// Returns an error if the schedule expression is invalid.
func (s *Scheduler) Add(schedule string, fn func(ctx context.Context)) error {
	idx := len(s.jobs)
	s.jobs = append(s.jobs, fn)
	_, err := s.cron.AddFunc(schedule, func() { s.trigger(idx) })
	return err
}

// trigger runs job idx now, tracked by the WaitGroup. Exported for tests.
func (s *Scheduler) trigger(idx int) {
	s.mu.RLock()
	ctx := s.ctx
	s.mu.RUnlock()

	s.wg.Add(1)
	defer s.wg.Done()
	s.jobs[idx](ctx)
}

// Run starts the cron loop and blocks until ctx is cancelled, then
// waits up to the grace period for running jobs.
func (s *Scheduler) Run(ctx context.Context) {
	s.mu.Lock()
	s.ctx = ctx
	s.mu.Unlock()

	s.cron.Start()
	<-ctx.Done()
	stopCtx := s.cron.Stop() // no new runs; returns a ctx done when cron jobs return

	graceTimer := time.NewTimer(s.grace)
	defer graceTimer.Stop()
	finished := make(chan struct{})
	go func() {
		<-stopCtx.Done()
		s.wg.Wait()
		close(finished)
	}()
	select {
	case <-finished:
	case <-graceTimer.C:
	}
}
