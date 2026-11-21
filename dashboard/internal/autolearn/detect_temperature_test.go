package autolearn

import (
	"context"
	"math"
	"math/rand"
	"path/filepath"
	"testing"
	"time"
)

// kneeSynthDays returns 120 days of temperatures spread over 0..25 degC and the
// energy produced by fn with a small deterministic noise.
func kneeSynthDays(seed int64, fn func(T float64) float64) (temp, y []float64) {
	rng := rand.New(rand.NewSource(seed))
	for i := 0; i < 120; i++ {
		T := 25 * rng.Float64()
		temp = append(temp, T)
		y = append(y, fn(T)+0.2*rng.NormFloat64())
	}
	return temp, y
}

func TestFitKneeHeatPump(t *testing.T) {
	temp, y := kneeSynthDays(1, func(T float64) float64 {
		return 2 + 0.8*math.Max(0, 15-T)
	})
	knee, _, b, explained, ok := FitKnee(temp, y, false)
	if math.Abs(knee-15) > 1 {
		t.Fatalf("knee = %v, want 15 +- 1", knee)
	}
	if math.Abs(b-0.8) > 0.15 {
		t.Fatalf("b = %v, want 0.8 +- 0.15", b)
	}
	if !ok {
		t.Fatalf("ok = false, explained = %v, want a trusted fit", explained)
	}
}

func TestFitKneeCooling(t *testing.T) {
	// Most days sit above the cooling knee, so the median of y lies on the
	// sloped part and the explained share is meaningful.
	rng := rand.New(rand.NewSource(2))
	var temp, y []float64
	for i := 0; i < 120; i++ {
		T := 10 + 20*rng.Float64()
		temp = append(temp, T)
		y = append(y, 1+0.5*math.Max(0, T-18)+0.2*rng.NormFloat64())
	}
	knee, _, b, _, ok := FitKnee(temp, y, true)
	if math.Abs(knee-18) > 1 {
		t.Fatalf("knee = %v, want 18 +- 1", knee)
	}
	if math.Abs(b-0.5) > 0.15 {
		t.Fatalf("b = %v, want 0.5 +- 0.15", b)
	}
	if !ok {
		t.Fatal("ok = false, want a trusted cooling fit")
	}
}

func TestFitKneeNoTemperatureLink(t *testing.T) {
	rng := rand.New(rand.NewSource(3))
	var temp, y []float64
	for i := 0; i < 120; i++ {
		temp = append(temp, 25*rng.Float64())
		y = append(y, 5+rng.NormFloat64())
	}
	_, _, _, explained, ok := FitKnee(temp, y, false)
	if ok {
		t.Fatalf("ok = true for a device without temperature link, explained = %v", explained)
	}
}

func TestFitKneeFewerThanSixPoints(t *testing.T) {
	temp := []float64{1, 5, 10, 15, 20}
	y := []float64{9, 7, 5, 3, 1}
	if _, _, _, _, ok := FitKnee(temp, y, false); ok {
		t.Fatal("ok = true with five points, want false")
	}
}

func TestFitKneeDropsNaN(t *testing.T) {
	temp, y := kneeSynthDays(4, func(T float64) float64 {
		return 2 + 0.8*math.Max(0, 15-T)
	})
	temp = append(temp, math.NaN(), 3)
	y = append(y, 4, math.Inf(1))
	knee, _, _, _, ok := FitKnee(temp, y, false)
	if math.Abs(knee-15) > 1 {
		t.Fatalf("knee = %v with NaN input, want 15 +- 1", knee)
	}
	if !ok {
		t.Fatal("ok = false with NaN pairs dropped, want true")
	}
}

func TestFitKneeLengthMismatch(t *testing.T) {
	temp := []float64{1, 2, 3, 4, 5, 6, 7}
	y := []float64{1, 2, 3}
	if _, _, _, _, ok := FitKnee(temp, y, false); ok {
		t.Fatal("ok = true for mismatched lengths, want false")
	}
}

