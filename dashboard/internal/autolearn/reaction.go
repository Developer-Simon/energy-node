package autolearn

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/energystats"
)

const (
	reactionFileVersion = 1
	reactionSamples     = 20
	reactionDefault     = 5 * time.Second
)

// reactionFile ist das Format der Reaktionsdatei.
type reactionFile struct {
	Version  int                        `json:"version"`
	Entities map[string][]time.Duration `json:"entities"`
}

// Reactions haelt die letzten Reaktionszeiten je Entitaet, also die Zeit
// zwischen einem Befehl und dem Schaltvorgang.
type Reactions struct {
	mu     sync.Mutex
	values map[string][]time.Duration
	path   string
	logf   func(string, ...any)
	dirty  bool
}

// NewReactions liefert einen leeren Speicher ohne Datei.
func NewReactions() *Reactions {
	return &Reactions{values: map[string][]time.Duration{}}
}

// LoadReactions liest die Reaktionszeiten aus path. Eine fehlende Datei ergibt
// einen leeren Speicher. Eine kaputte Datei ergibt ebenfalls einen leeren
// Speicher und genau einen Logeintrag.
func LoadReactions(path string, logf func(string, ...any)) *Reactions {
	if logf == nil {
		logf = log.Printf
	}
	r := NewReactions()
	r.SetPersistence(path, logf)
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return r
	}
	if err == nil {
		err = r.restore(data)
	}
	if err != nil {
		logf("autolearn: %s unreadable, starting empty: %v", path, err)
		fresh := NewReactions()
		fresh.SetPersistence(path, logf)
		return fresh
	}
	return r
}

// restore fuellt den leeren Speicher aus der Datei.
func (r *Reactions) restore(data []byte) error {
	var f reactionFile
	if err := json.Unmarshal(data, &f); err != nil {
		return err
	}
	if f.Version != reactionFileVersion {
		return fmt.Errorf("version %d", f.Version)
	}
	values := make(map[string][]time.Duration, len(f.Entities))
	for id, vals := range f.Entities {
		if id == "" {
			return fmt.Errorf("entity with empty id")
		}
		for _, d := range vals {
			if d < 0 {
				return fmt.Errorf("entity %s: negative reaction", id)
			}
		}
		if len(vals) > reactionSamples {
			vals = vals[len(vals)-reactionSamples:]
		}
		values[id] = vals
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.values = values
	return nil
}

// SetPersistence legt fest, wohin Save schreibt, und wohin Ladefehler gemeldet
// werden. Ohne Aufruf wird nichts geschrieben.
func (r *Reactions) SetPersistence(path string, logf func(string, ...any)) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if logf == nil {
		logf = log.Printf
	}
	r.path = path
	r.logf = logf
}

// Save schreibt die Datei, wenn sich seit dem letzten Speichern etwas geaendert
// hat. Ohne SetPersistence passiert nichts.
func (r *Reactions) Save() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.path == "" || !r.dirty {
		return nil
	}
	data, err := json.Marshal(reactionFile{Version: reactionFileVersion, Entities: r.values})
	if err != nil {
		return err
	}
	if err := energystats.WriteFileAtomic(filepath.Dir(r.path), r.path, data); err != nil {
		return err
	}
	r.dirty = false
	return nil
}

// Reset verwirft alle Reaktionszeiten und loescht die Datei.
func (r *Reactions) Reset() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.values = map[string][]time.Duration{}
	r.dirty = false
	if r.path == "" {
		return nil
	}
	if err := os.Remove(r.path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// Observe merkt eine Reaktionszeit der Entitaet. Es bleiben die letzten 20.
func (r *Reactions) Observe(entity string, d time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()
	vals := append(r.values[entity], d)
	if len(vals) > reactionSamples {
		vals = vals[len(vals)-reactionSamples:]
	}
	r.values[entity] = vals
	r.dirty = true
}

// sortedLocked liefert eine sortierte Kopie der Werte. Der Aufrufer haelt mu.
func (r *Reactions) sortedLocked(entity string) []time.Duration {
	vals := append([]time.Duration(nil), r.values[entity]...)
	sort.Slice(vals, func(i, j int) bool { return vals[i] < vals[j] })
	return vals
}

// Typical liefert den Median der Reaktionszeiten, ohne Werte 5 s.
func (r *Reactions) Typical(entity string) time.Duration {
	r.mu.Lock()
	defer r.mu.Unlock()
	vals := r.sortedLocked(entity)
	n := len(vals)
	switch {
	case n == 0:
		return reactionDefault
	case n%2 == 1:
		return vals[n/2]
	default:
		return (vals[n/2-1] + vals[n/2]) / 2
	}
}

// P95 liefert den 95. Perzentil der Reaktionszeiten (nächster Rang), ohne
// Werte 5 s.
func (r *Reactions) P95(entity string) time.Duration {
	r.mu.Lock()
	defer r.mu.Unlock()
	vals := r.sortedLocked(entity)
	n := len(vals)
	if n == 0 {
		return reactionDefault
	}
	idx := (95*n+99)/100 - 1
	if idx < 0 {
		idx = 0
	}
	return vals[idx]
}
