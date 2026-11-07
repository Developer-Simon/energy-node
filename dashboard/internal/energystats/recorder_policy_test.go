package energystats

import (
	"testing"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/energy"
)

type policyBox struct{ p Policy }

func (b *policyBox) get() Policy { return b.p }

func newPolicyRecorder(t *testing.T, p Policy) (*Recorder, *Store, *policyBox) {
	t.Helper()
	store := NewStore(t.TempDir())
	r := NewRecorder(store, alwaysReady)
	box := &policyBox{p: p}
	r.SetPolicy(box.get)
	return r, store, box
}

func TestWithoutPolicyTheRecorderRecords(t *testing.T) {
	store := NewStore(t.TempDir())
	r := NewRecorder(store, alwaysReady)
	start := hourAt("2026-10-05T10:00:00Z")
	run(r, start, start.Add(time.Hour), 0, snapshotWith(energy.Balance{PV: 1000}))
	if _, _, source := storedPV(t, store, start); source != SourceOwn {
		t.Fatalf("Quelle %q", source)
	}
	if st := r.Status(); st.State != StateRecording || st.Mode != RecordingAlways {
		t.Fatalf("Status = %+v", st)
	}
}

func TestUnknownModeCountsAsAlways(t *testing.T) {
	r, store, _ := newPolicyRecorder(t, Policy{Mode: ""})
	start := hourAt("2026-10-05T10:00:00Z")
	run(r, start, start.Add(time.Hour), 0, snapshotWith(energy.Balance{PV: 1000}))
	if _, _, source := storedPV(t, store, start); source != SourceOwn {
		t.Fatalf("Quelle %q", source)
	}
	if st := r.Status(); st.Mode != RecordingAlways {
		t.Fatalf("Mode = %q", st.Mode)
	}
}

func TestOffRecordsNothing(t *testing.T) {
	r, store, _ := newPolicyRecorder(t, Policy{Mode: RecordingOff})
	start := hourAt("2026-10-05T10:00:00Z")
	run(r, start, start.Add(2*time.Hour), 0, snapshotWith(energy.Balance{PV: 1000}))
	if _, _, source := storedPV(t, store, start); source != SourceNone {
		t.Fatalf("Quelle %q, erwartet keine Daten", source)
	}
	if st := r.Status(); st.State != StateOff || st.Mode != RecordingOff {
		t.Fatalf("Status = %+v", st)
	}
}

func TestSwitchingOffMidHourKeepsThePartAndResumesWithoutTheGap(t *testing.T) {
	r, store, box := newPolicyRecorder(t, Policy{Mode: RecordingAlways})
	start := hourAt("2026-10-05T10:00:00Z")
	mono := run(r, start, start.Add(15*time.Minute), 0, snapshotWith(energy.Balance{PV: 4000}))
	box.p = Policy{Mode: RecordingOff}
	mono = run(r, start.Add(15*time.Minute+10*time.Second), start.Add(30*time.Minute), mono, snapshotWith(energy.Balance{PV: 4000}))
	// Beim Abschalten: 15 min * 4 kW = 1 kWh gespeichert.
	kwh, covered, source := storedPV(t, store, start)
	near(t, "pv nach Aus", kwh, 1.0)
	if source != SourceOwn || covered != 900 {
		t.Fatalf("covered=%d source=%q", covered, source)
	}
	box.p = Policy{Mode: RecordingAlways}
	run(r, start.Add(45*time.Minute), start.Add(time.Hour), mono, snapshotWith(energy.Balance{PV: 4000}))
	// 45..60 kommt dazu, 15..45 zaehlt nicht.
	kwh, covered, _ = storedPV(t, store, start)
	near(t, "pv nach Wiedereinschalten", kwh, 2.0)
	if covered != 1800 {
		t.Fatalf("covered=%d, erwartet 1800", covered)
	}
}

func TestPauseWritesNothingWhileTheCompanionIsCurrent(t *testing.T) {
	r, store, _ := newPolicyRecorder(t, Policy{Mode: RecordingPauseWithCompanion, CompanionCurrent: true})
	start := hourAt("2026-10-05T10:00:00Z")
	run(r, start, start.Add(2*time.Hour), 0, snapshotWith(energy.Balance{PV: 1000}))
	if _, _, source := storedPV(t, store, start); source != SourceNone {
		t.Fatalf("Quelle %q, erwartet nichts geschrieben", source)
	}
	if st := r.Status(); st.State != StatePaused {
		t.Fatalf("State = %q", st.State)
	}
}

func TestPauseModeRecordsWhenTheCompanionIsNotCurrent(t *testing.T) {
	r, store, _ := newPolicyRecorder(t, Policy{Mode: RecordingPauseWithCompanion, CompanionCurrent: false})
	start := hourAt("2026-10-05T10:00:00Z")
	run(r, start, start.Add(time.Hour), 0, snapshotWith(energy.Balance{PV: 1000}))
	if _, _, source := storedPV(t, store, start); source != SourceOwn {
		t.Fatalf("Quelle %q", source)
	}
	if st := r.Status(); st.State != StateRecording {
		t.Fatalf("State = %q", st.State)
	}
}

