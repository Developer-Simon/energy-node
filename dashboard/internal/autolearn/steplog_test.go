package autolearn

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// stepPayload baut die JSON-Form eines Reglerschritts.
func stepPayload(at float64, err, net float64, from, to *float64, why string) []byte {
	f := func(v *float64) string {
		if v == nil {
			return "null"
		}
		return fmt.Sprintf("%g", *v)
	}
	return []byte(fmt.Sprintf(`{"at":%.1f,"err":%g,"net":%g,"from":%s,"to":%s,"why":%q}`,
		at, err, net, f(from), f(to), why))
}

func fptr(v float64) *float64 { return &v }

// newTestStepLog legt einen StepLog mit fester Uhr an.
func newTestStepLog(days int, now time.Time) *StepLog {
	l := NewStepLog(func() int { return days }, func() string { return "" })
	l.now = func() time.Time { return now }
	return l
}

func TestStepLogKeepsADayOfSteps(t *testing.T) {
	loc := berlinLoc(t)
	start := time.Date(2026, 11, 19, 0, 0, 0, 0, loc)
	l := newTestStepLog(1, start.Add(24*time.Hour-time.Second))
	for i := 0; i < 8640; i++ {
		at := float64(start.Add(time.Duration(i) * 10 * time.Second).Unix())
		if err := l.Add("heizung", stepPayload(at, -5, -5, fptr(1200), fptr(1200), "unchanged")); err != nil {
			t.Fatalf("step %d: %v", i, err)
		}
	}
	if got := len(l.Steps("heizung", start, start.Add(24*time.Hour))); got != 8640 {
		t.Fatalf("steps = %d, want 8640", got)
	}
	// 10:00:00 bis 10:01:00 einschliesslich: 7 Schritte.
	got := l.Steps("heizung", start.Add(10*time.Hour), start.Add(10*time.Hour+time.Minute))
	if len(got) != 7 {
		t.Fatalf("range steps = %d, want 7", len(got))
	}
	if got[0].At != start.Add(10*time.Hour).Unix() {
		t.Errorf("first at = %d, want 10:00:00", got[0].At)
	}
}

func TestStepLogPrunesDaysOutsideTheRing(t *testing.T) {
	loc := berlinLoc(t)
	old := time.Date(2026, 11, 19, 12, 0, 0, 0, loc)
	now := time.Date(2026, 11, 22, 12, 0, 0, 0, loc)
	l := newTestStepLog(2, now)
	if err := l.Add("heizung", stepPayload(float64(old.Unix()), 1, 1, nil, nil, "deadband")); err != nil {
		t.Fatal(err)
	}
	if err := l.Flush(now); err != nil {
		t.Fatal(err)
	}
	if got := l.Steps("heizung", old.Add(-time.Hour), now); len(got) != 0 {
		t.Errorf("steps = %d, want the old day pruned", len(got))
	}
}

func TestStepLogCountsChangesAndSignFlips(t *testing.T) {
	loc := berlinLoc(t)
	base := time.Date(2026, 11, 19, 10, 0, 0, 0, loc)
	now := base.Add(time.Hour)
	l := newTestStepLog(1, now)
	steps := []struct {
		err, net float64
		from, to *float64
		why      string
	}{
		{5, 5, fptr(1200), fptr(1180), "sent"},
		{-3, -3, fptr(1180), fptr(1180), "unchanged"},
		{-2, -2, fptr(1180), fptr(1180), "unchanged"},
		{0, 0, fptr(1180), fptr(1180), "deadband"},
		{4, 4, fptr(1180), fptr(1200), "sent"},
	}
	for i, s := range steps {
		at := float64(base.Add(time.Duration(i) * time.Minute).Unix())
		if err := l.Add("heizung", stepPayload(at, s.err, s.net, s.from, s.to, s.why)); err != nil {
			t.Fatalf("step %d: %v", i, err)
		}
	}
	fig := l.DayFigures("2026-11-19")["heizung"]
	if fig == nil {
		t.Fatal("no figures for heizung")
	}
	if fig.N != 4 {
		t.Errorf("n = %d, want 4 (ohne Totzone)", fig.N)
	}
	if fig.Changes != 2 {
		t.Errorf("changes = %d, want 2", fig.Changes)
	}
	// +5 gegen -3 und -2 gegen +4 wechseln das Vorzeichen, der Nullfehler zaehlt nicht.
	if fig.SignFlips != 2 {
		t.Errorf("flips = %d, want 2", fig.SignFlips)
	}
}

