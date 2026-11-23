package autolearn

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// blStart is the first day of the base load fixtures.
var blStart = time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)

// blNight reports whether quarter q lies in the night of the fixtures: before
// 06:00 or from 23:00 on, the bounds blDayStart and blNightRest.
func blNight(q int) bool {
	m := float64(q) * baseQuarterMin
	return m < blDayStart || m >= blNightRest
}

const (
	blDayStart  = 360.0
	blNightRest = 1380.0
)

// blInput builds days day files. night(i) is the night load of day i in W,
// heater(i) the average power of plug.heater in its night quarters, false for
// no record of the device on that day. Everything else is 1000 W by day.
func blInput(days int, night func(i int) float64, heater func(i int) (float64, bool)) *Input {
	var list []DayFile
	types := map[string]int{}
	for i := 0; i < days; i++ {
		day := blStart.AddDate(0, 0, i)
		f := DayFile{V: 1, Day: day.Format(dayLayout), Devices: map[string]*DeviceDay{}}
		n := night(i)
		for q := 0; q < dayQuarters; q++ {
			f.Site.Seen[q] = 1
			f.Site.LoadTotal[q] = 1000
			if blNight(q) {
				f.Site.LoadTotal[q] = float32(n)
			}
		}
		if heater != nil {
			if w, ok := heater(i); ok {
				dd := &DeviceDay{}
				for q := 0; q < dayQuarters; q++ {
					dd.Seen[q] = 1
					if blNight(q) {
						dd.AvgW[q] = float32(w)
					}
				}
				f.Devices["plug.heater"] = dd
			}
		}
		types[f.Day] = 0
		list = append(list, f)
	}
	return &Input{
		Now:  blStart.AddDate(0, 0, days).Add(12 * time.Hour),
		Loc:  time.UTC,
		Days: list,
		Base: Base{
			DayType:   types,
			Groups:    [][]time.Weekday{{0, 1, 2, 3, 4, 5, 6}},
			DayStart:  map[int]float64{0: blDayStart},
			NightRest: map[int]float64{0: blNightRest},
		},
		Devices: map[string]DeviceInfo{"plug.heater": {ID: "plug.heater", Name: "Heizlüfter", Switchable: true,
			Entities: []string{"switch.plug.heater"}}},
	}
}

// blSplit returns the drift findings and the learned values of the detector run.
func blSplit(in *Input) ([]Finding, map[string]Value) {
	var findings []Finding
	values := map[string]Value{}
	for _, c := range baseLoadRun(context.Background(), in, "") {
		if c.Finding.ID != "" {
			findings = append(findings, c.Finding)
		}
		if c.Value != nil {
			values[c.Value.Key] = *c.Value
		}
	}
	return findings, values
}

func TestBaseLoadDriftWithMeasuredDeviceNamesIt(t *testing.T) {
	in := blInput(60, func(i int) float64 {
		if i < 20 {
			return 180
		}
		return 260
	}, func(i int) (float64, bool) {
		if i < 20 {
			return 0, true
		}
		return 80, true
	})
	findings, values := blSplit(in)
	if len(findings) != 1 {
		t.Fatalf("want one drift finding, got %d", len(findings))
	}
	f := findings[0]
	if want := "baseload:" + blStart.AddDate(0, 0, 20).Format(dayLayout); f.ID != want {
		t.Errorf("ID = %q, want %q", f.ID, want)
	}
	if f.Summary.Code != "baseload.drift" || f.Group != "energy" {
		t.Errorf("summary %q in group %q, want baseload.drift in energy", f.Summary.Code, f.Group)
	}
	if f.Device != "plug.heater" {
		t.Errorf("Device = %q, want plug.heater", f.Device)
	}
	if w := f.Summary.Params["w"]; w != 80.0 {
		t.Errorf("w = %v, want 80", w)
	}
	if len(f.Apply) != 1 || f.Apply[0].Template != "night_off" || f.Apply[0].Prefill["device"] != "plug.heater" {
		t.Errorf("Apply = %+v, want night_off prefilled with plug.heater", f.Apply)
	}
	if f.Benefit == nil || f.Benefit.Unit != "kwh_week" || f.Benefit.Value < 13.4 || f.Benefit.Value > 13.5 {
		t.Errorf("Benefit = %+v, want about 13.44 kwh_week", f.Benefit)
	}
	if v, ok := values["site/base_w"]; !ok || v.Num != 260 || v.Unit != "W" {
		t.Errorf("site/base_w = %+v, want 260 W from the last 14 nights", v)
	}
}

func TestBaseLoadDriftWithoutMeasuredDeviceHasNoCandidate(t *testing.T) {
	in := blInput(60, func(i int) float64 {
		if i < 20 {
			return 180
		}
		return 260
	}, nil)
	in.Devices = map[string]DeviceInfo{}
	findings, _ := blSplit(in)
	if len(findings) != 1 {
		t.Fatalf("want one drift finding, got %d", len(findings))
	}
	if f := findings[0]; f.Device != "" || len(f.Apply) != 0 {
		t.Errorf("Device %q, Apply %+v: want no candidate and no fix", f.Device, f.Apply)
	}
	if f := findings[0]; f.Group != "check" {
		t.Errorf("Group %q, want check without a fix", f.Group)
	}
}

