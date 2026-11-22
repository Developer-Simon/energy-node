package main

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/autolearn"
	"github.com/Developer-Simon/energy-node-dashboard/internal/learnrec"
)

// boilerFrame is one minute of a device that draws w watts.
func boilerFrame(at time.Time, w float64) learnrec.Frame {
	return learnrec.Frame{Minute: at, Devices: []learnrec.DeviceMinute{
		{ID: "boiler", Power: map[string]float64{"power": w}, Reachable: true},
	}}
}

// feedRun feeds minutes from start, alternating on and off blocks, and returns
// the minute after the last one.
func feedRun(t *testing.T, r *cycleRunner, start time.Time, blocks []struct {
	on  bool
	min int
}) time.Time {
	t.Helper()
	at := start
	for _, b := range blocks {
		w := 2.0
		if b.on {
			w = 800
		}
		for i := 0; i < b.min; i++ {
			r.ObserveMinute(boilerFrame(at, w))
			at = at.Add(time.Minute)
		}
	}
	return at
}

type block = struct {
	on  bool
	min int
}

func countCycles(t *testing.T, days *autolearn.DayStore, from, to time.Time) int {
	t.Helper()
	list, err := days.Days(from, to)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, d := range list {
		n += len(d.Cycles)
	}
	return n
}

func TestCycleRunnerCollectsCyclesAndSurvivesRestart(t *testing.T) {
	loc := time.FixedZone("CET", 3600)
	dir := t.TempDir()
	days := autolearn.OpenDayStore(filepath.Join(dir, "days"), loc, func() int { return 100000 }, noLogf)
	statePath := filepath.Join(dir, "cycles.json")
	start := time.Date(2026, 11, 19, 0, 0, 0, 0, loc)

	r := newCycleRunner(statePath, days, nil, noLogf)
	// 2880 minutes in 30 on, 30 off blocks give the threshold its history.
	var warmup []block
	for i := 0; i < 2880/30; i++ {
		warmup = append(warmup, block{on: i%2 == 0, min: 30})
	}
	at := feedRun(t, r, start, warmup)
	// Two cycles of 20 minutes on, each ended by 20 minutes off (the pause that
	// ends a cycle starts at 15 minutes).
	at = feedRun(t, r, at, []block{{true, 20}, {false, 20}, {true, 20}, {false, 20}})

	to := at.Add(48 * time.Hour)
	if got := countCycles(t, days, start, to); got != 2 {
		t.Fatalf("cycles before restart = %d, want 2", got)
	}
	if err := r.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := days.Save(); err != nil {
		t.Fatalf("day store Save: %v", err)
	}

	// Restart: the restored threshold must still detect a new cycle, and the
	// old cycles must not be counted again.
	days2 := autolearn.OpenDayStore(filepath.Join(dir, "days"), loc, func() int { return 100000 }, noLogf)
	r2 := newCycleRunner(statePath, days2, nil, noLogf)
	feedRun(t, r2, at, []block{{true, 20}, {false, 20}})
	if got := countCycles(t, days2, start, to); got != 3 {
		t.Fatalf("cycles after restart = %d, want 3 (no duplicates, one new)", got)
	}
}

func TestCycleRunnerMissingStateStartsEmpty(t *testing.T) {
	loc := time.FixedZone("CET", 3600)
	days := autolearn.OpenDayStore(t.TempDir(), loc, func() int { return 100000 }, noLogf)
	r := newCycleRunner(filepath.Join(t.TempDir(), "missing.json"), days, nil, noLogf)
	r.ObserveMinute(boilerFrame(time.Date(2026, 11, 19, 10, 0, 0, 0, loc), 800))
	if err := r.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
}

func noLogf(string, ...any) {}
