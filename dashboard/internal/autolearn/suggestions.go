package autolearn

import (
	"encoding/json"
	"errors"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/energystats"
)

// laterDuration is how long a postponed suggestion stays hidden.
const laterDuration = 7 * 24 * time.Hour

// suggestionFile is the content of suggestions.json.
type suggestionFile struct {
	Dismissed []string         `json:"dismissed"`
	Later     map[string]int64 `json:"later"`
	Shown     map[string]Shown `json:"shown"`
}

// SuggestionState keeps the suggestions the user dismissed or postponed, and
// what the suggestion band showed last time. It is safe for concurrent use.
type SuggestionState struct {
	mu        sync.Mutex
	path      string
	logf      func(string, ...any)
	dismissed map[string]bool
	later     map[string]int64
	shown     map[string]Shown
	// dirty is set while a change is not yet on disk.
	dirty bool
}

// LoadSuggestionState reads the suggestion state from path. A missing file
// means an empty state. A damaged file is logged and starts empty.
func LoadSuggestionState(path string, logf func(string, ...any)) *SuggestionState {
	if logf == nil {
		logf = log.Printf
	}
	s := &SuggestionState{
		path:      path,
		logf:      logf,
		dismissed: map[string]bool{},
		later:     map[string]int64{},
		shown:     map[string]Shown{},
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return s
	}
	var f suggestionFile
	if err == nil {
		err = json.Unmarshal(data, &f)
	}
	if err != nil {
		logf("autolearn: %s unreadable, starting empty: %v", path, err)
		return s
	}
	for _, id := range f.Dismissed {
		s.dismissed[id] = true
	}
	for id, until := range f.Later {
		s.later[id] = until
	}
	for id, sh := range f.Shown {
		s.shown[id] = copyShown(sh)
	}
	return s
}

// Dismiss hides the finding with the given ID for good, until Undo.
func (s *SuggestionState) Dismiss(id string, _ time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.dismissed[id] = true
	delete(s.later, id)
	s.dirty = true
	return s.saveLocked()
}

// Later hides the finding with the given ID for seven days.
func (s *SuggestionState) Later(id string, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.dismissed, id)
	s.later[id] = now.Add(laterDuration).Unix()
	s.dirty = true
	return s.saveLocked()
}

// Undo brings back a finding that was dismissed or postponed.
func (s *SuggestionState) Undo(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.dismissed, id)
	delete(s.later, id)
	s.dirty = true
	return s.saveLocked()
}

// Visible returns the findings that may be shown now: not dismissed, not
// postponed past now, and not on a device that does not exist any more. A
// finding without device stays. exists may be nil, which keeps every device.
// A finding with a heating season rests while the mean outdoor temperature
// recentC is above the season limit. Nil recentC is unknown and keeps it.
func (s *SuggestionState) Visible(all []Finding, now time.Time, exists func(device string) bool, recentC *float64) []Finding {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Finding
	for _, f := range all {
		if s.dismissed[f.ID] {
			continue
		}
		if until, ok := s.later[f.ID]; ok && now.Unix() < until {
			continue
		}
		if f.Device != "" && exists != nil && !exists(f.Device) {
			continue
		}
		if f.Season != nil && f.Season.HeatingBelowC != nil && recentC != nil && *recentC > *f.Season.HeatingBelowC {
			continue
		}
		out = append(out, f)
	}
	return out
}

// Hidden reports whether the state holds a dismissal or a postponement for the
// finding. It needs no finding list, so it also works after a restart.
func (s *SuggestionState) Hidden(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.dismissed[id] {
		return true
	}
	_, ok := s.later[id]
	return ok
}

// Shown returns a copy of what the suggestion band showed last time.
func (s *SuggestionState) Shown() map[string]Shown {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]Shown, len(s.shown))
	for id, sh := range s.shown {
		out[id] = copyShown(sh)
	}
	return out
}

// SetShown replaces what is shown now, as returned by Gate. A new ID gets
// Since = now, a known ID keeps its old Since.
func (s *SuggestionState) SetShown(shown map[string]Shown, now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := make(map[string]Shown, len(shown))
	for id, sh := range shown {
		if old, ok := s.shown[id]; ok {
			sh.Since = old.Since
		} else {
			sh.Since = now.Unix()
		}
		next[id] = copyShown(sh)
	}
	s.shown = next
	s.dirty = true
}

// Prune drops the shown entries of findings whose device does not exist any
// more. It works in memory, Save writes the result. Call it before Save.
func (s *SuggestionState) Prune(exists func(device string) bool, all []Finding) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if exists == nil {
		return
	}
	for _, f := range all {
		if f.Device == "" || exists(f.Device) {
			continue
		}
		if _, ok := s.shown[f.ID]; ok {
			delete(s.shown, f.ID)
			s.dirty = true
		}
	}
}

// ResetLearned clears the shown entries and writes the result. Dismissed and
// postponed findings stay.
func (s *SuggestionState) ResetLearned() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.shown = map[string]Shown{}
	s.dirty = true
	return s.saveLocked()
}

// Save writes the state atomically if it changed since the last write. A
// failed write stays dirty and is retried by the next Save.
func (s *SuggestionState) Save() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.saveLocked()
}

// saveLocked writes the state if it is dirty. The caller holds mu.
func (s *SuggestionState) saveLocked() error {
	if s.path == "" || !s.dirty {
		return nil
	}
	dismissed := make([]string, 0, len(s.dismissed))
	for id := range s.dismissed {
		dismissed = append(dismissed, id)
	}
	sort.Strings(dismissed)
	data, err := json.Marshal(suggestionFile{Dismissed: dismissed, Later: s.later, Shown: s.shown})
	if err != nil {
		return err
	}
	if err := energystats.WriteFileAtomic(filepath.Dir(s.path), s.path, data); err != nil {
		return err
	}
	s.dirty = false
	return nil
}

// copyShown returns sh with its own Values map.
func copyShown(sh Shown) Shown {
	out := sh
	if sh.Values != nil {
		out.Values = make(map[string]float64, len(sh.Values))
		for k, v := range sh.Values {
			out.Values[k] = v
		}
	}
	return out
}