func TestStepLogOutOfDeadbandEnergySplitsBySign(t *testing.T) {
	loc := berlinLoc(t)
	base := time.Date(2026, 11, 19, 10, 0, 0, 0, loc)
	l := newTestStepLog(1, base.Add(time.Hour))
	// Export 100 W, Abstand bis zum naechsten Schritt ohne Totzone 120 s: 100 * 120 / 3600 Wh.
	steps := [][]byte{
		stepPayload(float64(base.Unix()), -100, -100, fptr(1200), fptr(1200), "unchanged"),
		stepPayload(float64(base.Unix()+60), -100, -100, fptr(1200), fptr(1200), "deadband"),
		stepPayload(float64(base.Unix()+120), 50, 50, fptr(1200), fptr(1200), "unchanged"),
	}
	for _, p := range steps {
		if err := l.Add("heizung", p); err != nil {
			t.Fatal(err)
		}
	}
	fig := l.DayFigures("2026-11-19")["heizung"]
	if fig == nil {
		t.Fatal("no figures")
	}
	wantExp := 100.0 * 120 / 3600
	if math.Abs(float64(fig.OutWhExport)-wantExp) > 1e-3 {
		t.Errorf("out_exp = %v, want %v", fig.OutWhExport, wantExp)
	}
	if fig.OutWhImport != 0 {
		t.Errorf("out_imp = %v, want 0 (the last step has no next step)", fig.OutWhImport)
	}
}

func TestStepLogRejectsBadPayloadsAndIDs(t *testing.T) {
	loc := berlinLoc(t)
	now := time.Date(2026, 11, 19, 10, 0, 0, 0, loc)
	at := float64(now.Unix())
	l := newTestStepLog(1, now)

	bad := map[string][]byte{
		"not json":     []byte(`{"at":`),
		"number":       []byte(`12`),
		"no why":       []byte(fmt.Sprintf(`{"at":%g,"err":1,"net":1,"from":null,"to":null}`, at)),
		"unknown why":  stepPayload(at, 1, 1, nil, nil, "bogus"),
		"no err":       []byte(fmt.Sprintf(`{"at":%g,"net":1,"why":"sent"}`, at)),
		"future 301 s": stepPayload(at+301, 1, 1, nil, nil, "sent"),
		"huge err":     []byte(fmt.Sprintf(`{"at":%g,"err":1e300,"net":1,"why":"sent"}`, at)),
	}
	for name, p := range bad {
		if err := l.Add("heizung", p); err == nil {
			t.Errorf("%s: accepted, want rejected", name)
		}
	}
	if err := l.Add("heizung", stepPayload(at+299, 1, 1, nil, nil, "sent")); err != nil {
		t.Errorf("299 s in the future: %v, want accepted", err)
	}

	for _, id := range []string{"", "Heizung", "a b", "a/b", "a.b", strings.Repeat("x", 65)} {
		if err := l.Add(id, stepPayload(at, 1, 1, nil, nil, "sent")); err == nil {
			t.Errorf("scenario %q accepted, want rejected", id)
		}
	}
	for _, id := range []string{"a", "heizung-1", "a_b", strings.Repeat("x", 64)} {
		if err := l.Add(id, stepPayload(at, 1, 1, nil, nil, "sent")); err != nil {
			t.Errorf("scenario %q: %v, want accepted", id, err)
		}
	}
}

func TestDayFileWithoutStepsLoads(t *testing.T) {
	loc := berlinLoc(t)
	dir := t.TempDir()
	writeRawDay(t, dir, "2026-11-19", `{"v":1,"day":"2026-11-19","devices":{},"site":{},"events":[],"actions":[]}`)
	base := time.Date(2026, 11, 19, 10, 0, 0, 0, loc)
	s := OpenDayStore(dir, loc, keepAll, noLog)
	days := mustDays(t, s, base, base)
	if len(days) != 1 {
		t.Fatalf("days = %d, want the old day", len(days))
	}
	if days[0].Steps != nil {
		t.Errorf("steps = %v, want nil", days[0].Steps)
	}
}

