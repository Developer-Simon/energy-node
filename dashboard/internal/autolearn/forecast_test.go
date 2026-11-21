package autolearn

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/sun"
)

// closeForecastDays closes count finished days with the given forecast, actual
// rest and season median, starting at first.
func closeForecastDays(c *ForecastCheck, first time.Time, count int, forecast, actual, median float64) {
	for i := 0; i < count; i++ {
		day := first.AddDate(0, 0, i).Format(dayLayout)
		c.Record(day, forecast)
		c.Close(day, actual, median)
	}
}

func TestForecastCheckFewerThanFourteenDaysKeepsForecast(t *testing.T) {
	c := LoadForecastCheck("", nil)
	closeForecastDays(c, time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC), 13, 100, 1000, 900)
	if c.UseSeasonMedian() {
		t.Error("UseSeasonMedian = true with 13 days, want false")
	}
}

func TestForecastCheckWorseThanSeasonMedianUsesMedian(t *testing.T) {
	c := LoadForecastCheck("", nil)
	// The forecast is 500 Wh off, the season median only 100 Wh off.
	closeForecastDays(c, time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC), 14, 1500, 1000, 900)
	if !c.UseSeasonMedian() {
		t.Error("UseSeasonMedian = false with a worse forecast over 14 days, want true")
	}
}

func TestForecastCheckBetterThanSeasonMedianKeepsForecast(t *testing.T) {
	c := LoadForecastCheck("", nil)
	closeForecastDays(c, time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC), 14, 1020, 1000, 700)
	if c.UseSeasonMedian() {
		t.Error("UseSeasonMedian = true with a better forecast, want false")
	}
}

func TestForecastCheckKeepsLastFourteenDays(t *testing.T) {
	c := LoadForecastCheck("", nil)
	first := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	// Ten bad days first, then fourteen good ones: only the good ones count.
	closeForecastDays(c, first, 10, 1500, 1000, 900)
	closeForecastDays(c, first.AddDate(0, 0, 10), 14, 1000, 1000, 900)
	if c.UseSeasonMedian() {
		t.Error("UseSeasonMedian = true, the old bad days should be dropped")
	}
	if n := len(c.days); n != forecastDays {
		t.Errorf("kept %d days, want %d", n, forecastDays)
	}
}

func TestForecastCheckRecordIsFirstWinsAndCloseNeedsForecast(t *testing.T) {
	c := LoadForecastCheck("", nil)
	c.Record("2026-11-02", 300)
	c.Record("2026-11-02", 999)
	c.Close("2026-11-03", 100, 100) // no forecast for this day
	c.Close("2026-11-02", 250, 200)
	if !c.Recorded("2026-11-02") || c.Recorded("2026-11-03") {
		t.Error("Recorded wrong after close")
	}
	if len(c.Pending()) != 0 {
		t.Errorf("pending = %v, want none", c.Pending())
	}
	if len(c.days) != 1 || c.days[0].Forecast != 300 {
		t.Errorf("days = %+v, want the first forecast 300", c.days)
	}
}

func TestForecastCheckNaNMedianDropsDay(t *testing.T) {
	c := LoadForecastCheck("", nil)
	c.Record("2026-11-02", 300)
	c.Close("2026-11-02", 250, math.NaN())
	if len(c.Pending()) != 0 || len(c.days) != 0 {
		t.Errorf("pending %v days %+v, want both empty", c.Pending(), c.days)
	}
}

func TestForecastCheckSaveAndLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "forecast.json")
	c := LoadForecastCheck(path, nil)
	first := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	closeForecastDays(c, first, 14, 1500, 1000, 900)
	c.Record("2026-11-15", 420)
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	back := LoadForecastCheck(path, nil)
	if !back.UseSeasonMedian() {
		t.Error("reloaded UseSeasonMedian = false, want true")
	}
	if p := back.Pending(); len(p) != 1 || p[0] != "2026-11-15" {
		t.Errorf("reloaded pending = %v, want [2026-11-15]", p)
	}
	if n := len(back.days); n != 14 {
		t.Errorf("reloaded %d days, want 14", n)
	}
}

func TestForecastCheckSaveWritesOnlyWhenChanged(t *testing.T) {
	path := filepath.Join(t.TempDir(), "forecast.json")
	c := LoadForecastCheck(path, nil)
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err == nil {
		t.Error("Save wrote a file without a change")
	}
	c.Record("2026-11-02", 300)
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("Save did not write after a change: %v", err)
	}
}

// forecastInput is a sunny history of days before today with a flat house load
// and the roof model, the day before today is the one the check closes.
func forecastInput(t *testing.T, days []DayFile, m PVModel, now time.Time) *Input {
	t.Helper()
	in, _ := surplusInput(t, days, m, true)
	in.Now = now
	return in
}

// forecastRun runs the surplus detector with check and returns the published
// value, nil when none.
func forecastRun(t *testing.T, check *ForecastCheck, in *Input) *Value {
	t.Helper()
	cands := NewLearnSurplusDetector(check).Run(context.Background(), in, "")
	if len(cands) == 0 {
		return nil
	}
	if len(cands) != 1 || cands[0].Value == nil {
		t.Fatalf("candidates = %+v, want one value", cands)
	}
	return cands[0].Value
}

