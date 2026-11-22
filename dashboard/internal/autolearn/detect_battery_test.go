package autolearn

import (
	"context"
	"testing"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/sun"
)

// btNovStart is the first day of the November fixtures, btDecStart of the December ones.
var (
	btNovStart = time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	btDecStart = time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)
)

// btSoC returns the charge in percent of quarter q of day i, false for no value.
type btSoC func(i, q int) (float32, bool)

// btInput builds days day files from start. The house draws loadW in every
// quarter, a heater 1500 W and a lamp 40 W, both on every quarter. The heater is
// switchable, the lamp not. The location is 52 N, 0 E on UTC, so the sun rises
// at about 07:00 and sets at about 16:00 and noon is near 12:00. prior is the
// share of empty days of the year before, nil without a record.
func btInput(days int, start time.Time, soc btSoC, loadW float32, prior func(week int) (float64, bool)) *Input {
	var list []DayFile
	types := map[string]int{}
	for i := 0; i < days; i++ {
		day := start.AddDate(0, 0, i)
		f := DayFile{V: 1, Day: day.Format(dayLayout), Devices: map[string]*DeviceDay{}}
		heater := &DeviceDay{}
		lamp := &DeviceDay{}
		for q := 0; q < dayQuarters; q++ {
			f.Site.Seen[q] = 1
			f.Site.LoadTotal[q] = loadW
			heater.Seen[q] = 1
			heater.AvgW[q] = 1500
			lamp.Seen[q] = 1
			lamp.AvgW[q] = 40
			if v, ok := soc(i, q); ok {
				x := v
				f.Site.SoC[q] = &x
			}
		}
		f.Devices["plug.heater"] = heater
		f.Devices["lamp.desk"] = lamp
		types[f.Day] = 0
		list = append(list, f)
	}
	return &Input{
		Now:  start.AddDate(0, 0, days).Add(12 * time.Hour),
		Loc:  time.UTC,
		Days: list,
		Doc:  Doc{Location: &sun.Location{Lat: 52, Lon: 0}},
		Base: Base{
			DayType:   types,
			Groups:    [][]time.Weekday{{0, 1, 2, 3, 4, 5, 6}},
			DayStart:  map[int]float64{0: 360},
			NightRest: map[int]float64{0: 1380},
		},
		Devices: map[string]DeviceInfo{
			"plug.heater": {ID: "plug.heater", Name: "Heizlüfter", Switchable: true, Entities: []string{"switch.plug.heater"}},
			"lamp.desk":   {ID: "lamp.desk", Name: "Schreibtischlampe", Switchable: false, Entities: []string{"light.desk"}},
		},
		PriorEmptyShare: prior,
	}
}

// btSplit returns the findings and the learned values of one run of the detector.
func btSplit(in *Input) ([]Finding, map[string]Value) {
	var findings []Finding
	values := map[string]Value{}
	for _, c := range batteryRun(context.Background(), in, "") {
		if c.Finding.ID != "" {
			findings = append(findings, c.Finding)
		}
		if c.Value != nil {
			values[c.Value.Key] = *c.Value
		}
	}
	return findings, values
}

// btFindingByID returns the finding with the ID, false when there is none.
func btFindingByID(findings []Finding, id string) (Finding, bool) {
	for _, f := range findings {
		if f.ID == id {
			return f, true
		}
	}
	return Finding{}, false
}

// btFullAt11 is the charge of a battery that is full from 11:15 on, the first
// quarter from 11:15 counts as full, the quarters before stay at 40 %.
func btFullAt11(i, q int) (float32, bool) {
	if float64(q)*baseQuarterMin >= 11*60+15 {
		return 98, true
	}
	return 40, true
}

// btEmptyAll is a battery at 10 % all day and night, so it is empty before
// sunrise every day.
func btEmptyAll(i, q int) (float32, bool) {
	return 10, true
}

