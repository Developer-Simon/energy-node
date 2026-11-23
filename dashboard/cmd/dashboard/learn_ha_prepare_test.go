package main

import (
	"context"
	"testing"
	"time"
)

type fakeOwn struct {
	keys    []string
	covered map[string]bool
}

func (f *fakeOwn) Keys(from, to time.Time) []string { return f.keys }

func (f *fakeOwn) Coverage(from, to time.Time) int {
	if f.covered[from.Format("2006-01-02")] {
		return 1
	}
	return 0
}

type fakeHA struct {
	prefetches int
	keys       []string
	days       []string
	deadline   time.Time
	drops      int
	noOffer    bool
}

func (f *fakeHA) Offered() bool { return !f.noOffer }

func (f *fakeHA) Prefetch(keys []string, days []string, deadline time.Time) {
	f.prefetches++
	f.keys, f.days, f.deadline = keys, days, deadline
}

func (f *fakeHA) WaitIdle(ctx context.Context) bool { return true }

func (f *fakeHA) Drop() { f.drops++ }

func TestHAPreparerSkipsPrefetchWithoutOffer(t *testing.T) {
	now := time.Date(2026, 11, 20, 3, 30, 0, 0, time.UTC)
	ha := &fakeHA{noOffer: true}
	own := &fakeOwn{keys: []string{"e:uid1"}}
	p := &haPreparer{own: own, ha: ha, active: func() bool { return true }, loc: time.UTC}
	p.Prepare(now)
	if ha.prefetches != 0 {
		t.Fatalf("Prefetch ohne Angebot des Companion aufgerufen: %d", ha.prefetches)
	}
}

func TestHAPreparerDoesNothingWhenReconcileIsOff(t *testing.T) {
	ha := &fakeHA{}
	p := &haPreparer{own: &fakeOwn{}, ha: ha, active: func() bool { return false }, loc: time.UTC}
	p.Prepare(time.Date(2026, 11, 20, 3, 30, 0, 0, time.UTC))
	if ha.prefetches != 0 {
		t.Fatalf("Prefetch ohne Abgleich aufgerufen: %d", ha.prefetches)
	}
	p.Release()
	if ha.drops != 1 {
		t.Fatalf("Release muss den Zwischenspeicher leeren, Drop = %d", ha.drops)
	}
}

func TestHAPreparerPrefetchesOnlyIncompleteDays(t *testing.T) {
	now := time.Date(2026, 11, 20, 3, 30, 0, 0, time.UTC)
	ha := &fakeHA{}
	own := &fakeOwn{
		keys:    []string{"e:uid1"},
		covered: map[string]bool{"2026-11-19": true},
	}
	p := &haPreparer{own: own, ha: ha, active: func() bool { return true }, loc: time.UTC}
	p.Prepare(now)

	if ha.prefetches != 1 {
		t.Fatalf("Prefetch-Aufrufe = %d, want 1", ha.prefetches)
	}
	if len(ha.days) != 13 {
		t.Fatalf("Tage = %d, want 13 (14 minus der lueckenlose 19.11.)", len(ha.days))
	}
	for _, d := range ha.days {
		if d == "2026-11-19" {
			t.Fatal("der lueckenlose Tag darf nicht nachgeholt werden")
		}
	}
	if ha.days[0] != "2026-11-06" || ha.days[len(ha.days)-1] != "2026-11-18" {
		t.Fatalf("Tage = %v, want 2026-11-06 bis 2026-11-18 ohne 2026-11-19", ha.days)
	}
	want := map[string]bool{"b:pv": true, "b:battery_soc": true, "e:uid1": true}
	if len(ha.keys) != len(want) {
		t.Fatalf("Schluessel = %v", ha.keys)
	}
	for _, k := range ha.keys {
		if !want[k] {
			t.Fatalf("unerwarteter Schluessel %q in %v", k, ha.keys)
		}
	}
	if !ha.deadline.Equal(now.Add(90 * time.Second)) {
		t.Fatalf("Frist = %v, want jetzt+90 s", ha.deadline)
	}
}

func TestHAPreparerNoPrefetchWhenAllDaysComplete(t *testing.T) {
	now := time.Date(2026, 11, 20, 3, 30, 0, 0, time.UTC)
	covered := map[string]bool{}
	for k := 1; k <= 14; k++ {
		covered[now.AddDate(0, 0, -k).Format("2006-01-02")] = true
	}
	ha := &fakeHA{}
	p := &haPreparer{own: &fakeOwn{covered: covered}, ha: ha, active: func() bool { return true }, loc: time.UTC}
	p.Prepare(now)
	if ha.prefetches != 0 {
		t.Fatalf("lueckenlose Tage duerfen keine Nachfrage ausloesen: %d", ha.prefetches)
	}
}
