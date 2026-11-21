package main

import (
	"log"
	"sync"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/autolearn"
)

// suggestionCandidates is the part of the engine the suggestions read.
type suggestionCandidates interface {
	Candidates() []autolearn.Candidate
	RecentOutdoorC() *float64
	HeatingKnee(device string) (float64, bool)
}

// suggestionBook keeps the suggestions of the last engine round and the
// user's dismissals. It is safe for concurrent use.
type suggestionBook struct {
	state *autolearn.SuggestionState
	mu    sync.Mutex
	all   []autolearn.Finding
	// recentC is the outdoor temperature of the last round, nil when unknown.
	recentC *float64
}

// Visible returns the suggestions to show now, without dismissed or postponed
// ones and without devices exists rejects.
func (b *suggestionBook) Visible(now time.Time, exists func(device string) bool) []autolearn.Finding {
	b.mu.Lock()
	all := append([]autolearn.Finding(nil), b.all...)
	recentC := b.recentC
	b.mu.Unlock()
	return b.state.Visible(all, now, exists, recentC)
}

// Known reports whether id is in the list of the last round, or whether the
// state holds it as dismissed or postponed. The second keeps undo working after
// a restart, before the next round.
func (b *suggestionBook) Known(id string) bool {
	if b.state.Hidden(id) {
		return true
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, f := range b.all {
		if f.ID == id {
			return true
		}
	}
	return false
}

func (b *suggestionBook) Dismiss(id string, now time.Time) error {
	return b.state.Dismiss(id, now)
}

func (b *suggestionBook) Later(id string, now time.Time) error {
	return b.state.Later(id, now)
}

func (b *suggestionBook) Undo(id string) error {
	return b.state.Undo(id)
}

// Reset forgets the list and the shown state, dismissals stay.
func (b *suggestionBook) Reset() error {
	b.mu.Lock()
	b.all = nil
	b.mu.Unlock()
	return b.state.ResetLearned()
}

// gateSuggestions runs the habit and energy candidates of the last engine round
// through the gates and stores the result in book. Only the groups that are on
// in groups count. Candidates of devices that do not exist any more are dropped
// before the gate, so they cannot take a slot. A finding on a device with a
// heating knee gets the knee as its season limit, except temperature findings.
func gateSuggestions(book *suggestionBook, source suggestionCandidates, readDoc func() ([]byte, error), groups map[string]bool, exists func(device string) bool, now time.Time) {
	var cands []autolearn.Candidate
	for _, c := range source.Candidates() {
		g := c.Finding.Group
		if (g != "habit" && g != "energy") || !groups[g] {
			continue
		}
		if c.Finding.Device != "" && exists != nil && !exists(c.Finding.Device) {
			continue
		}
		cands = append(cands, c)
	}
	// The detector of a finding decides below whether it gets the heating limit.
	detectors := make(map[string]string, len(cands))
	for _, c := range cands {
		detectors[c.Finding.ID] = c.Detector
	}
	var doc autolearn.Doc
	if raw, err := readDoc(); err == nil {
		if parsed, err := autolearn.ParseDoc(raw); err == nil {
			doc = parsed
		}
	}
	before := shownIDs(book.state.Shown())
	out, shown := autolearn.Gate(cands, doc, book.state.Shown())
	for i := range out {
		f := &out[i]
		// The temperature limit is the cure for warm weather, so it is never rested.
		if f.Device == "" || detectors[f.ID] == "temperature" {
			continue
		}
		knee, ok := source.HeatingKnee(f.Device)
		if !ok {
			continue
		}
		// A copy, the season of the candidate belongs to the engine. A finding
		// without a season gets one that holds only the heating limit.
		var season autolearn.Season
		if f.Season != nil {
			season = *f.Season
		}
		season.HeatingBelowC = &knee
		f.Season = &season
	}
	book.mu.Lock()
	book.all = out
	book.recentC = source.RecentOutdoorC()
	book.mu.Unlock()
	book.state.SetShown(shown, now)
	book.state.Prune(exists, out)
	// A changed set of shown IDs is saved at once, so a restart keeps the calm
	// rule. Changed numbers alone wait for the hourly saver.
	if !sameIDs(before, shownIDs(book.state.Shown())) {
		if err := book.state.Save(); err != nil {
			log.Printf("suggestions: save after a changed set failed: %v", err)
		}
	}
}

// shownIDs is the set of the IDs in shown.
func shownIDs(shown map[string]autolearn.Shown) map[string]bool {
	ids := make(map[string]bool, len(shown))
	for id := range shown {
		ids[id] = true
	}
	return ids
}

// sameIDs reports whether both sets hold the same IDs.
func sameIDs(a, b map[string]bool) bool {
	if len(a) != len(b) {
		return false
	}
	for id := range a {
		if !b[id] {
			return false
		}
	}
	return true
}
