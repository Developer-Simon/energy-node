package main

import (
	"context"
	"testing"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/learnrec"
)

func TestEngineRunnerDropsTickWhenChannelIsFull(t *testing.T) {
	r := newEngineRunner(nil, func() map[string]bool { return nil }, func() {})
	done := make(chan struct{})
	go func() {
		defer close(done)
		start := time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC)
		for i := 0; i < 3; i++ {
			r.ObserveMinute(learnrec.Frame{Minute: start.Add(time.Duration(i) * time.Minute)})
		}
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("ObserveMinute blocked on a full channel")
	}
	if got := len(r.ticks); got != 1 {
		t.Fatalf("queued ticks = %d, want 1", got)
	}
	first := <-r.ticks
	if !first.Equal(time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC)) {
		t.Fatalf("kept tick = %s, want the first minute", first)
	}
}

func TestEngineRunnerStopsWithContext(t *testing.T) {
	r := newEngineRunner(nil, func() map[string]bool { return nil }, func() {})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		r.Run(ctx)
	}()
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after the context ended")
	}
}
