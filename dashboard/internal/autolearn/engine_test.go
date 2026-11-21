package autolearn

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	_ "time/tzdata"

	"github.com/Developer-Simon/energy-node-dashboard/internal/learnrec"
)

func engineLoc(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Fatal(err)
	}
	return loc
}

// newTestEngine builds an engine with an empty store, an empty document and
// the given limits. The devices map is empty unless a test adds one.
func newTestEngine(t *testing.T, limits learnrec.Limits) *Engine {
	t.Helper()
	loc := engineLoc(t)
	days := OpenDayStore(t.TempDir(), loc, func() int { return 7 }, func(string, ...any) {})
	return NewEngine(days, NewReactions(),
		func() ([]byte, error) { return []byte(`{}`), nil },
		func() map[string]DeviceInfo { return map[string]DeviceInfo{} },
		func() learnrec.Limits { return limits },
		loc)
}

func at(t *testing.T, day, hhmm string) time.Time {
	t.Helper()
	loc := engineLoc(t)
	ts, err := time.ParseInLocation("2006-01-02 15:04", day+" "+hhmm, loc)
	if err != nil {
		t.Fatal(err)
	}
	return ts
}

var habitOnly = map[string]bool{"habit": true}

func TestEngineDailyDetectorRunsOncePerDayAt0330(t *testing.T) {
	e := newTestEngine(t, learnrec.LimitsFor(learnrec.LevelBalanced, 4))
	var runs atomic.Int32
	e.Register(Detector{Name: "daily", Group: "habit", Light: false, Run: func(ctx context.Context, in *Input, device string) []Candidate {
		runs.Add(1)
		return nil
	}})

	e.Tick(at(t, "2026-11-20", "03:00"), habitOnly)
	if got := runs.Load(); got != 0 {
		t.Fatalf("runs before 03:30 = %d, want 0", got)
	}
	e.Tick(at(t, "2026-11-20", "03:30"), habitOnly)
	if got := runs.Load(); got != 1 {
		t.Fatalf("runs at 03:30 = %d, want 1", got)
	}
	e.Tick(at(t, "2026-11-20", "03:31"), habitOnly)
	e.Tick(at(t, "2026-11-20", "08:00"), habitOnly)
	if got := runs.Load(); got != 1 {
		t.Fatalf("runs later the same day = %d, want still 1", got)
	}
	e.Tick(at(t, "2026-11-21", "03:30"), habitOnly)
	if got := runs.Load(); got != 2 {
		t.Fatalf("runs on the next day = %d, want 2", got)
	}
}

func TestEngineLightDetectorFollowsLightEvery(t *testing.T) {
	limits := learnrec.LimitsFor(learnrec.LevelBalanced, 4)
	limits.LightEvery = 15 * time.Minute
	e := newTestEngine(t, limits)
	var runs atomic.Int32
	e.Register(Detector{Name: "light", Group: "habit", Light: true, Run: func(ctx context.Context, in *Input, device string) []Candidate {
		runs.Add(1)
		return nil
	}})

	e.Tick(at(t, "2026-11-20", "10:00"), habitOnly)
	if got := runs.Load(); got != 1 {
		t.Fatalf("first tick runs = %d, want 1", got)
	}
	e.Tick(at(t, "2026-11-20", "10:05"), habitOnly)
	e.Tick(at(t, "2026-11-20", "10:14"), habitOnly)
	if got := runs.Load(); got != 1 {
		t.Fatalf("runs before LightEvery = %d, want 1", got)
	}
	e.Tick(at(t, "2026-11-20", "10:15"), habitOnly)
	if got := runs.Load(); got != 2 {
		t.Fatalf("runs after LightEvery = %d, want 2", got)
	}
}

func TestEngineGroupsFilterDetectors(t *testing.T) {
	e := newTestEngine(t, learnrec.LimitsFor(learnrec.LevelBalanced, 4))
	var runs atomic.Int32
	e.Register(Detector{Name: "rel", Group: "reliability", Light: true, Run: func(ctx context.Context, in *Input, device string) []Candidate {
		runs.Add(1)
		return nil
	}})
	e.Tick(at(t, "2026-11-20", "10:00"), habitOnly)
	if got := runs.Load(); got != 0 {
		t.Fatalf("disabled group ran %d times", got)
	}
	e.Tick(at(t, "2026-11-20", "10:10"), map[string]bool{"reliability": true})
	if got := runs.Load(); got != 1 {
		t.Fatalf("enabled group ran %d times, want 1", got)
	}
}

func TestEngineCandidatesKeepLastRun(t *testing.T) {
	e := newTestEngine(t, learnrec.LimitsFor(learnrec.LevelBalanced, 4))
	var answer atomic.Pointer[string]
	first := "first"
	answer.Store(&first)
	e.Register(Detector{Name: "rule", Group: "habit", Light: true, Run: func(ctx context.Context, in *Input, device string) []Candidate {
		return []Candidate{{Finding: Finding{Kind: *answer.Load()}}}
	}})

	e.Tick(at(t, "2026-11-20", "10:00"), habitOnly)
	got := e.Candidates()
	if len(got) != 1 || got[0].Finding.Kind != "first" || got[0].Detector != "rule" {
		t.Fatalf("candidates after first run = %+v", got)
	}
	empty := ""
	answer.Store(&empty)
	e.Tick(at(t, "2026-11-20", "10:20"), habitOnly)
	if got := e.Candidates(); len(got) != 1 || got[0].Finding.Kind != "" {
		t.Fatalf("candidates after second run = %+v, want the new empty answer", got)
	}
}

