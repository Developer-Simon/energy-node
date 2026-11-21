package sun

import (
	"encoding/json"
	"math"
	"os"
	"testing"
	"time"
)

type vector struct {
	Date string  `json:"date"`
	Lat  float64 `json:"lat"`
	Lon  float64 `json:"lon"`
	Rise *string `json:"rise"`
	Set  *string `json:"set"`
}

func loadVectors(t *testing.T) []vector {
	t.Helper()
	data, err := os.ReadFile("testdata/sun_vectors.json")
	if err != nil {
		t.Fatal(err)
	}
	var vectors []vector
	if err := json.Unmarshal(data, &vectors); err != nil {
		t.Fatal(err)
	}
	if len(vectors) != 40 {
		t.Fatalf("want 40 vectors, got %d", len(vectors))
	}
	return vectors
}

func parseDay(t *testing.T, s string) time.Time {
	t.Helper()
	day, err := time.Parse("2006-01-02", s)
	if err != nil {
		t.Fatal(err)
	}
	return day
}

func parseUTC(t *testing.T, s string) time.Time {
	t.Helper()
	moment, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatal(err)
	}
	return moment
}

func TestTimesMatchPythonVectors(t *testing.T) {
	for _, v := range loadVectors(t) {
		day := parseDay(t, v.Date)
		rise, set, ok := Times(day, v.Lat, v.Lon)
		if v.Rise == nil {
			if ok {
				t.Errorf("%s %.2f/%.2f: want polar case (ok false), got rise %v set %v",
					v.Date, v.Lat, v.Lon, rise, set)
			}
			continue
		}
		if !ok {
			t.Errorf("%s %.2f/%.2f: want ok, got polar case", v.Date, v.Lat, v.Lon)
			continue
		}
		wantRise := parseUTC(t, *v.Rise)
		wantSet := parseUTC(t, *v.Set)
		if d := rise.Sub(wantRise).Abs(); d > time.Minute {
			t.Errorf("%s %.2f/%.2f: rise %s, want %s (diff %s)",
				v.Date, v.Lat, v.Lon, rise.UTC().Format(time.RFC3339), *v.Rise, d)
		}
		if d := set.Sub(wantSet).Abs(); d > time.Minute {
			t.Errorf("%s %.2f/%.2f: set %s, want %s (diff %s)",
				v.Date, v.Lat, v.Lon, set.UTC().Format(time.RFC3339), *v.Set, d)
		}
	}
}

func TestPolarNightAt70NorthOnDecember21(t *testing.T) {
	_, _, ok := Times(time.Date(2026, 12, 21, 0, 0, 0, 0, time.UTC), 70, 25)
	if ok {
		t.Fatal("want ok false during polar night at 70 N")
	}
}

// Solar noon for a longitude sits at 12:00 UTC minus lon/15 hours, up to the
// equation of time (about 2 minutes on 21 June).
func solarNoon(lon float64) time.Time {
	base := time.Date(2026, 6, 21, 12, 0, 0, 0, time.UTC)
	return base.Add(-time.Duration(lon / 15 * float64(time.Hour)))
}

func TestPositionAtSolarNoonIn48North(t *testing.T) {
	const lat, lon = 48.0, 11.576
	az, el := Position(solarNoon(lon), lat, lon)
	if math.Abs(el-65.4) > 0.5 {
		t.Errorf("elevation %.2f, want 65.4 +- 0.5", el)
	}
	if math.Abs(az-180) > 2 {
		t.Errorf("azimuth %.2f, want 180 +- 2", az)
	}
}

func TestPositionAtLocalMidnightIsBelowHorizon(t *testing.T) {
	const lat, lon = 48.0, 11.576
	midnight := solarNoon(lon).Add(-12 * time.Hour)
	_, el := Position(midnight, lat, lon)
	if el >= 0 {
		t.Errorf("elevation at local midnight %.2f, want negative", el)
	}
}
