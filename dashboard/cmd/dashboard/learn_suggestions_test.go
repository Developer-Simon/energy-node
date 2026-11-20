package main

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/autolearn"
)

// fakeCandidates answers fixed engine candidates.
type fakeCandidates []autolearn.Candidate

func (f fakeCandidates) Candidates() []autolearn.Candidate { return f }

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