func TestBaseLoadSingleNightIsNoFinding(t *testing.T) {
	in := blInput(60, func(i int) float64 {
		if i == 45 {
			return 400
		}
		return 180
	}, nil)
	findings, _ := blSplit(in)
	if len(findings) != 0 {
		t.Fatalf("one night of 400 W gave %d findings, want none", len(findings))
	}
}

// TestBaseLoadFridgeSwapGivesDownNoticeAndBaseFollows is review focus 4: the base
// load falls after a fridge is swapped. The earlier "no finding on a fall" was
// replaced by Task 7 of PR 6a, so the fall is one downward notice without a fix,
// and the learned site/base_w follows the new level.
func TestBaseLoadFridgeSwapGivesDownNoticeAndBaseFollows(t *testing.T) {
	in := blInput(60, func(i int) float64 {
		if i < 40 {
			return 260
		}
		return 180
	}, nil)
	findings, values := blSplit(in)
	if len(findings) != 1 {
		t.Fatalf("a falling base load gave %d findings, want exactly one", len(findings))
	}
	if f := findings[0]; f.Summary.Code != "baseload.drift.down" || len(f.Apply) != 0 {
		t.Errorf("finding %q with Apply %+v, want baseload.drift.down without a fix", f.Summary.Code, f.Apply)
	}
	if v, ok := values["site/base_w"]; !ok || v.Num != 180 {
		t.Errorf("site/base_w = %+v, want 180 after the swap", v)
	}
}

func TestBaseLoadNeedsEnoughNights(t *testing.T) {
	in := blInput(20, func(i int) float64 {
		if i < 10 {
			return 180
		}
		return 260
	}, nil)
	findings, values := blSplit(in)
	if len(findings) != 0 {
		t.Errorf("20 nights gave %d findings, want none", len(findings))
	}
	if _, ok := values["site/base_w"]; !ok {
		t.Errorf("20 nights should still learn site/base_w")
	}
}

func TestBaseLoadDetectorIsEnergyDaily(t *testing.T) {
	if BaseLoadDetector.Group != "energy" || BaseLoadDetector.Light || BaseLoadDetector.PerDevice {
		t.Errorf("detector = %+v, want energy, daily, not per device", BaseLoadDetector)
	}
}

func TestCUSUMFindsSustainedShiftInBothDirections(t *testing.T) {
	up := make([]float64, 40)
	down := make([]float64, 40)
	for i := range up {
		if i >= 20 {
			up[i] = 1.4
			down[i] = -1.4
		}
	}
	if at, rising, ok := CUSUM(up, 0, 1); !ok || !rising || at != 20 {
		t.Errorf("rise: got (%d, %v, %v), want (20, true, true)", at, rising, ok)
	}
	if at, rising, ok := CUSUM(down, 0, 1); !ok || rising || at != 20 {
		t.Errorf("fall: got (%d, %v, %v), want (20, false, true)", at, rising, ok)
	}
}

func TestBaseLoadDriftFallingNightsReportsDown(t *testing.T) {
	in := blInput(60, func(i int) float64 {
		if i < 20 {
			return 260
		}
		return 180
	}, func(i int) (float64, bool) { return 0, false })
	findings, _ := blSplit(in)
	if len(findings) != 1 {
		t.Fatalf("want one drift finding for falling nights, got %d", len(findings))
	}
	f := findings[0]
	if f.Summary.Code != "baseload.drift.down" {
		t.Errorf("summary %q, want baseload.drift.down", f.Summary.Code)
	}
	if w := f.Summary.Params["w"]; w != 80.0 {
		t.Errorf("w = %v, want 80 (the size of the fall)", w)
	}
	if len(f.Apply) != 0 {
		t.Errorf("Apply = %+v, want none for a fall", f.Apply)
	}
}

// blSeason is a season store with the night base load of the fixture weeks in
// the three years before the current year.
func blSeason(baseW float64) *SeasonStore {
	s := LoadSeasonStore("", func(string, ...any) {})
	year := time.Now().Year()
	for k := 1; k <= 3; k++ {
		for w := 30; w <= 45; w++ {
			s.Put(Week{Key: fmt.Sprintf("%d-W%02d", year-k, w), Source: seasonSourceDays, Days: 7, BaseW: baseW})
		}
	}
	return s
}

func TestBaseLoadDriftWithSeasonNeedsOnlyFourteenNights(t *testing.T) {
	in := blInput(20, func(i int) float64 {
		if i < 10 {
			return 180
		}
		return 260
	}, nil)
	in.Devices = map[string]DeviceInfo{}
	in.Season = blSeason(180)
	findings, _ := blSplit(in)
	if len(findings) != 1 {
		t.Fatalf("20 nights with a season expectation gave %d findings, want 1", len(findings))
	}
	in.Season = nil
	if findings, _ := blSplit(in); len(findings) != 0 {
		t.Fatalf("20 nights without a season gave %d findings, want none", len(findings))
	}
}