// tempHeatPump is the energy of a heat pump in kWh per day at the outdoor temperature T.
func tempHeatPump(T float64) float64 { return 2 + 0.8*math.Max(0, 15-T) }

// tempInput builds days of a heat pump "hp" (plug.hp) with the outdoor
// temperature, the season store with the given weeks and the rules.
func tempInput(t *testing.T, days int, weeks []Week, rules []Rule) *Input {
	t.Helper()
	rng := rand.New(rand.NewSource(3))
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	var list []DayFile
	for i := 0; i < days; i++ {
		temp := -5 + 30*rng.Float64()
		energy := tempHeatPump(temp) + 0.2*(rng.Float64()-0.5)
		f := DayFile{V: 1, Day: start.AddDate(0, 0, i).Format(dayLayout), Devices: map[string]*DeviceDay{}}
		dd := &DeviceDay{}
		for q := 0; q < dayQuarters; q++ {
			dd.Seen[q] = 1
			dd.Wh[q] = float32(energy * 1000 / dayQuarters)
			f.Site.Seen[q] = 1
			f.Site.LoadTotal[q] = float32(energy * 1000 / (dayQuarters * 0.25))
			tq := float32(temp)
			f.Site.TempC[q] = &tq
		}
		f.Devices["hp"] = dd
		list = append(list, f)
	}
	store := LoadSeasonStore("", nil)
	for _, w := range weeks {
		store.Put(w)
	}
	return &Input{
		Now:     start.AddDate(0, 0, days+1),
		Loc:     time.UTC,
		Days:    list,
		Doc:     Doc{Rules: rules},
		Devices: map[string]DeviceInfo{"hp": {ID: "hp", Entities: []string{"plug.hp"}}},
		Season:  store,
		Outdoor: OutdoorSource{EntityID: "sensor.out", Topic: "ha/out", ValueTemplate: "{{ value_json.t }}"},
	}
}

// tempWeeks are winter weeks at -2 degC and summer weeks at 20 degC, the rule
// fires in all of them when fireSummer is set, otherwise only in winter.
func tempWeeks(rule string, fireSummer bool) []Week {
	var out []Week
	for w := 1; w <= 10; w++ {
		c := -2.0
		out = append(out, Week{Key: "2026-W" + twoDigits(w), TempC: &c, Triggers: map[string]float64{rule: 3}})
	}
	for w := 20; w <= 24; w++ {
		c := 20.0
		week := Week{Key: "2026-W" + twoDigits(w), TempC: &c}
		if fireSummer {
			week.Triggers = map[string]float64{rule: 3}
		}
		out = append(out, week)
	}
	return out
}

func twoDigits(n int) string {
	return string([]byte{byte('0' + n/10), byte('0' + n%10)})
}

// tempRule switches the heat pump on.
func tempRule(id string) Rule {
	return Rule{ID: id, Enabled: true, Actions: []Action{{Type: "publish", EntityID: "plug.hp", PayloadSource: "constant", Payload: "ON"}}}
}

// tempOffRule switches the heat pump off, like the protective demo rule.
func tempOffRule(id string) Rule {
	return Rule{ID: id, Enabled: true, Actions: []Action{{Type: "publish", EntityID: "plug.hp", PayloadSource: "constant", Payload: "OFF"}}}
}

