package autolearn

import (
	"context"
	"log"
	"math"
	"strings"
	"time"
)

// Knee fit of daily energy over the daily mean outdoor temperature. A heating
// load rises below a knee temperature, a cooling load rises above it. The fit
// is a hinge: y = a + b*max(0, knee-T) for heating and y = a + b*max(0, T-knee)
// for cooling. The knee is searched on a 1 degree grid and every candidate is
// fitted robustly with Theil-Sen.

const (
	kneeMinC       = 8.0
	kneeMaxC       = 20.0
	kneeMinPoints  = 6
	kneeMinExplain = 0.5
)

// FitKnee fits a hinge of y over temp and returns the knee temperature, the
// intercept a, the slope b, the share of the spread of y that the fit explains
// (clamped to [0, 1]) and whether the fit is trustworthy. ok requires an
// explained share of at least 0.5 and a slope with the same sign on both halves
// of the days (split by index). Pairs with NaN or infinite values are dropped.
// Fewer than six remaining pairs, or a length mismatch, give ok false.
func FitKnee(temp, y []float64, cooling bool) (knee, a, b float64, explained float64, ok bool) {
	n := len(temp)
	if len(y) != n {
		return 0, 0, 0, 0, false
	}
	var t, v []float64
	for i := 0; i < n; i++ {
		if isFinite(temp[i]) && isFinite(y[i]) {
			t = append(t, temp[i])
			v = append(v, y[i])
		}
	}
	if len(v) < kneeMinPoints {
		return 0, 0, 0, 0, false
	}

	bestMAE := math.Inf(1)
	for k := kneeMinC; k <= kneeMaxC; k++ {
		x := hingeX(t, k, cooling)
		slope, icept := TheilSen(x, v)
		mae := 0.0
		for i := range x {
			mae += math.Abs(v[i] - (icept + slope*x[i]))
		}
		mae /= float64(len(x))
		if mae < bestMAE {
			bestMAE = mae
			knee, a, b = k, icept, slope
		}
	}

	explained = explainedShare(v, bestMAE)

	x := hingeX(t, knee, cooling)
	half := len(v) / 2
	bFirst, _ := TheilSen(x[:half], v[:half])
	bSecond, _ := TheilSen(x[half:], v[half:])
	sameSign := b != 0 && bFirst != 0 && bSecond != 0 &&
		math.Signbit(bFirst) == math.Signbit(b) && math.Signbit(bSecond) == math.Signbit(b)

	ok = explained >= kneeMinExplain && sameSign
	return knee, a, b, explained, ok
}

// hingeX maps temperatures to the hinge regressor for the given knee.
func hingeX(temp []float64, knee float64, cooling bool) []float64 {
	x := make([]float64, len(temp))
	for i, T := range temp {
		if cooling {
			x[i] = math.Max(0, T-knee)
		} else {
			x[i] = math.Max(0, knee-T)
		}
	}
	return x
}

// explainedShare returns 1 minus the mean absolute error over the mean
// absolute deviation of y around its median, clamped to [0, 1]. The mean (not
// the median) of the deviations is used, so a device that sits on its baseline
// on most days still has a spread. Without spread it is 0.
func explainedShare(y []float64, mae float64) float64 {
	med := Median(y)
	dev := make([]float64, len(y))
	for i, v := range y {
		dev[i] = math.Abs(v - med)
	}
	spread := 0.0
	for _, d := range dev {
		spread += d
	}
	spread /= float64(len(dev))
	if spread == 0 {
		return 0
	}
	share := 1 - mae/spread
	return math.Max(0, math.Min(1, share))
}

func isFinite(f float64) bool {
	return !math.IsNaN(f) && !math.IsInf(f, 0)
}

const (
	// tempMinDays is the number of days with an outdoor temperature the detector needs.
	tempMinDays = 21
	// tempMinWarmWeeks is the number of weeks above a knee in which a rule must
	// have fired. The spec gives no number, the value is an assumption.
	tempMinWarmWeeks = 3
)

// tempDay is one day with an outdoor temperature. The energies are kWh of the
// day, and a series is only used for days that have all quarters seen.
type tempDay struct {
	t       float64
	house   float64
	houseOK bool
	dev     map[string]float64
	devOK   map[string]bool
}

