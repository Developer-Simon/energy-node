package autolearn

import (
	"os"
	"path/filepath"
	"testing"
	"time"
	_ "time/tzdata"

	"github.com/Developer-Simon/energy-node-dashboard/internal/learnrec"
)

func berlinLoc(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Fatal(err)
	}
	return loc
}

func lampMinute(on bool, w float64) learnrec.DeviceMinute {
	return learnrec.DeviceMinute{
		ID:        "lamp",
		Power:     map[string]float64{"power": w},
		Switch:    map[string]bool{"switch": on},
		Reachable: true,
	}
}

func frameWith(at time.Time, devs ...learnrec.DeviceMinute) learnrec.Frame {
	return learnrec.Frame{Minute: at, Devices: devs}
}

func noLog(string, ...any) {}

func mustDays(t *testing.T, s *DayStore, from, to time.Time) []DayFile {
	t.Helper()
	days, err := s.Days(from, to)
	if err != nil {
		t.Fatal(err)
	}
	return days
}

func TestDayStoreQuarterAggregation(t *testing.T) {
	loc := berlinLoc(t)
	s := OpenDayStore(t.TempDir(), loc, nil, noLog)
	base := time.Date(2026, 11, 19, 10, 0, 0, 0, loc)
	for i := 0; i < 15; i++ {
		s.ObserveMinute(frameWith(base.Add(time.Duration(i)*time.Minute), lampMinute(true, 120)))
	}
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	days := mustDays(t, s, base, base)
	if len(days) != 1 {
		t.Fatalf("days = %d, want 1", len(days))
	}
	d := days[0].Devices["lamp"]
	if d == nil {
		t.Fatal("lamp missing")
	}
	const q = 40
	if d.Seen[q] != 15 {
		t.Errorf("seen = %d, want 15", d.Seen[q])
	}
	if d.OnMin[q] != 15 {
		t.Errorf("on_min = %d, want 15", d.OnMin[q])
	}
	if d.AvgW[q] != 120 {
		t.Errorf("avg_w = %v, want 120", d.AvgW[q])
	}
	if d.Wh[q] != 30 {
		t.Errorf("wh = %v, want 30", d.Wh[q])
	}
}

func TestDayStoreAvgIgnoresSwitchOnlyMinutes(t *testing.T) {
	loc := berlinLoc(t)
	s := OpenDayStore(t.TempDir(), loc, nil, noLog)
	base := time.Date(2026, 11, 19, 10, 0, 0, 0, loc)
	for i := 0; i < 5; i++ {
		s.ObserveMinute(frameWith(base.Add(time.Duration(i)*time.Minute), lampMinute(true, 100)))
	}
	for i := 5; i < 15; i++ {
		s.ObserveMinute(frameWith(base.Add(time.Duration(i)*time.Minute),
			learnrec.DeviceMinute{ID: "lamp", Switch: map[string]bool{"switch": true}, Reachable: true}))
	}
	days := mustDays(t, s, base, base)
	d := days[0].Devices["lamp"]
	if d.Seen[40] != 15 || d.OnMin[40] != 15 {
		t.Errorf("seen %d on_min %d, want 15 and 15", d.Seen[40], d.OnMin[40])
	}
	if d.AvgW[40] != 100 {
		t.Errorf("avg_w = %v, want 100 (switch-only minutes must not count)", d.AvgW[40])
	}
}

func TestDayStoreRestartKeepsToday(t *testing.T) {
	loc := berlinLoc(t)
	dir := t.TempDir()
	base := time.Date(2026, 11, 19, 10, 0, 0, 0, loc)

	first := OpenDayStore(dir, loc, nil, noLog)
	first.ObserveMinute(frameWith(base, lampMinute(true, 120)))
	if err := first.Save(); err != nil {
		t.Fatal(err)
	}

	second := OpenDayStore(dir, loc, nil, noLog)
	second.ObserveMinute(frameWith(base.Add(time.Minute), lampMinute(true, 120)))
	days := mustDays(t, second, base, base)
	if len(days) != 1 {
		t.Fatalf("days = %d, want 1", len(days))
	}
	if got := days[0].Devices["lamp"].Seen[40]; got != 2 {
		t.Errorf("seen = %d, want 2", got)
	}
}

