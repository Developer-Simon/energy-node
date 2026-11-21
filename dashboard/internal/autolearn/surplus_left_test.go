package autolearn

import (
	"context"
	"testing"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/learnrec"
	"github.com/Developer-Simon/energy-node-dashboard/internal/sun"
)

// surplusModel is a roof for the surplus tests, without a raster.
func surplusModel() PVModel {
	return PVModel{Surfaces: []Surface{{PMax: 6000, TiltDeg: 30, AzimuthDeg: 180}}, CapW: 6000, Safe: true}
}

// surplusDay builds a day with the weather factor k on every quarter and the
// measured PV equal to the clear sky curve times k.
func surplusDay(t *testing.T, day string, m PVModel, k float32, site *sun.Location, loc *time.Location) DayFile {
	t.Helper()
	midnight, err := time.ParseInLocation(dayLayout, day, loc)
	if err != nil {
		t.Fatal(err)
	}
	f := DayFile{V: 1, Day: day}
	for q := 0; q < dayQuarters; q++ {
		f.Site.Seen[q] = 1
		v := k
		f.Site.K[q] = &v
		az, el := sun.Position(shadeQuarterTime(midnight, q), site.Lat, site.Lon)
		f.Site.PV[q] = float32(m.Best(az, el, nil) * float64(k))
	}
	return f
}

// surplusDays returns count days from first, one per day, with the factor k.
func surplusDays(t *testing.T, first time.Time, count int, m PVModel, k float32, site *sun.Location, loc *time.Location) []DayFile {
	t.Helper()
	var out []DayFile
	for i := 0; i < count; i++ {
		out = append(out, surplusDay(t, first.AddDate(0, 0, i).Format(dayLayout), m, k, site, loc))
	}
	return out
}

// surplusInput is the input for a forecast of today at noon in Berlin.
func surplusInput(t *testing.T, days []DayFile, m PVModel, withModel bool) (*Input, time.Time) {
	t.Helper()
	loc := berlinLoc(t)
	now := time.Date(2026, 11, 21, 12, 0, 0, 0, loc)
	in := &Input{
		Now:    now,
		Loc:    loc,
		Days:   days,
		Doc:    Doc{Location: shadeSite},
		Limits: learnrec.Limits{SimilarDays: 7, SimilarPoolDays: 0},
	}
	if withModel {
		in.PV = func() (PVModel, *Raster, bool) { return m, nil, true }
	}
	return in, now
}

// surplusExpected is the clear sky energy of today from now to midnight in Wh,
// scaled by k. It is the value a forecast of sunny days should reach.
func surplusExpected(t *testing.T, m PVModel, k float64, now time.Time, site *sun.Location) float64 {
	t.Helper()
	loc := now.Location()
	midnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	q0 := (now.Hour()*60 + now.Minute()) / shadeQuarterMin
	sum := 0.0
	for q := q0; q < dayQuarters; q++ {
		az, el := sun.Position(shadeQuarterTime(midnight, q), site.Lat, site.Lon)
		sum += m.Best(az, el, nil) * k * shadeQuarterMin / 60
	}
	return sum
}

func TestSurplusLeftSimilarSunnyDays(t *testing.T) {
	loc := berlinLoc(t)
	m := surplusModel()
	sunny := surplusDays(t, time.Date(2026, 11, 1, 0, 0, 0, 0, loc), 10, m, 0.9, shadeSite, loc)
	cloudy := surplusDays(t, time.Date(2026, 11, 11, 0, 0, 0, 0, loc), 10, m, 0.3, shadeSite, loc)
	today := surplusDay(t, "2026-11-21", m, 0.9, shadeSite, loc)
	in, now := surplusInput(t, append(append(sunny, cloudy...), today), m, true)

	wh, est, ok := SurplusLeft(in, now)
	if !ok {
		t.Fatal("SurplusLeft ok = false, want a forecast")
	}
	if est {
		t.Errorf("est = true with 7 similar days found, want false")
	}
	want := surplusExpected(t, m, 0.9, now, shadeSite)
	if wh < 0.85*want || wh > 1.15*want {
		t.Errorf("wh = %.0f, want within 15%% of the sunny rest %.0f", wh, want)
	}
	cloudyWh := surplusExpected(t, m, 0.3, now, shadeSite)
	if wh < 2*cloudyWh {
		t.Errorf("wh = %.0f, want clearly above the cloudy rest %.0f", wh, cloudyWh)
	}
}

func TestSurplusLeftWithoutModel(t *testing.T) {
	loc := berlinLoc(t)
	m := surplusModel()
	days := surplusDays(t, time.Date(2026, 11, 1, 0, 0, 0, 0, loc), 10, m, 0.9, shadeSite, loc)
	days = append(days, surplusDay(t, "2026-11-21", m, 0.9, shadeSite, loc))
	in, now := surplusInput(t, days, m, false)
	if _, _, ok := SurplusLeft(in, now); ok {
		t.Error("ok = true without a model, want false")
	}
	det := LearnSurplusDetector.Run(context.Background(), in, "")
	if len(det) != 0 {
		t.Errorf("detector returned %d candidates without a model, want 0", len(det))
	}
}

func TestSurplusLeftFewerThanThreeDays(t *testing.T) {
	loc := berlinLoc(t)
	m := surplusModel()
	days := surplusDays(t, time.Date(2026, 11, 1, 0, 0, 0, 0, loc), 2, m, 0.9, shadeSite, loc)
	days = append(days, surplusDay(t, "2026-11-21", m, 0.9, shadeSite, loc))
	in, now := surplusInput(t, days, m, true)
	if _, _, ok := SurplusLeft(in, now); ok {
		t.Error("ok = true with 2 similar days, want false")
	}
}

func TestSurplusLeftNoTodayFile(t *testing.T) {
	loc := berlinLoc(t)
	m := surplusModel()
	days := surplusDays(t, time.Date(2026, 11, 1, 0, 0, 0, 0, loc), 10, m, 0.9, shadeSite, loc)
	in, now := surplusInput(t, days, m, true)
	if _, _, ok := SurplusLeft(in, now); ok {
		t.Error("ok = true without today's day file, want false")
	}
}

func TestLearnSurplusDetectorPublishesRounded(t *testing.T) {
	loc := berlinLoc(t)
	m := surplusModel()
	sunny := surplusDays(t, time.Date(2026, 11, 1, 0, 0, 0, 0, loc), 10, m, 0.9, shadeSite, loc)
	today := surplusDay(t, "2026-11-21", m, 0.9, shadeSite, loc)
	in, _ := surplusInput(t, append(sunny, today), m, true)

	cands := LearnSurplusDetector.Run(context.Background(), in, "")
	if len(cands) != 1 || cands[0].Value == nil {
		t.Fatalf("candidates = %+v, want one learned value", cands)
	}
	v := cands[0].Value
	if v.Key != "site/surplus_left_wh" || v.Unit != "Wh" || v.Group != "energy" || v.Kind != ValueNumber {
		t.Errorf("value = %+v, want site/surplus_left_wh in Wh", v)
	}
	if int(v.Num)%100 != 0 {
		t.Errorf("Num = %v, want a multiple of 100 Wh", v.Num)
	}
	if v.N != 7 {
		t.Errorf("N = %d, want 7 similar days", v.N)
	}
}
