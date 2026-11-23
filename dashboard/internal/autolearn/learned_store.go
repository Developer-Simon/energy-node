package autolearn

import (
	"encoding/json"
	"errors"
	"io/fs"
	"log"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/energystats"
)

const (
	// learnedFileVersion is the version of learned.json.
	learnedFileVersion = 1
	// learnedTopicPrefix is the retained topic tree of the learned values.
	learnedTopicPrefix = "outstation/energy_node/learned/"
	// learnedChangeGap is the time after a publish before a changed value goes out.
	learnedChangeGap = 15 * time.Minute
	// learnedRefreshGap is the time after a publish before an unchanged value
	// goes out again with a new time, so that it does not age out.
	learnedRefreshGap = 60 * time.Minute
)

// LearnedEntry is one learned value as it was last published. Value holds the
// published value, a clock text, a state or a number. Unit and Estimate describe
// the value for the endpoint and are not part of the change check beyond
// Estimate. Prev is the value before the last change, ChangedAt and
// PublishedAt are Unix seconds.
type LearnedEntry struct {
	Key         string `json:"key"`
	Device      string `json:"device,omitempty"`
	Kind        string `json:"kind"`
	Unit        string `json:"unit,omitempty"`
	Value       any    `json:"value"`
	Estimate    bool   `json:"estimate,omitempty"`
	N           int    `json:"n"`
	At          int64  `json:"at"`
	Prev        any    `json:"prev,omitempty"`
	ChangedAt   int64  `json:"changed_at"`
	PublishedAt int64  `json:"published_at"`
	// Programs is the cycle energy of the other programs, endpoint only.
	Programs []float64 `json:"programs,omitempty"`
}

// Message is a retained publish. An empty payload deletes the retained topic.
type Message struct {
	Topic   string
	Payload string
}

// learnedFile is the content of learned.json.
type learnedFile struct {
	Version int            `json:"version"`
	Entries []LearnedEntry `json:"entries"`
}

// learnedPayload is the payload of a learned value topic. Est is set only for
// an estimate, so the payload of a measured value stays as it was.
type learnedPayload struct {
	Value any   `json:"value"`
	N     int   `json:"n"`
	At    int64 `json:"at"`
	Est   bool  `json:"est,omitempty"`
}

// LearnedStore keeps the learned values and what was published last. It decides
// which values go out, and it is safe for concurrent use.
type LearnedStore struct {
	mu      sync.Mutex
	path    string
	logf    func(string, ...any)
	entries map[string]*LearnedEntry
	// before holds the entries as they were before the last Update, nil for an
	// entry that did not exist. Revert restores them for failed publishes.
	before map[string]*LearnedEntry
}

// LoadLearnedStore reads the store from path. A missing file means an empty
// store. A damaged file is logged and starts empty.
func LoadLearnedStore(path string, logf func(string, ...any)) *LearnedStore {
	if logf == nil {
		logf = log.Printf
	}
	s := &LearnedStore{path: path, logf: logf, entries: map[string]*LearnedEntry{}}
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return s
	}
	var f learnedFile
	if err == nil {
		err = json.Unmarshal(data, &f)
	}
	if err != nil {
		logf("autolearn: %s unreadable, starting empty: %v", path, err)
		return s
	}
	for i := range f.Entries {
		e := f.Entries[i]
		s.entries[e.Key] = &e
	}
	return s
}

