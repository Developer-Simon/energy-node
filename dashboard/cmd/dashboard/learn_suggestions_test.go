package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/autolearn"
)

// fakeCandidates answers fixed engine candidates.
type fakeCandidates []autolearn.Candidate

func (f fakeCandidates) Candidates() []autolearn.Candidate { return f }

// RecentOutdoorC reports no temperature, so no heating season rests.
func (f fakeCandidates) RecentOutdoorC() *float64 { return nil }

// HeatingKnee reports no knee, so no finding gets a season limit.
func (f fakeCandidates) HeatingKnee(string) (float64, bool) { return 0, false }

// Groups of the suggestion gate: only the habit group, and none.
var (
	groupsHabit = map[string]bool{"habit": true}
	groupsNone  = map[string]bool{}
)

// habitCandidate is a candidate that passes every gate of the habit group.
func habitCandidate(id, device string, kwh float64) autolearn.Candidate {
	return autolearn.Candidate{
		Finding:     autolearn.Finding{ID: id, Kind: "switchtime", Group: "habit", Device: device, Severity: "info", Confidence: "high"},
		Detector:    "switchtime",
		NEff:        10,
		MinNEff:     5,
		Lower:       1,
		Limit:       0.5,
		HalvesAgree: true,
		Benefit:     autolearn.Benefit{Unit: "kwh_week", Value: kwh},
	}
}

func noRules() ([]byte, error) { return []byte(`{"rules":[],"scenarios":[]}`), nil }

func TestGateSuggestionsDropsVanishedDevicesBeforeGate(t *testing.T) {
	state := autolearn.LoadSuggestionState(filepath.Join(t.TempDir(), "suggestions.json"), nil)
	book := &suggestionBook{state: state}
	// The vanished device has the larger benefit, so it would take the first slot.
	engine := fakeCandidates{habitCandidate("switchtime:gone", "gone", 9), habitCandidate("switchtime:a", "dev-a", 2)}
	exists := func(device string) bool { return device == "dev-a" }

	gateSuggestions(book, engine, noRules, groupsHabit, exists, time.Now())

	visible := book.Visible(time.Now(), exists)
	if len(visible) != 1 || visible[0].ID != "switchtime:a" {
		t.Fatalf("visible = %+v, want only switchtime:a", visible)
	}
	if book.Known("switchtime:gone") {
		t.Fatal("vanished device kept in the suggestion list")
	}
	if _, ok := state.Shown()["switchtime:gone"]; ok {
		t.Fatal("vanished device kept in the shown state")
	}
}

func TestGateSuggestionsHabitOffIsEmpty(t *testing.T) {
	state := autolearn.LoadSuggestionState(filepath.Join(t.TempDir(), "suggestions.json"), nil)
	book := &suggestionBook{state: state}
	engine := fakeCandidates{habitCandidate("switchtime:a", "dev-a", 2)}
	exists := func(string) bool { return true }

	gateSuggestions(book, engine, noRules, groupsNone, exists, time.Now())

	if got := book.Visible(time.Now(), exists); len(got) != 0 {
		t.Fatalf("visible with habit off = %+v", got)
	}
}

func TestSuggestionBookDismissHidesAndKeepsKnown(t *testing.T) {
	state := autolearn.LoadSuggestionState(filepath.Join(t.TempDir(), "suggestions.json"), nil)
	book := &suggestionBook{state: state}
	gateSuggestions(book, fakeCandidates{habitCandidate("switchtime:a", "dev-a", 2)}, noRules, groupsHabit, func(string) bool { return true }, time.Now())
	exists := func(string) bool { return true }

	if err := book.Dismiss("switchtime:a", time.Now()); err != nil {
		t.Fatal(err)
	}
	if got := book.Visible(time.Now(), exists); len(got) != 0 {
		t.Fatalf("dismissed suggestion visible: %+v", got)
	}
	if !book.Known("switchtime:a") {
		t.Fatal("dismissed suggestion must stay known for undo")
	}
	if err := book.Undo("switchtime:a"); err != nil {
		t.Fatal(err)
	}
	if got := book.Visible(time.Now(), exists); len(got) != 1 {
		t.Fatalf("undone suggestion not visible: %+v", got)
	}
}

func TestSuggestionUndoWorksAfterRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "suggestions.json")
	state := autolearn.LoadSuggestionState(path, nil)
	book := &suggestionBook{state: state}
	gateSuggestions(book, fakeCandidates{habitCandidate("switchtime:a", "dev-a", 2)}, noRules, groupsHabit, func(string) bool { return true }, time.Now())
	if err := book.Dismiss("switchtime:a", time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := book.Later("switchtime:b", time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := state.Save(); err != nil {
		t.Fatal(err)
	}

	// After a restart the list is empty until the next engine round.
	restarted := &suggestionBook{state: autolearn.LoadSuggestionState(path, nil)}
	for _, id := range []string{"switchtime:a", "switchtime:b"} {
		if !restarted.Known(id) {
			t.Fatalf("%s unknown after restart, undo would answer 404", id)
		}
	}
	if restarted.Known("switchtime:never") {
		t.Fatal("unknown ID reported as known")
	}
	if err := restarted.Undo("switchtime:a"); err != nil {
		t.Fatal(err)
	}
	if restarted.state.Hidden("switchtime:a") {
		t.Fatal("undo after restart did not bring the suggestion back")
	}
}

// TestGateSuggestionsSavesOnlyWhenTheShownSetChanges: a new suggestion is written
// at once, the same IDs with changed numbers are not written, so the SD card is
// spared the minute rounds. The file is removed between the rounds to see the
// writes without timing.
func TestGateSuggestionsSavesOnlyWhenTheShownSetChanges(t *testing.T) {
	path := filepath.Join(t.TempDir(), "suggestions.json")
	state := autolearn.LoadSuggestionState(path, nil)
	book := &suggestionBook{state: state}
	exists := func(string) bool { return true }
	now := time.Now()

	gateSuggestions(book, fakeCandidates{habitCandidate("switchtime:a", "dev-a", 2)}, noRules, groupsHabit, exists, now)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("new suggestion not saved: %v", err)
	}
	if !strings.Contains(string(data), "switchtime:a") {
		t.Fatalf("saved file lacks the shown suggestion: %s", data)
	}

	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	changed := habitCandidate("switchtime:a", "dev-a", 2)
	changed.Lower = 0.9
	gateSuggestions(book, fakeCandidates{changed}, noRules, groupsHabit, exists, now)
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("same IDs with changed numbers were written (stat err %v)", err)
	}

	gateSuggestions(book, fakeCandidates{changed, habitCandidate("switchtime:b", "dev-b", 1)}, noRules, groupsHabit, exists, now)
	data, err = os.ReadFile(path)
	if err != nil {
		t.Fatalf("appearing suggestion not saved: %v", err)
	}
	if !strings.Contains(string(data), "switchtime:a") || !strings.Contains(string(data), "switchtime:b") {
		t.Fatalf("saved file lacks a shown suggestion: %s", data)
	}
}

// kneeCandidates answers a heating knee for dev-a and a fixed mean temperature.
type kneeCandidates struct {
	fakeCandidates
	recent float64
}

func (k kneeCandidates) HeatingKnee(device string) (float64, bool) { return 15, device == "dev-a" }

func (k kneeCandidates) RecentOutdoorC() *float64 { r := k.recent; return &r }

// energyCandidate is a heating season finding of the energy group on dev-a.
func energyCandidate() autolearn.Candidate {
	c := habitCandidate("temp_limit:r1", "dev-a", 2)
	c.Finding.Group = "energy"
	c.Finding.Season = &autolearn.Season{FromWeek: 1, ToWeek: 53}
	return c
}