func TestDayFileStepsRoundTrip(t *testing.T) {
	loc := berlinLoc(t)
	dir := t.TempDir()
	s := OpenDayStore(dir, loc, keepAll, noLog)
	base := time.Date(2026, 11, 19, 10, 0, 0, 0, loc)
	f := DayFile{V: dayFileVersion, Day: "2026-11-19", Devices: map[string]*DeviceDay{},
		Events: []EventRec{}, Actions: []ActionRec{},
		Steps: map[string]*StepDay{"heizung": {N: 3, Changes: 1, SignFlips: 2, OutWhImport: 1.5, OutWhExport: 0.5}}}
	if err := s.writeFile("2026-11-19", f); err != nil {
		t.Fatal(err)
	}
	days := mustDays(t, s, base, base)
	if len(days) != 1 || days[0].Steps["heizung"] == nil {
		t.Fatalf("days = %+v, want the steps back", days)
	}
	got := days[0].Steps["heizung"]
	if got.N != 3 || got.Changes != 1 || got.SignFlips != 2 || got.OutWhImport != 1.5 || got.OutWhExport != 0.5 {
		t.Errorf("steps = %+v, want the figures back", got)
	}
}

func TestStepRecordRoundTrip(t *testing.T) {
	v := float32(1200)
	in := Step{At: 1794000000, Err: -35, Net: -35, From: &v, To: nil, Why: StepLimit}
	buf := encodeStep(in)
	if len(buf) != 24 {
		t.Fatalf("record = %d bytes, want 24", len(buf))
	}
	out := decodeStep(buf)
	if out.At != in.At || out.Err != in.Err || out.Net != in.Net || out.Why != in.Why {
		t.Errorf("decoded = %+v, want %+v", out, in)
	}
	if out.From == nil || *out.From != 1200 || out.To != nil {
		t.Errorf("from/to = %v/%v, want 1200/nil", out.From, out.To)
	}
}

func TestStepLogGapIsCappedAt300Seconds(t *testing.T) {
	loc := berlinLoc(t)
	base := time.Date(2026, 11, 19, 10, 0, 0, 0, loc)
	l := newTestStepLog(1, base.Add(2*time.Hour))
	// Zwei Schritte 1000 s auseinander: gezaehlt werden nur 300 s, 60 * 300 / 3600 Wh.
	for _, at := range []int64{base.Unix(), base.Unix() + 1000} {
		if err := l.Add("heizung", stepPayload(float64(at), -60, -60, fptr(1200), fptr(1200), "unchanged")); err != nil {
			t.Fatal(err)
		}
	}
	fig := l.DayFigures("2026-11-19")["heizung"]
	if fig == nil {
		t.Fatal("no figures")
	}
	if want := 60.0 * 300 / 3600; math.Abs(float64(fig.OutWhExport)-want) > 1e-3 {
		t.Errorf("out_exp = %v, want %v", fig.OutWhExport, want)
	}
}

func TestStepLogFlushAppendsOncePerStepAndRoundTrips(t *testing.T) {
	loc := berlinLoc(t)
	dir := t.TempDir()
	base := time.Date(2026, 11, 19, 10, 0, 0, 0, loc)
	l := NewStepLog(func() int { return 2 }, func() string { return dir })
	l.now = func() time.Time { return base.Add(time.Hour) }
	if err := l.Add("heizung", stepPayload(float64(base.Unix()), 5, 5, fptr(1200), nil, "sent")); err != nil {
		t.Fatal(err)
	}
	if err := l.Add("heizung", stepPayload(float64(base.Unix()+10), -3, -3, nil, nil, "deadband")); err != nil {
		t.Fatal(err)
	}
	if err := l.Add("lampe", stepPayload(float64(base.Unix()+20), 1, 1, nil, nil, "limit")); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := l.Flush(base.Add(time.Hour)); err != nil {
			t.Fatalf("flush %d: %v", i, err)
		}
	}
	file := stepFileName(dir, "2026-11-19", "heizung")
	raw, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) != 2*stepRecordSize {
		t.Fatalf("file = %d bytes, want 2 records after a repeated flush", len(raw))
	}
	if _, err := os.Stat(stepFileName(dir, "2026-11-19", "lampe")); err != nil {
		t.Errorf("lampe file: %v", err)
	}

	// Ein neuer Speicher ohne RAM liest den Tag aus der Datei, auch nach dem Ring.
	later := NewStepLog(func() int { return 1 }, func() string { return dir })
	later.now = func() time.Time { return base.Add(48 * time.Hour) }
	got := later.Steps("heizung", base.Add(-time.Hour), base.Add(time.Hour))
	if len(got) != 2 {
		t.Fatalf("read back %d steps, want 2", len(got))
	}
	if got[0].At != base.Unix() || got[0].Err != 5 || got[0].Why != StepSent {
		t.Errorf("first = %+v, want the sent step back", got[0])
	}
	if got[0].From == nil || *got[0].From != 1200 || got[0].To != nil {
		t.Errorf("from/to = %v/%v, want 1200/nil", got[0].From, got[0].To)
	}
	if got[1].Why != StepDeadband || got[1].From != nil {
		t.Errorf("second = %+v, want the deadband step with from nil", got[1])
	}
}

