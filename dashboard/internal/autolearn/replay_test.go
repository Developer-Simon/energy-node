package autolearn

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"slices"
	"sort"
	"strconv"
	"testing"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/sun"
)

// replayVectorsPath is the shared vector file that the Python engine tests too.
const replayVectorsPath = "../../../services/automation/tests/replay_vectors.json"

// defaultLearnedMaxAgeS mirrors DEFAULT_MAX_AGE_S of learned.py.
const defaultLearnedMaxAgeS = 604800

type replayVector struct {
	Name     string                       `json:"name"`
	Rule     json.RawMessage              `json:"rule"`
	Scenario json.RawMessage              `json:"scenario"`
	Settings map[string]float64           `json:"settings"`
	Start    string                       `json:"start"`
	Steps    []map[string]json.RawMessage `json:"steps"`
	Expect   struct {
		Fires  []int64  `json:"fires"`
		Phases []string `json:"phases"`
	} `json:"expect"`
}

func loadReplayVectors(t *testing.T) []replayVector {
	t.Helper()
	raw, err := os.ReadFile(replayVectorsPath)
	if err != nil {
		t.Fatalf("read vectors: %v", err)
	}
	var vectors []replayVector
	if err := json.Unmarshal(raw, &vectors); err != nil {
		t.Fatalf("parse vectors: %v", err)
	}
	return vectors
}

// stepTime reads the "t" of a step in seconds.
func stepTime(t *testing.T, step map[string]json.RawMessage) int64 {
	t.Helper()
	var at int64
	if err := json.Unmarshal(step["t"], &at); err != nil {
		t.Fatalf("step t: %v", err)
	}
	return at
}

// vectorMaxAges collects max_age_s per learned key from the conditions of a
// vector. A key without max_age_s gets the default.
func vectorMaxAges(t *testing.T, v replayVector) map[string]float64 {
	t.Helper()
	ages := map[string]float64{}
	var conds []json.RawMessage
	collect := func(raw json.RawMessage) {
		var doc struct {
			Conditions []json.RawMessage `json:"conditions"`
		}
		if len(raw) > 0 && string(raw) != "null" && json.Unmarshal(raw, &doc) == nil {
			conds = append(conds, doc.Conditions...)
		}
	}
	if len(v.Rule) > 0 {
		var rule struct {
			Conditions []json.RawMessage `json:"conditions"`
		}
		if err := json.Unmarshal(v.Rule, &rule); err != nil {
			t.Fatalf("rule: %v", err)
		}
		conds = append(conds, rule.Conditions...)
	} else {
		var scenario struct {
			When  json.RawMessage `json:"when"`
			Until json.RawMessage `json:"until"`
		}
		if err := json.Unmarshal(v.Scenario, &scenario); err != nil {
			t.Fatalf("scenario: %v", err)
		}
		collect(scenario.When)
		collect(scenario.Until)
	}
	for _, c := range conds {
		var fields struct {
			Learned string   `json:"learned"`
			MaxAge  *float64 `json:"max_age_s"`
		}
		if err := json.Unmarshal(c, &fields); err != nil || fields.Learned == "" {
			continue
		}
		age := float64(defaultLearnedMaxAgeS)
		if fields.MaxAge != nil {
			age = *fields.MaxAge
		}
		ages[fields.Learned] = age
	}
	return ages
}

// vectorTicks builds the ticks of a vector on its tick grid, as the Python
// driver does: a step applies at its tick and holds until the next step.
// Learned values expire by their max_age_s and are dropped from the tick.
func vectorTicks(t *testing.T, v replayVector, loc *time.Location, tick int64) ([]ReplayMinute, []int64) {
	t.Helper()
	start, err := time.ParseInLocation("2006-01-02T15:04:05", v.Start, loc)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	steps := map[int64]map[string]json.RawMessage{}
	var last int64
	for _, step := range v.Steps {
		at := stepTime(t, step)
		steps[at] = step
		last = max(last, at)
	}
	ages := vectorMaxAges(t, v)

	type stored struct {
		value float64
		at    time.Time
	}
	store := map[string]stored{}
	var balance map[string]float64
	var ticks []ReplayMinute
	var offsets []int64
	for off := int64(0); off <= last; off += tick {
		if step, ok := steps[off]; ok {
			if raw, ok := step["balance"]; ok {
				balance = nil
				if string(raw) != "null" {
					balance = map[string]float64{}
					if err := json.Unmarshal(raw, &balance); err != nil {
						t.Fatalf("balance at %d: %v", off, err)
					}
				}
			}
			if raw, ok := step["learned"]; ok {
				var spec map[string]json.RawMessage
				if err := json.Unmarshal(raw, &spec); err != nil {
					t.Fatalf("learned at %d: %v", off, err)
				}
				now := start.Add(time.Duration(off) * time.Second)
				for key, value := range spec {
					if string(value) == "null" {
						delete(store, key)
						continue
					}
					var entry struct {
						Value float64 `json:"value"`
						AgeS  float64 `json:"age_s"`
					}
					if err := json.Unmarshal(value, &entry.Value); err != nil {
						if err := json.Unmarshal(value, &entry); err != nil {
							t.Fatalf("learned %s at %d: %v", key, off, err)
						}
					}
					store[key] = stored{value: entry.Value, at: now.Add(-time.Duration(entry.AgeS) * time.Second)}
				}
			}
		}
		now := start.Add(time.Duration(off) * time.Second)
		learned := map[string]float64{}
		for key, st := range store {
			maxAge, ok := ages[key]
			if !ok {
				maxAge = defaultLearnedMaxAgeS
			}
			if now.Sub(st.at).Seconds() > maxAge {
				continue
			}
			learned[key] = st.value
		}
		ticks = append(ticks, ReplayMinute{At: now, Balance: balance, Learned: learned})
		offsets = append(offsets, off)
	}
	return ticks, offsets
}

