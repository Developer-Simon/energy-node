package autolearn

import (
	"math"
	"testing"
	"time"
)

// baseTestDays erzeugt acht Wochen ab Montag, 5. Januar 2026. Werktage haben
// Licht um 06:30 an und um 22:00 aus, dazu Kaffee um 06:45. Samstag und Sonntag
// haben Licht um 17:00 an und um 23:00 aus, dazu Fernseher um 10:00 bis 12:00.
// Alle Tage schalten den Fernseher zusätzlich um 18:00 bis 19:00.
// Die Zeiten sind ohne Streuung, damit die Tage gleich aussehen. Das Profil
// braucht mehr Schaltungen als eine Grundlast, damit die Unruhe eines
// stillen Tages sich von den Werktagen abhebt.
func baseTestDays(t *testing.T, loc *time.Location) ([]DayFile, time.Time) {
	t.Helper()
	start := time.Date(2026, 1, 5, 0, 0, 0, 0, loc)
	workdays := []time.Weekday{time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday}
	weekend := []time.Weekday{time.Saturday, time.Sunday}
	opts := synthOpts{
		Seed:    11,
		Devices: []string{"light.kitchen", "plug.coffee", "plug.tv"},
		Habits: []synthHabit{
			{Device: "light.kitchen", On: true, Weekdays: workdays, Minute: 390, Hit: 1},
			{Device: "light.kitchen", On: false, Weekdays: workdays, Minute: 1320, Hit: 1},
			{Device: "light.kitchen", On: true, Weekdays: weekend, Minute: 1020, Hit: 1},
			{Device: "light.kitchen", On: false, Weekdays: weekend, Minute: 1380, Hit: 1},
			{Device: "plug.coffee", On: true, Weekdays: workdays, Minute: 405, Hit: 1},
			{Device: "plug.coffee", On: false, Weekdays: workdays, Minute: 420, Hit: 1},
			{Device: "plug.tv", On: true, Minute: 1080, Hit: 1},
			{Device: "plug.tv", On: false, Minute: 1140, Hit: 1},
			{Device: "plug.tv", On: true, Weekdays: weekend, Minute: 600, Hit: 1},
			{Device: "plug.tv", On: false, Weekdays: weekend, Minute: 720, Hit: 1},
		},
	}
	return synthDays(start, 56, opts), start
}

// baseWeekday liefert den Wochentag eines Datums YYYY-MM-DD.
func baseWeekday(t *testing.T, day string, loc *time.Location) time.Weekday {
	t.Helper()
	d, err := time.ParseInLocation(dayLayout, day, loc)
	if err != nil {
		t.Fatalf("parse %q: %v", day, err)
	}
	return d.Weekday()
}

func TestBaseWorkdaysAndWeekendAreTwoGroups(t *testing.T) {
	loc := berlinLoc(t)
	days, _ := baseTestDays(t, loc)
	base := BuildBase(days, loc)
	if len(base.Groups) != 2 {
		t.Fatalf("groups = %v, want two", base.Groups)
	}
	work := base.DayType["2026-01-05"]    // Montag
	weekend := base.DayType["2026-01-10"] // Samstag
	if work == weekend {
		t.Fatalf("workdays and weekend share group %d", work)
	}
	for _, d := range days {
		wd := baseWeekday(t, d.Day, loc)
		want := work
		if wd == time.Saturday || wd == time.Sunday {
			want = weekend
		}
		if got, ok := base.DayType[d.Day]; !ok || got != want {
			t.Errorf("%s (%s): group %d (ok %v), want %d", d.Day, wd, got, ok, want)
		}
	}
	members := map[time.Weekday]bool{}
	for _, wd := range base.Groups[work] {
		members[wd] = true
	}
	for _, wd := range []time.Weekday{time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday} {
		if !members[wd] {
			t.Errorf("workday group %v misses %s", base.Groups[work], wd)
		}
	}
	if len(base.Groups[work]) != 5 || len(base.Groups[weekend]) != 2 {
		t.Errorf("groups = %v, want 5 workdays and 2 weekend days", base.Groups)
	}
}

