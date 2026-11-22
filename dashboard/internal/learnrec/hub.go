package learnrec

import (
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/energy"
	"github.com/Developer-Simon/energy-node-dashboard/internal/energydiscovery"
	"github.com/Developer-Simon/energy-node-dashboard/internal/registry"
)

// MinuteConsumer erhaelt jeden abgeschlossenen Minutenrahmen.
type MinuteConsumer interface {
	ObserveMinute(f Frame)
}

// ReplayConsumer ist ein Minutenverbraucher, der die Minuten ab ReplayFrom aus
// der Aufzeichnung nachlesen kann. Eine Zeit null heisst: nichts nachlesen.
type ReplayConsumer interface {
	MinuteConsumer
	ReplayFrom() time.Time
}

// TickConsumer erhaelt jede Beobachtung sofort.
type TickConsumer interface {
	ObserveTick(at time.Time, snap energy.Snapshot)
}

// Saver speichert seinen Zustand. Der Hub ruft Save hoechstens stuendlich auf.
type Saver interface {
	Save() error
}

// TickFunc macht eine Funktion zum TickConsumer.
type TickFunc func(time.Time, energy.Snapshot)

// ObserveTick ruft die Funktion auf.
func (f TickFunc) ObserveTick(at time.Time, snap energy.Snapshot) { f(at, snap) }

// saveRetryAfter ist die Wartezeit bis zum erneuten Versuch eines fehlgeschlagenen Save.
const saveRetryAfter = 5 * time.Minute

// clockReady ist der fruehste Zeitpunkt, dem die Uhr vertraut wird. Vor einer
// gestellten Uhr (Pi ohne Netz) wuerden Minuten in das Jahr 1970 fallen.
var clockReady = time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)

type namedMinute struct {
	name string
	c    MinuteConsumer
}

type namedReplay struct {
	name string
	c    ReplayConsumer
}

type namedTick struct {
	name string
	c    TickConsumer
}

type namedSaver struct {
	name    string
	s       Saver
	pending bool
	retryAt time.Time
}

type sumCount struct {
	sum float64
	n   int
}

// devAcc sammelt die Beobachtungen eines Geraets in der laufenden Minute.
type devAcc struct {
	power    map[string]*sumCount
	switches map[string]bool
	usable   bool
}

// Hub verteilt Beobachtungen an Verbraucher und sichert die Saver stuendlich.
// Consumer duerfen den Hub nicht selbst aufrufen, sonst blockiert der Mutex.
type Hub struct {
	mu      sync.Mutex
	loc     *time.Location
	limits  func() Limits
	logf    func(string, ...any)
	minutes []namedMinute
	replays []namedReplay
	ticks   []namedTick
	savers  []*namedSaver
	minute  time.Time
	acc     map[string]*devAcc
	balance map[string]float64
	// outdoorID liefert die unique_id der Aussentemperatur-Entitaet. outdoor
	// sammelt deren Beobachtungen in der laufenden Minute.
	outdoorID func() string
	outdoor   sumCount
	hour      time.Time
	stats     map[string]time.Duration
}

// NewHub legt einen Hub an. limits wird bei jeder Beobachtung gelesen.
func NewHub(loc *time.Location, limits func() Limits, logf func(string, ...any)) *Hub {
	return &Hub{
		loc:    loc,
		limits: limits,
		logf:   logf,
		acc:    map[string]*devAcc{},
		stats:  map[string]time.Duration{},
	}
}

// SetOutdoor legt fest, welche Entitaet die Aussentemperatur liefert. id wird
// bei jeder Beobachtung gelesen. Leer oder nil heisst: keine Temperatur.
func (h *Hub) SetOutdoor(id func() string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.outdoorID = id
}

// AddMinute meldet einen Minutenverbraucher an.
func (h *Hub) AddMinute(name string, c MinuteConsumer) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.minutes = append(h.minutes, namedMinute{name: name, c: c})
}

// AddReplay meldet einen Verbraucher an, der mit Replay die Minuten nachlesen
// kann. Er erhaelt nur die Rahmen aus Replay, nicht die Live-Minuten.
func (h *Hub) AddReplay(name string, c ReplayConsumer) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.replays = append(h.replays, namedReplay{name: name, c: c})
}