func TestReplayVectors(t *testing.T) {
	loc, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Skipf("no Europe/Berlin zone: %v", err)
	}
	vectors := loadReplayVectors(t)
	if len(vectors) < 24 {
		t.Fatalf("want at least 24 vectors, got %d", len(vectors))
	}
	names := map[string]bool{}
	for _, v := range vectors {
		if names[v.Name] {
			t.Fatalf("duplicate vector name %s", v.Name)
		}
		names[v.Name] = true
	}
	for _, v := range vectors {
		t.Run(v.Name, func(t *testing.T) {
			tick := int64(10)
			if s, ok := v.Settings["tick_interval_s"]; ok {
				tick = int64(s)
			}
			lat, lon := 52.52, 13.405
			if s, ok := v.Settings["latitude"]; ok {
				lat = s
			}
			if s, ok := v.Settings["longitude"]; ok {
				lon = s
			}
			raw := v.Rule
			if len(raw) == 0 {
				raw = v.Scenario
			}
			ticks, offsets := vectorTicks(t, v, loc, tick)
			res, err := replayCore(context.Background(), json.RawMessage(raw), ReplayTarget{}, ticks, nil,
				time.Duration(tick)*time.Second, loc, sun.Location{Lat: lat, Lon: lon})
			if err != nil {
				t.Fatalf("replay: %v", err)
			}
			index := map[int64]int{}
			for i, off := range offsets {
				index[off] = i
			}
			var phases []string
			var stepOffsets []int64
			for _, step := range v.Steps {
				stepOffsets = append(stepOffsets, stepTime(t, step))
			}
			sort.Slice(stepOffsets, func(i, j int) bool { return stepOffsets[i] < stepOffsets[j] })
			for _, off := range stepOffsets {
				phases = append(phases, res.Phases[index[off]])
			}
			if !slices.Equal(res.Fires, v.Expect.Fires) {
				t.Errorf("fires: got %v, want %v", res.Fires, v.Expect.Fires)
			}
			if !slices.Equal(phases, v.Expect.Phases) {
				t.Errorf("phases: got %v, want %v", phases, v.Expect.Phases)
			}
		})
	}
}

func TestReplayClosedLoopCapsGridImport(t *testing.T) {
	rule := `{"id":"r1","enabled":true,"conditions":[{"type":"balance_threshold","field":"grid_export","comparison":"above","threshold":800,"hysteresis":0,"hold_seconds":0}]}`
	target := ReplayTarget{Device: "heater", OpW: 2000, CycleWh: 100000}
	ticks := make([]ReplayMinute, 4)
	base := time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)
	for i := range ticks {
		ticks[i] = ReplayMinute{At: base.Add(time.Duration(i) * time.Minute),
			Balance: map[string]float64{"grid_export": 2500, "grid_import": 0}}
	}
	res, err := replayCore(context.Background(), json.RawMessage(rule), target, ticks, nil, time.Minute, time.UTC, sun.Location{})
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if !res.On[1] || !res.On[3] {
		t.Fatalf("device should stay on, On = %v", res.On)
	}
	// Tick 0 has no device load yet. From tick 1 on the device takes 2000 W,
	// so 2500 W of export leave 500 W and no grid import.
	if res.ExportWh != (2500+500+500+500)/60.0 {
		t.Errorf("export Wh = %v, want %v", res.ExportWh, (2500+500+500+500)/60.0)
	}
	if res.GridWh != 0 {
		t.Errorf("grid Wh = %v, want 0", res.GridWh)
	}
}

