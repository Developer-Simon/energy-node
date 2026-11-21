package autolearn

import (
	"context"
	"math"
	"time"
)

// AwayStandbyDetector suggests a quiet switch-off for a switchable device that
// keeps drawing power while the house is away. A device that draws the same on
// every day, such as a fridge, is not suggested, because switching it off
// would do harm. It follows the absence suggestion of the learned values.
var AwayStandbyDetector = Detector{
	Name:      "away_standby",
	Group:     "habit",
	Light:     false,
	PerDevice: true,
	Run:       awayStandbyRun,
}

const (
	// awayMinKWh is the mean energy on absence days a standby load must exceed.
	awayMinKWh = 0.2
	// awayShareOfOther is the share of the mean energy on other days the mean
	// on absence days must stay below.
	awayShareOfOther = 0.5
	// awayMinNEff is the minimum effective number of absence days.
	awayMinNEff = 2.0
	// awayLimit is the lower bound for high confidence.
	awayLimit = 0.6
	// awayQuietKWh is the step of the shown mean energy, the quiet check
	// shows a new value only when it moves by this much.
	awayQuietKWh = 0.05
)

// awayStandbyRun finds the device when its mean energy on absence days is above
// awayMinKWh and below half the mean on the other days of the learning period.
func awayStandbyRun(ctx context.Context, in *Input, device string) []Candidate {
	if ctx.Err() != nil {
		return nil
	}
	if info, ok := in.Devices[device]; !ok || !info.Switchable {
		return nil
	}
	loc := in.Loc
	if loc == nil {
		loc = time.Local
	}
	today := in.Now.In(loc).Format(dayLayout)

	var away, other, awayWeights []float64
	var first, last time.Time
	hits := 0
	for i := range in.Days {
		f := &in.Days[i]
		t, err := time.ParseInLocation(dayLayout, f.Day, loc)
		if err != nil {
			continue
		}
		if first.IsZero() || t.Before(first) {
			first = t
		}
		if last.IsZero() || t.After(last) {
			last = t
		}
		dd := f.Devices[device]
		if _, ok := in.Base.DayType[f.Day]; !ok || dd == nil || f.Day == today {
			continue
		}
		kwh := deviceDayKWh(dd)
		if !in.Base.Away[f.Day] {
			other = append(other, kwh)
			continue
		}
		age := math.Max(0, in.Now.Sub(t).Hours()/24)
		away = append(away, kwh)
		awayWeights = append(awayWeights, DayWeight(age))
		if kwh > awayMinKWh {
			hits++
		}
	}
	if len(away) == 0 || len(other) == 0 {
		return nil
	}
	// The first and the second half of the absence days must both show the
	// standby load, the halves check of the other detectors.
	mid := len(away) / 2
	firstHalf, secondHalf := away[:mid], away[mid:]

	meanAway, meanOther := mean(away), mean(other)
	if !(meanAway > awayMinKWh && meanAway < awayShareOfOther*meanOther) {
		return nil
	}
	neff := NEff(awayWeights)
	lower := WilsonLower(hits, len(away))
	halves := mean(firstHalf) > awayMinKWh && mean(secondHalf) > awayMinKWh

	total := 0.0
	for _, v := range away {
		total += v
	}
	weeks := math.Max(last.Sub(first).Hours()/24/7, 1)
	benefit := Benefit{Unit: "kwh_week", Value: total / weeks}

	conf := "estimate"
	if lower >= awayLimit && halves {
		conf = "high"
	}
	shown := math.Round(meanAway*100) / 100
	zero := 0.0
	finding := Finding{
		ID:         "away_standby:" + device,
		Kind:       "away_standby",
		Group:      "habit",
		Severity:   "info",
		Confidence: conf,
		Summary: Text{Code: "away.standby", Params: map[string]any{
			"device":    deviceName(in.Devices[device], device),
			"kwh_day":   round1(shown),
			"away_days": len(away),
		}},
		Evidence: Evidence{Shape: "scale", Unit: "kWh", IstValue: &shown, SollValue: &zero},
		Apply: []Fix{{Code: "quiet_off", Template: "quiet_off",
			Prefill: map[string]any{"device": device}}},
		Device:  device,
		Benefit: &benefit,
	}
	return []Candidate{{
		Finding:     finding,
		Detector:    "away_standby",
		NEff:        neff,
		MinNEff:     awayMinNEff,
		Lower:       lower,
		Limit:       awayLimit,
		HalvesAgree: halves,
		Benefit:     benefit,
		Values:      map[string]float64{"kwh_day": shown},
		Spread:      map[string]float64{"kwh_day": awayQuietKWh},
	}}
}

// deviceDayKWh is the energy of one device on one day in kWh.
func deviceDayKWh(dd *DeviceDay) float64 {
	var wh float64
	for _, v := range dd.Wh {
		wh += float64(v)
	}
	return wh / 1000
}

// mean is the arithmetic mean of xs, zero for no values.
func mean(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	sum := 0.0
	for _, x := range xs {
		sum += x
	}
	return sum / float64(len(xs))
}