// TemperatureDetector learns the heating knee of the house and of each device
// from the daily energy over the daily mean outdoor temperature. It stores the
// knees in the season store and suggests a temperature limit for every rule
// that switches a heating device in warm weeks too. It runs once per round for
// all devices, the store is the learned state the findings read from.
var TemperatureDetector = Detector{
	Name:      "temperature",
	Group:     "energy",
	Light:     false,
	PerDevice: false,
	Run:       temperatureRun,
}

// temperatureRun fits the knees, saves them and returns the temperature limits.
// Without an outdoor entity, fewer than tempMinDays days with a temperature or
// without a season store it returns nothing.
func temperatureRun(ctx context.Context, in *Input, _ string) []Candidate {
	if ctx.Err() != nil || in.Outdoor.EntityID == "" {
		return nil
	}
	days := temperatureDays(in)
	if len(days) < tempMinDays {
		return nil
	}
	ids := sortedDeviceIDs(in.Devices)
	knees := map[string]float64{}
	for _, id := range ids {
		var t, y []float64
		for _, d := range days {
			if d.devOK[id] {
				t = append(t, d.t)
				y = append(y, d.dev[id])
			}
		}
		if knee, _, _, _, ok := FitKnee(t, y, false); ok {
			knees[id] = knee
		}
	}
	var ht, hy []float64
	for _, d := range days {
		if d.houseOK {
			ht = append(ht, d.t)
			hy = append(hy, d.house)
		}
	}
	var house *float64
	if knee, _, _, _, ok := FitKnee(ht, hy, false); ok {
		house = &knee
	} else if len(knees) > 0 {
		vals := make([]float64, 0, len(knees))
		for _, k := range knees {
			vals = append(vals, k)
		}
		m := Median(vals)
		house = &m
	}
	if in.Season != nil {
		in.Season.SetHeatingKnees(knees, house)
		if err := in.Season.Save(); err != nil {
			log.Printf("autolearn: save heating knees: %v", err)
		}
	}
	if in.Season == nil || ctx.Err() != nil {
		return nil
	}

	weeks := in.Season.Weeks("0000-W00", "9999-W99")
	best := map[string]Candidate{}
	for _, id := range ids {
		knee, ok := knees[id]
		if !ok {
			continue
		}
		kwhWeek, warmDays := warmEnergyWeek(days, id, knee)
		if warmDays == 0 {
			continue
		}
		info := in.Devices[id]
		for _, rule := range in.Doc.Rules {
			if !ruleTurnsDeviceOn(rule, info) || hasOutdoorCondition(rule, in.Outdoor.EntityID) {
				continue
			}
			if warmWeeksFired(weeks, rule.ID, knee) < tempMinWarmWeeks {
				continue
			}
			c := temperatureCandidate(in, id, knee, rule, kwhWeek, len(days))
			if prev, seen := best[rule.ID]; !seen || c.Benefit.Value > prev.Benefit.Value {
				best[rule.ID] = c
			}
		}
	}
	var out []Candidate
	for _, rule := range in.Doc.Rules {
		if c, ok := best[rule.ID]; ok {
			out = append(out, c)
		}
	}
	return out
}

// temperatureDays reads the days with an outdoor temperature, today excluded.
func temperatureDays(in *Input) []tempDay {
	loc := in.Loc
	if loc == nil {
		loc = time.Local
	}
	today := in.Now.In(loc).Format(dayLayout)
	var out []tempDay
	for i := range in.Days {
		f := &in.Days[i]
		if f.Day == today {
			continue
		}
		sum, n := 0.0, 0
		for _, c := range f.Site.TempC {
			if c != nil {
				sum += float64(*c)
				n++
			}
		}
		if n == 0 {
			continue
		}
		d := tempDay{t: sum / float64(n), dev: map[string]float64{}, devOK: map[string]bool{}}
		d.house, d.houseOK = dayHouseKWh(f)
		for id, dd := range f.Devices {
			if dd == nil {
				continue
			}
			d.dev[id], d.devOK[id] = dayDeviceKWh(dd)
		}
		out = append(out, d)
	}
	return out
}

// dayHouseKWh is the house energy of a day in kWh, false unless every quarter was seen.
func dayHouseKWh(f *DayFile) (float64, bool) {
	sum := 0.0
	for q := 0; q < dayQuarters; q++ {
		if f.Site.Seen[q] == 0 {
			return 0, false
		}
		sum += float64(f.Site.LoadTotal[q])
	}
	return sum * baseWattQuarterKWh, true
}

