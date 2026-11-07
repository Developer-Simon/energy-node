package energystats

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/energy"
)

func snapshotWith(b energy.Balance, roles ...energy.RoleState) energy.Snapshot {
	return energy.Snapshot{Values: map[energy.Role]float64{energy.RolePV: b.PV}, Roles: roles, Balance: b}
}

func alwaysReady(time.Time, time.Duration) bool { return true }

// run fuettert den Recorder alle 10 s von from bis to (inklusive) mit
// demselben Snapshot. Wand- und Monotonzeit laufen gleich.
func run(r *Recorder, from, to time.Time, mono time.Duration, s energy.Snapshot) time.Duration {
	for at := from; !at.After(to); at = at.Add(10 * time.Second) {
		r.Observe(at, mono, s)
		mono += 10 * time.Second
	}
	return mono
}

func storedPV(t *testing.T, store *Store, hour time.Time) (float64, int32, byte) {
	t.Helper()
	var kwh float64
	var covered int32
	source := SourceNone
	store.Each(hour, hour.Add(time.Hour), func(_ time.Time, m *Month, i int) {
		if m == nil || m.Roles["pv"] == nil {
			return
		}
		kwh, covered, source = m.Roles["pv"].KWh[i], m.Covered[i], m.Roles["pv"].Source[i]
	})
	return kwh, covered, source
}

func TestPowerOfMapsBalanceFieldsAndClampsNegatives(t *testing.T) {
	s := energy.Snapshot{
		Values: map[energy.Role]float64{energy.RolePV: 1},
		Balance: energy.Balance{PV: 1000, GridImport: 200, GridExport: -5, BatteryCharge: 300, BatteryDischarge: 0,
			LoadTotal: 900, Base: 400, Wallbox: 500, HeatPump: 0,
			Categories: map[string]float64{"sauna": 1200, "bhkw": -30, "akku2": 50}},
		Categories: map[string]energy.Category{
			"sauna": {Base: energy.CategoryConsumer},
			"bhkw":  {Base: energy.CategoryProducer},
			"akku2": {Base: energy.CategoryStorage},
		},
	}
	p := PowerOf(s)
	want := map[string]float64{"pv": 1000, "grid_import": 200, "grid_export": 0, "battery_charge": 300, "battery_discharge": 0,
		"load_total": 900, "base": 400, "wallbox": 500, "heat_pump": 0, "custom:sauna": 1200, "custom:bhkw": 0}
	if len(p) != len(want) {
		t.Fatalf("PowerOf = %v", p)
	}
	for role, value := range want {
		if p[role] != value {
			t.Errorf("%s = %v, erwartet %v", role, p[role], value)
		}
	}
}

func TestPowerOfIsNilWithoutValuesOrWithAStalePowerRole(t *testing.T) {
	if PowerOf(energy.Snapshot{Balance: energy.Balance{PV: 5}}) != nil {
		t.Fatal("ohne Values erwartet nil")
	}
	stale := snapshotWith(energy.Balance{PV: 5}, energy.RoleState{Role: energy.RoleWallbox, Freshness: "stale"})
	if PowerOf(stale) != nil {
		t.Fatal("veraltete Wallbox: erwartet nil")
	}
	socStale := snapshotWith(energy.Balance{PV: 5}, energy.RoleState{Role: energy.RoleBatterySoC, Freshness: "stale"})
	if PowerOf(socStale) == nil {
		t.Fatal("veraltetes battery_soc darf nicht blockieren")
	}
}

func TestRecorderStoresClosedHours(t *testing.T) {
	store := NewStore(t.TempDir())
	r := NewRecorder(store, alwaysReady)
	start := hourAt("2026-10-05T10:00:00Z")
	run(r, start, start.Add(time.Hour), 0, snapshotWith(energy.Balance{PV: 2000}))
	kwh, covered, source := storedPV(t, store, start)
	near(t, "pv", kwh, 2.0)
	if covered != 3600 || source != SourceOwn {
		t.Fatalf("covered=%d source=%q", covered, source)
	}
	if st := r.Status(); st.LastHour == nil || !st.LastHour.Equal(start) || !st.ClockReady || st.Stale {
		t.Fatalf("Status = %+v", st)
	}
}

