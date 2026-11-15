package performance

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestEvaluateOkAndBad(t *testing.T) {
	at := time.Date(2026, 11, 15, 10, 0, 0, 0, time.UTC)
	// 20 samples: nearest-rank P95 is the value at index 18 of the sorted set.
	okSamples := []float64{10, 20, 30, 40, 50}
	for len(okSamples) < 20 {
		okSamples = append(okSamples, 57)
	}
	// Two samples of 76 at the top make the P95 76.
	badSamples := append(append([]float64(nil), okSamples[:18]...), 76, 76)

	t.Run("ok", func(t *testing.T) {
		c := Change{At: at, Available: 70, State: "running"}
		c.Evaluate(at.Add(16*time.Minute), okSamples)
		if c.State != "ok" {
			t.Fatalf("State = %q, want ok", c.State)
		}
		if c.Measured == nil || *c.Measured != 57 {
			t.Fatalf("Measured = %v, want 57", c.Measured)
		}
	})
	t.Run("bad", func(t *testing.T) {
		c := Change{At: at, Available: 70, State: "running"}
		c.Evaluate(at.Add(16*time.Minute), badSamples)
		if c.State != "bad" {
			t.Fatalf("State = %q, want bad", c.State)
		}
		if c.Measured == nil || *c.Measured != 76 {
			t.Fatalf("Measured = %v, want 76", c.Measured)
		}
	})
	t.Run("before 15 minutes stays running", func(t *testing.T) {
		c := Change{At: at, Available: 70, State: "running"}
		c.Evaluate(at.Add(10*time.Minute), badSamples)
		if c.State != "running" {
			t.Fatalf("State = %q, want running", c.State)
		}
		if c.Measured != nil {
			t.Fatalf("Measured = %v, want nil", *c.Measured)
		}
	})
	t.Run("decided change is not re-evaluated", func(t *testing.T) {
		c := Change{At: at, Available: 70, State: "undone"}
		c.Evaluate(at.Add(16*time.Minute), badSamples)
		if c.State != "undone" || c.Measured != nil {
			t.Fatalf("State = %q, Measured = %v, want undone and nil", c.State, c.Measured)
		}
	})
	t.Run("empty samples after 15 minutes stay running", func(t *testing.T) {
		c := Change{At: at, Available: 70, State: "running"}
		c.Evaluate(at.Add(16*time.Minute), nil)
		if c.State != "running" {
			t.Fatalf("State = %q, want running", c.State)
		}
	})
}

func TestConflictsListsChangedRows(t *testing.T) {
	c := Change{
		After: map[RowID]float64{
			RowShelly: 5,
			RowTick:   3,
		},
	}
	current := map[RowID]float64{
		RowShelly: 10,
		RowTick:   3,
		RowNode:   7,
	}
	got := Conflicts(c, current)
	want := map[RowID]float64{RowShelly: 10}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Conflicts = %v, want %v", got, want)
	}
}

func TestChangeSurvivesRestart(t *testing.T) {
	dir := t.TempDir()

	missing, err := LoadChange(dir)
	if err != nil || missing != nil {
		t.Fatalf("LoadChange(missing) = %v, %v, want nil, nil", missing, err)
	}

	measured := 42.5
	orig := Change{
		At:        time.Date(2026, 11, 15, 10, 0, 0, 0, time.UTC),
		Before:    map[RowID]float64{RowShelly: 10, RowTick: 2},
		After:     map[RowID]float64{RowShelly: 5, RowTick: 2},
		Expected:  4.5,
		Available: 70,
		State:     "ok",
		Measured:  &measured,
		Restarted: false,
	}
	if err := SaveChange(dir, orig); err != nil {
		t.Fatalf("SaveChange: %v", err)
	}
	got, err := LoadChange(dir)
	if err != nil || got == nil {
		t.Fatalf("LoadChange = %v, %v, want a change", got, err)
	}
	if !got.At.Equal(orig.At) {
		t.Fatalf("At = %v, want %v", got.At, orig.At)
	}
	got.At = orig.At
	if !reflect.DeepEqual(*got, orig) {
		t.Fatalf("loaded change = %+v, want %+v", *got, orig)
	}
}

func TestSaveChangeAtomic(t *testing.T) {
	dir := t.TempDir()
	c := Change{At: time.Date(2026, 11, 15, 10, 0, 0, 0, time.UTC), State: "running"}
	if err := SaveChange(dir, c); err != nil {
		t.Fatalf("SaveChange: %v", err)
	}
	entries, err := os.ReadDir(filepath.Join(dir, "performance"))
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "last-change.json" {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("files = %v, want only last-change.json", names)
	}
}