// dayDeviceKWh is the energy of a device on a day in kWh, false unless every quarter was seen.
func dayDeviceKWh(dd *DeviceDay) (float64, bool) {
	sum := 0.0
	for q := 0; q < dayQuarters; q++ {
		if dd.Seen[q] == 0 {
			return 0, false
		}
		sum += float64(dd.Wh[q])
	}
	return sum / 1000, true
}

// warmEnergyWeek is the energy of a device in kWh per week on the days above
// the knee, and the number of those days. It is the mean daily energy of the
// warm days times seven.
func warmEnergyWeek(days []tempDay, id string, knee float64) (float64, int) {
	sum, n := 0.0, 0
	for _, d := range days {
		if d.devOK[id] && d.t > knee {
			sum += d.dev[id]
			n++
		}
	}
	if n == 0 {
		return 0, 0
	}
	return sum / float64(n) * 7, n
}

// warmWeeksFired counts the weeks above the knee in which the rule fired.
func warmWeeksFired(weeks []Week, rule string, knee float64) int {
	n := 0
	for _, w := range weeks {
		if w.TempC != nil && *w.TempC > knee && w.Triggers[rule] > 0 {
			n++
		}
	}
	return n
}

// ruleTurnsDeviceOn reports whether an action of the rule switches on an entity
// of the device. A rule that switches the device off, or does something else,
// is no basis for a temperature limit: a limit would then stop the protective
// switch in warm weather.
func ruleTurnsDeviceOn(rule Rule, info DeviceInfo) bool {
	for _, a := range rule.Actions {
		if a.EntityID == "" || !actionTurnsOn(a) {
			continue
		}
		for _, e := range info.Entities {
			if e == a.EntityID {
				return true
			}
		}
	}
	return false
}

// actionTurnsOn reports whether a publish action sends a constant on payload.
func actionTurnsOn(a Action) bool {
	if a.Type != "publish" || (a.PayloadSource != "" && a.PayloadSource != "constant") {
		return false
	}
	switch strings.ToUpper(strings.TrimSpace(a.Payload)) {
	case "ON", "1", "TRUE":
		return true
	}
	return false
}

// hasOutdoorCondition reports whether the rule already reads the outdoor entity.
func hasOutdoorCondition(rule Rule, entityID string) bool {
	for _, c := range rule.Conditions {
		if c.Type == "entity_value" && c.EntityID == entityID {
			return true
		}
	}
	return false
}

// round1 rounds v to one decimal, the precision the suggestion texts show.
func round1(v float64) float64 {
	return math.Round(v*10) / 10
}

// temperatureCandidate is the limit suggestion of a rule for one device. The
// condition is an entity_value below the knee, in the form the rule schema
// uses: the numeric limit is value.
func temperatureCandidate(in *Input, id string, knee float64, rule Rule, kwhWeek float64, nDays int) Candidate {
	devName := id
	if info := in.Devices[id]; info.Name != "" {
		devName = info.Name
	}
	cond := map[string]any{
		"type":           "entity_value",
		"topic":          in.Outdoor.Topic,
		"entity_id":      in.Outdoor.EntityID,
		"value_template": in.Outdoor.ValueTemplate,
		"comparison":     "below",
		"value":          knee,
	}
	name := rule.Name
	if name == "" {
		name = rule.ID
	}
	benefit := Benefit{Unit: "kwh_week", Value: kwhWeek}
	finding := Finding{
		ID:         FindingID("temp_limit", rule.ID),
		Kind:       "temp_limit",
		Group:      "energy",
		RuleIDs:    []string{rule.ID},
		Severity:   "info",
		Confidence: "high",
		Summary: Text{Code: "temperature.limit", Params: map[string]any{
			"rule":     name,
			"device":   devName,
			"knee":     knee,
			"kwh_warm": round1(kwhWeek),
		}},
		Evidence: Evidence{Shape: "scale", Unit: "kwh_week", IstValue: &kwhWeek},
		Facts:    []Fact{{Code: "knee", Value: knee}},
		Apply: []Fix{{Code: "add_condition", RuleID: rule.ID,
			Ops: []Op{{Path: []any{"conditions", len(rule.Conditions)}, Value: cond}}}},
		Device:  id,
		Benefit: &benefit,
	}
	return Candidate{
		Finding:     finding,
		Detector:    "temperature",
		NEff:        float64(nDays),
		MinNEff:     tempMinDays,
		Lower:       1,
		HalvesAgree: true,
		Benefit:     benefit,
	}
}
