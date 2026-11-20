package autolearn

import (
	"fmt"
	"math"
	"math/rand"
	"os"
	"sort"
	"time"
)

// Die Demo-Daten erzeugen vier Gewohnheiten, je eine für die vier Erkenner der
// Gewohnheiten und der Überstimmung. Die Geräte sind die Fake-Geräte der Demo.
const (
	demoSeedDays = 21
	demoSeed     = 20261120

	demoPump   = "demo_pump"
	demoHeater = "demo_heater"
	demoWasher = "demo_washer"
	demoBoiler = "demo_boiler"
	// demoRuleID ist die Regel „Akku leer, WP aus“ der Demo. Die Wärmepumpe
	// (demo_pump) schaltet sie aus, werktags schaltet der Nutzer sie wieder ein.
	demoRuleID = "regel_akku_leer_wp_aus"

	// demoPumpOffMin ist die Abschaltzeit der Wärmepumpe, 22:30 in Minuten.
	demoPumpOffMin = 1350
	// demoRunMinutes ist die übliche Laufzeit der Heizung in Minuten.
	demoRunMinutes = 40
	// demoLongDays sind die Tagesindizes mit vergessener Heizung.
	demoLongDay1 = 6
	demoLongDay2 = 15
	// demoCouplingLag ist die Verzögerung des Boilers hinter der Waschmaschine.
	demoCouplingLag = 40.0 / 60.0
)

// demoWatts ist die Leistung der Fake-Geräte, wenn sie laufen.
var demoWatts = map[string]float64{demoPump: 400, demoHeater: 1800, demoWasher: 500, demoBoiler: 2000}

// SeedDemo schreibt die Tagesdateien der letzten 21 Tage vor now in dir, für die
// Fake-Geräte der Demo. Das Ergebnis ist fest, derselbe Aufruf schreibt dieselben
// Bytes. Der Aufrufer prüft, ob dir leer ist.
func SeedDemo(dir string, now time.Time, loc *time.Location) error {
	if loc == nil {
		loc = time.Local
	}
	store := &DayStore{dir: dir}
	for _, f := range demoDays(now.In(loc), loc) {
		if err := store.writeFile(f.Day, f); err != nil {
			return fmt.Errorf("autolearn: demo seed: %w", err)
		}
	}
	return nil
}

// demoBuilder sammelt die Ereignisse eines Demo-Tages.
type demoBuilder struct {
	f       DayFile
	day     time.Time
	loc     *time.Location
	changes map[string][]demoChange
}

// demoChange ist ein Schaltvorgang eines Geräts, minute ab Mitternacht.
type demoChange struct {
	minute int
	on     bool
}

// demoDays erzeugt die 21 Tage vor dem heutigen Tag. Die Zeiten sind zufällig
// nur innerhalb ihrer Grenzen, der Zufall hat den festen Seed demoSeed.
func demoDays(now time.Time, loc *time.Location) []DayFile {
	rng := rand.New(rand.NewSource(demoSeed))
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	out := make([]DayFile, 0, demoSeedDays)
	for i := 0; i < demoSeedDays; i++ {
		day := today.AddDate(0, 0, i-demoSeedDays)
		b := &demoBuilder{
			f: DayFile{V: dayFileVersion, Day: day.Format(dayLayout), Devices: map[string]*DeviceDay{},
				Events: []EventRec{}, Actions: []ActionRec{}},
			day:     day,
			loc:     loc,
			changes: map[string][]demoChange{},
		}
		for _, id := range []string{demoPump, demoHeater, demoWasher, demoBoiler} {
			b.f.Devices[id] = &DeviceDay{}
		}
		// Ohne Gewohnheit: drei Tage, an denen die Wärmepumpe nicht um 22:30 abschaltet,
		// die Boiler-Kopplung nicht greift.
		skip := i%7 == 3
		weekday := day.Weekday() != time.Saturday && day.Weekday() != time.Sunday

		// Wärmepumpe: die Regel schaltet sie zwischen 10 und 17 Uhr aus. Werktags
		// schaltet der Nutzer nach einer Minute wieder ein (überstimmt), am Wochenende
		// erst nach zweieinhalb Stunden (keine Überstimmung).
		off := demoMinute(rng, 600, 1020)
		b.emit(off, demoPump, false, OriginRule, demoRuleID)
		b.action(off, demoPump, false)
		if weekday {
			b.emit(off+1, demoPump, true, OriginHand, "")
		} else {
			b.emit(off+150+30*rng.Float64(), demoPump, true, OriginHand, "")
		}
		if !skip {
			b.emit(demoPumpOffMin+rng.NormFloat64()*8, demoPump, false, OriginHand, "")
		}

		// Heizung: ein Lauf von knapp 40 Minuten, an zwei Tagen Stunden, weil niemand
		// abschaltet.
		start := demoMinute(rng, 600, 1020)
		run := float64(36 + rng.Intn(9))
		if i == demoLongDay1 || i == demoLongDay2 {
			run = 300 + 60*rng.Float64()
		}
		b.emit(start, demoHeater, true, OriginHand, "")
		b.emit(start+run, demoHeater, false, OriginHand, "")

		// Kopplung: Die Waschmaschine läuft zu einer menschlichen Zeit, der Boiler
		// folgt an den meisten Tagen 40 Sekunden später.
		wash := demoMinute(rng, 600, 1000)
		b.emit(wash, demoWasher, true, OriginHand, "")
		b.emit(wash+90, demoWasher, false, OriginHand, "")
		if !skip {
			b.emit(wash+demoCouplingLag, demoBoiler, true, OriginHand, "")
			b.emit(wash+demoCouplingLag+30, demoBoiler, false, OriginHand, "")
		}

		b.fill(rng)
		out = append(out, b.f)
	}
	return out
}

