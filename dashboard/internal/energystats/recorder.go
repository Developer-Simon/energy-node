package energystats

import (
	"math"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/energy"
)

// ClockJumpTolerance: laufen Wand- und Monotonzeit zwischen zwei Proben
// weiter auseinander, hat jemand die Uhr gestellt (NTP nach einem Start mit
// fake-hwclock). Die angefangene Stunde wird dann verworfen.
const ClockJumpTolerance = 30 * time.Second

// MinPlausibleYear: frueher gilt die Uhr als nicht gestellt. Der Pi 1 hat
// keine Echtzeituhr.
const MinPlausibleYear = 2026

// PowerOf liest die Leistungen in W aus der abgeleiteten Bilanz. Die
// Bilanz statt der Rohwerte, weil DeriveBalance pro Zeitpunkt klemmt und
// die Lueckentoleranz in W rechnet: auf kWh-Summen angewendet kaeme etwas
// anderes heraus. nil heisst "kein gueltiger Messwert": keine Werte oder
// eine veraltete Leistungsrolle, die sonst stundenlang weiterzaehlen
// wuerde.
func PowerOf(s energy.Snapshot) map[string]float64 {
	if len(s.Values) == 0 {
		return nil
	}
	for _, state := range s.Roles {
		if state.Freshness == "stale" && state.Role != energy.RoleBatterySoC {
			return nil
		}
	}
	b := s.Balance
	power := map[string]float64{
		"pv": b.PV, "grid_import": b.GridImport, "grid_export": b.GridExport,
		"battery_charge": b.BatteryCharge, "battery_discharge": b.BatteryDischarge,
		"load_total": b.LoadTotal, "base": b.Base, "wallbox": b.Wallbox, "heat_pump": b.HeatPump,
	}
	// Speicherkategorien stecken bereits in battery_charge/_discharge.
	for id, value := range b.Categories {
		category, ok := s.Categories[id]
		if !ok || category.Base == energy.CategoryStorage {
			continue
		}
		power["custom:"+id] = value
	}
	for role, value := range power {
		if value < 0 || math.IsNaN(value) || math.IsInf(value, 0) {
			power[role] = 0
		}
	}
	return power
}

// ClockReady entscheidet, ob die Systemzeit fuer die Aufzeichnung taugt.
type ClockReady func(wall time.Time, sinceStart time.Duration) bool

// SystemClockReady: synchronisiert laut systemd-timesyncd, oder (ohne
// timesyncd, bzw. nach grace ohne Synchronisation, etwa ohne Internet) ein
// plausibles Jahr.
func SystemClockReady(timesyncDir string, grace time.Duration) ClockReady {
	return func(wall time.Time, sinceStart time.Duration) bool {
		if _, err := os.Stat(filepath.Join(timesyncDir, "synchronized")); err == nil {
			return true
		}
		if info, err := os.Stat(timesyncDir); err == nil && info.IsDir() && sinceStart < grace {
			return false
		}
		return wall.Year() >= MinPlausibleYear
	}
}

// Status ist der Zustand der eigenen Aufzeichnung fuer die API.
type Status struct {
	ClockReady bool       `json:"clock_ready"`
	Stale      bool       `json:"stale"`
	LastHour   *time.Time `json:"last_hour,omitempty"`
	WriteError string     `json:"write_error,omitempty"`
}

// Recorder verbindet Snapshots, Integrator und Store.
type Recorder struct {
	store *Store
	ready ClockReady

	mu       sync.Mutex
	integ    Integrator
	lastWall time.Time
	lastMono time.Duration
	hasLast  bool
	status   Status
}

func NewRecorder(store *Store, ready ClockReady) *Recorder {
	return &Recorder{store: store, ready: ready}
}

// Observe nimmt einen Snapshot samt Bilanz. sinceStart kommt aus einer
// monotonen Uhr (time.Since eines festen Startzeitpunkts). wall wird von
// seiner Monotonablesung befreit, sonst rechnete Sub mit der Monotonzeit
// und kein Uhrsprung waere sichtbar.
func (r *Recorder) Observe(wall time.Time, sinceStart time.Duration, snapshot energy.Snapshot) {
	wall = wall.Round(0)
	r.mu.Lock()
	defer r.mu.Unlock()
	r.status.ClockReady = r.ready(wall, sinceStart)
	if !r.status.ClockReady {
		r.integ.Reset()
		r.hasLast = false
		return
	}
	if r.hasLast {
		drift := wall.Sub(r.lastWall) - (sinceStart - r.lastMono)
		if drift > ClockJumpTolerance || drift < -ClockJumpTolerance {
			r.integ.Reset()
		}
	}
	r.lastWall, r.lastMono, r.hasLast = wall, sinceStart, true
	power := PowerOf(snapshot)
	r.status.Stale = power == nil && len(snapshot.Values) > 0
	r.storeLocked(r.integ.Add(Sample{At: wall, Power: power}))
}

// Close schreibt die angefangene Stunde. Ein Neustart in derselben Stunde
// addiert spaeter dazu.
func (r *Recorder) Close() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.storeLocked(r.integ.Flush())
	r.hasLast = false
}

func (r *Recorder) Status() Status {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.status
}

func (r *Recorder) storeLocked(records []HourRecord) {
	for _, record := range records {
		if err := r.store.AddOwn(record); err != nil {
			r.status.WriteError = err.Error()
			r.store.logf("energystats: Stunde %s nicht gespeichert: %v", record.Hour.Format(time.RFC3339), err)
			continue
		}
		r.status.WriteError = ""
		hour := record.Hour
		r.status.LastHour = &hour
	}
}
