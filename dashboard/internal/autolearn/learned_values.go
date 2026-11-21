package autolearn

import (
	"context"
	"math"
	"time"
)

// Value kinds. A time value carries Text as "HH:MM", a state value carries its
// state in Text, and a duration value carries its seconds in Num.
const (
	ValueTime     = "time"
	ValueDuration = "duration"
	ValueState    = "state"
)

// Value is a learned number, clock time or state. It is published under Key
// as a retained topic and feeds the rules that reference it. N is the number
// of days or runs the value rests on.
type Value struct {
	Key, Device, Kind, Group string
	Num                      float64
	Text                     string
	N                        int
}

// learnTodayGroup returns the day type group of the weekday of in.Now. A
// learned value describes the day of today, so the group comes from the
// weekday and not from the day profiles of today.
func learnTodayGroup(in *Input) (int, bool) {
	loc := in.Loc
	if loc == nil {
		loc = time.Local
	}
	wd := in.Now.In(loc).Weekday()
	for g, days := range in.Base.Groups {
		for _, d := range days {
			if d == wd {
				return g, true
			}
		}
	}
	return 0, false
}

// LearnDayBoundsDetector learns the day start and the night rest of the day
// type of today. It is not per device, the bounds come from the day profiles.
var LearnDayBoundsDetector = Detector{
	Name:      "learn_day_bounds",
	Group:     "habit",
	Light:     false,
	PerDevice: false,
	Run:       learnDayBoundsRun,
}

// learnDayBoundsRun returns site/day_start and site/night_rest, each rounded to
// 5 minutes. N counts the days of the group.
func learnDayBoundsRun(ctx context.Context, in *Input, _ string) []Candidate {
	g, ok := learnTodayGroup(in)
	if !ok || ctx.Err() != nil {
		return nil
	}
	loc := in.Loc
	if loc == nil {
		loc = time.Local
	}
	today := in.Now.In(loc).Format(dayLayout)
	n := 0
	for _, f := range in.Days {
		if dg, ok := in.Base.DayType[f.Day]; f.Day != today && ok && dg == g {
			n++
		}
	}
	var out []Candidate
	bounds := []struct {
		key string
		min map[int]float64
	}{
		{"site/day_start", in.Base.DayStart},
		{"site/night_rest", in.Base.NightRest},
	}
	for _, b := range bounds {
		m, ok := b.min[g]
		if !ok {
			continue
		}
		out = append(out, Candidate{
			Detector: "learn_day_bounds",
			NEff:     float64(n),
			MinNEff:  1,
			Value: &Value{Key: b.key, Kind: ValueTime, Group: "habit",
				Text: switchTimeClock(learnRoundClock(m)), N: n},
		})
	}
	return out
}

// learnRoundClock rounds minutes of the day to 5 minutes, wrapped to one day.
func learnRoundClock(m float64) float64 {
	return math.Mod(math.Round(m/learnClockStep)*learnClockStep, minutesPerDay)
}

// learnClockStep is the rounding step of learned clock times in minutes.
const learnClockStep = 5.0

// LearnHouseStateDetector learns whether the house is quiet over the last three
// hours. It is light and not per device.
var LearnHouseStateDetector = Detector{
	Name:      "learn_house_state",
	Group:     "habit",
	Light:     true,
	PerDevice: false,
	Run:       learnHouseStateRun,
}

// learnHouseStateRun returns site/house_state when HouseState is ok.
func learnHouseStateRun(ctx context.Context, in *Input, _ string) []Candidate {
	if ctx.Err() != nil {
		return nil
	}
	state, n, ok := HouseState(in)
	if !ok {
		return nil
	}
	return []Candidate{{
		Detector: "learn_house_state",
		NEff:     float64(n),
		MinNEff:  houseMinDays,
		Value:    &Value{Key: "site/house_state", Kind: ValueState, Group: "habit", Text: state, N: n},
	}}
}