func TestTemperatureHeatPumpLimit(t *testing.T) {
	in := tempInput(t, 60, tempWeeks("r1", true), []Rule{tempRule("r1")})
	got := TemperatureDetector.Run(context.Background(), in, "")
	if len(got) != 1 {
		t.Fatalf("candidates = %d, want 1", len(got))
	}
	c := got[0]
	if c.Finding.ID != "temp_limit:r1" || c.Finding.Device != "hp" {
		t.Fatalf("finding = %q device %q", c.Finding.ID, c.Finding.Device)
	}
	if len(c.Finding.Apply) != 1 || c.Finding.Apply[0].Code != "add_condition" {
		t.Fatalf("apply = %+v, want one add_condition", c.Finding.Apply)
	}
	cond, ok := c.Finding.Apply[0].Ops[0].Value.(map[string]any)
	if !ok {
		t.Fatalf("condition = %#v", c.Finding.Apply[0].Ops[0].Value)
	}
	knee, _ := cond["value"].(float64)
	if cond["type"] != "entity_value" || cond["comparison"] != "below" || cond["entity_id"] != "sensor.out" ||
		math.Abs(knee-15) > 1 {
		t.Errorf("condition = %+v, want entity_value below near 15", cond)
	}
	if k, ok := in.Season.HeatingKnee("hp"); !ok || math.Abs(k-15) > 1 {
		t.Errorf("stored device knee = %v %v, want 15 +- 1", k, ok)
	}
	if h := in.Season.HouseKnee(); h == nil || math.Abs(*h-15) > 1 {
		t.Errorf("stored house knee = %v, want 15 +- 1", h)
	}
	if c.Benefit.Unit != "kwh_week" || c.Benefit.Value <= 0 {
		t.Errorf("benefit = %+v, want positive kwh_week", c.Benefit)
	}
}

func TestTemperatureNoLimitForSwitchOffRule(t *testing.T) {
	in := tempInput(t, 60, tempWeeks("r1", true), []Rule{tempOffRule("r1")})
	if got := TemperatureDetector.Run(context.Background(), in, ""); len(got) != 0 {
		t.Fatalf("candidates = %d, want 0 for a rule that switches the device off", len(got))
	}
}

func TestTemperatureFewerThanTwentyOneDays(t *testing.T) {
	in := tempInput(t, 20, tempWeeks("r1", true), []Rule{tempRule("r1")})
	if got := TemperatureDetector.Run(context.Background(), in, ""); len(got) != 0 {
		t.Fatalf("candidates = %d, want 0 with 20 days", len(got))
	}
}

func TestTemperatureNeedsOutdoorEntity(t *testing.T) {
	in := tempInput(t, 60, tempWeeks("r1", true), []Rule{tempRule("r1")})
	in.Outdoor = OutdoorSource{}
	if got := TemperatureDetector.Run(context.Background(), in, ""); len(got) != 0 {
		t.Fatalf("candidates = %d, want 0 without outdoor entity", len(got))
	}
}

func TestTemperatureRuleOnlyInWinter(t *testing.T) {
	in := tempInput(t, 60, tempWeeks("r1", false), []Rule{tempRule("r1")})
	if got := TemperatureDetector.Run(context.Background(), in, ""); len(got) != 0 {
		t.Fatalf("candidates = %d, want 0 for a rule that stops in summer", len(got))
	}
}

func TestTemperatureSkipsRuleWithOutdoorCondition(t *testing.T) {
	rule := tempRule("r1")
	rule.Conditions = []Condition{{Type: "entity_value", EntityID: "sensor.out", Comparison: "below"}}
	in := tempInput(t, 60, tempWeeks("r1", true), []Rule{rule})
	if got := TemperatureDetector.Run(context.Background(), in, ""); len(got) != 0 {
		t.Fatalf("candidates = %d, want 0 when the rule already reads the outdoor entity", len(got))
	}
}

func TestTemperatureKneesSurviveReload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "season.json")
	store := LoadSeasonStore(path, nil)
	house := 14.0
	store.SetHeatingKnees(map[string]float64{"hp": 16}, &house)
	if err := store.Save(); err != nil {
		t.Fatal(err)
	}
	again := LoadSeasonStore(path, nil)
	if k, ok := again.HeatingKnee("hp"); !ok || k != 16 {
		t.Errorf("device knee after reload = %v %v, want 16", k, ok)
	}
	if h := again.HouseKnee(); h == nil || *h != 14 {
		t.Errorf("house knee after reload = %v, want 14", h)
	}
}
