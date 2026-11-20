package autolearn

import (
	"context"
	"math"
	"math/rand"
	"testing"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/learnrec"
)

// learnedValues keeps the values of the candidates of one run.
func learnedValues(cands []Candidate) []Value {
	var out []Value
	for _, c := range cands {
		if c.Value != nil {
			out = append(out, *c.Value)
		}
	}
	return out
}

// learnedValueFor returns the value with the given key, and false when there is none.
func learnedValueFor(vals []Value, key string) (Value, bool) {
	for _, v := range vals {
		if v.Key == key {
			return v, true
		}
	}
	return Value{}, false
}

// learnSwitchRun runs the switch time learner on the days of one device.
func learnSwitchRun(t *testing.T, days []DayFile, device string) []Candidate {
	t.Helper()
	return LearnSwitchTimeDetector.Run(context.Background(), switchTimeInput(t, days), device)
}

func TestLearnSwitchTimeUsualOffIs2230(t *testing.T) {
	loc := berlinLoc(t)
	start := time.Date(2026, 1, 5, 0, 0, 0, 0, loc)
	days := synthDays(start, 14, synthOpts{
		Seed:    11,
		Devices: []string{"lamp"},
		Habits: []synthHabit{
			// The lamp must be on before it goes off, otherwise every day is
			// already in the target state.
			{Device: "lamp", On: true, Minute: 1080, Spread: 0, Hit: 1},
			{Device: "lamp", On: false, Minute: 1350, Spread: 8, Hit: 0.85},
		},
	})
	vals := learnedValues(learnSwitchRun(t, days, "lamp"))
	off, ok := learnedValueFor(vals, "device/lamp/usual_off")
	if !ok {
		t.Fatalf("no device/lamp/usual_off in %+v", vals)
	}
	if off.Kind != "time" || off.Text != "22:30" || off.Device != "lamp" || off.Group != "habit" {
		t.Errorf("usual_off = %+v, want time 22:30 for lamp in habit", off)
	}
	if off.N < 10 {
		t.Errorf("usual_off counts %d days, want at least 10", off.N)
	}
}

func TestLearnSwitchTimeUsualOnIsEarliestAndOffLatest(t *testing.T) {
	loc := berlinLoc(t)
	start := time.Date(2026, 1, 5, 0, 0, 0, 0, loc)
	days := synthDays(start, 14, synthOpts{
		Seed:    11,
		Devices: []string{"lamp"},
		Habits: []synthHabit{
			{Device: "lamp", On: true, Minute: 420, Hit: 1},
			{Device: "lamp", On: false, Minute: 480, Hit: 1},
			{Device: "lamp", On: true, Minute: 1080, Hit: 1},
			{Device: "lamp", On: false, Minute: 1380, Hit: 1},
		},
	})
	vals := learnedValues(learnSwitchRun(t, days, "lamp"))
	on, ok := learnedValueFor(vals, "device/lamp/usual_on")
	if !ok || on.Text != "07:00" {
		t.Errorf("usual_on = %+v (found %v), want 07:00", on, ok)
	}
	off, ok := learnedValueFor(vals, "device/lamp/usual_off")
	if !ok || off.Text != "23:00" {
		t.Errorf("usual_off = %+v (found %v), want 23:00", off, ok)
	}
}

func TestLearnSwitchTimeNoiseGivesNoValue(t *testing.T) {
	loc := berlinLoc(t)
	start := time.Date(2026, 1, 5, 0, 0, 0, 0, loc)
	for seed := int64(15); seed < 20; seed++ {
		days := synthDays(start, 14, synthOpts{Seed: seed, Devices: []string{"lamp"}})
		rng := rand.New(rand.NewSource(seed + 100))
		for i := range days {
			day, err := time.ParseInLocation(dayLayout, days[i].Day, loc)
			if err != nil {
				t.Fatal(err)
			}
			for k := 0; k < 3; k++ {
				at := day.Add(time.Duration(rng.Intn(1440)) * time.Minute)
				days[i].Events = append(days[i].Events, EventRec{At: at.Unix(), Device: "lamp",
					Entity: "switch.lamp", On: rng.Intn(2) == 0, Origin: string(OriginHand)})
			}
		}
		if vals := learnedValues(learnSwitchRun(t, days, "lamp")); len(vals) != 0 {
			t.Errorf("seed %d: noise gave %d values, first %s", seed, len(vals), vals[0].Key)
		}
	}
}