func TestGateSuggestionsEnergyGroupSwitch(t *testing.T) {
	state := autolearn.LoadSuggestionState(filepath.Join(t.TempDir(), "suggestions.json"), nil)
	book := &suggestionBook{state: state}
	exists := func(string) bool { return true }
	src := fakeCandidates{energyCandidate()}
	gateSuggestions(book, src, noRules, groupsNone, exists, time.Now())
	if got := book.Visible(time.Now(), exists); len(got) != 0 {
		t.Fatalf("energy group off: visible = %d, want 0", len(got))
	}
	gateSuggestions(book, src, noRules, map[string]bool{"energy": true}, exists, time.Now())
	if got := book.Visible(time.Now(), exists); len(got) != 1 {
		t.Fatalf("energy group on: visible = %d, want 1", len(got))
	}
}

func TestGateSuggestionsHeatingKneeRestsWarmDays(t *testing.T) {
	energy := map[string]bool{"energy": true}
	exists := func(string) bool { return true }
	for _, tc := range []struct {
		name   string
		recent float64
		want   int
	}{
		{"warm three-day mean rests", 18, 0},
		{"cold three-day mean shows", 10, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state := autolearn.LoadSuggestionState(filepath.Join(t.TempDir(), "suggestions.json"), nil)
			book := &suggestionBook{state: state}
			src := kneeCandidates{fakeCandidates: fakeCandidates{energyCandidate()}, recent: tc.recent}
			gateSuggestions(book, src, noRules, energy, exists, time.Now())
			if got := book.Visible(time.Now(), exists); len(got) != tc.want {
				t.Fatalf("visible = %d, want %d", len(got), tc.want)
			}
		})
	}
}

// The device finding gets the knee of dev-a, the temperature finding stays without a season.
func TestGateSuggestionsDeviceFindingGetsHeatingLimit(t *testing.T) {
	state := autolearn.LoadSuggestionState(filepath.Join(t.TempDir(), "suggestions.json"), nil)
	book := &suggestionBook{state: state}
	exists := func(string) bool { return true }
	src := kneeCandidates{fakeCandidates: fakeCandidates{habitCandidate("switchtime:a", "dev-a", 2)}, recent: 10}
	gateSuggestions(book, src, noRules, groupsHabit, exists, time.Now())
	got := book.Visible(time.Now(), exists)
	if len(got) != 1 || got[0].Season == nil || got[0].Season.HeatingBelowC == nil {
		t.Fatalf("visible = %+v, want one finding with a heating limit", got)
	}
	if *got[0].Season.HeatingBelowC != 15 {
		t.Fatalf("heating limit = %v, want 15", *got[0].Season.HeatingBelowC)
	}
	if got[0].Season.FromWeek != 0 || got[0].Season.ToWeek != 0 {
		t.Fatalf("season = %+v, want no week range", got[0].Season)
	}
}

func TestGateSuggestionsDeviceFindingWithoutKneeGetsNoSeason(t *testing.T) {
	state := autolearn.LoadSuggestionState(filepath.Join(t.TempDir(), "suggestions.json"), nil)
	book := &suggestionBook{state: state}
	exists := func(string) bool { return true }
	src := kneeCandidates{fakeCandidates: fakeCandidates{habitCandidate("switchtime:b", "dev-b", 2)}, recent: 18}
	gateSuggestions(book, src, noRules, groupsHabit, exists, time.Now())
	got := book.Visible(time.Now(), exists)
	if len(got) != 1 || got[0].Season != nil {
		t.Fatalf("visible = %+v, want one finding without a season", got)
	}
}

func TestGateSuggestionsTemperatureFindingIsNeverRested(t *testing.T) {
	state := autolearn.LoadSuggestionState(filepath.Join(t.TempDir(), "suggestions.json"), nil)
	book := &suggestionBook{state: state}
	exists := func(string) bool { return true }
	temp := habitCandidate("temp_limit:a", "dev-a", 2)
	temp.Detector = "temperature"
	src := kneeCandidates{fakeCandidates: fakeCandidates{temp}, recent: 18}
	gateSuggestions(book, src, noRules, groupsHabit, exists, time.Now())
	got := book.Visible(time.Now(), exists)
	if len(got) != 1 {
		t.Fatalf("visible = %d, want the temperature finding shown in warm weather", len(got))
	}
	if got[0].Season != nil {
		t.Fatalf("season = %+v, want none for a temperature finding", got[0].Season)
	}
}

