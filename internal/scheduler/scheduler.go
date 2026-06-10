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

	// mu guards ctx so that trigger calls that race with Run's setCtx
	// assignment (possible in tests via the test-hook trigger path)
	// are safe under -race.
	mu  sync.RWMutex
	ctx context.Context

	// cancelJobs cancels the context passed to jobs. It is separate from
	// the outer stop-signal context so that SIGTERM does not immediately
	// kill in-flight dump tools; only grace-period expiry cancels them.
	cancelJobs context.CancelFunc
}

// New creates a scheduler; grace bounds how long Run waits for
// in-flight jobs after the context is cancelled.
//
// The job context is created here (not in Run) so that trigger calls
// that race with Run — only possible in tests via the trigger test-hook —
// always receive a context that is independent of the outer stop-signal
// context and that can be cancelled by Run on grace expiry.
func New(grace time.Duration) *Scheduler {
	parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)
	jobCtx, cancelJobs := context.WithCancel(context.Background())
	return &Scheduler{
		cron:       cron.New(cron.WithParser(parser)),
		grace:      grace,
		ctx:        jobCtx,
		cancelJobs: cancelJobs,
	}
}

// setCtx replaces the context handed to new job invocations.
func (s *Scheduler) setCtx(ctx context.Context) {
	s.mu.Lock()
	s.ctx = ctx
	s.mu.Unlock()
}

// Add registers fn under a cron schedule (5-field or @daily-style).
// Returns an error if the schedule expression is invalid.
func (s *Scheduler) Add(schedule string, fn func(ctx context.Context)) error {
	s.mu.Lock()
	idx := len(s.jobs)
	s.jobs = append(s.jobs, fn)
	s.mu.Unlock()
	_, err := s.cron.AddFunc(schedule, func() { s.trigger(idx) })
	return err
}

// trigger runs job idx now, tracked by the WaitGroup. Called by cron and from package tests.
func (s *Scheduler) trigger(idx int) {
	s.mu.RLock()
	ctx := s.ctx
	s.mu.RUnlock()

	s.wg.Add(1)
	defer s.wg.Done()
	s.jobs[idx](ctx)
}

// Run starts the cron loop and blocks until ctx is cancelled, then
// waits up to the grace period for running jobs. Jobs receive a
// separate context that is cancelled only when the grace period
// expires — a stop signal must not kill in-flight backups.
// Jobs still running when the grace period expires are abandoned
// (their context is cancelled, terminating the dump tools).
//
// In the pathological case where a job ignores its context entirely,
// the final <-finished after cancelJobs() is bounded by a 10-second
// hard timeout to prevent Run from blocking indefinitely.
func (s *Scheduler) Run(ctx context.Context) {
	// s.ctx and s.cancelJobs are already set to an independent job context
	// by New. Jobs dispatched via trigger always use s.ctx, which is never
	// the outer stop-signal context, so cancelling ctx does not immediately
	// kill in-flight dump tools.
	defer s.cancelJobs()

	s.cron.Start()
	<-ctx.Done()
	stopCtx := s.cron.Stop() // no new runs; returns a ctx done when cron-invoked jobs return

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
		s.cancelJobs() // grace exhausted: cancel job contexts to terminate dump tools
		// Wait for jobs to notice the cancellation and return.
		// A 10-second hard timeout guards against jobs that ignore their context.
		hardStop := time.NewTimer(10 * time.Second)
		defer hardStop.Stop()
		select {
		case <-finished:
		case <-hardStop.C:
		}
	}
}
