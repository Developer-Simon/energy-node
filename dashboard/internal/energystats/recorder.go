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

// Aufzeichnungsmodi. Spiegeln settings.EnergyStatsRecording*, ohne das
// Paket zu importieren.
const (
	RecordingAlways             = "always"
	RecordingPauseWithCompanion = "pause_with_companion"
	RecordingOff                = "off"
)

// Zustand der eigenen Aufzeichnung fuer die API.
const (
	StateRecording       = "recording"
	StatePaused          = "paused"
	StateOff             = "off"
	StateWaitingForClock = "waiting_for_clock"
)

// PauseBacklogHours: so viele abgeschlossene Stunden haelt der Recorder
// waehrend der Pause im Speicher. Das Lebenszeichen des Companion
// (SyncStatus.Current) bemerkt einen stehenden HA-Recorder erst nach bis zu
// zwei Stunden. Faellt die Pause weg, schreibt der Recorder den Rueckstau,
// AddOwn laesst Stunden aus Home Assistant dabei unberuehrt.
const PauseBacklogHours = 3

// Policy sagt dem Recorder je Probe, ob er aufzeichnen soll.
type Policy struct {
	Mode             string
	CompanionCurrent bool
}

func normalizeMode(mode string) string {
	switch mode {
	case RecordingPauseWithCompanion, RecordingOff:
		return mode
	default:
		return RecordingAlways
	}
}

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
	State         string     `json:"state"`
	Mode          string     `json:"mode"`
	ClockReady    bool       `json:"clock_ready"`
	Stale         bool       `json:"stale"`
	LastHour      *time.Time `json:"last_hour,omitempty"`
	WriteError    string     `json:"write_error,omitempty"`
	CorruptMonths []string   `json:"corrupt_months,omitempty"`
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
	policy   func() Policy
	backlog  []HourRecord
	off      bool
}

func NewRecorder(store *Store, ready ClockReady) *Recorder {
	return &Recorder{store: store, ready: ready}
}

// SetPolicy setzt die Quelle fuer Modus und Lebenszeichen. Ohne Policy
// zeichnet der Recorder immer auf.
func (r *Recorder) SetPolicy(fn func() Policy) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.policy = fn
}

// Observe nimmt einen Snapshot samt Bilanz. sinceStart kommt aus einer
// monotonen Uhr (time.Since eines festen Startzeitpunkts). wall wird von
// seiner Monotonablesung befreit, sonst rechnete Sub mit der Monotonzeit
// und kein Uhrsprung waere sichtbar.
func (r *Recorder) Observe(wall time.Time, sinceStart time.Duration, snapshot energy.Snapshot) {
	wall = wall.Round(0)
	// Die Policy vor dem eigenen Lock lesen: sie fragt Sync, und Sync
	// sperrt seinerseits den Store.
	r.mu.Lock()
	policyFn := r.policy
	r.mu.Unlock()
	policy := Policy{Mode: RecordingAlways}
	if policyFn != nil {
		policy = policyFn()
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	// Die Statuszeile zeigt Zustand, Modus und Schreibfehler. Aendert sich
	// eins davon, hebt die Revision, damit Seite und Kachel nachladen.
	// Laeuft vor dem Unlock (defer ist LIFO).
	before := r.status
	defer func() {
		if before.State != r.status.State || before.Mode != r.status.Mode || before.WriteError != r.status.WriteError {
			r.store.bump()
		}
	}()
	r.status.Mode = normalizeMode(policy.Mode)
	r.status.ClockReady = r.ready(wall, sinceStart)
	if r.status.Mode == RecordingOff {
		if !r.off {
			// Beim Abschalten den angefangenen Anteil behalten, wie bei
			// einem Neustart. Wird wieder eingeschaltet, waechst die
			// Stunde weiter.
			r.storeLocked(r.takeBacklogLocked())
			r.storeLocked(r.integ.Flush())
			r.off = true
		}
		r.hasLast = false
		r.status.State = StateOff
		return
	}
	r.off = false
	if !r.status.ClockReady {
		r.integ.Reset()
		r.hasLast = false
		r.status.State = StateWaitingForClock
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
	done := r.integ.Add(Sample{At: wall, Power: power})
	if r.status.Mode == RecordingPauseWithCompanion && policy.CompanionCurrent {
		r.backlog = append(r.backlog, done...)
		if n := len(r.backlog); n > PauseBacklogHours {
			r.backlog = append([]HourRecord(nil), r.backlog[n-PauseBacklogHours:]...)
		}
		r.status.State = StatePaused
		return
	}
	r.storeLocked(r.takeBacklogLocked())
	r.storeLocked(done)
	r.status.State = StateRecording
}

func (r *Recorder) takeBacklogLocked() []HourRecord {
	backlog := r.backlog
	r.backlog = nil
	return backlog
}

// Close schreibt den Rueckstau der Pause und die angefangene Stunde. Ein
// Neustart in derselben Stunde addiert spaeter dazu. Home Assistant ersetzt
// beides, sobald es die Stunden liefert.
func (r *Recorder) Close() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.storeLocked(r.takeBacklogLocked())
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