func TestBaseSundayProfileOnTuesdayJoinsSundays(t *testing.T) {
	loc := berlinLoc(t)
	days, _ := baseTestDays(t, loc)
	// Dienstag 20. Januar bekommt das Profil des Sonntags 18. Januar.
	if baseWeekday(t, days[15].Day, loc) != time.Tuesday || baseWeekday(t, days[13].Day, loc) != time.Sunday {
		t.Fatal("test days are not Tuesday 20 and Sunday 18 January")
	}
	days[15].Site = days[13].Site
	base := BuildBase(days, loc)
	sunday := base.DayType["2026-01-18"]
	if got := base.DayType["2026-01-20"]; got != sunday {
		t.Errorf("Tuesday with Sunday profile in group %d, want Sunday group %d", got, sunday)
	}
	if monday := base.DayType["2026-01-19"]; monday == sunday {
		t.Errorf("Monday shares the Sunday group %d", sunday)
	}
}

func TestBaseNoiseIsOneGroup(t *testing.T) {
	loc := berlinLoc(t)
	start := time.Date(2026, 1, 5, 0, 0, 0, 0, loc)
	days := synthDays(start, 56, synthOpts{
		Seed:    5,
		Devices: []string{"light.kitchen", "plug.coffee"},
		Noise:   0.5,
	})
	base := BuildBase(days, loc)
	if len(base.Groups) != 1 {
		t.Fatalf("groups = %v, want one", base.Groups)
	}
	for _, d := range days {
		if got := base.DayType[d.Day]; got != 0 {
			t.Errorf("%s in group %d, want 0", d.Day, got)
		}
	}
}

func TestBaseFourQuietDaysAreAway(t *testing.T) {
	loc := berlinLoc(t)
	days, start := baseTestDays(t, loc)
	// Vier Tage nur mit Grundlast: die Aufzeichnung läuft, aber nichts schaltet.
	quiet := synthDays(start.AddDate(0, 0, 56), 4, synthOpts{Seed: 9, Devices: []string{"light.kitchen"}})
	days = append(days, quiet...)
	base := BuildBase(days, loc)
	for _, d := range quiet {
		if !base.Away[d.Day] {
			t.Errorf("%s not away, want away", d.Day)
		}
	}
	if len(base.Away) != len(quiet) {
		t.Errorf("away days = %v, want exactly the %d quiet days", base.Away, len(quiet))
	}
}

func TestBaseDayStartAndNightRest(t *testing.T) {
	loc := berlinLoc(t)
	days, _ := baseTestDays(t, loc)
	base := BuildBase(days, loc)
	g := base.DayType["2026-01-06"] // Dienstag
	if got := base.DayStart[g]; math.Abs(got-390) > 15 {
		t.Errorf("day start = %.1f, want 390 +- 15", got)
	}
	if got := base.NightRest[g]; math.Abs(got-1320) > 15 {
		t.Errorf("night rest = %.1f, want 1320 +- 15", got)
	}
}

func TestBaseQuietDaysWithoutManualSwitching(t *testing.T) {
	loc := berlinLoc(t)
	start := time.Date(2026, 1, 5, 0, 0, 0, 0, loc)
	// Alles schaltet eine Regel: die Handgriffe sind an jedem Tag 0, die
	// Streuung der Handgriffe also auch.
	rules := []synthRule{
		{RuleID: "r1", Device: "light.kitchen", On: true, Minute: 390, Hit: 1},
		{RuleID: "r2", Device: "light.kitchen", On: false, Minute: 1320, Hit: 1},
		{RuleID: "r3", Device: "plug.coffee", On: true, Minute: 405, Hit: 1},
		{RuleID: "r4", Device: "plug.coffee", On: false, Minute: 420, Hit: 1},
		{RuleID: "r5", Device: "plug.tv", On: true, Minute: 1080, Hit: 1},
		{RuleID: "r6", Device: "plug.tv", On: false, Minute: 1140, Hit: 1},
	}
	devices := []string{"light.kitchen", "plug.coffee", "plug.tv"}
	days := synthDays(start, 56, synthOpts{Seed: 11, Devices: devices, Rules: rules})
	quiet := synthDays(start.AddDate(0, 0, 56), 4, synthOpts{Seed: 9, Devices: devices})
	days = append(days, quiet...)
	base := BuildBase(days, loc)
	for _, d := range quiet {
		if !base.Away[d.Day] {
			t.Errorf("%s not away, want away (no hand actions in this household)", d.Day)
		}
	}
	if len(base.Away) != len(quiet) {
		t.Errorf("away days = %v, want exactly the %d quiet days", base.Away, len(quiet))
	}
}
