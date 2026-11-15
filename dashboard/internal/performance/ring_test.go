package performance

import (
	"testing"
	"time"
)

func TestRingP95(t *testing.T) {
	r := NewRing(200, 0)
	base := time.Unix(1000, 0)
	for i := 1; i <= 100; i++ {
		r.Add(base.Add(time.Duration(i)*time.Second), float64(i))
	}
	if got := r.P95(); got != 95 {
		t.Fatalf("P95() = %v, want 95", got)
	}
}

func TestRingP95Empty(t *testing.T) {
	if got := NewRing(10, 0).P95(); got != 0 {
		t.Fatalf("P95() of empty ring = %v, want 0", got)
	}
}

func TestRingEvictsOldestWhenFull(t *testing.T) {
	r := NewRing(3, 0)
	base := time.Unix(1000, 0)
	for i := 1; i <= 5; i++ {
		r.Add(base.Add(time.Duration(i)*time.Second), float64(i))
	}
	got := r.Values()
	want := []float64{3, 4, 5}
	if len(got) != len(want) {
		t.Fatalf("Values() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Values() = %v, want %v", got, want)
		}
	}
}

func TestRingWindowDropsOldSamplesOnAdd(t *testing.T) {
	r := NewRing(100, time.Minute)
	base := time.Unix(1000, 0)
	r.Add(base, 1)
	r.Add(base.Add(30*time.Second), 2)
	r.Add(base.Add(90*time.Second), 3)
	// Der erste Wert liegt 90 s vor dem neuen Eintrag und faellt aus dem Fenster.
	if r.Len() != 2 {
		t.Fatalf("Len() = %d, want 2 after the window moved past the first sample", r.Len())
	}
	if got := r.Since(base.Add(60 * time.Second)); len(got) != 1 || got[0] != 3 {
		t.Fatalf("Since(60s) = %v, want [3]", got)
	}
}

func TestRingPruneWithoutWindowKeepsEverything(t *testing.T) {
	r := NewRing(10, 0)
	base := time.Unix(1000, 0)
	r.Add(base, 1)
	r.Prune(base.Add(24 * time.Hour))
	if r.Len() != 1 {
		t.Fatalf("Len() = %d, want 1 without a window", r.Len())
	}
}