func TestResumeAfterPauseWritesTheLastThreeHours(t *testing.T) {
	r, store, box := newPolicyRecorder(t, Policy{Mode: RecordingPauseWithCompanion, CompanionCurrent: true})
	start := hourAt("2026-10-05T08:00:00Z")
	// 08:00 bis 13:00 pausiert: fuenf abgeschlossene Stunden, drei bleiben im Rueckstau.
	mono := run(r, start, start.Add(5*time.Hour), 0, snapshotWith(energy.Balance{PV: 1000}))
	box.p.CompanionCurrent = false
	run(r, start.Add(5*time.Hour+10*time.Second), start.Add(5*time.Hour+20*time.Second), mono, snapshotWith(energy.Balance{PV: 1000}))
	for i, want := range []byte{SourceNone, SourceNone, SourceOwn, SourceOwn, SourceOwn} {
		hour := start.Add(time.Duration(i) * time.Hour)
		if _, _, source := storedPV(t, store, hour); source != want {
			t.Errorf("%s: Quelle %q, erwartet %q", hour.Format("15:04"), source, want)
		}
	}
	kwh, _, _ := storedPV(t, store, start.Add(4*time.Hour))
	near(t, "pv 12:00", kwh, 1.0)
}

func TestResumeNeverOverwritesHomeAssistant(t *testing.T) {
	r, store, box := newPolicyRecorder(t, Policy{Mode: RecordingPauseWithCompanion, CompanionCurrent: true})
	start := hourAt("2026-10-05T10:00:00Z")
	mono := run(r, start, start.Add(time.Hour), 0, snapshotWith(energy.Balance{PV: 1000}))
	m, err := store.monthForTest(start)
	if err != nil {
		t.Fatal(err)
	}
	m.SetHA(start, "pv", 0.7)
	box.p.CompanionCurrent = false
	run(r, start.Add(time.Hour+10*time.Second), start.Add(time.Hour+20*time.Second), mono, snapshotWith(energy.Balance{PV: 1000}))
	kwh, _, source := storedPV(t, store, start)
	if source != SourceHA {
		t.Fatalf("Quelle %q", source)
	}
	near(t, "pv", kwh, 0.7)
}

func TestCloseWhilePausedWritesTheBacklog(t *testing.T) {
	r, store, _ := newPolicyRecorder(t, Policy{Mode: RecordingPauseWithCompanion, CompanionCurrent: true})
	start := hourAt("2026-10-05T10:00:00Z")
	run(r, start, start.Add(90*time.Minute), 0, snapshotWith(energy.Balance{PV: 1000}))
	r.Close()
	if _, _, source := storedPV(t, store, start); source != SourceOwn {
		t.Fatalf("10:00 Quelle %q", source)
	}
	kwh, _, _ := storedPV(t, store, start.Add(time.Hour))
	near(t, "pv 11:00 angebrochen", kwh, 0.5)
}

func TestRecorderKeepsRecordingChainsWhilePaused(t *testing.T) {
	r, store, _ := newPolicyRecorder(t, Policy{Mode: RecordingPauseWithCompanion, CompanionCurrent: true})
	chains := NewChainStore(t.TempDir(), nil)
	r.SetChainStore(chains)
	start := hourAt("2026-10-05T10:00:00Z")
	run(r, start, start.Add(time.Hour), 0, withChain(snapshotWith(energy.Balance{PV: 1000}), 1000, 900))

	if st := r.Status(); st.State != StatePaused {
		t.Fatalf("Status = %+v, erwartet paused", st)
	}
	if _, _, source := storedPV(t, store, start); source != SourceNone {
		t.Fatalf("Rolle waehrend der Pause geschrieben: Quelle %q", source)
	}
	got, ok := storedChainPair(t, chains, start, chainPair)
	if !ok || got.Seconds != 3600 || !approx(got.SourceKWh, 1) {
		t.Fatalf("Kettenstunde waehrend der Pause = %+v, %v", got, ok)
	}
}

func TestRecorderStopsChainsWhenOff(t *testing.T) {
	r, _, box := newPolicyRecorder(t, Policy{Mode: RecordingAlways})
	chains := NewChainStore(t.TempDir(), nil)
	r.SetChainStore(chains)
	s := withChain(snapshotWith(energy.Balance{PV: 1000}), 1000, 900)
	start := hourAt("2026-10-05T10:00:00Z")
	mono := run(r, start, hourAt("2026-10-05T10:30:00Z"), 0, s)

	box.p = Policy{Mode: RecordingOff}
	run(r, hourAt("2026-10-05T10:30:10Z"), hourAt("2026-10-05T11:30:00Z"), mono, s)

	// Die angefangene Stunde wird beim Abschalten geschrieben: 10:00 bis 10:30 sind 1800 s.
	got, ok := storedChainPair(t, chains, start, chainPair)
	if !ok || got.Seconds != 1800 || !approx(got.SourceKWh, 0.5) {
		t.Fatalf("angefangene Kettenstunde = %+v, %v, erwartet 1800 s und 0.5 kWh", got, ok)
	}
	if _, ok := storedChainPair(t, chains, hourAt("2026-10-05T11:00:00Z"), chainPair); ok {
		t.Fatal("Kettenstunde bei abgeschalteter Aufzeichnung geschrieben")
	}
	if st := r.Status(); st.State != StateOff {
		t.Fatalf("Status = %+v", st)
	}
}