func TestReplayStartPeakInFirstTwoMinutes(t *testing.T) {
	rule := `{"id":"r1","enabled":true,"conditions":[{"type":"balance_threshold","field":"grid_export","comparison":"above","threshold":800,"hysteresis":0,"hold_seconds":0}]}`
	target := ReplayTarget{Device: "heater", OpW: 1000, PeakW: 1500, CycleWh: 100000}
	base := time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)
	ticks := make([]ReplayMinute, 3)
	for i := range ticks {
		ticks[i] = ReplayMinute{At: base.Add(time.Duration(i) * time.Minute),
			Balance: map[string]float64{"grid_export": 3000, "grid_import": 0}}
	}
	res, err := replayCore(context.Background(), json.RawMessage(rule), target, ticks, nil, time.Minute, time.UTC, sun.Location{})
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if !res.On[0] {
		t.Fatalf("device should be on after the fire, On = %v", res.On)
	}
	// Tick 1 is inside the first two minutes: 1000 W plus 1500 W peak.
	// Export 3000 W leaves 500 W, tick 2 is outside the peak: 1000 W leaves 2000 W.
	exportWh := 3000.0 + 500.0 + 2000.0
	if res.ExportWh != exportWh/60.0 {
		t.Errorf("export Wh = %v, want %v", res.ExportWh, exportWh/60.0)
	}
}

func TestReplayCycleEndsAtCycleEnergy(t *testing.T) {
	rule := `{"id":"r1","enabled":true,"conditions":[{"type":"balance_threshold","field":"grid_export","comparison":"above","threshold":800,"hysteresis":0,"hold_seconds":0}]}`
	// One minute at 1000 W is 16.67 Wh, so a cycle of 60 Wh runs four ticks.
	target := ReplayTarget{Device: "wallbox", OpW: 1000, CycleWh: 60}
	base := time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)
	ticks := make([]ReplayMinute, 8)
	for i := range ticks {
		ticks[i] = ReplayMinute{At: base.Add(time.Duration(i) * time.Minute),
			Balance: map[string]float64{"grid_export": 2000, "grid_import": 0}}
	}
	res, err := replayCore(context.Background(), json.RawMessage(rule), target, ticks, nil, time.Minute, time.UTC, sun.Location{})
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	want := []bool{true, true, true, true, false, false, false, false}
	if !slices.Equal(res.On, want) {
		t.Errorf("On = %v, want %v", res.On, want)
	}
	if res.Switches != 2 {
		t.Errorf("switches = %d, want 2", res.Switches)
	}
}

func TestReplayChainConditionIsNotReplayable(t *testing.T) {
	raw := `{"id":"r1","enabled":true,"conditions":[{"type":"chain_value","chain_id":"c","field":"x","comparison":"above","value":1}]}`
	var rule Rule
	rule.ID = "r1"
	rule.Enabled = true
	rule.Conditions = []Condition{{Type: "chain_value", Raw: json.RawMessage(`{"type":"chain_value","chain_id":"c","field":"x","comparison":"above","value":1}`)}}
	if err := Replayable(rule); !errors.Is(err, ErrNotReplayable) {
		t.Fatalf("Replayable: got %v, want ErrNotReplayable", err)
	}
	base := time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)
	ticks := []ReplayMinute{{At: base, Balance: map[string]float64{}}}
	if _, err := Replay(context.Background(), json.RawMessage(raw), ReplayTarget{}, ticks, nil, time.UTC, sun.Location{}); !errors.Is(err, ErrNotReplayable) {
		t.Fatalf("Replay: got %v, want ErrNotReplayable", err)
	}
}

func TestReplayContextCancelReturnsContextError(t *testing.T) {
	rule := `{"id":"r1","enabled":true,"conditions":[{"type":"balance_threshold","field":"grid_export","comparison":"above","threshold":800,"hysteresis":0,"hold_seconds":0}]}`
	base := time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)
	ticks := []ReplayMinute{{At: base, Balance: map[string]float64{"grid_export": 900}}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Replay(ctx, json.RawMessage(rule), ReplayTarget{}, ticks, nil, time.UTC, sun.Location{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v, want context.Canceled", err)
	}
}

func TestReplayClockHelpers(t *testing.T) {
	h, m, err := parseClock("06:30")
	if err != nil || h != 6 || m != 30 {
		t.Fatalf("parseClock: %d %d %v", h, m, err)
	}
	if _, _, err := parseClock("25:00"); !errors.Is(err, ErrNotReplayable) {
		t.Fatalf("parseClock out of range: %v", err)
	}
	if got := strconv.Itoa(pyWeekday(time.Date(2026, 11, 22, 0, 0, 0, 0, time.UTC))); got != "6" {
		t.Fatalf("Sunday should be 6, got %s", got)
	}
}