func TestBatteryFullBeforeNoonFindsAndLearnsTime(t *testing.T) {
	in := btInput(14, btNovStart, btFullAt11, 1000, nil)
	findings, values := btSplit(in)
	f, ok := btFindingByID(findings, "battery:full")
	if !ok {
		t.Fatalf("want a battery:full finding, got %d findings", len(findings))
	}
	if f.Summary.Code != "battery.full_early" || f.Group != "check" || f.Severity != "info" {
		t.Errorf("summary %q in %q with severity %q, want battery.full_early info in check", f.Summary.Code, f.Group, f.Severity)
	}
	if got := f.Summary.Params["time"]; got != "11:15" {
		t.Errorf("time = %v, want 11:15", got)
	}
	if _, ok := f.Summary.Params["kwh_after"]; !ok {
		t.Errorf("params %+v lack kwh_after", f.Summary.Params)
	}
	if f.Evidence.Shape != "curve" || len(f.Evidence.Quarters) != dayQuarters {
		t.Errorf("evidence shape %q with %d quarters, want curve with %d", f.Evidence.Shape, len(f.Evidence.Quarters), dayQuarters)
	}
	if len(f.Apply) != 0 {
		t.Errorf("Apply = %+v, want none, a load is not offered before PR 5", f.Apply)
	}
	if v, ok := values["site/battery_full_at"]; !ok || v.Kind != ValueTime || v.Text != "11:15" {
		t.Errorf("site/battery_full_at = %+v, want time 11:15", v)
	}
}

func TestBatteryFullNeedsTenDays(t *testing.T) {
	in := btInput(9, btNovStart, btFullAt11, 1000, nil)
	findings, values := btSplit(in)
	if _, ok := btFindingByID(findings, "battery:full"); ok {
		t.Errorf("9 days gave a battery:full finding, want none")
	}
	if _, ok := values["site/battery_full_at"]; ok {
		t.Errorf("9 days learned site/battery_full_at, want none")
	}
}

// TestBatteryDecemberEmptyEveryNightWithLastYearEmptyIsNoFinding is review
// focus 5: the battery is empty every night in December, the same weeks of last
// year were empty as well. That is the season, not a finding.
func TestBatteryDecemberEmptyEveryNightWithLastYearEmptyIsNoFinding(t *testing.T) {
	lastYearEmpty := func(week int) (float64, bool) { return 1.0, true }
	in := btInput(40, btDecStart, btEmptyAll, 1600, lastYearEmpty)
	findings, _ := btSplit(in)
	if f, ok := btFindingByID(findings, "battery:empty"); ok {
		t.Errorf("empty every night with last year empty gave %+v, want no finding", f.Summary)
	}
}

func TestBatteryEmptyWithoutLastYearNamesLargestConsumer(t *testing.T) {
	lastYearNever := func(week int) (float64, bool) { return 0, true }
	in := btInput(40, btDecStart, btEmptyAll, 1600, lastYearNever)
	findings, _ := btSplit(in)
	f, ok := btFindingByID(findings, "battery:empty")
	if !ok {
		t.Fatalf("want a battery:empty finding, got %d findings", len(findings))
	}
	if f.Summary.Code != "battery.empty_early" || f.Group != "energy" {
		t.Errorf("summary %q in %q, want battery.empty_early in energy", f.Summary.Code, f.Group)
	}
	if got := f.Summary.Params["candidates"]; got != "Heizlüfter" {
		t.Errorf("candidates = %v, want Heizlüfter only", got)
	}
	if kwh, ok := f.Summary.Params["kwh_missing"].(float64); !ok || kwh < 10 {
		t.Errorf("kwh_missing = %v, want more than 10 kWh of night energy", f.Summary.Params["kwh_missing"])
	}
	if len(f.Apply) != 1 || f.Apply[0].Template != "night_off" || f.Apply[0].Prefill["device"] != "plug.heater" {
		t.Errorf("Apply = %+v, want night_off prefilled with plug.heater", f.Apply)
	}
	if f.Benefit == nil || f.Benefit.Unit != "kwh_week" || f.Benefit.Value <= 0 {
		t.Errorf("Benefit = %+v, want kwh_week above zero", f.Benefit)
	}
}

func TestBatteryEmptyWithoutPriorRecordIsNoFinding(t *testing.T) {
	in := btInput(40, btDecStart, btEmptyAll, 1600, nil)
	findings, _ := btSplit(in)
	if _, ok := btFindingByID(findings, "battery:empty"); ok {
		t.Errorf("no record of last year gave a battery:empty finding, want none")
	}
}

func TestBatteryWithoutSoCGivesNothing(t *testing.T) {
	none := func(i, q int) (float32, bool) { return 0, false }
	in := btInput(40, btNovStart, none, 1000, func(week int) (float64, bool) { return 0, true })
	findings, values := btSplit(in)
	if len(findings) != 0 || len(values) != 0 {
		t.Errorf("no SoC gave %d findings and %d values, want none", len(findings), len(values))
	}
}

func TestBatteryNightWhIsLearnedFromTheNight(t *testing.T) {
	in := btInput(20, btDecStart, btEmptyAll, 1600, nil)
	_, values := btSplit(in)
	v, ok := values["site/night_wh"]
	if !ok || v.Unit != "Wh" || v.Num < 12000 || v.Num > 14000 {
		t.Fatalf("site/night_wh = %+v, want about 13000 Wh from sunset to the empty battery at midnight", v)
	}
	if int(v.Num)%100 != 0 {
		t.Errorf("site/night_wh = %v, want a multiple of 100 Wh", v.Num)
	}
}