func TestLearnRuntimeUsualRuntimeFromTwelveRuns(t *testing.T) {
	loc := berlinLoc(t)
	start := time.Date(2026, 1, 5, 0, 0, 0, 0, loc)
	days := runDays(t, start, 14, "heater", 1200, runtimeRuns(runtimeNormal40, nil, ""))
	vals := learnedValues(LearnRuntimeDetector.Run(context.Background(), switchTimeInput(t, days), "heater"))
	v, ok := learnedValueFor(vals, "device/heater/usual_runtime_s")
	if !ok {
		t.Fatalf("no device/heater/usual_runtime_s in %+v", vals)
	}
	if v.Kind != "duration" || math.Abs(v.Num-2400) > 60 {
		t.Errorf("usual_runtime_s = %+v, want duration near 2400 s", v)
	}
	if v.N != len(runtimeNormal40) {
		t.Errorf("usual_runtime_s counts %d runs, want %d", v.N, len(runtimeNormal40))
	}
}

func TestLearnRuntimeLongRunsDoNotMoveUsualRuntime(t *testing.T) {
	loc := berlinLoc(t)
	start := time.Date(2026, 1, 5, 0, 0, 0, 0, loc)
	days := runDays(t, start, 14, "heater", 1200, runtimeRuns(runtimeNormal40, []float64{360, 360}, ""))
	vals := learnedValues(LearnRuntimeDetector.Run(context.Background(), switchTimeInput(t, days), "heater"))
	v, ok := learnedValueFor(vals, "device/heater/usual_runtime_s")
	if !ok || math.Abs(v.Num-2400) > 60 {
		t.Errorf("usual_runtime_s = %+v (found %v), want near 2400 s", v, ok)
	}
}

func TestLearnRuntimeNeedsEightRuns(t *testing.T) {
	loc := berlinLoc(t)
	start := time.Date(2026, 1, 5, 0, 0, 0, 0, loc)
	days := runDays(t, start, 14, "heater", 1200, runtimeRuns(runtimeNormal40[:7], nil, ""))
	if vals := learnedValues(LearnRuntimeDetector.Run(context.Background(), switchTimeInput(t, days), "heater")); len(vals) != 0 {
		t.Errorf("seven runs gave values %+v, want none", vals)
	}
}

func TestEngineKeepsValuesApartFromCandidates(t *testing.T) {
	e := newTestEngine(t, learnrec.LimitsFor(learnrec.LevelBalanced, 4))
	e.Register(Detector{Name: "learn_test", Group: "habit", Light: true, Run: func(ctx context.Context, in *Input, device string) []Candidate {
		v := &Value{Key: "device/lamp/usual_on", Device: "lamp", Kind: "time", Group: "habit", Text: "07:00", N: 12}
		return []Candidate{{Value: v}, {Finding: Finding{ID: "finding", Kind: "switch_time"}}}
	}})
	e.Tick(at(t, "2026-11-20", "10:00"), habitOnly)

	cands := e.Candidates()
	if len(cands) != 1 || cands[0].Value != nil || cands[0].Finding.ID != "finding" {
		t.Fatalf("candidates = %+v, want only the finding", cands)
	}
	vals := e.Values()
	if len(vals) != 1 || vals[0].Key != "device/lamp/usual_on" || vals[0].Text != "07:00" || vals[0].N != 12 {
		t.Fatalf("values = %+v, want the learned usual_on", vals)
	}
}
