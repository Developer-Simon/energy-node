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

// hhmmMinutes reads a clock text HH:MM as minutes of the day.
func hhmmMinutes(t *testing.T, text string) int {
	t.Helper()
	tm, err := time.Parse("15:04", text)
	if err != nil {
		t.Fatalf("clock text %q: %v", text, err)
	}
	return tm.Hour()*60 + tm.Minute()
}

func TestLearnDayBoundsFromRiseAndRest(t *testing.T) {
	loc := berlinLoc(t)
	start := time.Date(2026, 1, 5, 0, 0, 0, 0, loc)
	days := synthDays(start, 14, synthOpts{
		Seed:    11,
		Devices: []string{"light"},
		Habits: []synthHabit{
			{Device: "light", On: true, Minute: 390, Hit: 1},
			{Device: "light", On: false, Minute: 1380, Hit: 1},
		},
	})
	in := &Input{Now: start.AddDate(0, 0, 14), Loc: loc, Days: days, Base: BuildBase(days, loc)}
	vals := learnedValues(LearnDayBoundsDetector.Run(context.Background(), in, ""))
	start6, ok := learnedValueFor(vals, "site/day_start")
	if !ok || start6.Kind != "time" {
		t.Fatalf("no site/day_start in %+v", vals)
	}
	if d := hhmmMinutes(t, start6.Text) - 390; d < -15 || d > 15 {
		t.Errorf("day_start = %s, want 06:30 +/- 15 min", start6.Text)
	}
	rest, ok := learnedValueFor(vals, "site/night_rest")
	if !ok || rest.Kind != "time" {
		t.Fatalf("no site/night_rest in %+v", vals)
	}
	if d := hhmmMinutes(t, rest.Text) - 1380; d < -15 || d > 15 {
		t.Errorf("night_rest = %s, want 23:00 +/- 15 min", rest.Text)
	}
	if start6.N != 14 {
		t.Errorf("day_start counts %d days, want 14", start6.N)
	}
}

// houseInput builds the input of HouseState over the given days, all in one day type.
func houseInput(t *testing.T, days []DayFile, now time.Time) *Input {
	t.Helper()
	base := Base{
		DayType:   map[string]int{},
		Groups:    [][]time.Weekday{{time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday, time.Saturday, time.Sunday}},
		Away:      map[string]bool{},
		DayStart:  map[int]float64{},
		NightRest: map[int]float64{},
	}
	for _, f := range days {
		base.DayType[f.Day] = 0
	}
	return &Input{Now: now, Loc: berlinLoc(t), Days: days, Base: base}
}

// houseDays returns the comparison days, which have the television on from 20:00
// to 21:30, and then today with the given habits.
func houseDays(t *testing.T, comparison int, todayHabits []synthHabit) []DayFile {
	t.Helper()
	loc := berlinLoc(t)
	start := time.Date(2026, 1, 5, 0, 0, 0, 0, loc)
	evening := []synthHabit{
		{Device: "plug.tv", On: true, Minute: 1200, Hit: 1},
		{Device: "plug.tv", On: false, Minute: 1290, Hit: 1},
	}
	days := synthDays(start, comparison, synthOpts{Seed: 11, Devices: []string{"plug.tv"}, Habits: evening})
	today := synthDays(start.AddDate(0, 0, comparison), 1, synthOpts{Seed: 11, Devices: []string{"plug.tv"}, Habits: todayHabits})
	return append(days, today...)
}

func TestHouseStateQuietWithoutHandChanges(t *testing.T) {
	loc := berlinLoc(t)
	days := houseDays(t, 13, nil)
	in := houseInput(t, days, time.Date(2026, 1, 18, 22, 0, 0, 0, loc))
	state, _, ok := HouseState(in)
	if !ok || state != "quiet" {
		t.Errorf("HouseState = %q (ok %v), want quiet", state, ok)
	}
	vals := learnedValues(LearnHouseStateDetector.Run(context.Background(), in, ""))
	v, found := learnedValueFor(vals, "site/house_state")
	if !found || v.Text != "quiet" || v.Kind != ValueState {
		t.Errorf("learned house state = %+v (found %v), want quiet", v, found)
	}
}

func TestHouseStateActiveWithHandChange(t *testing.T) {
	loc := berlinLoc(t)
	days := houseDays(t, 13, []synthHabit{{Device: "plug.tv", On: true, Minute: 1200, Hit: 1}})
	in := houseInput(t, days, time.Date(2026, 1, 18, 22, 0, 0, 0, loc))
	state, _, ok := HouseState(in)
	if !ok || state != "active" {
		t.Errorf("HouseState = %q (ok %v), want active", state, ok)
	}
}

func TestHouseStateNeedsSevenComparisonDays(t *testing.T) {
	loc := berlinLoc(t)
	days := houseDays(t, 5, nil)
	in := houseInput(t, days, time.Date(2026, 1, 10, 22, 0, 0, 0, loc))
	if _, _, ok := HouseState(in); ok {
		t.Errorf("HouseState ok with 5 comparison days, want false")
	}
}
