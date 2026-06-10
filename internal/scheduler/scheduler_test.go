package scheduler

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestAddRejectsBadSchedule(t *testing.T) {
	s := New(time.Minute)
	if err := s.Add("not a cron", func(ctx context.Context) {}); err == nil {
		t.Fatal("want error for invalid schedule, got nil")
	}
}

func TestRunStopsOnContextCancel(t *testing.T) {
	s := New(time.Second)
	if err := s.Add("* * * * *", func(ctx context.Context) {}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { s.Run(ctx); close(done) }()
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after context cancellation")
	}
}

func TestRunWaitsForInFlightJobs(t *testing.T) {
	s := New(5 * time.Second)
	var finished atomic.Bool
	started := make(chan struct{})
	// Trigger the job manually to avoid waiting a real cron minute.
	job := func(ctx context.Context) {
		close(started)
		time.Sleep(200 * time.Millisecond)
		finished.Store(true)
	}
	if err := s.Add("* * * * *", job); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { s.Run(ctx); close(done) }()
	go s.trigger(0) // test hook: fire entry 0 now
	<-started
	cancel()
	<-done
	if !finished.Load() {
		t.Error("Run returned before the in-flight job finished")
	}
}
