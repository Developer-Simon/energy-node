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
}

// suggestionBook keeps the suggestions of the last engine round and the
// user's dismissals. It is safe for concurrent use.
type suggestionBook struct {
	state *autolearn.SuggestionState
	mu    sync.Mutex
	all   []autolearn.Finding
}

// Visible returns the suggestions to show now, without dismissed or postponed
// ones and without devices exists rejects.
func (b *suggestionBook) Visible(now time.Time, exists func(device string) bool) []autolearn.Finding {
	b.mu.Lock()
	all := append([]autolearn.Finding(nil), b.all...)
	b.mu.Unlock()
	return b.state.Visible(all, now, exists)
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

// gateSuggestions runs the habit candidates of the last engine round through
// the gates and stores the result in book. Candidates of devices that do not
// exist any more are dropped before the gate, so they cannot take a slot.
// Without the habit group the list is empty.
func gateSuggestions(book *suggestionBook, source suggestionCandidates, readDoc func() ([]byte, error), habitOn bool, exists func(device string) bool, now time.Time) {
	var cands []autolearn.Candidate
	if habitOn {
		for _, c := range source.Candidates() {
			if c.Finding.Group != "habit" {
				continue
			}
			if c.Finding.Device != "" && exists != nil && !exists(c.Finding.Device) {
				continue
			}
			cands = append(cands, c)
		}
	}
	var doc autolearn.Doc
	if raw, err := readDoc(); err == nil {
		if parsed, err := autolearn.ParseDoc(raw); err == nil {
			doc = parsed
		}
	}
	before := shownIDs(book.state.Shown())
	out, shown := autolearn.Gate(cands, doc, book.state.Shown())
	book.mu.Lock()
	book.all = out
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
