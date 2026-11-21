package autolearn

import (
	"bytes"
	"compress/gzip"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
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

// keepAll haelt die festen Testdaten aus dem Jahr 2026 ueber die Aufbewahrung hinaus.
func keepAll() int { return 100000 }

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
	s := OpenDayStore(t.TempDir(), loc, keepAll, noLog)
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
	s := OpenDayStore(t.TempDir(), loc, keepAll, noLog)
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

	first := OpenDayStore(dir, loc, keepAll, noLog)
	first.ObserveMinute(frameWith(base, lampMinute(true, 120)))
	if err := first.Save(); err != nil {
		t.Fatal(err)
	}

	second := OpenDayStore(dir, loc, keepAll, noLog)
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
	s := OpenDayStore(t.TempDir(), loc, keepAll, noLog)
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
	s := OpenDayStore(t.TempDir(), loc, keepAll, noLog)
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
	s := OpenDayStore(dir, loc, keepAll, noLog)
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
	s := OpenDayStore(dir, loc, keepAll, logf)
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

func TestDayStoreEventsByOrigin(t *testing.T) {
	loc := berlinLoc(t)
	s := OpenDayStore(t.TempDir(), loc, keepAll, noLog)
	base := time.Date(2026, 11, 19, 10, 0, 0, 0, loc)
	events := []SwitchEvent{
		{At: base, Device: "lamp", Entity: "lamp/switch", On: true, Origin: OriginHand, House: 312.4},
		{At: base.Add(time.Minute), Device: "lamp", Entity: "lamp/switch", On: false, Origin: OriginHand},
		{At: base.Add(2 * time.Minute), Device: "lamp", Entity: "lamp/switch", On: true, Origin: OriginRule, RuleID: "r1"},
	}
	actions := []RuleAction{{At: base.Add(2 * time.Minute), RuleID: "r1", Device: "lamp",
		Entity: "lamp/switch", WantOn: boolPtr(true), Effective: true, Reaction: 2100 * time.Millisecond}}
	s.Add(events, actions)
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}

	days := mustDays(t, s, base, base)
	if len(days) != 1 {
		t.Fatalf("days = %d, want 1", len(days))
	}
	d := days[0].Devices["lamp"]
	if d.Hand[40] != 2 || d.Rule[40] != 1 || d.Ext[40] != 0 {
		t.Errorf("hand/rule/ext = %d/%d/%d, want 2/1/0", d.Hand[40], d.Rule[40], d.Ext[40])
	}
	if len(days[0].Events) != 3 {
		t.Fatalf("events = %d, want 3", len(days[0].Events))
	}
	if ev := days[0].Events[2]; ev.Origin != "rule" || ev.Rule != "r1" {
		t.Errorf("rule event = %+v", ev)
	}
	if days[0].Events[0].House != 312 {
		t.Errorf("house = %v, want 312", days[0].Events[0].House)
	}
	if len(days[0].Actions) != 1 {
		t.Fatalf("actions = %d, want 1", len(days[0].Actions))
	}
	a := days[0].Actions[0]
	if a.Want == nil || !*a.Want || !a.OK || a.Reaction != 2.1 || a.Rule != "r1" {
		t.Errorf("action = %+v", a)
	}
}

func TestDayStoreRetention(t *testing.T) {
	loc := berlinLoc(t)
	dir := t.TempDir()
	s := OpenDayStore(dir, loc, func() int { return 56 }, noLog)
	now := time.Now().In(loc)
	today := time.Date(now.Year(), now.Month(), now.Day(), 12, 0, 0, 0, loc)
	s.ObserveMinute(frameWith(today.AddDate(0, 0, -60), lampMinute(false, 10)))
	s.ObserveMinute(frameWith(today.AddDate(0, 0, -50), lampMinute(false, 10)))
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, today.AddDate(0, 0, -60).Format(dayLayout)+".json.gz")); !os.IsNotExist(err) {
		t.Errorf("60-day file must be removed, stat err = %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, today.AddDate(0, 0, -50).Format(dayLayout)+".json.gz")); err != nil {
		t.Errorf("50-day file must stay: %v", err)
	}
}