func TestRecorderWaitsForTheClock(t *testing.T) {
	store := NewStore(t.TempDir())
	ready := false
	r := NewRecorder(store, func(time.Time, time.Duration) bool { return ready })
	start := hourAt("2026-10-05T10:00:00Z")
	mono := run(r, start, start.Add(30*time.Minute), 0, snapshotWith(energy.Balance{PV: 1000}))
	if r.Status().ClockReady {
		t.Fatal("Uhr gilt als bereit")
	}
	ready = true
	run(r, start.Add(30*time.Minute+10*time.Second), start.Add(time.Hour), mono, snapshotWith(energy.Balance{PV: 1000}))
	_, covered, _ := storedPV(t, store, start)
	if covered > 1800 {
		t.Fatalf("covered=%d, vor Uhrfreigabe darf nichts zaehlen", covered)
	}
}

func TestRecorderDropsTheRunningHourOnAClockJump(t *testing.T) {
	store := NewStore(t.TempDir())
	r := NewRecorder(store, alwaysReady)
	start := hourAt("2026-10-05T10:00:00Z")
	mono := run(r, start, start.Add(20*time.Minute), 0, snapshotWith(energy.Balance{PV: 1000}))
	// Wanduhr springt eine Stunde vor, monoton vergehen nur 10 s.
	jumped := start.Add(80*time.Minute + 10*time.Second)
	run(r, jumped, hourAt("2026-10-05T12:00:00Z"), mono, snapshotWith(energy.Balance{PV: 1000}))
	// Die Serie pv existiert durch Stunde 11, Stunde 10 muss leer sein.
	if _, covered, source := storedPV(t, store, start); covered != 0 || source != SourceNone {
		t.Fatalf("Stunde 10 nach Uhrsprung: covered=%d source=%q, erwartet verworfen", covered, source)
	}
}

func TestRecorderReportsStaleAndSkips(t *testing.T) {
	store := NewStore(t.TempDir())
	r := NewRecorder(store, alwaysReady)
	start := hourAt("2026-10-05T10:00:00Z")
	stale := snapshotWith(energy.Balance{PV: 1000}, energy.RoleState{Role: energy.RolePV, Freshness: "stale"})
	run(r, start, start.Add(time.Hour), 0, stale)
	if !r.Status().Stale {
		t.Fatal("Status.Stale erwartet")
	}
	if _, covered, _ := storedPV(t, store, start); covered != 0 {
		t.Fatalf("covered=%d", covered)
	}
}

func TestRestartWithinTheHourAddsUpWithoutDoubleCounting(t *testing.T) {
	dir := t.TempDir()
	start := hourAt("2026-10-05T10:00:00Z")
	s := snapshotWith(energy.Balance{PV: 3600})

	first := NewRecorder(NewStore(dir), alwaysReady)
	run(first, start, start.Add(20*time.Minute), 0, s)
	first.Close()

	second := NewRecorder(NewStore(dir), alwaysReady)
	run(second, start.Add(21*time.Minute), start.Add(40*time.Minute), 0, s)
	second.Close()

	third := NewRecorder(NewStore(dir), alwaysReady)
	run(third, start.Add(41*time.Minute), start.Add(time.Hour), 0, s)

	kwh, covered, _ := storedPV(t, NewStore(dir), start)
	// 3 x 20 min gedeckt, je 1 min Luecke beim Neustart zaehlt nicht.
	near(t, "pv", kwh, 3.6*58.0/60.0)
	if covered != 58*60 {
		t.Fatalf("covered=%d, erwartet %d", covered, 58*60)
	}
}

func TestSystemClockReady(t *testing.T) {
	dir := t.TempDir()
	sync := filepath.Join(dir, "timesync")
	ready := SystemClockReady(sync, 15*time.Minute)
	y2025 := hourAt("2025-06-01T00:00:00Z")
	y2026 := hourAt("2026-06-01T00:00:00Z")

	if ready(y2025, 0) || !ready(y2026, 0) {
		t.Fatal("ohne timesyncd: nur das Jahr zaehlt")
	}
	os.Mkdir(sync, 0o755)
	if ready(y2026, time.Minute) {
		t.Fatal("timesyncd nicht synchron und Schonfrist nicht vorbei: nicht bereit")
	}
	if !ready(y2026, 15*time.Minute) || ready(y2025, time.Hour) {
		t.Fatal("nach der Schonfrist zaehlt das Jahr")
	}
	os.WriteFile(filepath.Join(sync, "synchronized"), nil, 0o644)
	if !ready(y2025, 0) {
		t.Fatal("synchronisiert: immer bereit")
	}
}

