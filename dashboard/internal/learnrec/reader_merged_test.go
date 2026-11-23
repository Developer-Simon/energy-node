package learnrec

import (
	"math"
	"testing"
	"time"
)

// mrNewOwn legt einen eigenen Speicher im Ortszeit-Gebiet der HA-Tests an.
func mrNewOwn(t *testing.T) *MinuteStore {
	t.Helper()
	env := newMsEnv(t, LocRAM)
	return NewMinuteStore(hsLoc,
		func() MinutePlan { return env.plan },
		func() MinuteDirs { return env.dirs },
		func() bool { return env.up },
		func() Limits { return env.limits },
		func(string, ...any) {},
	)
}

// mrInject legt Werte eines HA-Tages direkt in den Zwischenspeicher, wie eine Antwort es tut.
func mrInject(s *HASync, day, key string, vals map[int]float32, five bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.days[day] == nil {
		s.days[day] = map[string]*[minutesPerDay]float32{}
		s.fiveMin[day] = map[string]bool{}
	}
	arr := new([minutesPerDay]float32)
	for i := range arr {
		arr[i] = float32(math.NaN())
	}
	for m, v := range vals {
		arr[m] = v
	}
	s.days[day][key] = arr
	if five {
		s.fiveMin[day][key] = true
	}
}

// mrFill liefert Werte fuer die Minuten [from, to).
func mrFill(from, to int, v float32) map[int]float32 {
	out := map[int]float32{}
	for m := from; m < to; m++ {
		out[m] = v
	}
	return out
}

func TestMergedOwnMinuteBeatsHA(t *testing.T) {
	own := mrNewOwn(t)
	s := NewHASync(hsClock, hsLimits(1<<30), hsLoc)
	at := time.Date(2026, 11, 18, 10, 0, 0, 0, hsLoc)
	own.ObserveMinute(rdFrame(at, 7))
	mrInject(s, "2026-11-18", "e:uid1", map[int]float32{600: 100, 601: 200}, false)

	got := NewMergedReader(own, s).Range("e:uid1", at, at.Add(2*time.Minute))
	if len(got.Values) != 2 || got.Values[0] != 7 || got.Values[1] != 200 {
		t.Fatalf("Werte = %v, erwartet [7 200] (eigene Minute schlaegt HA, HA fuellt die Luecke)", got.Values)
	}
}

func TestMergedFiveMinuteFillsGapButNotCoverage(t *testing.T) {
	own := mrNewOwn(t)
	s := NewHASync(hsClock, hsLimits(1<<30), hsLoc)
	mrInject(s, "2026-11-18", "e:uid1", mrFill(600, 605, 300), true)

	at := time.Date(2026, 11, 18, 10, 0, 0, 0, hsLoc)
	r := NewMergedReader(own, s)
	got := r.Range("e:uid1", at, at.Add(6*time.Minute))
	if got.Values[0] != 300 || got.Values[4] != 300 || !math.IsNaN(float64(got.Values[5])) {
		t.Fatalf("5m-Werte = %v, erwartet 300 fuer fuenf Minuten, dann Luecke", got.Values)
	}
}

func TestMergedDayWithThirtyPercentFiveMinuteIsNotCovered(t *testing.T) {
	own := mrNewOwn(t)
	s := NewHASync(hsClock, hsLimits(1<<30), hsLoc)
	day1 := time.Date(2026, 11, 17, 0, 0, 0, 0, hsLoc)
	for m := 0; m < 1440; m++ {
		own.ObserveMinute(Frame{Minute: day1.Add(time.Duration(m) * time.Minute), Balance: map[string]float64{"load_total": 10}})
	}
	day2 := time.Date(2026, 11, 18, 0, 0, 0, 0, hsLoc)
	for m := 0; m < 1000; m++ {
		own.ObserveMinute(Frame{Minute: day2.Add(time.Duration(m) * time.Minute), Balance: map[string]float64{"load_total": 10}})
	}
	mrInject(s, "2026-11-18", coverageKey, mrFill(1000, 1440, 10), true)

	r := NewMergedReader(own, s)
	if got := r.Coverage(day1, day1.AddDate(0, 0, 2)); got != 1 {
		t.Fatalf("Coverage = %d, erwartet 1 (der 5m-Tag zaehlt nicht)", got)
	}
}

func TestMergedHAFiveMinuteShareAtTwentyPercentStillCounts(t *testing.T) {
	own := mrNewOwn(t)
	s := NewHASync(hsClock, hsLimits(1<<30), hsLoc)
	day := time.Date(2026, 11, 18, 0, 0, 0, 0, hsLoc)
	for m := 0; m < 1152; m++ {
		own.ObserveMinute(Frame{Minute: day.Add(time.Duration(m) * time.Minute), Balance: map[string]float64{"load_total": 10}})
	}
	mrInject(s, "2026-11-18", coverageKey, mrFill(1152, 1440, 10), true)
	if got := NewMergedReader(own, s).Coverage(day, day.AddDate(0, 0, 1)); got != 1 {
		t.Fatalf("Coverage = %d, erwartet 1 (genau 20 %% aus 5m zaehlt noch)", got)
	}
}

func TestMergedKeysUnionOwnAndHA(t *testing.T) {
	own := mrNewOwn(t)
	s := NewHASync(hsClock, hsLimits(1<<30), hsLoc)
	at := time.Date(2026, 11, 18, 10, 0, 0, 0, hsLoc)
	own.ObserveMinute(rdFrame(at, 7))
	mrInject(s, "2026-11-18", "b:pv", map[int]float32{600: 1}, false)

	keys := NewMergedReader(own, s).(keyLister).Keys(at, at.Add(time.Hour))
	if len(keys) != 2 || keys[0] != "b:pv" || keys[1] != "e:uid1" {
		t.Fatalf("Keys = %v, erwartet [b:pv e:uid1]", keys)
	}
}

func TestMergedNilHABehavesLikeOwn(t *testing.T) {
	own := mrNewOwn(t)
	at := time.Date(2026, 11, 18, 10, 0, 0, 0, hsLoc)
	own.ObserveMinute(rdFrame(at, 7))
	r := NewMergedReader(own, nil)
	if r != Reader(own) {
		t.Fatal("ohne HA muss der eigene Speicher unveraendert zurueckkommen")
	}
	if got := r.Range("e:uid1", at, at.Add(time.Minute)); got.Values[0] != 7 {
		t.Fatalf("Range = %v", got.Values)
	}
}
