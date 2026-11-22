package chainsuggest

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/learnrec"
)

// replayFrame macht den Rahmen einer nachgelesenen Minute wie der Hub ihn baut.
// Der Hub nennt in einem nachgelesenen Rahmen jede Entitaet ein Geraet mit ID = unique_id.
func replayFrame(minute time.Time, values map[string]float64) learnrec.Frame {
	f := learnrec.Frame{Minute: minute, Replayed: true}
	for id, w := range values {
		f.Devices = append(f.Devices, learnrec.DeviceMinute{
			ID:        id,
			Power:     map[string]float64{id: w},
			Reachable: true,
		})
	}
	return f
}

func TestReplayFromIsMinuteAfterLastCounted(t *testing.T) {
	start := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	c := NewCollector(DefaultThresholds, time.UTC)
	feed(c, start, 5, func(int) map[string]float64 { return map[string]float64{"a": 100, "b": 90} })

	want := start.Add(4 * time.Minute).Add(time.Minute)
	if got := c.ReplayFrom(); !got.Equal(want) {
		t.Errorf("ReplayFrom = %v, want %v", got, want)
	}
}

func TestReplayFromZeroWithoutHistory(t *testing.T) {
	c := NewCollector(DefaultThresholds, time.UTC)
	if got := c.ReplayFrom(); !got.IsZero() {
		t.Errorf("ReplayFrom = %v, want zero time", got)
	}
}

func TestReplaySurvivesRestartWithoutDoubleCount(t *testing.T) {
	start := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	path := filepath.Join(t.TempDir(), "suggest.json")

	first := Load(path, DefaultThresholds, time.UTC, noLog)
	first.SetPersistence(path, noLog)
	feed(first, start, 5, func(int) map[string]float64 { return map[string]float64{"a": 100, "b": 90} })
	if err := first.Save(); err != nil {
		t.Fatal(err)
	}

	second := Load(path, DefaultThresholds, time.UTC, noLog)
	from := second.ReplayFrom()
	if !from.Equal(start.Add(5 * time.Minute)) {
		t.Fatalf("ReplayFrom after restart = %v, want %v", from, start.Add(5*time.Minute))
	}
	for m := 5; m < 10; m++ {
		second.ObserveMinute(replayFrame(start.Add(time.Duration(m)*time.Minute), map[string]float64{"a": 100, "b": 90}))
	}

	s := second.sums[pairDay{a: "a", b: "b", day: dayIndex(start)}]
	if s == nil || s.n != 10 {
		t.Fatalf("active minutes after replay = %+v, want 10", s)
	}
}

func TestReplayKeepsDeviceOwner(t *testing.T) {
	start := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	c := NewCollector(DefaultThresholds, time.UTC)
	feed(c, start, 1, func(int) map[string]float64 { return map[string]float64{"a": 100, "b": 90} })
	if got := c.owner["a"]; got != "dev-a" {
		t.Fatalf("owner of a = %q, want dev-a", got)
	}

	c.ObserveMinute(replayFrame(start.Add(time.Minute), map[string]float64{"a": 100, "b": 90}))

	if got := c.owner["a"]; got != "dev-a" {
		t.Errorf("replay changed owner of a to %q, want dev-a", got)
	}
	if got := c.owner["b"]; got != "dev-b" {
		t.Errorf("replay changed owner of b to %q, want dev-b", got)
	}
}

func TestReplayPersistsLastMinute(t *testing.T) {
	start := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	path := filepath.Join(t.TempDir(), "suggest.json")
	c := Load(path, DefaultThresholds, time.UTC, noLog)
	c.SetPersistence(path, noLog)
	// Nur leere Minuten: die Summen bleiben leer, die Minute muss trotzdem gespeichert werden.
	c.ObserveMinute(learnrec.Frame{Minute: start})
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}

	reloaded := Load(path, DefaultThresholds, time.UTC, noLog)
	if got := reloaded.ReplayFrom(); !got.Equal(start.Add(time.Minute)) {
		t.Errorf("ReplayFrom after reload = %v, want %v", got, start.Add(time.Minute))
	}
}
