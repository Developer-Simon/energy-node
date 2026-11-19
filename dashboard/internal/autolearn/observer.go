package autolearn

import (
	"log"
	"math"
	"sort"
	"sync"
	"time"
)

const (
	// observerKeepDays ist das Fenster fuer Stunden und Tagesbereiche.
	observerKeepDays = 30
	// observerMaxEvents ist die Laenge des Ereignisrings je Regel.
	observerMaxEvents = 200
)

// clockReady ist der fruehste Zeitpunkt, dem die Uhr vertraut wird, wie in
// chainsuggest. Davor wird nichts gesammelt.
var clockReady = time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)

// Observer sammelt Ausloesungen je Regel und Tagesbereiche je Schluessel. Alle
// Methoden sind nebenlaeufig nutzbar.
type Observer struct {
	mu  sync.Mutex
	loc *time.Location

	rules  map[string]*RuleStats
	values map[string][]DayRange
	// last ist der zuletzt gesehene trigger_count je Regel.
	last map[string]int

	// path ist die Datei fuer die Beobachtungen, leer ohne Speichern.
	path string
	logf func(string, ...any)
	// dirty ist gesetzt, solange sich seit dem letzten Speichern etwas geaendert hat.
	dirty bool
	// next ist der Zeitpunkt der naechsten Speicherung, wenn dirty.
	next time.Time
}

// ObserverState ist eine Kopie der Beobachtungen fuer die Pruefungen.
type ObserverState struct {
	Rules  map[string]RuleStats
	Values map[string][]DayRange
}

// RuleStats sind die Beobachtungen einer Regel. Hours zaehlt Ausloesungen je
// Unix-Stunde ueber 30 Tage, Events ist der Ring der letzten Ausloesungen als
// Unixzeit.
type RuleStats struct {
	FirstSeen time.Time
	Hours     map[int64]int
	Events    []int64
}

// DayRange ist der Tagesbereich eines Schluessels. Day ist die Nummer des
// lokalen Kalendertages seit der Unixzeit.
type DayRange struct {
	Day      int64
	Min, Max float64
}

// NewObserver legt einen leeren Beobachter an. loc bestimmt die lokalen Tage.
func NewObserver(loc *time.Location) *Observer {
	if loc == nil {
		loc = time.Local
	}
	return &Observer{
		loc:    loc,
		rules:  map[string]*RuleStats{},
		values: map[string][]DayRange{},
		last:   map[string]int{},
		logf:   log.Printf,
	}
}

// ObserveTriggers nimmt den trigger_count je Regel zur Zeit at entgegen. Eine
// Erhoehung zaehlt in die Stunde von at, ein Rueckgang setzt nur den neuen Wert.
func (o *Observer) ObserveTriggers(at time.Time, counts map[string]int) {
	if at.Before(clockReady) {
		return
	}

	o.mu.Lock()
	defer o.mu.Unlock()

	hour := at.Unix() / 3600
	for id, count := range counts {
		st := o.rules[id]
		if st == nil {
			st = &RuleStats{FirstSeen: at, Hours: map[int64]int{}}
			o.rules[id] = st
			o.last[id] = count
			o.dirty = true
			continue
		}
		prev, seen := o.last[id]
		o.last[id] = count
		if count != prev {
			o.dirty = true
		}
		if !seen || count < prev {
			continue
		}
		if diff := count - prev; diff > 0 {
			st.Hours[hour] += diff
			st.Events = append(st.Events, at.Unix())
			if len(st.Events) > observerMaxEvents {
				st.Events = append([]int64(nil), st.Events[len(st.Events)-observerMaxEvents:]...)
			}
			o.dirty = true
		}
	}
	for _, st := range o.rules {
		for h := range st.Hours {
			if hour-h > observerKeepDays*24 {
				delete(st.Hours, h)
				o.dirty = true
			}
		}
	}
	o.saveDueLocked(at)
}

// ObserveValues nimmt die Werte je Schluessel zur Zeit at entgegen und haelt
// Minimum und Maximum des laufenden Tages. Nicht endliche Werte fallen weg.
func (o *Observer) ObserveValues(at time.Time, values map[string]float64) {
	if at.Before(clockReady) {
		return
	}

	o.mu.Lock()
	defer o.mu.Unlock()

	day := o.dayIndex(at)
	for key, v := range values {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			continue
		}
		ranges := o.values[key]
		i := sort.Search(len(ranges), func(i int) bool { return ranges[i].Day >= day })
		if i < len(ranges) && ranges[i].Day == day {
			if v < ranges[i].Min {
				ranges[i].Min = v
			}
			if v > ranges[i].Max {
				ranges[i].Max = v
			}
		} else {
			ranges = append(ranges, DayRange{})
			copy(ranges[i+1:], ranges[i:])
			ranges[i] = DayRange{Day: day, Min: v, Max: v}
		}
		o.values[key] = ranges
		o.dirty = true
	}
	for key, ranges := range o.values {
		kept := ranges[:0]
		for _, r := range ranges {
			if day-r.Day < observerKeepDays {
				kept = append(kept, r)
			} else {
				o.dirty = true
			}
		}
		o.values[key] = kept
	}
	o.saveDueLocked(at)
}

// Snapshot liefert eine Kopie der Beobachtungen. Aenderungen an ihr wirken
// nicht auf den Beobachter.
func (o *Observer) Snapshot() ObserverState {
	o.mu.Lock()
	defer o.mu.Unlock()

	st := ObserverState{
		Rules:  make(map[string]RuleStats, len(o.rules)),
		Values: make(map[string][]DayRange, len(o.values)),
	}
	for id, r := range o.rules {
		hours := make(map[int64]int, len(r.Hours))
		for h, n := range r.Hours {
			hours[h] = n
		}
		st.Rules[id] = RuleStats{
			FirstSeen: r.FirstSeen,
			Hours:     hours,
			Events:    append([]int64(nil), r.Events...),
		}
	}
	for key, ranges := range o.values {
		st.Values[key] = append([]DayRange(nil), ranges...)
	}
	return st
}

// Prune wirft Regeln und Schluessel weg, die nicht in keepRules oder keepKeys
// stehen. Sie sind nicht mehr in der Regeldatei.
func (o *Observer) Prune(keepRules map[string]bool, keepKeys map[string]bool) {
	o.mu.Lock()
	defer o.mu.Unlock()

	for id := range o.rules {
		if !keepRules[id] {
			delete(o.rules, id)
			delete(o.last, id)
			o.dirty = true
		}
	}
	for key := range o.values {
		if !keepKeys[key] {
			delete(o.values, key)
			o.dirty = true
		}
	}
}

// dayIndex ist die Nummer des lokalen Kalendertages seit der Unixzeit.
func (o *Observer) dayIndex(t time.Time) int64 {
	y, m, d := t.In(o.loc).Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC).Unix() / 86400
}
