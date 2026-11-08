// Package chainsuggest sammelt Minutenmittel der Leistungs-Entitaeten und
// haelt je Paar und Tag die Summen, aus denen spaeter Kettenvorschlaege
// berechnet werden. Es veraendert keine Konfiguration.
package chainsuggest

import (
	"math"
	"sort"
	"sync"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/energy"
	"github.com/Developer-Simon/energy-node-dashboard/internal/energydiscovery"
	"github.com/Developer-Simon/energy-node-dashboard/internal/registry"
)

// Thresholds bündelt die Schwellen fuer Sammeln und Vorschlaege.
type Thresholds struct {
	ActiveW          float64
	MinR             float64
	MinREdge         float64
	MinRatio         float64
	MinStdW          float64
	MinMeanAbsDiffW  float64
	MinDays          int
	MinActiveMinutes int
	WindowDays       int
	MaxEntities      int
	MaxSuggestions   int
}

// DefaultThresholds sind die Werte aus der Spec.
var DefaultThresholds = Thresholds{
	ActiveW:          20,
	MinR:             0.95,
	MinREdge:         0.90,
	MinRatio:         0.7,
	MinStdW:          10,
	MinMeanAbsDiffW:  0.5,
	MinDays:          3,
	MinActiveMinutes: 720,
	WindowDays:       7,
	MaxEntities:      64,
	MaxSuggestions:   20,
}

// FastThresholds gilt fuer die Demo (ENERGY_NODE_CHAIN_SUGGEST_FAST=1).
var FastThresholds = func() Thresholds {
	th := DefaultThresholds
	th.MinDays = 0
	th.MinActiveMinutes = 3
	return th
}()

// clockReady ist der fruehste Zeitpunkt, dem die Uhr vertraut wird. Vor einer
// gestellten Uhr (Pi ohne Netz) wuerden Minuten in das Jahr 1970 fallen.
var clockReady = time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)

// pairKey ordnet immer die kleinere Entitaets-ID zuerst.
type pairKey struct {
	a, b string
}

type pairDay struct {
	a, b string
	day  int
}

// sums sind die Summen eines Paares an einem Tag ueber aktive Minuten.
type sums struct {
	n                          int
	sa, sb, saa, sbb, sab, sad float64
}

type minuteSample struct {
	sum float64
	n   int
}

type participant struct {
	device string
	w      float64
}

// Collector sammelt Minutenmittel. Alle Methoden sind nebenlaeufig nutzbar.
type Collector struct {
	mu  sync.Mutex
	th  Thresholds
	loc *time.Location

	// minute ist der Beginn der laufenden Minute, zero solange nichts gesammelt wurde.
	minute time.Time
	buf    map[string]*minuteSample
	// devices sind die Teilnehmer der letzten Beobachtung: Entitaet -> Geraete-ID.
	devices map[string]string
	// owner ordnet jeder Entitaet das Geraet zu, das sie zuletzt hatte. Anders als
	// devices wird es nie geleert, damit Suggestions auch nach einem leeren Abschluss
	// der Minute die Geraete kennt.
	owner   map[string]string
	sums    map[pairDay]*sums
	lastDay int

	// dismissed haelt verworfene Paare. Es ist in dieser Stufe noch leer.
	dismissed map[pairKey]bool
}

// NewCollector legt einen leeren Sammler an. loc bestimmt die lokalen Tage.
func NewCollector(th Thresholds, loc *time.Location) *Collector {
	if loc == nil {
		loc = time.Local
	}
	return &Collector{
		th:        th,
		loc:       loc,
		buf:       map[string]*minuteSample{},
		devices:   map[string]string{},
		owner:     map[string]string{},
		sums:      map[pairDay]*sums{},
		dismissed: map[pairKey]bool{},
	}
}

