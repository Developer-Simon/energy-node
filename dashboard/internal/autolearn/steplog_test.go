package autolearn

import (
	"fmt"
	"math"
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
	if fig.N != 5 {
		t.Errorf("n = %d, want 5", fig.N)
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
	// Export 100 W ueber 60 s: 100 * 60 / 3600 Wh. Die Totzone-Zeile zaehlt nicht.
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
	wantExp := 100.0 * 60 / 3600
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
