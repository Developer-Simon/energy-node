package autolearn

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/sun"
)

// regTestSite is the site of the regulator tests, in daylight on the test day.
var regTestSite = sun.Location{Lat: 52.52, Lon: 13.405}

// regTestModel is a roof model with one south facing surface.
func regTestModel() PVModel {
	return PVModel{Surfaces: []Surface{{PMax: 6000, TiltDeg: 30, AzimuthDeg: 180}}, CapW: 6000, Safe: true}
}

// regTestMinutes returns a minute record whose b:pv equals the best curve of
// the model on the test day, so every step sits in the upper PV band.
func regTestMinutes(model PVModel, day time.Time) fakeMinutes {
	vals := make([]float32, 1440)
	for m := range vals {
		t := day.Add(time.Duration(m) * time.Minute)
		az, el := sun.Position(t, regTestSite.Lat, regTestSite.Lon)
		vals[m] = float32(model.Best(az, el, nil))
	}
	return fakeMinutes{loc: time.UTC, days: map[string]map[string][]float32{
		day.Format(dayLayout): {"b:pv": vals},
	}}
}

// regTestSteps builds n regulator steps 10 s apart. The output To moves by
// pseudo random steps of 10 or 20 W. The net changes by kOf(i-d) times the
// output change of step i-d, so the dead time of the plant is d steps.
func regTestSteps(start time.Time, n, d int, kOf func(i int) float64) []Step {
	to := make([]float64, n)
	seed := uint32(7)
	cur := 500.0
	for i := range to {
		seed = seed*1103515245 + 12345
		step := []float64{-20, -10, 10, 20}[(seed>>16)%4]
		if i > 0 {
			cur += step
		}
		to[i] = cur
	}
	net := make([]float64, n)
	net[0] = 1000
	for i := 1; i < n; i++ {
		dNet := 0.0
		if j := i - d; j >= 1 {
			dNet = kOf(j) * (to[j] - to[j-1])
		}
		net[i] = net[i-1] + dNet
	}
	out := make([]Step, n)
	for i := range out {
		v := float32(to[i])
		out[i] = Step{At: start.Add(time.Duration(i) * 10 * time.Second).Unix(), Net: float32(net[i]), To: &v, Why: StepSent}
	}
	return out
}

// regTestInput wires the steps of one scenario, the minutes and the model.
func regTestInput(steps []Step, now time.Time, reg *RegulatorSpec) *Input {
	model := regTestModel()
	day := time.Date(2026, 11, 22, 0, 0, 0, 0, time.UTC)
	site := regTestSite
	return &Input{
		Now: now,
		Loc: time.UTC,
		Doc: Doc{Location: &site, Scenarios: []Scenario{{ID: "reg", Name: "Regler", Enabled: true, Regulator: reg}}},
		PV: func() (PVModel, *Raster, bool) {
			return model, nil, true
		},
		Minutes: regTestMinutes(model, day),
		Steps: func(scenario string, from, to time.Time) []Step {
			if scenario != "reg" {
				return nil
			}
			return steps
		},
	}
}

// regTestNow is the end of the test steps, in daylight on the test day.
func regTestNow(n int) time.Time {
	return time.Date(2026, 11, 22, 9, 0, 0, 0, time.UTC).Add(time.Duration(n) * 10 * time.Second)
}

func regTestStart(n int) time.Time {
	return regTestNow(n).Add(-time.Duration(n) * 10 * time.Second)
}

func regTestOne(t *testing.T, in *Input) []Candidate {
	t.Helper()
	return regulatorRun(context.Background(), in, "")
}

func constK(k float64) func(int) float64 {
	return func(int) float64 { return k }
}