func TestEngineDowngradeAfterThreeOverruns(t *testing.T) {
	limits := learnrec.LimitsFor(learnrec.LevelBalanced, 4)
	limits.Workers = 1
	limits.Budget = 20 * time.Millisecond
	limits.LightEvery = 0 // light detectors run on every tick
	e := newTestEngine(t, limits)
	e.Register(Detector{Name: "heavy", Group: "habit", Light: true, Run: func(ctx context.Context, in *Input, device string) []Candidate {
		<-ctx.Done()
		return nil
	}})

	e.Tick(at(t, "2026-11-20", "10:00"), habitOnly)
	e.Tick(at(t, "2026-11-20", "10:01"), habitOnly)
	if e.Downgrade() {
		t.Fatal("downgrade after two overruns, want none")
	}
	e.Tick(at(t, "2026-11-20", "10:02"), habitOnly)
	if !e.Downgrade() {
		t.Fatal("no downgrade after three overruns in a row")
	}
	if e.Downgrade() {
		t.Fatal("downgrade signal not cleared after it was read")
	}

	overruns, share, took := e.Load()
	if overruns != 0 {
		t.Fatalf("overruns after reset = %d, want 0", overruns)
	}
	if share < 0 || share > 0.01 {
		t.Fatalf("share = %v, want a small value for 20 ms budgets", share)
	}
	_ = took
}

func TestEngineDisabledGroupDoesNotAdvanceTimers(t *testing.T) {
	e := newTestEngine(t, learnrec.LimitsFor(learnrec.LevelBalanced, 4))
	var daily, light atomic.Int32
	e.Register(Detector{Name: "daily", Group: "habit", Light: false, Run: func(ctx context.Context, in *Input, device string) []Candidate {
		daily.Add(1)
		return nil
	}})
	e.Register(Detector{Name: "light", Group: "reliability", Light: true, Run: func(ctx context.Context, in *Input, device string) []Candidate {
		light.Add(1)
		return nil
	}})

	// The engine starts before the slot, so 03:30 is a slot and not a missed one.
	e.Tick(at(t, "2026-11-20", "03:00"), map[string]bool{})
	// Both groups off: the 03:30 and the light round must not count.
	e.Tick(at(t, "2026-11-20", "03:30"), map[string]bool{})
	if daily.Load() != 0 || light.Load() != 0 {
		t.Fatalf("disabled groups ran daily=%d light=%d", daily.Load(), light.Load())
	}

	// Switching the groups on later: the rounds are still due at once.
	e.Tick(at(t, "2026-11-20", "03:31"), map[string]bool{"habit": true, "reliability": true})
	if daily.Load() != 1 {
		t.Fatalf("daily after enabling habit = %d, want 1", daily.Load())
	}
	if light.Load() != 1 {
		t.Fatalf("light after enabling reliability = %d, want 1", light.Load())
	}
}

func TestEngineLightTimerOnlyCountsWhenItsGroupIsOn(t *testing.T) {
	e := newTestEngine(t, learnrec.LimitsFor(learnrec.LevelBalanced, 4))
	var runs atomic.Int32
	e.Register(Detector{Name: "light", Group: "reliability", Light: true, Run: func(ctx context.Context, in *Input, device string) []Candidate {
		runs.Add(1)
		return nil
	}})

	e.Tick(at(t, "2026-11-20", "10:00"), habitOnly)
	e.Tick(at(t, "2026-11-20", "10:01"), map[string]bool{"reliability": true})
	if got := runs.Load(); got != 1 {
		t.Fatalf("first enabled round after a disabled one = %d runs, want 1", got)
	}
}

func TestEngineUnfinishedWaitsForDueRound(t *testing.T) {
	limits := learnrec.LimitsFor(learnrec.LevelBalanced, 4)
	limits.Workers = 1
	limits.Budget = 20 * time.Millisecond
	limits.LightEvery = 15 * time.Minute
	e := newTestEngine(t, limits)
	var runs atomic.Int32
	e.Register(Detector{Name: "slow", Group: "habit", Light: true, Run: func(ctx context.Context, in *Input, device string) []Candidate {
		runs.Add(1)
		<-ctx.Done()
		return nil
	}})

	e.Tick(at(t, "2026-11-20", "10:00"), habitOnly) // due, runs and overruns
	if got := runs.Load(); got != 1 {
		t.Fatalf("first round runs = %d, want 1", got)
	}
	e.Tick(at(t, "2026-11-20", "10:05"), habitOnly) // not due: the carried task must wait
	e.Tick(at(t, "2026-11-20", "10:10"), habitOnly)
	if got := runs.Load(); got != 1 {
		t.Fatalf("non-due ticks ran the carried task: runs = %d, want 1", got)
	}
	e.Tick(at(t, "2026-11-20", "10:15"), habitOnly) // due: the carried task runs again
	if got := runs.Load(); got != 2 {
		t.Fatalf("due round runs = %d, want 2", got)
	}
}

