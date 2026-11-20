package autolearn

import (
	"context"
	"testing"
	"time"
)

// awayInput builds the days of one device with kWh per day, and the base that
// marks the days for which away reports true. All days share one day type.
func awayInput(t *testing.T, n int, device string, kwh func(i int) float64, away func(i int) bool, switchable bool) *Input {
	t.Helper()
	loc := berlinLoc(t)
	start := time.Date(2026, 1, 5, 0, 0, 0, 0, loc)
	base := Base{
		DayType:   map[string]int{},
		Groups:    [][]time.Weekday{{time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday, time.Saturday, time.Sunday}},
		Away:      map[string]bool{},
		DayStart:  map[int]float64{},
		NightRest: map[int]float64{},
	}
	days := make([]DayFile, n)
	for i := range days {
		day := time.Date(start.Year(), start.Month(), start.Day()+i, 0, 0, 0, 0, loc)
		dd := &DeviceDay{}
		for q := 0; q < dayQuarters; q++ {
			dd.Seen[q] = dayCounterMax
			dd.Wh[q] = float32(kwh(i) * 1000 / dayQuarters)
		}
		f := DayFile{V: dayFileVersion, Day: day.Format(dayLayout),
			Devices: map[string]*DeviceDay{device: dd}, Events: []EventRec{}, Actions: []ActionRec{}}
		days[i] = f
		base.DayType[f.Day] = 0
		if away(i) {
			base.Away[f.Day] = true
		}
	}
	last := time.Date(start.Year(), start.Month(), start.Day()+n-1, 0, 0, 0, 0, loc)
	return &Input{
		Now:     last.Add(24 * time.Hour),
		Loc:     loc,
		Days:    days,
		Devices: map[string]DeviceInfo{device: {ID: device, Switchable: switchable}},
		Base:    base,
	}
}

// awayDaysFrom reports whether day i lies in the given set of day indexes.
func awayDaysFrom(set ...int) func(int) bool {
	return func(i int) bool {
		for _, s := range set {
			if s == i {
				return true
			}
		}
		return false
	}
}

func TestAwayStandbyDeviceDrawingWhileAwayIsOneCandidate(t *testing.T) {
	// 14 days, four of them away. The heater draws 0.4 kWh while away and 1.5 kWh otherwise.
	in := awayInput(t, 14, "heater",
		func(i int) float64 {
			if awayDaysFrom(2, 5, 8, 11)(i) {
				return 0.4
			}
			return 1.5
		},
		awayDaysFrom(2, 5, 8, 11), true)
	cands := AwayStandbyDetector.Run(context.Background(), in, "heater")
	if len(cands) != 1 {
		t.Fatalf("candidates = %d, want 1: %+v", len(cands), cands)
	}
	c := cands[0]
	if c.Finding.ID != "away_standby:heater" || c.Finding.Summary.Code != "away.standby" {
		t.Fatalf("finding = %s / %s, want away_standby:heater / away.standby", c.Finding.ID, c.Finding.Summary.Code)
	}
	if c.Finding.Summary.Params["away_days"] != 4 {
		t.Errorf("away_days = %v, want 4", c.Finding.Summary.Params["away_days"])
	}
	if c.Finding.Evidence.Shape != "scale" || c.Finding.Evidence.IstValue == nil || c.Finding.Evidence.SollValue == nil {
		t.Fatalf("evidence = %+v, want scale with ist and soll", c.Finding.Evidence)
	}
	if *c.Finding.Evidence.SollValue != 0 || *c.Finding.Evidence.IstValue < 0.39 || *c.Finding.Evidence.IstValue > 0.41 {
		t.Errorf("evidence ist/soll = %v/%v, want about 0.4 and 0", *c.Finding.Evidence.IstValue, *c.Finding.Evidence.SollValue)
	}
	if len(c.Finding.Apply) != 1 || c.Finding.Apply[0].Template != "quiet_off" || c.Finding.Apply[0].Prefill["device"] != "heater" {
		t.Errorf("apply = %+v, want one quiet_off fix for heater", c.Finding.Apply)
	}
	if c.Finding.Benefit == nil || c.Finding.Benefit.Unit != "kwh_week" || c.Finding.Benefit.Value <= 0 {
		t.Errorf("benefit = %+v, want kwh_week above zero", c.Finding.Benefit)
	}
	if c.Detector != "away_standby" || c.Limit != 0.6 || c.MinNEff != 2 {
		t.Errorf("gate numbers = detector %q limit %v min %v", c.Detector, c.Limit, c.MinNEff)
	}
}

func TestAwayStandbyFridgeDrawingEveryDayIsNil(t *testing.T) {
	// The fridge draws 0.8 kWh on every day, away or not. Switching it off would do harm.
	in := awayInput(t, 14, "fridge", func(int) float64 { return 0.8 }, awayDaysFrom(2, 5, 8, 11), true)
	if cands := AwayStandbyDetector.Run(context.Background(), in, "fridge"); len(cands) != 0 {
		t.Fatalf("candidates = %+v, want none", cands)
	}
}

func TestAwayStandbyNotSwitchableIsNil(t *testing.T) {
	in := awayInput(t, 14, "heater",
		func(i int) float64 {
			if awayDaysFrom(2, 5, 8, 11)(i) {
				return 0.4
			}
			return 1.5
		},
		awayDaysFrom(2, 5, 8, 11), false)
	if cands := AwayStandbyDetector.Run(context.Background(), in, "heater"); len(cands) != 0 {
		t.Fatalf("candidates = %+v, want none for a device that is not switchable", cands)
	}
}

func TestAwayStandbyNoiseWithoutAwayIsNil(t *testing.T) {
	noise := []float64{0.3, 1.8, 0.9, 2.4, 0.2, 1.1, 0.7, 1.9, 0.4, 2.2, 0.6, 1.3, 0.5, 1.6}
	in := awayInput(t, len(noise), "heater", func(i int) float64 { return noise[i] }, awayDaysFrom(), true)
	if cands := AwayStandbyDetector.Run(context.Background(), in, "heater"); len(cands) != 0 {
		t.Fatalf("candidates = %+v, want none without away days", cands)
	}
}

func TestAwayStandbyAwayEnergyBelowThresholdIsNil(t *testing.T) {
	// Away days draw 0.1 kWh, below the 0.2 kWh that a standby load must reach.
	in := awayInput(t, 14, "heater",
		func(i int) float64 {
			if awayDaysFrom(2, 5, 8, 11)(i) {
				return 0.1
			}
			return 1.5
		},
		awayDaysFrom(2, 5, 8, 11), true)
	if cands := AwayStandbyDetector.Run(context.Background(), in, "heater"); len(cands) != 0 {
		t.Fatalf("candidates = %+v, want none below 0.2 kWh while away", cands)
	}
}