// demoMinute liefert eine zufällige Minute zwischen lo und hi.
func demoMinute(rng *rand.Rand, lo, hi float64) float64 {
	return lo + rng.Float64()*(hi-lo)
}

// at liefert den Wandzeit-Zeitpunkt minute ab Mitternacht des Tages. Die
// Sekunden laufen über time.Date, so bleibt die Wandzeit an Zeitumstellungen richtig.
func (b *demoBuilder) at(minute float64) time.Time {
	sec := int(math.Round(minute * 60))
	return time.Date(b.day.Year(), b.day.Month(), b.day.Day(), 0, 0, sec, 0, b.loc)
}

// emit schreibt einen Schaltvorgang. Handschaltungen und Regeln zählen in die
// Viertelstunde wie beim Sammeln, Regeln mit ihrer ID.
func (b *demoBuilder) emit(minute float64, device string, on bool, origin Origin, rule string) {
	at := b.at(minute)
	dd := b.f.Devices[device]
	q := (at.Hour()*60 + at.Minute()) / 15
	switch origin {
	case OriginHand:
		bump(&dd.Hand[q])
	case OriginRule:
		bump(&dd.Rule[q])
	}
	b.f.Events = append(b.f.Events, EventRec{At: at.Unix(), Device: device, Entity: "switch." + device,
		On: on, Origin: string(origin), Rule: rule, House: 300, Surplus: 0})
	b.changes[device] = append(b.changes[device], demoChange{minute: at.Hour()*60 + at.Minute(), on: on})
}

// action schreibt die Regelaktion zu einem Schaltvorgang der Regel demoRuleID.
func (b *demoBuilder) action(minute float64, device string, on bool) {
	want := on
	b.f.Actions = append(b.f.Actions, ActionRec{At: b.at(minute).Unix(), Rule: demoRuleID, Device: device,
		Entity: "switch." + device, Want: &want, There: false, OK: true, Reaction: 2.4})
}

// fill setzt Erreichbarkeit, Zähler, Leistung und die Last des Hauses aus den
// Schaltvorgängen des Tages, wie der Sammler es täte.
func (b *demoBuilder) fill(rng *rand.Rand) {
	var load [dayQuarters]float64
	ids := make([]string, 0, len(b.f.Devices))
	for id := range b.f.Devices {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		dd := b.f.Devices[id]
		list := b.changes[id]
		sort.SliceStable(list, func(x, y int) bool { return list[x].minute < list[y].minute })
		state, next := false, 0
		var onMin [dayQuarters]int
		for m := 0; m < 1440; m++ {
			for next < len(list) && list[next].minute <= m {
				state = list[next].on
				next++
			}
			if state {
				onMin[m/15]++
			}
		}
		watts := demoWatts[id]
		for q := 0; q < dayQuarters; q++ {
			dd.Seen[q] = dayCounterMax
			dd.Reach[q] = dayCounterMax
			dd.PN[q] = dayCounterMax
			dd.OnMin[q] = uint8(onMin[q])
			dd.AvgW[q] = float32(watts * float64(onMin[q]) / 15)
			dd.Wh[q] = float32(watts * float64(onMin[q]) / 60)
			load[q] += watts * float64(onMin[q]) / 15
		}
	}
	for q := 0; q < dayQuarters; q++ {
		b.f.Site.Seen[q] = dayCounterMax
		b.f.Site.LoadTotal[q] = float32(300 + load[q] + 10*rng.NormFloat64())
		b.f.Site.GridImport[q] = b.f.Site.LoadTotal[q]
	}
}

// DaysDirEmpty meldet, ob dir keine Tagesdatei enthält oder fehlt. Der Aufrufer
// entscheidet, ob die Demo-Daten geschrieben werden.
func DaysDirEmpty(dir string) (bool, error) {
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	return len(entries) == 0, nil
}
