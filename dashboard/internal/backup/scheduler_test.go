package backup

import (
	"context"
	"io"
	"testing"
	"time"
)

// schedFixture liefert einen Scheduler mit festem Takt und einer Create-Funktion,
// die zaehlt, wie oft ein Archiv entsteht.
type schedFixture struct {
	sched   *Scheduler
	now     time.Time
	creates int
	dataDir string
}

func newSchedFixture(t *testing.T, set Settings, fallback bool) *schedFixture {
	t.Helper()
	f := &schedFixture{dataDir: t.TempDir()}
	set.SystemFallback = fallback
	f.sched = &Scheduler{
		Now:      func() time.Time { return f.now },
		Settings: func() Settings { return set },
		Location: SystemLocation{DataDir: f.dataDir, Settings: func() Settings { return set }},
		Version:  "test",
		Create: func(ctx context.Context, s Settings, w io.Writer) error {
			f.creates++
			_, err := w.Write([]byte("archiv"))
			return err
		},
	}
	return f
}

func autoSettings(every string) Settings {
	s := DefaultSettings()
	s.Schedule = true
	s.Every = every
	return s
}

func TestSchedulerRunsOncePerSlot(t *testing.T) {
	f := newSchedFixture(t, autoSettings("day"), true)
	ctx := context.Background()

	f.now = time.Date(2026, 11, 9, 3, 0, 0, 0, time.Local)
	f.sched.Tick(ctx)
	f.now = time.Date(2026, 11, 9, 3, 1, 0, 0, time.Local)
	f.sched.Tick(ctx)

	if f.creates != 1 {
		t.Fatalf("Create called %d times, want 1", f.creates)
	}
}

func TestSchedulerWeeklyOnlySunday(t *testing.T) {
	f := newSchedFixture(t, autoSettings("week"), true)
	ctx := context.Background()

	// 2026-11-09 ist ein Montag, 2026-11-08 ein Sonntag.
	f.now = time.Date(2026, 11, 9, 3, 0, 0, 0, time.Local)
	f.sched.Tick(ctx)
	if f.creates != 0 {
		t.Fatalf("Monday: Create called %d times, want 0", f.creates)
	}

	f.now = time.Date(2026, 11, 8, 3, 0, 0, 0, time.Local)
	f.sched.Tick(ctx)
	if f.creates != 1 {
		t.Fatalf("Sunday: Create called %d times, want 1", f.creates)
	}
}

func TestSchedulerSkipsWithoutLocation(t *testing.T) {
	f := newSchedFixture(t, autoSettings("day"), false)
	f.now = time.Date(2026, 11, 9, 3, 0, 0, 0, time.Local)

	f.sched.Tick(context.Background())

	if f.creates != 0 {
		t.Fatalf("Create called %d times, want 0", f.creates)
	}
	at, reason := f.sched.LastSkipped()
	if !at.Equal(f.now) || reason != "no_location" {
		t.Fatalf("LastSkipped = (%v, %q), want (%v, \"no_location\")", at, reason, f.now)
	}
}

// TestSchedulerClearsSkippedOnSuccess prüft, dass ein erfolgreicher Lauf den
// Hinweis auf den letzten übersprungenen Lauf löscht.
func TestSchedulerClearsSkippedOnSuccess(t *testing.T) {
	f := newSchedFixture(t, autoSettings("day"), false)
	ctx := context.Background()

	f.now = time.Date(2026, 11, 9, 3, 0, 0, 0, time.Local)
	f.sched.Tick(ctx)
	if at, reason := f.sched.LastSkipped(); at.IsZero() || reason != "no_location" {
		t.Fatalf("LastSkipped = (%v, %q), want skipped without location", at, reason)
	}

	// Der Speicherort ist jetzt da, der nächste Lauf gelingt.
	f.sched.Location = SystemLocation{DataDir: f.dataDir, Settings: func() Settings {
		s := autoSettings("day")
		s.SystemFallback = true
		return s
	}}
	f.now = time.Date(2026, 11, 10, 3, 0, 0, 0, time.Local)
	f.sched.Tick(ctx)
	if f.creates != 1 {
		t.Fatalf("Create called %d times, want 1", f.creates)
	}
	if at, reason := f.sched.LastSkipped(); !at.IsZero() || reason != "" {
		t.Fatalf("LastSkipped = (%v, %q), want cleared after success", at, reason)
	}
}