func TestGateSuggestionsDeviceFindingWithKneeRestsWarmDays(t *testing.T) {
	exists := func(string) bool { return true }
	for _, tc := range []struct {
		name   string
		recent float64
		want   int
	}{
		{"warm three-day mean rests", 18, 0},
		{"cold three-day mean shows", 10, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state := autolearn.LoadSuggestionState(filepath.Join(t.TempDir(), "suggestions.json"), nil)
			book := &suggestionBook{state: state}
			src := kneeCandidates{fakeCandidates: fakeCandidates{habitCandidate("switchtime:a", "dev-a", 2)}, recent: tc.recent}
			gateSuggestions(book, src, noRules, groupsHabit, exists, time.Now())
			if got := book.Visible(time.Now(), exists); len(got) != tc.want {
				t.Fatalf("visible = %d, want %d", len(got), tc.want)
			}
		})
	}
}

// outdoorRuleDoc holds one rule with a below condition on the outdoor sensor.
const outdoorRuleDoc = `{"rules":[{"id":"r1","name":"Heizung","enabled":true,"conditions":[{"type":"entity_value","topic":"t/out","entity_id":"sensor.out","comparison":"below","value":15.4}],"actions":[]}],"scenarios":[]}`

func outdoorRule() ([]byte, error) { return []byte(outdoorRuleDoc), nil }

// appliedCandidate is a habit candidate whose Apply adds the outdoor limit to rule r1.
func appliedCandidate() autolearn.Candidate {
	c := habitCandidate("temp_limit:r1", "dev-a", 2)
	c.Finding.Apply = []autolearn.Fix{{Code: "add_condition", RuleID: "r1", Ops: []autolearn.Op{{
		Path:  []any{"conditions", 1},
		Value: map[string]any{"type": "entity_value", "topic": "t/out", "entity_id": "sensor.out", "comparison": "below", "value": 15.0},
	}}}}
	return c
}

func TestGateSuggestionsDropsAppliedConditionAtGate(t *testing.T) {
	state := autolearn.LoadSuggestionState(filepath.Join(t.TempDir(), "suggestions.json"), nil)
	book := &suggestionBook{state: state}
	exists := func(string) bool { return true }
	gateSuggestions(book, fakeCandidates{appliedCandidate()}, outdoorRule, groupsHabit, exists, time.Now())
	if got := book.Visible(time.Now(), exists); len(got) != 0 {
		t.Fatalf("visible = %d, want 0 for an applied condition", len(got))
	}
}

// The suggestion was shown before the owner applied it. The next refresh hides it
// without a new engine round, because the book reads the current rules.
func TestVisibleHidesSuggestionOnceRuleHoldsCondition(t *testing.T) {
	state := autolearn.LoadSuggestionState(filepath.Join(t.TempDir(), "suggestions.json"), nil)
	rules := noRules
	book := &suggestionBook{state: state, readDoc: func() ([]byte, error) { return rules() }}
	exists := func(string) bool { return true }
	gateSuggestions(book, fakeCandidates{appliedCandidate()}, noRules, groupsHabit, exists, time.Now())
	if got := book.Visible(time.Now(), exists); len(got) != 1 {
		t.Fatalf("before apply: visible = %d, want 1", len(got))
	}
	rules = outdoorRule
	if got := book.Visible(time.Now(), exists); len(got) != 0 {
		t.Fatalf("after apply: visible = %d, want 0 on the next refresh", len(got))
	}
}

func TestVisibleKeepsSuggestionWhenRuleUnreadable(t *testing.T) {
	state := autolearn.LoadSuggestionState(filepath.Join(t.TempDir(), "suggestions.json"), nil)
	book := &suggestionBook{state: state, readDoc: func() ([]byte, error) { return nil, os.ErrNotExist }}
	exists := func(string) bool { return true }
	gateSuggestions(book, fakeCandidates{appliedCandidate()}, noRules, groupsHabit, exists, time.Now())
	if got := book.Visible(time.Now(), exists); len(got) != 1 {
		t.Fatalf("visible = %d, want 1 when the rules cannot be read", len(got))
	}
}