// Replay gibt jedem Wiederholungsverbraucher die Minuten ab seinem ReplayFrom bis
// vor die Minute von now, aus r. Pro Minute entsteht ein Rahmen mit Leistung je
// Schluessel e:<unique_id> und Bilanz aus b:<feld>. Ohne Aufzeichnung passiert
// nichts. Die Verbraucher laufen ausserhalb des Hub-Mutex.
func (h *Hub) Replay(r Reader, now time.Time) {
	if r == nil {
		return
	}
	h.mu.Lock()
	list := append([]namedReplay(nil), h.replays...)
	h.mu.Unlock()

	for _, nr := range list {
		from := nr.c.ReplayFrom()
		if from.IsZero() {
			continue
		}
		h.replayOne(r, nr.c, from, now)
	}
}

// replayOne gibt einem Verbraucher seine nachgelesenen Rahmen. Jeder Rahmen hat
// je Schluessel ein Geraet, dessen ID der unique_id entspricht.
func (h *Hub) replayOne(r Reader, c MinuteConsumer, from, now time.Time) {
	start := minuteStart(from, h.loc)
	end := minuteStart(now, h.loc)
	if !end.After(start) {
		return
	}
	kl, ok := r.(keyLister)
	if !ok {
		return
	}
	keys := kl.Keys(start, end)
	if len(keys) == 0 {
		return
	}

	series := map[string]Series{}
	var powerKeys, balanceKeys []string
	for _, key := range keys {
		switch {
		case strings.HasPrefix(key, "e:"):
			powerKeys = append(powerKeys, key)
		case strings.HasPrefix(key, "b:"):
			balanceKeys = append(balanceKeys, key)
		default:
			continue
		}
		series[key] = r.Range(key, start, end)
	}

	n := 0
	for _, sr := range series {
		if len(sr.Values) > n {
			n = len(sr.Values)
		}
	}
	for i := 0; i < n; i++ {
		f := Frame{Minute: start.Add(time.Duration(i) * time.Minute)}
		for _, key := range powerKeys {
			v, ok := valueAt(series[key], i)
			if !ok {
				continue
			}
			uid := strings.TrimPrefix(key, "e:")
			f.Devices = append(f.Devices, DeviceMinute{
				ID:        uid,
				Power:     map[string]float64{uid: v},
				Switch:    map[string]bool{},
				Reachable: true,
			})
		}
		for _, key := range balanceKeys {
			v, ok := valueAt(series[key], i)
			if !ok {
				continue
			}
			if f.Balance == nil {
				f.Balance = map[string]float64{}
			}
			f.Balance[strings.TrimPrefix(key, "b:")] = v
		}
		c.ObserveMinute(f)
	}
}

// valueAt liefert den Wert der Minute i, ohne Luecke.
func valueAt(sr Series, i int) (float64, bool) {
	if i >= len(sr.Values) || math.IsNaN(float64(sr.Values[i])) {
		return 0, false
	}
	return float64(sr.Values[i]), true
}

// AddTick meldet einen Taktverbraucher an.
func (h *Hub) AddTick(name string, c TickConsumer) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.ticks = append(h.ticks, namedTick{name: name, c: c})
}

// AddSaver meldet einen Saver an. Saves laufen in Anmeldereihenfolge.
func (h *Hub) AddSaver(name string, s Saver) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.savers = append(h.savers, &namedSaver{name: name, s: s})
}