func TestBatteryDetectorIsEnergyDaily(t *testing.T) {
	if BatteryDetector.Name != "battery" || BatteryDetector.Group != "energy" || BatteryDetector.Light || BatteryDetector.PerDevice {
		t.Errorf("detector = %+v, want battery, energy, daily, not per device", BatteryDetector)
	}
}

// btFileAt is a day file of the given charge in every quarter, the day of start plus i.
func btFileAt(start time.Time, i int, soc float32) DayFile {
	f := DayFile{V: 1, Day: start.AddDate(0, 0, i).Format(dayLayout)}
	for q := 0; q < dayQuarters; q++ {
		v := soc
		f.Site.SoC[q] = &v
		f.Site.Seen[q] = 1
	}
	return f
}

// TestEmptyShareOfCountsEmptyMornings: the prior 14 days sit at 40 %, the
// threshold is 43 %, so a week at 10 % is empty every morning and a week at 90 %
// never is. The share is the same definition the detector uses.
func TestEmptyShareOfCountsEmptyMornings(t *testing.T) {
	site := &sun.Location{Lat: 52, Lon: 0}
	priorStart := time.Date(2026, 10, 18, 0, 0, 0, 0, time.UTC)
	weekStart := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	var prior []DayFile
	for i := 0; i < 14; i++ {
		prior = append(prior, btFileAt(priorStart, i, 40))
	}
	var empty, full []DayFile
	for i := 0; i < 7; i++ {
		empty = append(empty, btFileAt(weekStart, i, 10))
		full = append(full, btFileAt(weekStart, i, 90))
	}
	share, ok := EmptyShareOf(prior, empty, site, time.UTC)
	if !ok || share != 1 {
		t.Errorf("empty week: share %v ok %v, want 1 true", share, ok)
	}
	share, ok = EmptyShareOf(prior, full, site, time.UTC)
	if !ok || share != 0 {
		t.Errorf("full week: share %v ok %v, want 0 true", share, ok)
	}
}

func TestEmptyShareOfNeedsFourDaysAndSun(t *testing.T) {
	weekStart := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	var three []DayFile
	for i := 0; i < 3; i++ {
		three = append(three, btFileAt(weekStart, i, 10))
	}
	if _, ok := EmptyShareOf(nil, three, &sun.Location{Lat: 52, Lon: 0}, time.UTC); ok {
		t.Errorf("three days gave a share, want none")
	}
	var seven []DayFile
	for i := 0; i < 7; i++ {
		seven = append(seven, btFileAt(weekStart, i, 10))
	}
	if _, ok := EmptyShareOf(nil, seven, nil, time.UTC); ok {
		t.Errorf("without a site gave a share, want none")
	}
}

// batteryTestDay is a day with the charge of soc(q) per quarter.
func batteryTestDay(soc func(q int) float32) *DayFile {
	f := &DayFile{}
	for q := 0; q < dayQuarters; q++ {
		v := soc(q)
		f.Site.SoC[q] = &v
	}
	return f
}

func TestBatteryFullEvidenceIsFullOnlyAtNoon(t *testing.T) {
	f := batteryTestDay(func(q int) float32 {
		switch {
		case q < 32:
			return 10
		case q < 40:
			return 10 + float32(q-32)*87/8
		default:
			return 97
		}
	})
	d := &batteryDay{f: f, sunOK: true, rise: 7 * 60, set: 17 * 60, fullQ: 40}
	ev := batteryFullEvidence(d)
	if len(ev.Quarters) != dayQuarters || len(ev.SollQ) != dayQuarters {
		t.Fatalf("ist %d, soll %d quarters, want %d each", len(ev.Quarters), len(ev.SollQ), dayQuarters)
	}
	if ev.SollQ[48] != 97 {
		t.Errorf("Soll at noon = %v, want full 97", ev.SollQ[48])
	}
	if !(ev.SollQ[44] > 10 && ev.SollQ[44] < ev.Quarters[44]) {
		t.Errorf("Soll at 11:00 = %v, want between 10 and the Ist %v", ev.SollQ[44], ev.Quarters[44])
	}
	if ev.SollQ[0] != ev.Quarters[0] || ev.SollQ[60] != ev.Quarters[60] {
		t.Errorf("Soll must follow the Ist before the charge and after noon")
	}
}