func TestRecorderMarksSubstitutedSnapshot(t *testing.T) {
	store := NewStore(t.TempDir())
	r := NewRecorder(store, alwaysReady)
	start := hourAt("2026-10-05T10:00:00Z")
	sub := snapshotWith(energy.Balance{PV: 1000}, energy.RoleState{Role: energy.RolePV, Quality: energy.QualitySubstitute, Freshness: "fresh"})
	run(r, start, start.Add(time.Hour), 0, sub)
	kwh, covered, source := storedPV(t, store, start)
	if covered != 3600 || source != SourceOwn {
		t.Fatalf("covered=%d source=%q, Ersatzwert ist keine Luecke", covered, source)
	}
	near(t, "pv", kwh, 1.0)
	if !substitutedAt(t, store, start) {
		t.Fatal("Stunde nicht als Ersatzwert markiert")
	}
	if PowerOf(sub) == nil {
		t.Fatal("PowerOf liefert bei Ersatzwert nil")
	}
}

func TestRecorderStaleSnapshotIsStillAGap(t *testing.T) {
	store := NewStore(t.TempDir())
	r := NewRecorder(store, alwaysReady)
	start := hourAt("2026-10-05T10:00:00Z")
	stale := snapshotWith(energy.Balance{PV: 1000}, energy.RoleState{Role: energy.RolePV, Quality: "stale", Freshness: "stale"})
	run(r, start, start.Add(time.Hour), 0, stale)
	if _, covered, _ := storedPV(t, store, start); covered != 0 {
		t.Fatalf("covered=%d, veraltet muss Luecke bleiben", covered)
	}
	if substitutedAt(t, store, start) {
		t.Fatal("veraltete Probe darf nicht als Ersatzwert zaehlen")
	}
}

func substitutedAt(t *testing.T, store *Store, hour time.Time) bool {
	t.Helper()
	var seconds int32
	store.Each(hour, hour.Add(time.Hour), func(_ time.Time, m *Month, i int) {
		if m != nil {
			seconds = m.Substituted[i]
		}
	})
	return seconds > 0
}

// withChain adds one chain a -> b to the snapshot: sourceW at a, targetW at b,
// both fresh, the segment available.
func withChain(s energy.Snapshot, sourceW, targetW float64) energy.Snapshot {
	eff := targetW / sourceW
	s.Chains = []energy.ChainState{{
		ID: "chain", Flow: "down", Counting: "source",
		Points: []energy.ChainPointState{
			{EntityID: "a", HasValue: true, Value: sourceW, Freshness: "fresh", Counting: true},
			{EntityID: "b", HasValue: true, Value: targetW, Freshness: "fresh", Counting: true},
		},
		Segments: []energy.ChainSegmentState{{From: "a", To: "b", Available: true, Efficiency: &eff}},
	}}
	return s
}

var chainPair = ChainPair{Source: "a", Target: "b"}