func TestDayStoreReset(t *testing.T) {
	loc := berlinLoc(t)
	dir := t.TempDir()
	s := OpenDayStore(dir, loc, keepAll, noLog)
	base := time.Date(2026, 11, 19, 10, 0, 0, 0, loc)
	s.ObserveMinute(frameWith(base, lampMinute(true, 50)))
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	if err := s.Reset(); err != nil {
		t.Fatal(err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Errorf("files after reset = %d, want 0", len(entries))
	}
	if u := s.Usage(); u.Bytes != 0 || u.Devices != 0 {
		t.Errorf("usage after reset = %+v", u)
	}
	s.ObserveMinute(frameWith(base, lampMinute(true, 50)))
	days := mustDays(t, s, base, base)
	if len(days) != 1 || days[0].Devices["lamp"].Seen[40] != 1 {
		t.Errorf("collecting after reset: %+v", days)
	}
}

func TestDayStoreUsage(t *testing.T) {
	loc := berlinLoc(t)
	s := OpenDayStore(t.TempDir(), loc, keepAll, noLog)
	now := time.Now().In(loc)
	today := time.Date(now.Year(), now.Month(), now.Day(), 10, 0, 0, 0, loc)
	s.ObserveMinute(frameWith(today, lampMinute(true, 120)))
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	u := s.Usage()
	midnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	if u.Bytes <= 0 || u.Devices != 1 || u.Since != midnight.Unix() {
		t.Errorf("usage = %+v, since want %d", u, midnight.Unix())
	}
	if u.WrittenPerDay <= 0 {
		t.Errorf("written per day = %d, want > 0", u.WrittenPerDay)
	}
}

func TestDayStoreMeanSurvivesRestart(t *testing.T) {
	loc := berlinLoc(t)
	dir := t.TempDir()
	base := time.Date(2026, 11, 19, 10, 0, 0, 0, loc)
	first := OpenDayStore(dir, loc, keepAll, noLog)
	for i := 0; i < 3; i++ {
		first.ObserveMinute(frameWith(base.Add(time.Duration(i)*time.Minute), lampMinute(true, 100)))
	}
	for i := 3; i < 7; i++ {
		first.ObserveMinute(frameWith(base.Add(time.Duration(i)*time.Minute),
			learnrec.DeviceMinute{ID: "lamp", Switch: map[string]bool{"switch": true}, Reachable: true}))
	}
	if err := first.Save(); err != nil {
		t.Fatal(err)
	}

	second := OpenDayStore(dir, loc, keepAll, noLog)
	second.ObserveMinute(frameWith(base.Add(7*time.Minute), lampMinute(true, 200)))
	days := mustDays(t, second, base, base)
	if len(days) != 1 {
		t.Fatalf("days = %d, want 1", len(days))
	}
	if got := days[0].Devices["lamp"].AvgW[40]; got != 125 {
		t.Errorf("avg_w = %v, want 125 (3 power minutes at 100 W and one at 200 W)", got)
	}
}

func TestDayStorePendingSurvivesWriteError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	loc := berlinLoc(t)
	dir := t.TempDir()
	s := OpenDayStore(dir, loc, keepAll, noLog)
	s.ObserveMinute(frameWith(time.Date(2026, 11, 19, 23, 59, 0, 0, loc), lampMinute(true, 60)))

	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
	s.ObserveMinute(frameWith(time.Date(2026, 11, 20, 0, 0, 0, 0, loc), lampMinute(true, 60)))
	if err := s.Save(); err == nil {
		t.Fatal("save into a read-only dir must fail")
	}

	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := s.Save(); err != nil {
		t.Fatalf("save after the dir is writable again: %v", err)
	}
	nov19 := time.Date(2026, 11, 19, 0, 0, 0, 0, loc)
	days := mustDays(t, s, nov19, nov19)
	if len(days) != 1 || days[0].Devices["lamp"] == nil || days[0].Devices["lamp"].Seen[95] != 1 {
		t.Errorf("previous day must be written after the retry, got %+v", days)
	}
}