// Update compares the learned values with what was published and returns the
// messages to publish now. A changed value goes out when the last publish is
// at least learnedChangeGap old. An unchanged value goes out again with a new
// time after learnedRefreshGap. An entry whose device is gone is removed with
// an empty payload. An entry missing from vals stays and is not published.
// Revert takes back the messages that could not be sent.
func (s *LearnedStore) Update(vals []Value, now time.Time, deviceExists func(string) bool) []Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.before = map[string]*LearnedEntry{}
	exists := func(id string) bool { return deviceExists == nil || id == "" || deviceExists(id) }
	at := now.Unix()
	var out []Message

	for _, key := range s.sortedKeysLocked() {
		if e := s.entries[key]; !exists(e.Device) {
			s.touchLocked(key)
			delete(s.entries, key)
			out = append(out, Message{Topic: learnedTopicPrefix + key})
		}
	}

	sorted := append([]Value(nil), vals...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Key < sorted[j].Key })
	for _, v := range sorted {
		cur := learnedCurrent(v)
		// A NaN or Inf does not encode as JSON. Its payload would be empty, which
		// deletes the retained topic, so it is neither stored nor published.
		if f, isNumber := cur.(float64); isNumber && (math.IsNaN(f) || math.IsInf(f, 0)) {
			continue
		}
		e, ok := s.entries[v.Key]
		switch {
		case !ok:
			s.touchLocked(v.Key)
			s.entries[v.Key] = &LearnedEntry{Key: v.Key, Device: v.Device, Kind: v.Kind, Unit: v.Unit,
				Value: cur, Estimate: v.Estimate, N: v.N, At: at, ChangedAt: at, PublishedAt: at,
				Programs: copyPrograms(v.Programs)}
			out = append(out, learnedMessage(v.Key, cur, v.N, at, v.Estimate))
		case e.Value != cur || e.Estimate != v.Estimate:
			if now.Sub(time.Unix(e.PublishedAt, 0)) < learnedChangeGap {
				continue
			}
			s.touchLocked(v.Key)
			e.Prev, e.Value, e.N = e.Value, cur, v.N
			e.Unit, e.Estimate = v.Unit, v.Estimate
			e.Programs = copyPrograms(v.Programs)
			e.At, e.ChangedAt, e.PublishedAt = at, at, at
			out = append(out, learnedMessage(v.Key, cur, v.N, at, v.Estimate))
		default:
			if now.Sub(time.Unix(e.PublishedAt, 0)) < learnedRefreshGap {
				continue
			}
			s.touchLocked(v.Key)
			e.N, e.At, e.PublishedAt = v.N, at, at
			e.Programs = copyPrograms(v.Programs)
			out = append(out, learnedMessage(v.Key, cur, v.N, at, v.Estimate))
		}
	}
	return out
}

// Revert takes back the changes of the last Update for the topics of messages
// that could not be published. The next Update sends them again.
func (s *LearnedStore) Revert(failed []Message) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, m := range failed {
		key := strings.TrimPrefix(m.Topic, learnedTopicPrefix)
		b, ok := s.before[key]
		if !ok {
			continue
		}
		if b == nil {
			delete(s.entries, key)
		} else {
			s.entries[key] = b
		}
		delete(s.before, key)
	}
}

// copyPrograms returns a copy of the programs, nil for none.
func copyPrograms(ps []float64) []float64 {
	if len(ps) == 0 {
		return nil
	}
	return append([]float64(nil), ps...)
}

// Entries returns the entries sorted by key.
func (s *LearnedStore) Entries() []LearnedEntry {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]LearnedEntry, 0, len(s.entries))
	for _, key := range s.sortedKeysLocked() {
		out = append(out, *s.entries[key])
	}
	return out
}

// Reset removes all entries and returns an empty payload for each topic, so the
// retained values are deleted. The file is written empty.
func (s *LearnedStore) Reset() []Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Message
	for _, key := range s.sortedKeysLocked() {
		out = append(out, Message{Topic: learnedTopicPrefix + key})
	}
	s.entries = map[string]*LearnedEntry{}
	s.before = map[string]*LearnedEntry{}
	if err := s.saveLocked(); err != nil {
		s.logf("autolearn: learned values not saved: %v", err)
	}
	return out
}

// Save writes the store atomically. Without a path nothing is written.
func (s *LearnedStore) Save() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.saveLocked()
}

// saveLocked writes learned.json through a temporary file and a rename.
func (s *LearnedStore) saveLocked() error {
	if s.path == "" {
		return nil
	}
	f := learnedFile{Version: learnedFileVersion, Entries: make([]LearnedEntry, 0, len(s.entries))}
	for _, key := range s.sortedKeysLocked() {
		f.Entries = append(f.Entries, *s.entries[key])
	}
	data, err := json.Marshal(f)
	if err != nil {
		return err
	}
	return energystats.WriteFileAtomic(filepath.Dir(s.path), s.path, data)
}

// touchLocked records the entry of key before the change, once per Update.
func (s *LearnedStore) touchLocked(key string) {
	if _, ok := s.before[key]; ok {
		return
	}
	if e, ok := s.entries[key]; ok {
		c := *e
		s.before[key] = &c
		return
	}
	s.before[key] = nil
}

func (s *LearnedStore) sortedKeysLocked() []string {
	keys := make([]string, 0, len(s.entries))
	for key := range s.entries {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// learnedCurrent is the value a learned value publishes: the seconds of a
// duration, the number of a number, the text of a time or a state.
func learnedCurrent(v Value) any {
	if v.Kind == ValueDuration || v.Kind == ValueNumber {
		return v.Num
	}
	return v.Text
}

// learnedMessage is the retained message of one learned value.
func learnedMessage(key string, value any, n int, at int64, est bool) Message {
	payload, _ := json.Marshal(learnedPayload{Value: value, N: n, At: at, Est: est})
	return Message{Topic: learnedTopicPrefix + key, Payload: string(payload)}
}