func TestEngineCatchUpFiveMinutesAfterStart(t *testing.T) {
	e := newTestEngine(t, learnrec.LimitsFor(learnrec.LevelBalanced, 4))
	var runs atomic.Int32
	e.Register(Detector{Name: "daily", Group: "habit", Light: false, Run: func(ctx context.Context, in *Input, device string) []Candidate {
		runs.Add(1)
		return nil
	}})

	e.Tick(at(t, "2026-11-20", "10:00"), habitOnly)
	e.Tick(at(t, "2026-11-20", "10:04"), habitOnly)
	if got := runs.Load(); got != 0 {
		t.Fatalf("runs before the catch-up delay = %d, want 0", got)
	}
	e.Tick(at(t, "2026-11-20", "10:05"), habitOnly)
	if got := runs.Load(); got != 1 {
		t.Fatalf("catch-up runs = %d, want 1", got)
	}
	e.Tick(at(t, "2026-11-20", "11:00"), habitOnly)
	if got := runs.Load(); got != 1 {
		t.Fatalf("second tick the same day ran again: runs = %d", got)
	}
	e.Tick(at(t, "2026-11-21", "03:30"), habitOnly)
	if got := runs.Load(); got != 2 {
		t.Fatalf("next 03:30 slot runs = %d, want 2", got)
	}
}

func TestEngineCatchUpAfterSetToZeroRunsOnFirstTick(t *testing.T) {
	e := newTestEngine(t, learnrec.LimitsFor(learnrec.LevelBalanced, 4))
	e.SetCatchUpAfter(0)
	var runs atomic.Int32
	e.Register(Detector{Name: "daily", Group: "habit", Light: false, Run: func(ctx context.Context, in *Input, device string) []Candidate {
		runs.Add(1)
		return nil
	}})

	e.Tick(at(t, "2026-11-20", "10:00"), habitOnly)
	if got := runs.Load(); got != 1 {
		t.Fatalf("catch-up runs on the first tick = %d, want 1", got)
	}
	e.Tick(at(t, "2026-11-20", "10:01"), habitOnly)
	if got := runs.Load(); got != 1 {
		t.Fatalf("second tick ran again: runs = %d", got)
	}
}

func TestEngineCatchUpAgeRule(t *testing.T) {
	cases := []struct {
		name     string
		lastRun  string
		wantRuns int32
	}{
		{"last run 25 hours ago", "2026-11-19 09:00", 1},
		{"last run 22 hours ago", "2026-11-19 12:00", 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := newTestEngine(t, learnrec.LimitsFor(learnrec.LevelBalanced, 4))
			var runs atomic.Int32
			e.Register(Detector{Name: "daily", Group: "habit", Light: false, Run: func(ctx context.Context, in *Input, device string) []Candidate {
				runs.Add(1)
				return nil
			}})
			e.lastDaily = at(t, strings.Fields(c.lastRun)[0], strings.Fields(c.lastRun)[1])
			e.Tick(at(t, "2026-11-20", "10:00"), habitOnly)
			e.Tick(at(t, "2026-11-20", "10:05"), habitOnly)
			if got := runs.Load(); got != c.wantRuns {
				t.Fatalf("runs = %d, want %d", got, c.wantRuns)
			}
		})
	}
}

func TestEngineShareLimitPerLevel(t *testing.T) {
	eco := learnrec.LimitsFor(learnrec.LevelEco, 1)
	balanced := learnrec.LimitsFor(learnrec.LevelBalanced, 4)
	if shareLimit(eco) != 0.05 || shareLimit(balanced) != 0.10 {
		t.Fatalf("share limits = %v, %v, want 0.05 and 0.10", shareLimit(eco), shareLimit(balanced))
	}

	now := at(t, "2026-11-20", "10:00")
	// 6 % of one core over 24 hours: over the eco limit, under the others.
	six := map[string]time.Duration{"x": 5184 * time.Second}
	e := newTestEngine(t, eco)
	e.record(now, eco, nil, nil, nil, six)
	if !e.Downgrade() {
		t.Fatal("eco: 6 % share did not ask for a downgrade")
	}
	e = newTestEngine(t, balanced)
	e.record(now, balanced, nil, nil, nil, six)
	if e.Downgrade() {
		t.Fatal("balanced: 6 % share asked for a downgrade, want none")
	}
	// 12 % is over the limit of the other levels.
	twelve := map[string]time.Duration{"x": 10368 * time.Second}
	e = newTestEngine(t, balanced)
	e.record(now, balanced, nil, nil, nil, twelve)
	if !e.Downgrade() {
		t.Fatal("balanced: 12 % share did not ask for a downgrade")
	}
}