// Observe nimmt eine Beobachtung entgegen. Ein Minutenwechsel schliesst den
// vorigen Rahmen ab. Die erste Beobachtung einer neuen Stunde stoesst die Saver an.
func (h *Hub) Observe(at time.Time, devices []registry.DeviceView, snap energy.Snapshot, balance map[string]float64) {
	if at.Before(clockReady) {
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	for _, t := range h.ticks {
		t.c.ObserveTick(at, snap)
	}

	local := at.In(h.loc)
	y, mo, d := local.Date()
	minute := time.Date(y, mo, d, local.Hour(), local.Minute(), 0, 0, h.loc)
	if !minute.Equal(h.minute) {
		if !h.minute.IsZero() {
			h.closeMinuteLocked()
		}
		h.minute = minute
	}
	if balance != nil {
		h.balance = balance
	}
	h.accumulateLocked(devices)
	h.accumulateOutdoorLocked(devices)

	hour := time.Date(y, mo, d, local.Hour(), 0, 0, 0, h.loc)
	if h.hour.IsZero() {
		h.hour = hour
	} else if !hour.Equal(h.hour) {
		h.hour = hour
		for _, s := range h.savers {
			s.pending = true
		}
	}
	h.saveDueLocked(at)
}

// accumulateLocked nimmt die Geraete einer Beobachtung in den Minutenrahmen auf.
func (h *Hub) accumulateLocked(devices []registry.DeviceView) {
	byID := make(map[string]registry.DeviceView, len(devices))
	for _, dev := range devices {
		if dev.ID == energydiscovery.DeviceIdentifier {
			continue
		}
		if _, dup := byID[dev.ID]; dup {
			continue
		}
		byID[dev.ID] = dev
	}
	ids := make([]string, 0, len(byID))
	for id := range byID {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	if limit := h.limits().MaxDevices; len(ids) > limit {
		ids = ids[:limit]
	}

	for _, id := range ids {
		a := h.acc[id]
		if a == nil {
			a = &devAcc{power: map[string]*sumCount{}, switches: map[string]bool{}}
			h.acc[id] = a
		}
		seen := map[string]bool{}
		for _, e := range byID[id].Entities {
			if seen[e.UniqueID] {
				continue
			}
			if w, ok := PowerOf(e); ok {
				seen[e.UniqueID] = true
				s := a.power[e.UniqueID]
				if s == nil {
					s = &sumCount{}
					a.power[e.UniqueID] = s
				}
				s.sum += w
				s.n++
				a.usable = true
			}
			if on, ok := SwitchOf(e); ok {
				seen[e.UniqueID] = true
				a.switches[e.UniqueID] = on
				a.usable = true
			}
		}
	}
}

// accumulateOutdoorLocked nimmt den Temperaturwert der gewaehlten Entitaet in
// den Minutenrahmen auf. Die erste Entitaet mit passender unique_id zaehlt.
func (h *Hub) accumulateOutdoorLocked(devices []registry.DeviceView) {
	if h.outdoorID == nil {
		return
	}
	uid := strings.TrimSpace(h.outdoorID())
	if uid == "" {
		return
	}
	for _, dev := range devices {
		if dev.ID == energydiscovery.DeviceIdentifier {
			continue
		}
		for _, e := range dev.Entities {
			if e.UniqueID != uid {
				continue
			}
			if c, ok := TempOf(e); ok {
				h.outdoor.sum += c
				h.outdoor.n++
			}
			return
		}
	}
}

// closeMinuteLocked baut den Rahmen der laufenden Minute und gibt ihn an die
// Minutenverbraucher. Danach beginnt der Sammelpuffer neu.
func (h *Hub) closeMinuteLocked() {
	f := Frame{Minute: h.minute, Balance: h.balance}
	ids := make([]string, 0, len(h.acc))
	for id := range h.acc {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		a := h.acc[id]
		dm := DeviceMinute{
			ID:        id,
			Power:     make(map[string]float64, len(a.power)),
			Switch:    make(map[string]bool, len(a.switches)),
			Reachable: a.usable,
		}
		for uid, s := range a.power {
			dm.Power[uid] = s.sum / float64(s.n)
		}
		for uid, on := range a.switches {
			dm.Switch[uid] = on
		}
		f.Devices = append(f.Devices, dm)
	}

	if h.outdoor.n > 0 {
		c := h.outdoor.sum / float64(h.outdoor.n)
		f.OutdoorC = &c
	}
	h.outdoor = sumCount{}
	h.acc = map[string]*devAcc{}
	h.balance = nil
	for _, m := range h.minutes {
		m.c.ObserveMinute(f)
	}
}

// saveDueLocked stoesst alle faelligen Saver an. Ein Fehler wird geloggt und
// der Saver nach saveRetryAfter erneut versucht.
func (h *Hub) saveDueLocked(at time.Time) {
	for _, s := range h.savers {
		if !s.pending || at.Before(s.retryAt) {
			continue
		}
		if err := h.runSaveLocked(s); err != nil {
			h.logf("learnrec: Speichern von %s fehlgeschlagen: %v, erneuter Versuch in %s", s.name, err, saveRetryAfter)
			s.retryAt = at.Add(saveRetryAfter)
			continue
		}
		s.pending = false
	}
}

func (h *Hub) runSaveLocked(s *namedSaver) error {
	start := time.Now()
	err := s.s.Save()
	h.stats[s.name] = time.Since(start)
	return err
}

// Close ruft alle Saver einmal auf, in Anmeldereihenfolge.
func (h *Hub) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, s := range h.savers {
		if err := h.runSaveLocked(s); err != nil {
			h.logf("learnrec: Speichern von %s beim Schliessen fehlgeschlagen: %v", s.name, err)
		}
	}
}

// Stats liefert die Dauer des letzten Save je Name.
func (h *Hub) Stats() map[string]time.Duration {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make(map[string]time.Duration, len(h.stats))
	for k, v := range h.stats {
		out[k] = v
	}
	return out
}
