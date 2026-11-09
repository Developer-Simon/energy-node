package chainsuggest

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/energy"
)

var storeT0 = time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)

func noLog(string, ...any) {}

func TestCollectorPersistsSumsAndDismissed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "chains", "suggest.json")
	th := baseThresholds()
	c := NewCollector(th, time.UTC)
	c.SetPersistence(path, noLog)
	feed(c, storeT0, 60, func(m int) map[string]float64 {
		return map[string]float64{"a": ramp(m), "b": ramp(m)*0.9 + wobble(m)}
	})
	flush(c, storeT0.Add(60*time.Minute))
	if got := c.Suggestions(energy.Model{}, noEdge); len(got) != 1 {
		t.Fatalf("before dismiss: %d suggestions, want 1", len(got))
	}

	if err := c.Dismiss("b", "a"); err != nil {
		t.Fatalf("Dismiss: %v", err)
	}
	if got := c.Suggestions(energy.Model{}, noEdge); len(got) != 0 {
		t.Fatalf("after dismiss: %d suggestions, want 0", len(got))
	}
	c.Close()

	reloaded := Load(path, th, time.UTC, noLog)
	if !reflect.DeepEqual(c.sums, reloaded.sums) {
		t.Errorf("reloaded sums differ:\n got %v\nwant %v", reloaded.sums, c.sums)
	}
	if !reflect.DeepEqual(c.owner, reloaded.owner) {
		t.Errorf("reloaded owner = %v, want %v", reloaded.owner, c.owner)
	}
	if got := reloaded.Suggestions(energy.Model{}, noEdge); len(got) != 0 {
		t.Errorf("dismissed pair came back after reload: %v", got)
	}
	if !reloaded.dismissed[pairKey{a: "a", b: "b"}] {
		t.Errorf("reloaded dismissed = %v, want a-b", reloaded.dismissed)
	}
}

func TestCollectorLoadBrokenFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "suggest.json")
	if err := os.WriteFile(path, []byte("{kaputt"), 0o644); err != nil {
		t.Fatal(err)
	}
	var logs int
	count := func(string, ...any) { logs++ }
	c := Load(path, baseThresholds(), time.UTC, count)
	if len(c.sums) != 0 || len(c.dismissed) != 0 || len(c.owner) != 0 {
		t.Errorf("broken file not empty: sums=%d dismissed=%d owner=%d", len(c.sums), len(c.dismissed), len(c.owner))
	}
	if logs != 1 {
		t.Errorf("log calls = %d, want 1", logs)
	}

	feed(c, storeT0, 2, func(int) map[string]float64 { return map[string]float64{"a": 100, "b": 90} })
	if len(c.sums) == 0 {
		t.Error("empty collector from broken file does not collect")
	}
}

func TestCollectorSavesHourly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "chains", "suggest.json")
	c := NewCollector(baseThresholds(), time.UTC)
	c.SetPersistence(path, noLog)
	for m := 0; m < 60; m++ {
		c.Observe(storeT0.Add(time.Duration(m)*time.Minute), devicesFor(map[string]float64{"a": 100, "b": 90}))
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("file written before one hour: %v", err)
	}
	c.Observe(storeT0.Add(60*time.Minute), devicesFor(map[string]float64{"a": 100, "b": 90}))
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("file missing after one hour: %v", err)
	}
}

func TestThresholdsFromEnv(t *testing.T) {
	cases := map[string]Thresholds{
		"1":   FastThresholds,
		"":    DefaultThresholds,
		"0":   DefaultThresholds,
		"x":   DefaultThresholds,
		" 1 ": FastThresholds,
	}
	for value, want := range cases {
		got := ThresholdsFromEnv(func(key string) string {
			if key != "ENERGY_NODE_CHAIN_SUGGEST_FAST" {
				t.Errorf("read %q, want ENERGY_NODE_CHAIN_SUGGEST_FAST", key)
			}
			return value
		})
		if got != want {
			t.Errorf("value %q: got %+v, want %+v", value, got, want)
		}
	}
}

func TestDismissRejectsBadIDs(t *testing.T) {
	c := NewCollector(baseThresholds(), time.UTC)
	for _, pair := range [][2]string{{"", "b"}, {"a", ""}, {"a", "a"}} {
		if err := c.Dismiss(pair[0], pair[1]); err == nil {
			t.Errorf("Dismiss(%q, %q) accepted", pair[0], pair[1])
		}
	}
}