func TestDayStoreDSTDays(t *testing.T) {
	loc := berlinLoc(t)
	s := OpenDayStore(t.TempDir(), loc, nil, noLog)
	march := time.Date(2026, 3, 29, 0, 0, 0, 0, loc)
	october := time.Date(2026, 10, 25, 0, 0, 0, 0, loc)
	for _, start := range []time.Time{march, october} {
		day := start.Format(dayLayout)
		for at := start; at.Format(dayLayout) == day; at = at.Add(time.Minute) {
			s.ObserveMinute(frameWith(at, lampMinute(false, 100)))
		}
	}
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	days := mustDays(t, s, march, october)
	if len(days) != 2 {
		t.Fatalf("days = %d, want 2", len(days))
	}

	marchLamp := days[0].Devices["lamp"]
	empty := 0
	for q := 0; q < dayQuarters; q++ {
		if marchLamp.Seen[q] == 0 {
			empty++
		}
	}
	if empty != 4 {
		t.Errorf("march: %d quarters without data, want 4", empty)
	}

	octoberLamp := days[1].Devices["lamp"]
	for q := 0; q < dayQuarters; q++ {
		if octoberLamp.Seen[q] > dayCounterMax {
			t.Errorf("october: seen[%d] = %d, above %d", q, octoberLamp.Seen[q], dayCounterMax)
		}
	}
}

func TestDayStoreExcluded(t *testing.T) {
	loc := berlinLoc(t)
	s := OpenDayStore(t.TempDir(), loc, nil, noLog)
	base := time.Date(2026, 11, 19, 10, 0, 0, 0, loc)
	s.ObserveMinute(frameWith(base, lampMinute(true, 50), learnrec.DeviceMinute{ID: "fan", Reachable: true}))

	s.SetExcluded([]string{"lamp"})
	s.ObserveMinute(frameWith(base.Add(time.Minute), lampMinute(true, 50), learnrec.DeviceMinute{ID: "fan", Reachable: true}))

	days := mustDays(t, s, base, base)
	if len(days) != 1 {
		t.Fatalf("days = %d, want 1", len(days))
	}
	lamp := days[0].Devices["lamp"]
	if lamp == nil || lamp.Seen[40] != 1 {
		t.Errorf("lamp entries before exclusion must stay, got %+v", lamp)
	}
	fan := days[0].Devices["fan"]
	if fan == nil || fan.Seen[40] != 2 {
		t.Errorf("fan seen wrong, got %+v", fan)
	}
}

func TestDayStoreMidnight(t *testing.T) {
	loc := berlinLoc(t)
	dir := t.TempDir()
	s := OpenDayStore(dir, loc, nil, noLog)
	s.ObserveMinute(frameWith(time.Date(2026, 11, 19, 23, 59, 0, 0, loc), lampMinute(false, 10)))
	s.ObserveMinute(frameWith(time.Date(2026, 11, 20, 0, 0, 0, 0, loc), lampMinute(false, 10)))
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"2026-11-19.json.gz", "2026-11-20.json.gz"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
	days := mustDays(t, s, time.Date(2026, 11, 19, 0, 0, 0, 0, loc), time.Date(2026, 11, 19, 0, 0, 0, 0, loc))
	if len(days) != 1 || days[0].Devices["lamp"].Seen[95] != 1 {
		t.Errorf("23:59 must land in quarter 95 of 2026-11-19, got %d days", len(days))
	}
}

func TestDayStoreBrokenFile(t *testing.T) {
	loc := berlinLoc(t)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "2026-11-19.json.gz"), []byte("kaputt"), 0o644); err != nil {
		t.Fatal(err)
	}
	var logs []string
	logf := func(format string, args ...any) {
		logs = append(logs, format)
	}
	s := OpenDayStore(dir, loc, nil, logf)
	base := time.Date(2026, 11, 19, 10, 0, 0, 0, loc)
	s.ObserveMinute(frameWith(base, lampMinute(true, 80)))
	if len(logs) != 1 {
		t.Fatalf("log calls = %d, want 1", len(logs))
	}
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	days := mustDays(t, s, base, base)
	if len(days) != 1 || days[0].Devices["lamp"].Seen[40] != 1 {
		t.Errorf("fresh start expected seen 1, got %+v", days)
	}
}
