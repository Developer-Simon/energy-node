package backup

import (
	"context"
	"errors"
	"io"
	"sync"
	"time"
)

// Scheduler erstellt automatische Backups im Slot 03:00 bis 03:04 Ortszeit.
type Scheduler struct {
	Now      func() time.Time // Standard time.Now
	Settings func() Settings
	Location Location
	Store    *Store
	Version  string // Versionsangabe des Knotens fuer den Sidecar
	Create   func(ctx context.Context, s Settings, w io.Writer) error

	mu            sync.Mutex
	lastDay       string
	skippedAt     time.Time
	skippedReason string
}

// Tick wird jede Minute aufgerufen. Im Slot laeuft hoechstens ein Backup pro Tag.
// Verpasste Slots werden nicht nachgeholt.
func (s *Scheduler) Tick(ctx context.Context) {
	now := s.now()
	set := s.Settings()
	if !set.Schedule {
		return
	}
	local := now.Local()
	if local.Hour() != 3 || local.Minute() > 4 {
		return
	}
	if set.Every == "week" && local.Weekday() != time.Sunday {
		return
	}
	day := local.Format("2006-01-02")

	s.mu.Lock()
	if s.lastDay == day {
		s.mu.Unlock()
		return
	}
	s.lastDay = day
	s.mu.Unlock()

	s.run(ctx, set, now)
}

// LastSkipped liefert Zeitpunkt und Grund des letzten uebersprungenen Laufs.
// Ohne Ueberspringen ist die Zeit der Nullwert.
func (s *Scheduler) LastSkipped() (time.Time, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.skippedAt, s.skippedReason
}

func (s *Scheduler) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Scheduler) run(ctx context.Context, set Settings, now time.Time) {
	dir, place, err := s.Location.ArchiveDir()
	if errors.Is(err, ErrNoLocation) {
		s.skip(now, "no_location")
		return
	}
	if err != nil {
		s.skip(now, "error: "+err.Error())
		return
	}
	store, err := NewStore(dir)
	if err != nil {
		s.skip(now, "error: "+err.Error())
		return
	}
	_, err = store.Add(Info{
		Trigger:   "auto",
		Place:     place,
		Version:   s.Version,
		Areas:     chosenAreas(set.Content),
		Secrets:   set.Secrets,
		Locked:    false,
		CreatedAt: now,
	}, set.Keep, func(w io.Writer) error {
		return s.Create(ctx, set, w)
	})
	if err != nil {
		s.skip(now, "error: "+err.Error())
	}
}

func (s *Scheduler) skip(at time.Time, reason string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.skippedAt = at
	s.skippedReason = reason
}