func TestDayStoreUnreadableFileIsNotOverwritten(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads files regardless of permissions")
	}
	loc := berlinLoc(t)
	dir := t.TempDir()
	base := time.Date(2026, 11, 19, 10, 0, 0, 0, loc)
	first := OpenDayStore(dir, loc, keepAll, noLog)
	first.ObserveMinute(frameWith(base, lampMinute(true, 50)))
	if err := first.Save(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "2026-11-19.json.gz")
	orig, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o644) })

	var logs int
	s := OpenDayStore(dir, loc, keepAll, func(string, ...any) { logs++ })
	s.ObserveMinute(frameWith(base.Add(time.Minute), lampMinute(true, 50)))
	if logs == 0 {
		t.Error("unreadable file must be logged")
	}
	if err := s.Save(); err == nil {
		t.Error("save while unreadable must return an error")
	}

	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	now, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(now) != string(orig) {
		t.Error("unreadable file was overwritten")
	}
}

func outdoorMinute(at time.Time, c float64) learnrec.Frame {
	f := frameWith(at, lampMinute(true, 100))
	f.OutdoorC = &c
	return f
}

// readRawDay liefert den entpackten JSON-Text der Tagesdatei von day.
func readRawDay(t *testing.T, dir, day string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dir, day+".json.gz"))
	if err != nil {
		t.Fatal(err)
	}
	zr, err := gzip.NewReader(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(zr)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestDayStoreTempQuarterMean(t *testing.T) {
	loc := berlinLoc(t)
	s := OpenDayStore(t.TempDir(), loc, keepAll, noLog)
	base := time.Date(2026, 11, 19, 10, 0, 0, 0, loc)
	s.ObserveMinute(outdoorMinute(base, 21))
	s.ObserveMinute(outdoorMinute(base.Add(time.Minute), 22))
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	days := mustDays(t, s, base, base)
	if len(days) != 1 || days[0].Site.TempC[40] == nil {
		t.Fatalf("days = %+v, want TempC at quarter 40", days)
	}
	if got := *days[0].Site.TempC[40]; got != 21.5 {
		t.Errorf("temp_c = %v, want 21.5", got)
	}
}

func TestDayStoreTempOmittedWithoutEntity(t *testing.T) {
	loc := berlinLoc(t)
	dir := t.TempDir()
	s := OpenDayStore(dir, loc, keepAll, noLog)
	base := time.Date(2026, 11, 19, 10, 0, 0, 0, loc)
	s.ObserveMinute(frameWith(base, lampMinute(true, 100)))
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(readRawDay(t, dir, "2026-11-19"), "temp_c") {
		t.Error("temp_c written without any outdoor value")
	}
}

func TestDayStoreOldFileWithoutTempLoads(t *testing.T) {
	loc := berlinLoc(t)
	dir := t.TempDir()
	base := time.Date(2026, 11, 19, 10, 0, 0, 0, loc)
	first := OpenDayStore(dir, loc, keepAll, noLog)
	first.ObserveMinute(frameWith(base, lampMinute(true, 100)))
	if err := first.Save(); err != nil {
		t.Fatal(err)
	}
	second := OpenDayStore(dir, loc, keepAll, noLog)
	second.ObserveMinute(outdoorMinute(base.Add(time.Minute), 20))
	days := mustDays(t, second, base, base)
	if len(days) != 1 || days[0].Devices["lamp"] == nil {
		t.Fatalf("days = %+v, want the old day with its lamp data", days)
	}
	if got := days[0].Site.TempC[40]; got == nil || *got != 20 {
		t.Errorf("temp_c = %v, want 20 from the minute of this run", got)
	}
}

func socMinute(at time.Time, pct float64) learnrec.Frame {
	f := frameWith(at, lampMinute(true, 100))
	f.Balance = map[string]float64{"battery_soc": pct}
	return f
}

func TestDayStoreSocQuarterMean(t *testing.T) {
	loc := berlinLoc(t)
	s := OpenDayStore(t.TempDir(), loc, keepAll, noLog)
	base := time.Date(2026, 11, 19, 10, 0, 0, 0, loc)
	s.ObserveMinute(socMinute(base, 50))
	s.ObserveMinute(socMinute(base.Add(time.Minute), 52))
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	days := mustDays(t, s, base, base)
	if len(days) != 1 || days[0].Site.SoC[40] == nil {
		t.Fatalf("days = %+v, want SoC at quarter 40", days)
	}
	if got := *days[0].Site.SoC[40]; got != 51 {
		t.Errorf("soc = %v, want 51", got)
	}
}

func TestDayStoreSocOmittedWithoutValue(t *testing.T) {
	loc := berlinLoc(t)
	dir := t.TempDir()
	s := OpenDayStore(dir, loc, keepAll, noLog)
	base := time.Date(2026, 11, 19, 10, 0, 0, 0, loc)
	s.ObserveMinute(frameWith(base, lampMinute(true, 100)))
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	if raw := readRawDay(t, dir, "2026-11-19"); strings.Contains(raw, `"soc"`) {
		t.Error("soc written without any battery value")
	}
}

func kMinute(at time.Time, k float64) learnrec.Frame {
	f := frameWith(at, lampMinute(true, 100))
	f.Balance = map[string]float64{"k": k}
	return f
}

func TestDayStoreKQuarterMean(t *testing.T) {
	loc := berlinLoc(t)
	s := OpenDayStore(t.TempDir(), loc, keepAll, noLog)
	base := time.Date(2026, 11, 19, 10, 0, 0, 0, loc)
	s.ObserveMinute(kMinute(base, 0.6))
	s.ObserveMinute(kMinute(base.Add(time.Minute), 0.8))
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	days := mustDays(t, s, base, base)
	if len(days) != 1 || days[0].Site.K[40] == nil {
		t.Fatalf("days = %+v, want k at quarter 40", days)
	}
	if got := *days[0].Site.K[40]; math.Abs(float64(got)-0.7) > 1e-6 {
		t.Errorf("k = %v, want 0.7", got)
	}
}

func TestDayStoreKOmittedWithoutValue(t *testing.T) {
	loc := berlinLoc(t)
	dir := t.TempDir()
	s := OpenDayStore(dir, loc, keepAll, noLog)
	base := time.Date(2026, 11, 19, 10, 0, 0, 0, loc)
	s.ObserveMinute(frameWith(base, lampMinute(true, 100)))
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	if raw := readRawDay(t, dir, "2026-11-19"); strings.Contains(raw, `"k"`) {
		t.Error("k written without any weather value")
	}
}

func TestDayStoreOldFileWithoutKLoads(t *testing.T) {
	loc := berlinLoc(t)
	dir := t.TempDir()
	base := time.Date(2026, 11, 19, 10, 0, 0, 0, loc)
	first := OpenDayStore(dir, loc, keepAll, noLog)
	first.ObserveMinute(frameWith(base, lampMinute(true, 100)))
	if err := first.Save(); err != nil {
		t.Fatal(err)
	}
	second := OpenDayStore(dir, loc, keepAll, noLog)
	second.ObserveMinute(kMinute(base.Add(time.Minute), 0.9))
	days := mustDays(t, second, base, base)
	if len(days) != 1 || days[0].Devices["lamp"] == nil {
		t.Fatalf("days = %+v, want the old day with its lamp data", days)
	}
	if got := days[0].Site.K[40]; got == nil || math.Abs(float64(*got)-0.9) > 1e-6 {
		t.Errorf("k = %v, want 0.9 from the minute of this run", got)
	}
}

// writeRawDay schreibt rohes JSON als gzip-Tagesdatei, wie eine alte Version es tat.
func writeRawDay(t *testing.T, dir, day, data string) {
	t.Helper()
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write([]byte(data)); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, day+".json.gz"), buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDayStoreOldFileWithoutCyclesAndSocLoads(t *testing.T) {
	loc := berlinLoc(t)
	dir := t.TempDir()
	writeRawDay(t, dir, "2026-11-19", `{"v":1,"day":"2026-11-19","devices":{},"site":{},"events":[],"actions":[]}`)
	base := time.Date(2026, 11, 19, 10, 0, 0, 0, loc)
	s := OpenDayStore(dir, loc, keepAll, noLog)
	days := mustDays(t, s, base, base)
	if len(days) != 1 {
		t.Fatalf("days = %+v, want the old day", days)
	}
	if len(days[0].Cycles) != 0 || days[0].Site.SoC[40] != nil {
		t.Errorf("cycles = %v, soc = %v, want none", days[0].Cycles, days[0].Site.SoC[40])
	}
}

func TestDayStoreCyclesAndSocRoundTrip(t *testing.T) {
	loc := berlinLoc(t)
	dir := t.TempDir()
	s := OpenDayStore(dir, loc, keepAll, noLog)
	base := time.Date(2026, 11, 19, 10, 0, 0, 0, loc)
	soc := float32(55)
	f := DayFile{V: dayFileVersion, Day: "2026-11-19", Devices: map[string]*DeviceDay{},
		Events: []EventRec{}, Actions: []ActionRec{},
		Cycles: []CycleRec{{Device: "washer", Start: base.Unix(), DurS: 3600, Wh: 1000,
			OpW: 2000, PeakW: 2100, Grid: 0.5, PV: 0.5, Bat: 0, End: "self"}}}
	f.Site.SoC[40] = &soc
	if err := s.writeFile("2026-11-19", f); err != nil {
		t.Fatal(err)
	}
	days := mustDays(t, s, base, base)
	if len(days) != 1 || len(days[0].Cycles) != 1 {
		t.Fatalf("days = %+v, want one day with one cycle", days)
	}
	c := days[0].Cycles[0]
	if c.Device != "washer" || c.DurS != 3600 || c.OpW != 2000 || c.End != "self" {
		t.Errorf("cycle = %+v, want the washer cycle back", c)
	}
	if got := days[0].Site.SoC[40]; got == nil || *got != 55 {
		t.Errorf("soc = %v, want 55", got)
	}
	if days[0].Site.SoC[41] != nil {
		t.Errorf("soc[41] = %v, want nil", days[0].Site.SoC[41])
	}
}

func TestDayStoreCyclesSurviveSaveOfLoadedDay(t *testing.T) {
	loc := berlinLoc(t)
	dir := t.TempDir()
	base := time.Date(2026, 11, 19, 10, 0, 0, 0, loc)
	first := OpenDayStore(dir, loc, keepAll, noLog)
	f := DayFile{V: dayFileVersion, Day: base.Format(dayLayout), Devices: map[string]*DeviceDay{},
		Events: []EventRec{}, Actions: []ActionRec{}, Cycles: []CycleRec{{Device: "dryer", DurS: 90}}}
	if err := first.writeFile(base.Format(dayLayout), f); err != nil {
		t.Fatal(err)
	}
	second := OpenDayStore(dir, loc, keepAll, noLog)
	second.ObserveMinute(frameWith(base, lampMinute(true, 100)))
	if err := second.Save(); err != nil {
		t.Fatal(err)
	}
	days := mustDays(t, second, base, base)
	if len(days) != 1 || len(days[0].Cycles) != 1 || days[0].Cycles[0].Device != "dryer" {
		t.Errorf("days = %+v, want the dryer cycle kept after a save", days)
	}
}

func TestDayStoreAddCyclesRespectsExcludedAndEnabled(t *testing.T) {
	loc := berlinLoc(t)
	s := OpenDayStore(t.TempDir(), loc, keepAll, noLog)
	base := time.Date(2026, 11, 19, 10, 0, 0, 0, loc)
	s.ObserveMinute(frameWith(base, lampMinute(true, 50)))

	s.SetExcluded([]string{"fan"})
	s.AddCycles([]CycleRec{{Device: "lamp", Start: base.Unix(), DurS: 120, End: "rule"}, {Device: "fan", Start: base.Unix()}})
	s.SetEnabled(false)
	s.AddCycles([]CycleRec{{Device: "lamp", Start: base.Unix() + 600}})
	s.SetEnabled(true)

	days := mustDays(t, s, base, base)
	if len(days) != 1 {
		t.Fatalf("days = %d, want 1", len(days))
	}
	if got := len(days[0].Cycles); got != 1 {
		t.Fatalf("cycles = %d, want 1 (excluded and disabled dropped)", got)
	}
	if c := days[0].Cycles[0]; c.Device != "lamp" || c.End != "rule" || c.DurS != 120 {
		t.Errorf("cycle = %+v", c)
	}
}