func TestRecorderStoresChainHours(t *testing.T) {
	start := hourAt("2026-10-05T10:00:00Z")
	end := start.Add(time.Hour)
	role := snapshotWith(energy.Balance{PV: 1000})

	plainDir := t.TempDir()
	run(NewRecorder(NewStore(plainDir), alwaysReady), start, end, 0, role)

	chainDir := t.TempDir()
	recorder := NewRecorder(NewStore(chainDir), alwaysReady)
	chains := NewChainStore(filepath.Join(chainDir, "chains"), nil)
	recorder.SetChainStore(chains)
	run(recorder, start, end, 0, withChain(role, 1000, 900))

	plain, err := os.ReadFile(filepath.Join(plainDir, "2026-10.json"))
	if err != nil {
		t.Fatal(err)
	}
	withChains, err := os.ReadFile(filepath.Join(chainDir, "2026-10.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(plain) != string(withChains) {
		t.Fatalf("Rollen-Monatsdatei veraendert durch die Kette:\n%s\nvs\n%s", withChains, plain)
	}
	if _, err := os.Stat(filepath.Join(chainDir, "chains", "2026-10.json")); err != nil {
		t.Fatalf("Kettendatei fehlt: %v", err)
	}
	got, ok := storedChainPair(t, chains, start, chainPair)
	if !ok || !approx(got.SourceKWh, 1) || !approx(got.TargetKWh, 0.9) || got.Seconds != 3600 {
		t.Fatalf("Kettenstunde = %+v, %v, erwartet 1 kWh, 0.9 kWh, 3600 s", got, ok)
	}
}

func TestRecorderResetsChainsOnClockJump(t *testing.T) {
	recorder := NewRecorder(NewStore(t.TempDir()), alwaysReady)
	chains := NewChainStore(t.TempDir(), nil)
	recorder.SetChainStore(chains)
	s := withChain(snapshotWith(energy.Balance{PV: 1000}), 1000, 900)

	start := hourAt("2026-10-05T10:00:00Z")
	mono := run(recorder, start, hourAt("2026-10-05T10:20:00Z"), 0, s)
	// Die Wanduhr springt eine Stunde vor, die Monotonzeit laeuft weiter.
	run(recorder, hourAt("2026-10-05T11:20:10Z"), hourAt("2026-10-05T12:00:00Z"), mono, s)

	if _, ok := storedChainPair(t, chains, start, chainPair); ok {
		t.Fatal("angefangene Stunde vor dem Uhrsprung wurde geschrieben")
	}
	got, ok := storedChainPair(t, chains, hourAt("2026-10-05T11:00:00Z"), chainPair)
	if !ok {
		t.Fatal("Stunde nach dem Uhrsprung fehlt")
	}
	// 11:20:10 bis 12:00:00 sind 2390 s.
	if got.Seconds != 2390 || !approx(got.SourceKWh, 1000*2390/3.6e6) {
		t.Fatalf("Stunde nach dem Uhrsprung = %+v, erwartet 2390 s", got)
	}
}

func TestRecorderCloseFlushesChains(t *testing.T) {
	recorder := NewRecorder(NewStore(t.TempDir()), alwaysReady)
	chains := NewChainStore(t.TempDir(), nil)
	recorder.SetChainStore(chains)
	start := hourAt("2026-10-05T10:00:00Z")
	run(recorder, start, start.Add(30*time.Minute), 0, withChain(snapshotWith(energy.Balance{PV: 1000}), 1000, 900))
	recorder.Close()

	got, ok := storedChainPair(t, chains, start, chainPair)
	if !ok || got.Seconds != 1800 || !approx(got.SourceKWh, 0.5) {
		t.Fatalf("angefangene Stunde nach Close = %+v, %v, erwartet 1800 s und 0.5 kWh", got, ok)
	}
}

func TestRecorderWithoutChainStoreBehavesAsBefore(t *testing.T) {
	store := NewStore(t.TempDir())
	recorder := NewRecorder(store, alwaysReady)
	start := hourAt("2026-10-05T10:00:00Z")
	run(recorder, start, start.Add(time.Hour), 0, withChain(snapshotWith(energy.Balance{PV: 1000}), 1000, 900))

	kwh, covered, source := storedPV(t, store, start)
	if source != SourceOwn || !approx(kwh, 1) || covered != 3600 {
		t.Fatalf("PV = %v kWh, %d s, Quelle %q", kwh, covered, source)
	}
	if st := recorder.Status(); st.WriteError != "" || st.State != StateRecording {
		t.Fatalf("Status = %+v", st)
	}
}

func TestRecorderKeepsRoleWriteErrorWhenChainWriteSucceeds(t *testing.T) {
	roleDir := t.TempDir()
	// Ein Verzeichnis an Stelle der Monatsdatei: der Rollenspeicher scheitert,
	// der Kettenspeicher in einem eigenen Verzeichnis schreibt erfolgreich.
	os.Mkdir(filepath.Join(roleDir, "2026-10.json"), 0o755)
	recorder := NewRecorder(NewStore(roleDir), alwaysReady)
	recorder.SetChainStore(NewChainStore(t.TempDir(), nil))
	start := hourAt("2026-10-05T10:00:00Z")
	run(recorder, start, start.Add(time.Hour), 0, withChain(snapshotWith(energy.Balance{PV: 1000}), 1000, 900))

	st := recorder.Status()
	if st.WriteError == "" {
		t.Fatal("Schreibfehler der Rolle wurde durch den Kettenerfolg verdeckt")
	}
	if st.LastHour != nil && st.LastHour.Equal(start) {
		t.Fatalf("fehlgeschlagene Rollenstunde gilt als gespeichert: %+v", st)
	}
}