// Observe nimmt einen Snapshot der Geraete zur Zeit at entgegen. Ein Wechsel
// der Minute schliesst die vorige Minute ab.
func (c *Collector) Observe(at time.Time, devices []registry.DeviceView) {
	if at.Before(clockReady) {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	local := at.In(c.loc)
	y, mo, d := local.Date()
	minute := time.Date(y, mo, d, local.Hour(), local.Minute(), 0, 0, c.loc)
	if !c.minute.IsZero() && !minute.Equal(c.minute) {
		c.closeMinuteLocked()
	}
	c.minute = minute

	participants := c.participants(devices)
	c.devices = make(map[string]string, len(participants))
	for id, p := range participants {
		c.devices[id] = p.device
		c.owner[id] = p.device
		s := c.buf[id]
		if s == nil {
			s = &minuteSample{}
			c.buf[id] = s
		}
		s.sum += p.w
		s.n++
	}
}

// participants waehlt die Entitaeten aus, die in diese Beobachtung eingehen.
// Geraet energy_node, veraltete oder nicht verfuegbare Werte, fehlende Werte
// und Nicht-Leistungs-Einheiten fallen weg. Ueber MaxEntities gewinnen die
// ersten nach ID.
func (c *Collector) participants(devices []registry.DeviceView) map[string]participant {
	found := map[string]participant{}
	for _, dev := range devices {
		if dev.ID == energydiscovery.DeviceIdentifier {
			continue
		}
		for _, e := range dev.Entities {
			if e.Stale || (e.HasAvailability && !e.Available) || !e.HasValue {
				continue
			}
			w, ok := energy.PowerW(e.Value, e.UnitOfMeasurement)
			if !ok || math.IsNaN(w) || math.IsInf(w, 0) {
				continue
			}
			if _, seen := found[e.UniqueID]; seen {
				continue
			}
			found[e.UniqueID] = participant{device: dev.ID, w: math.Abs(w)}
		}
	}

	ids := make([]string, 0, len(found))
	for id := range found {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	if len(ids) > c.th.MaxEntities {
		ids = ids[:c.th.MaxEntities]
	}
	kept := make(map[string]participant, len(ids))
	for _, id := range ids {
		kept[id] = found[id]
	}
	return kept
}

// closeMinuteLocked rechnet die Minutenmittel aus und addiert die aktiven
// Paare zu den Tagessummen. Die Minute muss schon in c.minute stehen.
func (c *Collector) closeMinuteLocked() {
	means := make(map[string]float64, len(c.buf))
	ids := make([]string, 0, len(c.buf))
	for id, s := range c.buf {
		if s.n == 0 {
			continue
		}
		means[id] = s.sum / float64(s.n)
		ids = append(ids, id)
	}
	c.buf = map[string]*minuteSample{}

	day := dayIndex(c.minute)
	if day != c.lastDay {
		c.lastDay = day
		c.pruneLocked(day)
	}
	if len(ids) < 2 {
		return
	}
	sort.Strings(ids)

	for i := range ids {
		for j := i + 1; j < len(ids); j++ {
			a, b := ids[i], ids[j]
			ma, mb := means[a], means[b]
			if ma < c.th.ActiveW && mb < c.th.ActiveW {
				continue
			}
			key := pairDay{a: a, b: b, day: day}
			s := c.sums[key]
			if s == nil {
				s = &sums{}
				c.sums[key] = s
			}
			s.n++
			s.sa += ma
			s.sb += mb
			s.saa += ma * ma
			s.sbb += mb * mb
			s.sab += ma * mb
			s.sad += math.Abs(ma - mb)
		}
	}
}

// pruneLocked wirft Tage aus dem Fenster. Mit WindowDays 7 bleiben die
// letzten sieben Tage einschliesslich des heutigen.
func (c *Collector) pruneLocked(day int) {
	for key := range c.sums {
		if key.day <= day-c.th.WindowDays {
			delete(c.sums, key)
		}
	}
}

// dayIndex ist die Nummer des lokalen Kalendertages seit der Unixzeit.
func dayIndex(t time.Time) int {
	y, m, d := t.Date()
	return int(time.Date(y, m, d, 0, 0, 0, 0, time.UTC).Unix() / 86400)
}