func TestBatteryEmptyEvidenceKeepsTheFloorUntilSunrise(t *testing.T) {
	f := batteryTestDay(func(q int) float32 {
		if q < 32 {
			return 6
		}
		return 6 + float32(q-32)*2
	})
	d := &batteryDay{f: f, sunOK: true, rise: 7*60 + 30, set: 17 * 60, hasThr: true, thr: 9}
	ev := batteryEmptyEvidence(d)
	floor := float32(9 + batteryEmptyMargin)
	if ev.SollQ[10] != floor || ev.SollQ[31] != floor {
		t.Errorf("Soll before sunrise = %v/%v, want the floor %v", ev.SollQ[10], ev.SollQ[31], floor)
	}
	if ev.SollQ[50] != ev.Quarters[50] {
		t.Errorf("Soll after the charge rose = %v, want the Ist %v", ev.SollQ[50], ev.Quarters[50])
	}
}

// btFullAt1120 is a battery that is full from 11:20 on, the quarters before stay at 40 %.
func btFullAt1120(i, q int) (float32, bool) {
	if float64(q)*baseQuarterMin >= 11*60+20 {
		return 98, true
	}
	return 40, true
}

// btAfterFull sets the export after 11:15 and the heater power on every day.
func btAfterFull(in *Input, exportW, heaterW float32) {
	for i := range in.Days {
		f := &in.Days[i]
		for q := 0; q < dayQuarters; q++ {
			if float64(q)*baseQuarterMin >= 11*60+15 {
				f.Site.GridExport[q] = exportW
			}
			f.Devices["plug.heater"].AvgW[q] = heaterW
		}
	}
}

// TestBatteryFullOffersHeaterThatFitsTheExport: 3 kW export after "voll", a
// 2 kW heater is offered with the after_battery_full template and the role
// minimum run time of a device without role (300 s).
func TestBatteryFullOffersHeaterThatFitsTheExport(t *testing.T) {
	in := btInput(14, btNovStart, btFullAt1120, 1000, nil)
	btAfterFull(in, 3000, 2000)
	findings, _ := btSplit(in)
	f, ok := btFindingByID(findings, "battery:full")
	if !ok {
		t.Fatalf("want a battery:full finding, got %d findings", len(findings))
	}
	if len(f.Apply) != 1 {
		t.Fatalf("Apply = %+v, want one offer for the heater", f.Apply)
	}
	fix := f.Apply[0]
	if fix.Code != "after_battery_full" || fix.Template != "after_battery_full" {
		t.Errorf("Apply code %q template %q, want after_battery_full", fix.Code, fix.Template)
	}
	if fix.Prefill["device"] != "plug.heater" || fix.Prefill["min_run_s"] != float64(300) {
		t.Errorf("Prefill = %+v, want plug.heater with min_run_s 300", fix.Prefill)
	}
	if f.Benefit == nil || f.Benefit.Unit != "kwh_week" || f.Benefit.Value <= 0 {
		t.Errorf("Benefit = %+v, want kwh_week above zero from the replay", f.Benefit)
	}
}

// TestBatteryFullNoOfferForLoadAboveExport: a 4 kW heater does not fit 3 kW export.
func TestBatteryFullNoOfferForLoadAboveExport(t *testing.T) {
	in := btInput(14, btNovStart, btFullAt1120, 1000, nil)
	btAfterFull(in, 3000, 4000)
	findings, _ := btSplit(in)
	f, ok := btFindingByID(findings, "battery:full")
	if !ok {
		t.Fatalf("want a battery:full finding, got %d findings", len(findings))
	}
	if len(f.Apply) != 0 {
		t.Errorf("Apply = %+v, want none for a 4 kW load and 3 kW export", f.Apply)
	}
}

// TestBatteryReplayRejectsMoreGridImport: a replay that adds more grid energy
// than the export it saves takes the offer away, a small extra import does not.
func TestBatteryReplayRejectsMoreGridImport(t *testing.T) {
	more := ReplayVerdict{Base: ReplayResult{GridWh: 100, ExportWh: 1000}, Alt: ReplayResult{GridWh: 500, ExportWh: 900}}
	if !replayAddsGrid(more) {
		t.Errorf("extra 400 Wh grid over 100 Wh export saved: want the offer dropped")
	}
	small := ReplayVerdict{Base: ReplayResult{GridWh: 0, ExportWh: 500000}, Alt: ReplayResult{GridWh: 1700, ExportWh: 185000}}
	if replayAddsGrid(small) {
		t.Errorf("1.7 kWh grid over 315 kWh export saved: want the offer kept")
	}
}