// forecastHistory is 14 days before 2026-11-21 that are all sunny, so the
// forecast and the season median agree closely on each of them.
func forecastHistory(t *testing.T, m PVModel) []DayFile {
	t.Helper()
	loc := berlinLoc(t)
	return surplusDays(t, time.Date(2026, 11, 1, 0, 0, 0, 0, loc), 14, m, 0.9, shadeSite, loc)
}

// closeWithBadForecast makes the check see fourteen finished days on which the
// forecast of 10:00 was far off the measured rest, so the season median wins.
func closeWithBadForecast(t *testing.T, check *ForecastCheck, days []DayFile) {
	t.Helper()
	for i := range days {
		check.Record(days[i].Day, 1)
		check.Close(days[i].Day, 3000, 2900)
	}
}

func TestLearnSurplusDetectorPublishesEstimateWhenSeasonMedianWins(t *testing.T) {
	loc := berlinLoc(t)
	m := surplusModel()
	days := forecastHistory(t, m)
	today := surplusDay(t, "2026-11-21", m, 0.9, shadeSite, loc)
	in := forecastInput(t, append(days, today), m, time.Date(2026, 11, 21, 12, 0, 0, 0, loc))

	check := LoadForecastCheck("", nil)
	closeWithBadForecast(t, check, days)
	if !check.UseSeasonMedian() {
		t.Fatal("test setup: UseSeasonMedian = false, want true")
	}
	v := forecastRun(t, check, in)
	if v == nil {
		t.Fatal("no value published")
	}
	if !v.Estimate {
		t.Errorf("Estimate = false, want true when the season median is published")
	}
	if v.Num <= 0 {
		t.Errorf("Num = %v, want the season median rest", v.Num)
	}
}

func TestLearnSurplusDetectorPublishesForecastWhenBetter(t *testing.T) {
	loc := berlinLoc(t)
	m := surplusModel()
	days := forecastHistory(t, m)
	today := surplusDay(t, "2026-11-21", m, 0.9, shadeSite, loc)
	in := forecastInput(t, append(days, today), m, time.Date(2026, 11, 21, 12, 0, 0, 0, loc))

	check := LoadForecastCheck("", nil)
	for i := range days {
		check.Record(days[i].Day, 3000)
		check.Close(days[i].Day, 3000, 2000)
	}
	if check.UseSeasonMedian() {
		t.Fatal("test setup: UseSeasonMedian = true, want false")
	}
	v := forecastRun(t, check, in)
	if v == nil || v.Estimate {
		t.Errorf("value = %+v, want the normal forecast without estimate", v)
	}
}

func TestLearnSurplusDetectorFewerThanFourteenDaysIsNormal(t *testing.T) {
	loc := berlinLoc(t)
	m := surplusModel()
	days := forecastHistory(t, m)
	today := surplusDay(t, "2026-11-21", m, 0.9, shadeSite, loc)
	in := forecastInput(t, append(days, today), m, time.Date(2026, 11, 21, 12, 0, 0, 0, loc))

	check := LoadForecastCheck("", nil)
	for i := 0; i < 13; i++ {
		check.Record(days[i].Day, 1)
		check.Close(days[i].Day, 3000, 2900)
	}
	v := forecastRun(t, check, in)
	if v == nil || v.Estimate {
		t.Errorf("value = %+v, want the normal forecast with 13 checked days", v)
	}
}

func TestSurplusSelfCheckRecordsAndClosesDays(t *testing.T) {
	loc := berlinLoc(t)
	m := surplusModel()
	days := forecastHistory(t, m)
	yesterday := surplusDay(t, "2026-11-20", m, 0.9, shadeSite, loc)
	today := surplusDay(t, "2026-11-21", m, 0.9, shadeSite, loc)
	all := append(append(days, yesterday), today)
	in := forecastInput(t, all, m, time.Date(2026, 11, 21, 12, 0, 0, 0, loc))

	check := LoadForecastCheck(filepath.Join(t.TempDir(), "forecast.json"), nil)
	forecastRun(t, check, in)
	if !check.Recorded("2026-11-21") {
		t.Error("today's 10:00 forecast was not recorded")
	}
	if p := check.Pending(); len(p) != 1 || p[0] != "2026-11-21" {
		t.Errorf("pending = %v, want only today", p)
	}

	// The next day closes today: the day file of today is the finished one.
	nextDay := surplusDay(t, "2026-11-22", m, 0.9, shadeSite, loc)
	in2 := forecastInput(t, append(all, nextDay), m, time.Date(2026, 11, 22, 12, 0, 0, 0, loc))
	forecastRun(t, check, in2)
	if len(check.days) != 1 || check.days[0].Day != "2026-11-21" {
		t.Fatalf("closed days = %+v, want 2026-11-21", check.days)
	}
	wantActual := 0.0
	dayMidnight := time.Date(2026, 11, 21, 0, 0, 0, 0, loc)
	for q := forecastQuarter; q < dayQuarters; q++ {
		az, el := sun.Position(shadeQuarterTime(dayMidnight, q), shadeSite.Lat, shadeSite.Lon)
		wantActual += math.Max(0, m.Best(az, el, nil)*0.9-surplusLoadW) * shadeQuarterMin / 60
	}
	if math.Abs(check.days[0].Actual-wantActual) > 0.01*wantActual {
		t.Errorf("actual = %.0f, want %.0f from 10:00", check.days[0].Actual, wantActual)
	}
}
