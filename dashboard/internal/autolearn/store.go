package autolearn

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/energystats"
)

const observerFileVersion = 1

// observerFile ist das Format der Beobachtungsdatei.
type observerFile struct {
	Version int                   `json:"version"`
	Rules   map[string]ruleFile   `json:"rules"`
	Values  map[string][]DayRange `json:"values"`
}

// ruleFile ist eine Regel in der Datei. Last ist der zuletzt gesehene Zaehlerstand.
type ruleFile struct {
	FirstSeen time.Time     `json:"first_seen"`
	Hours     map[int64]int `json:"hours"`
	Events    []int64       `json:"events"`
	Last      int           `json:"last"`
}

// LoadObserver liest die Beobachtungen aus path. Eine fehlende Datei ergibt
// einen leeren Beobachter. Eine kaputte Datei ergibt ebenfalls einen leeren
// Beobachter und genau einen Logeintrag.
func LoadObserver(path string, loc *time.Location, logf func(string, ...any)) *Observer {
	if logf == nil {
		logf = log.Printf
	}
	o := NewObserver(loc)
	o.SetPersistence(path)
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return o
	}
	if err == nil {
		err = o.restore(data)
	}
	if err != nil {
		logf("autolearn: %s unreadable, starting empty: %v", path, err)
		fresh := NewObserver(loc)
		fresh.SetPersistence(path)
		return fresh
	}
	return o
}

// restore fuellt den leeren Beobachter aus der Datei. Bei einem Fehler bleibt
// er unveraendert, damit der Aufrufer ihn verwerfen kann.
func (o *Observer) restore(data []byte) error {
	var f observerFile
	if err := json.Unmarshal(data, &f); err != nil {
		return err
	}
	if f.Version != observerFileVersion {
		return fmt.Errorf("version %d", f.Version)
	}
	for id, r := range f.Rules {
		if id == "" {
			return fmt.Errorf("rule with empty id")
		}
		if len(r.Events) > observerMaxEvents {
			return fmt.Errorf("rule %s: %d events", id, len(r.Events))
		}
	}
	for key, ranges := range f.Values {
		if key == "" {
			return fmt.Errorf("value with empty key")
		}
		for _, r := range ranges {
			if r.Min > r.Max {
				return fmt.Errorf("value %s day %d: min above max", key, r.Day)
			}
		}
	}

	rules := make(map[string]*RuleStats, len(f.Rules))
	last := make(map[string]int, len(f.Rules))
	for id, r := range f.Rules {
		hours := r.Hours
		if hours == nil {
			hours = map[int64]int{}
		}
		rules[id] = &RuleStats{FirstSeen: r.FirstSeen, Hours: hours, Events: r.Events}
		last[id] = r.Last
	}
	values := make(map[string][]DayRange, len(f.Values))
	for key, ranges := range f.Values {
		sort.Slice(ranges, func(i, j int) bool { return ranges[i].Day < ranges[j].Day })
		values[key] = ranges
	}

	o.rules = rules
	o.last = last
	o.values = values
	return nil
}

// SetPersistence legt fest, wohin gespeichert wird. Ohne Aufruf wird nichts
// geschrieben.
func (o *Observer) SetPersistence(path string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.path = path
}

// Save schreibt die Datei, wenn sich seit dem letzten Speichern etwas geaendert
// hat. Ohne SetPersistence passiert nichts. Der Lernhub ruft Save stuendlich auf
// und beim Beenden, ein Fehler kommt zurueck und wird dort erneut versucht.
func (o *Observer) Save() error {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.path == "" || !o.dirty {
		return nil
	}
	return o.saveLocked()
}

// saveLocked schreibt die Datei atomar.
func (o *Observer) saveLocked() error {
	f := observerFile{
		Version: observerFileVersion,
		Rules:   make(map[string]ruleFile, len(o.rules)),
		Values:  make(map[string][]DayRange, len(o.values)),
	}
	for id, r := range o.rules {
		f.Rules[id] = ruleFile{FirstSeen: r.FirstSeen, Hours: r.Hours, Events: r.Events, Last: o.last[id]}
	}
	for key, ranges := range o.values {
		f.Values[key] = ranges
	}

	data, err := json.Marshal(f)
	if err != nil {
		return err
	}
	if err := energystats.WriteFileAtomic(filepath.Dir(o.path), o.path, data); err != nil {
		return err
	}
	o.dirty = false
	return nil
}