func TestRegulatorDetectsOscillation(t *testing.T) {
	const n = 400
	steps := regTestSteps(regTestStart(n), n, 1, constK(1))
	in := regTestInput(steps, regTestNow(n), &RegulatorSpec{DeadbandW: 20, Gain: 0.8, BaseSource: "last"})
	cands := regTestOne(t, in)
	if len(cands) != 1 {
		t.Fatalf("candidates = %d, want 1 for a gain of 0.8 with a dead time of one step", len(cands))
	}
	f := cands[0].Finding
	if f.ID != "regulator:reg" || f.Group != "check" || f.Severity != "info" {
		t.Fatalf("finding = %+v, want regulator:reg in group check with severity info", f)
	}
	var gain *float64
	for _, op := range f.Apply[0].Ops {
		if len(op.Path) == 3 && op.Path[2] == "gain" {
			v := op.Value.(float64)
			gain = &v
		}
	}
	if gain == nil || math.Abs(*gain-0.2) > 1e-9 {
		t.Fatalf("gain op = %v, want 0.2 (0.8 * 1/4 * 1)", gain)
	}
}

func TestRegulatorWithoutDeadTimeKeepsTheGain(t *testing.T) {
	const n = 400
	steps := regTestSteps(regTestStart(n), n, 0, constK(1))
	in := regTestInput(steps, regTestNow(n), &RegulatorSpec{DeadbandW: 20, Gain: 0.8, BaseSource: "last"})
	res, ok := regulatorEval(in, in.Doc.Scenarios[0], steps)
	if !ok {
		t.Fatal("no evaluation for a clean plant with 400 steps")
	}
	if math.Abs(res.GainSoll-0.8) > 1e-9 {
		t.Fatalf("gain soll = %v, want 0.8 for a dead time of zero", res.GainSoll)
	}
	if len(regTestOne(t, in)) != 0 {
		t.Fatal("a gain of 0.8 at the limit must not be reported")
	}
}

func TestRegulatorNeedsTwoHundredChanges(t *testing.T) {
	const n = 150
	steps := regTestSteps(regTestStart(n), n, 1, constK(1))
	in := regTestInput(steps, regTestNow(n), &RegulatorSpec{DeadbandW: 20, Gain: 0.8, BaseSource: "last"})
	if got := regTestOne(t, in); len(got) != 0 {
		t.Fatalf("candidates = %d, want 0 with 150 steps", len(got))
	}
}

func TestRegulatorSkipsScatteredGain(t *testing.T) {
	const n = 400
	alt := func(i int) float64 {
		if i%2 == 0 {
			return 1.0
		}
		return 1.5
	}
	steps := regTestSteps(regTestStart(n), n, 1, alt)
	in := regTestInput(steps, regTestNow(n), &RegulatorSpec{DeadbandW: 20, Gain: 0.8, BaseSource: "last"})
	if got := regTestOne(t, in); len(got) != 0 {
		t.Fatalf("candidates = %d, want 0 when K scatters by 40 %%", len(got))
	}
}

func TestRegulatorMeasuredBaseOnlyRatesDeadTime(t *testing.T) {
	const n = 400
	steps := regTestSteps(regTestStart(n), n, 1, constK(1))
	in := regTestInput(steps, regTestNow(n), &RegulatorSpec{DeadbandW: 20, Gain: 0.8, BaseSource: "balance"})
	res, ok := regulatorEval(in, in.Doc.Scenarios[0], steps)
	if !ok || !res.Measured {
		t.Fatalf("eval = %+v, ok %v, want a measured result", res, ok)
	}
	if res.Dead != 1 {
		t.Fatalf("dead steps = %d, want 1", res.Dead)
	}
	if res.GainSoll != 0 {
		t.Fatalf("gain soll = %v, want none for a measured base", res.GainSoll)
	}
	if len(regTestOne(t, in)) != 0 {
		t.Fatal("a measured base must not produce a gain finding")
	}
}

func TestRegulatorSkipsWithoutPVModel(t *testing.T) {
	const n = 400
	steps := regTestSteps(regTestStart(n), n, 1, constK(1))
	in := regTestInput(steps, regTestNow(n), &RegulatorSpec{DeadbandW: 20, Gain: 0.8, BaseSource: "last"})
	in.PV = nil
	if got := regTestOne(t, in); len(got) != 0 {
		t.Fatalf("candidates = %d, want 0 without a PV model", len(got))
	}
}
