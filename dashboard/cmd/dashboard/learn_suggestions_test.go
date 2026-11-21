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

	gateSuggestions(book, engine, noRules, true, exists, time.Now())

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

	gateSuggestions(book, engine, noRules, false, exists, time.Now())

	if got := book.Visible(time.Now(), exists); len(got) != 0 {
		t.Fatalf("visible with habit off = %+v", got)
	}
}

func TestSuggestionBookDismissHidesAndKeepsKnown(t *testing.T) {
	state := autolearn.LoadSuggestionState(filepath.Join(t.TempDir(), "suggestions.json"), nil)
	book := &suggestionBook{state: state}
	gateSuggestions(book, fakeCandidates{habitCandidate("switchtime:a", "dev-a", 2)}, noRules, true, func(string) bool { return true }, time.Now())
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
	gateSuggestions(book, fakeCandidates{habitCandidate("switchtime:a", "dev-a", 2)}, noRules, true, func(string) bool { return true }, time.Now())
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

	gateSuggestions(book, fakeCandidates{habitCandidate("switchtime:a", "dev-a", 2)}, noRules, true, exists, now)
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
	gateSuggestions(book, fakeCandidates{changed}, noRules, true, exists, now)
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("same IDs with changed numbers were written (stat err %v)", err)
	}

	gateSuggestions(book, fakeCandidates{changed, habitCandidate("switchtime:b", "dev-b", 1)}, noRules, true, exists, now)
	data, err = os.ReadFile(path)
	if err != nil {
		t.Fatalf("appearing suggestion not saved: %v", err)
	}
	if !strings.Contains(string(data), "switchtime:a") || !strings.Contains(string(data), "switchtime:b") {
		t.Fatalf("saved file lacks a shown suggestion: %s", data)
	}
}