func TestStepLogFlushPrunesOldStepFiles(t *testing.T) {
	loc := berlinLoc(t)
	dir := t.TempDir()
	old := time.Date(2026, 11, 10, 10, 0, 0, 0, loc)
	now := time.Date(2026, 11, 22, 10, 0, 0, 0, loc)
	l := NewStepLog(func() int { return 3 }, func() string { return dir })
	l.now = func() time.Time { return old }
	if err := l.Add("heizung", stepPayload(float64(old.Unix()), 1, 1, nil, nil, "sent")); err != nil {
		t.Fatal(err)
	}
	if err := l.Flush(old); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stepFileName(dir, "2026-11-10", "heizung")); err != nil {
		t.Fatalf("old file not written: %v", err)
	}
	l.now = func() time.Time { return now }
	if err := l.Flush(now); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stepFileName(dir, "2026-11-10", "heizung")); !os.IsNotExist(err) {
		t.Errorf("old file still there, err = %v", err)
	}
}

func TestStepLogRAMOnlyWritesNoFiles(t *testing.T) {
	loc := berlinLoc(t)
	dir := t.TempDir()
	base := time.Date(2026, 11, 19, 10, 0, 0, 0, loc)
	l := newTestStepLog(1, base.Add(time.Hour))
	if err := l.Add("heizung", stepPayload(float64(base.Unix()), 1, 1, nil, nil, "sent")); err != nil {
		t.Fatal(err)
	}
	if err := l.Flush(base.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Errorf("entries = %d, want none", len(entries))
	}
}

func TestStepLogResetClearsRAMAndStepFiles(t *testing.T) {
	dir := t.TempDir()
	start := time.Date(2026, 11, 20, 10, 0, 0, 0, time.Local)
	l := NewStepLog(func() int { return 3 }, func() string { return dir })
	l.now = func() time.Time { return start }
	if err := l.Add("heizung", stepPayload(float64(start.Unix()), -5, -5, fptr(1200), fptr(1200), "unchanged")); err != nil {
		t.Fatal(err)
	}
	if err := l.Flush(start); err != nil {
		t.Fatal(err)
	}
	if err := l.Reset(); err != nil {
		t.Fatal(err)
	}
	if got := l.Steps("heizung", start.Add(-time.Hour), start.Add(time.Hour)); len(got) != 0 {
		t.Fatalf("steps survived reset: %d", len(got))
	}
	entries, err := os.ReadDir(filepath.Join(dir, "learnrec", "minutes"))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".steps") {
			t.Fatalf("step file survived reset: %s", e.Name())
		}
	}
}

func TestStepLogRingIsBoundedWhileFilesKeepTheirDays(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 11, 20, 10, 0, 0, 0, time.Local)
	l := NewStepLog(func() int { return 3 }, func() string { return dir }).WithRingDays(func() int { return 1 })
	l.now = func() time.Time { return now }
	for _, back := range []int{2, 1, 0} {
		at := now.AddDate(0, 0, -back)
		if err := l.Add("heizung", stepPayload(float64(at.Unix()), -5, -5, fptr(1200), fptr(1200), "unchanged")); err != nil {
			t.Fatal(err)
		}
	}
	// The first flush writes the old days, the second one drops them from the ring.
	for i := 0; i < 2; i++ {
		if err := l.Flush(now); err != nil {
			t.Fatal(err)
		}
	}
	if got := len(l.steps["heizung"]); got != 1 {
		t.Fatalf("ring holds %d steps, want only today", got)
	}
	for _, back := range []int{2, 1} {
		day := now.AddDate(0, 0, -back).Format(dayLayout)
		if _, err := os.Stat(stepFileName(dir, day, "heizung")); err != nil {
			t.Fatalf("file of day -%d missing: %v", back, err)
		}
	}
	if got := l.Steps("heizung", now.AddDate(0, 0, -3), now); len(got) != 3 {
		t.Fatalf("Steps = %d, want 3 (two days from files, today from RAM)", len(got))
	}
}
