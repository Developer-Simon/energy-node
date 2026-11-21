package autolearn

import (
	"fmt"
	"math"
	"math/rand"
	"os"
	"sort"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/sun"
)

// Die Demo-Daten erzeugen vier Gewohnheiten für die Geräte, die die Demo
// wirklich registriert (Discovery der Demo-Broker-Fixture). Device-IDs sind
// device.identifiers, Entitäten heißen wie ihre unique_id.
const (
	demoSeedDays = 21
	demoSeed     = 20261120

	// demoPlugBalkon ist die Steckdose des Balkons. An ihr sieht man die Schaltzeit.
	demoPlugBalkon = "plug_balkon"
	// demoPlugDry ist die Steckdose des Entfeuchters, die vergessen läuft.
	demoPlugDry = "plug_entfeuchter"
	// demoRelais ist das Kellerrelais, das die Balkonsteckdose mitschaltet.
	demoRelais = "relais_keller"
	// demoHeatPump ist die Wärmepumpe, deren Modus die Demo-Regel schaltet.
	demoHeatPump = "waermepumpe"
	// demoRuleID ist die Regel „Akku leer, WP aus“ der Demo. Sie schaltet die
	// Wärmepumpe aus, werktags schaltet der Nutzer sie nach einer Minute wieder ein.
	demoRuleID = "regel_akku_leer_wp_aus"

	// demoPlugOffMin ist die Abschaltzeit der Balkonsteckdose, 22:30 in Minuten.
	demoPlugOffMin = 1350
	// demoRunMinutes ist die übliche Laufzeit des Entfeuchters in Minuten.
	demoRunMinutes = 40
	// demoLongDay1 und demoLongDay2 sind die Tagesindizes mit vergessenem Entfeuchter.
	demoLongDay1 = 6
	demoLongDay2 = 15
	// demoCouplingLag ist die Verzögerung der Balkonsteckdose hinter dem Relais.
	demoCouplingLag = 40.0 / 60.0
	// demoAwayFrom und demoAwayTo sind die Tagesindizes der Abwesenheit, drei
	// Tage ohne Handgriff (Freitag bis Sonntag).
	demoAwayFrom = 7
	demoAwayTo   = 9
	// demoStandbyMinutes ist die Warmhaltezeit der Wärmepumpe an Abwesenheitstagen,
	// zwanzig Minuten bei 1500 W, also 0,5 kWh.
	demoStandbyMinutes = 20
	// demoLat und demoLon sind der Standort der Demo (Raum München).
	demoLat = 48.1
	demoLon = 11.6
	// demoRelayOffset ist die Zeit nach Sonnenuntergang, zu der das Kellerrelais
	// einschaltet, die Sonnengewohnheit der Demo.
	demoRelayOffset = 5.0
	// demoAwayC ist die Außentemperatur an den Abwesenheitstagen, warm wie im Urlaub.
	demoAwayC = 22.0
	// demoSeasonWeeks ist die Zahl der Wochen im Saisongedächtnis der Demo.
	demoSeasonWeeks = 8
)

// demoWeekTempC sind die Wochenmittel der Außentemperatur im Saisongedächtnis,
// eine Woche je Eintrag, die jüngste zuerst. Mehr als 15 °C sind warme Wochen.
var demoWeekTempC = []float64{17, 12, 16, 18, 13, 16.5, 19, 11}

// demoHeatKneeC ist der Heizknick der Wärmepumpe der Demo.
const demoHeatKneeC = 15.0

// demoEntities ist die unique_id der Schalter je Gerät, wie sie die Registry hat.
var demoEntities = map[string]string{
	demoPlugBalkon: "plug_balkon_steckdose",
	demoPlugDry:    "plug_entfeuchter_steckdose",
	demoRelais:     "relais_keller_switch_1",
	demoHeatPump:   "waermepumpe_mode",
}

// demoWatts ist die Leistung der Demo-Geräte, wenn sie laufen.
var demoWatts = map[string]float64{demoPlugBalkon: 60, demoPlugDry: 300, demoRelais: 500, demoHeatPump: 1500}

// SeedDemo schreibt die Tagesdateien der letzten 21 Tage vor now in dir, für die
// Geräte der Demo. Jede Datei trägt "fake": true, die Konsole der Demo entfernt
// nur solche. Das Ergebnis ist fest, derselbe Aufruf schreibt dieselben Bytes.
// Der Aufrufer prüft, ob dir leer ist.
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
	// heatKWh ist die Energie der Wärmepumpe des Tages nach dem Heizknick, 0 an
	// Abwesenheitstagen, dort gilt der Standby aus den Schaltzeiten.
	heatKWh float64
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
			f: DayFile{V: dayFileVersion, Day: day.Format(dayLayout), Fake: true,
				Devices: map[string]*DeviceDay{}, Events: []EventRec{}, Actions: []ActionRec{}},
			day:     day,
			loc:     loc,
			changes: map[string][]demoChange{},
		}
		for id := range demoEntities {
			b.f.Devices[id] = &DeviceDay{}
		}
		// Außentemperatur: je Viertelstunde derselbe Tageswert, deterministisch aus dem Tagesindex.
		outdoor := demoOutdoorC(i)
		for q := range b.f.Site.TempC {
			v := float32(outdoor)
			b.f.Site.TempC[q] = &v
		}
		b.heatKWh = demoHeatKWh(outdoor)
		if i >= demoAwayFrom && i <= demoAwayTo {
			b.heatKWh = 0
		}
		// Ohne Gewohnheit: drei Tage, an denen die Balkonsteckdose nicht um 22:30
		// abschaltet und die Kopplung nicht greift.
		skip := i%7 == 3
		weekday := day.Weekday() != time.Saturday && day.Weekday() != time.Sunday

		// Wärmepumpe: die Regel schaltet sie zwischen 10 und 17 Uhr aus. Werktags
		// schaltet der Nutzer nach einer Minute wieder ein (überstimmt), am Wochenende
		// erst nach zweieinhalb Stunden (keine Überstimmung).
		off := demoMinute(rng, 600, 1020)
		b.emit(off, demoHeatPump, false, OriginRule, demoRuleID)
		b.action(off, demoHeatPump, false)
		if weekday {
			b.emit(off+1, demoHeatPump, true, OriginHand, "")
		} else {
			b.emit(off+150+30*rng.Float64(), demoHeatPump, true, OriginHand, "")
		}

		// Entfeuchter: ein Lauf von knapp 40 Minuten, an zwei Tagen Stunden, weil
		// niemand abschaltet.
		start := demoMinute(rng, 600, 1020)
		run := float64(demoRunMinutes - 4 + rng.Intn(9))
		if i == demoLongDay1 || i == demoLongDay2 {
			run = 300 + 60*rng.Float64()
		}
		b.emit(start, demoPlugDry, true, OriginHand, "")
		b.emit(start+run, demoPlugDry, false, OriginHand, "")

		// Gewohnheit: die Balkonsteckdose wird an den meisten Tagen um 22:30 von
		// Hand abgeschaltet, mit etwas Streuung.
		if !skip {
			b.emit(demoPlugOffMin+rng.NormFloat64()*8, demoPlugBalkon, false, OriginHand, "")
		}

		// Kopplung und Sonnengewohnheit: Das Relais schaltet an den meisten Tagen kurz
		// nach Sonnenuntergang ein, die Balkonsteckdose folgt 40 Sekunden später und
		// bleibt an, bis sie um 22:30 abgeschaltet wird. Ohne Abschaltung vorher ist
		// sie um 22:30 an. Ohne Sonnenstandort fällt das Relais auf eine feste Zeit.
		relay := demoMinute(rng, 600, 1000)
		if _, set, ok := sun.Times(day, demoLat, demoLon); ok {
			relay = minuteOfDay(set.Unix(), loc) + demoRelayOffset + float64(i*5%7) - 3
		}
		b.emit(relay, demoRelais, true, OriginHand, "")
		b.emit(relay+90, demoRelais, false, OriginHand, "")
		if !skip {
			b.emit(relay+demoCouplingLag, demoPlugBalkon, true, OriginHand, "")
		}

		// Abwesenheit: der Tag wird neu aufgebaut, ohne Handgriff und ohne Regel.
		// Die Wärmepumpe hält nachts zwanzig Minuten warm, der Ursprung ist leer,
		// damit weder Hand noch Regel gezählt werden. Die Zufallswerte oben bleiben
		// gezogen, damit die übrigen Tage dieselben Bytes behalten.
		if i >= demoAwayFrom && i <= demoAwayTo {
			b.clear()
			b.emit(0, demoHeatPump, true, "", "")
			b.emit(demoStandbyMinutes, demoHeatPump, false, "", "")
		}

		b.fill(rng)
		out = append(out, b.f)
	}
	return out
}

// demoOutdoorC ist die Tagesmitteltemperatur des Tagesindex i in °C. Die Reihe
// läuft über 13 bis 19 °C, mit warmen Tagen über dem Heizknick. Die Abwesenheitstage
// sind mit demoAwayC warm (Urlaub), damit der Standby außerhalb der Heizkurve liegt.
func demoOutdoorC(i int) float64 {
	if i >= demoAwayFrom && i <= demoAwayTo {
		return demoAwayC
	}
	return float64(13 + (i*5)%7)
}

// demoHeatKWh ist die Tagesenergie der Wärmepumpe in kWh bei der Außentemperatur t,
// 2 + 0,8 * max(0, 15 - t). Die Werte folgen dem Heizknick der Demo exakt.
func demoHeatKWh(t float64) float64 {
	return 2 + 0.8*math.Max(0, demoHeatKneeC-t)
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
	b.f.Events = append(b.f.Events, EventRec{At: at.Unix(), Device: device, Entity: demoEntities[device],
		On: on, Origin: string(origin), Rule: rule, House: 300})
	b.changes[device] = append(b.changes[device], demoChange{minute: at.Hour()*60 + at.Minute(), on: on})
}

// clear verwirft Ereignisse, Regelaktionen und Zähler des Tages.
func (b *demoBuilder) clear() {
	b.f.Events = []EventRec{}
	b.f.Actions = []ActionRec{}
	b.changes = map[string][]demoChange{}
	for id := range demoEntities {
		b.f.Devices[id] = &DeviceDay{}
	}
}

// action schreibt die Regelaktion zu einem Schaltvorgang der Regel demoRuleID.
func (b *demoBuilder) action(minute float64, device string, on bool) {
	want := on
	b.f.Actions = append(b.f.Actions, ActionRec{At: b.at(minute).Unix(), Rule: demoRuleID, Device: device,
		Entity: demoEntities[device], Want: &want, There: false, OK: true, Reaction: 2.4})
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
			if id == demoHeatPump && b.heatKWh > 0 {
				// Die Tagesenergie der Wärmepumpe folgt dem Heizknick. Die Last des
				// Hauses bleibt die der Schaltzeiten, damit Abwesenheit erkennbar bleibt.
				dd.Wh[q] = float32(b.heatKWh * 1000 / dayQuarters)
			}
			load[q] += float64(dd.AvgW[q])
		}
	}
	for q := 0; q < dayQuarters; q++ {
		b.f.Site.Seen[q] = dayCounterMax
		b.f.Site.LoadTotal[q] = float32(300 + load[q] + 10*rng.NormFloat64())
		b.f.Site.GridImport[q] = b.f.Site.LoadTotal[q]
	}
}

// DemoSeason liefert das Saisongedächtnis der Demo: die acht Wochen vor now,
// die älteste zuerst. Jede Woche trägt ihre Außentemperatur und fünf Auslösungen
// der Demo-Regel, damit der Temperaturvorschlag warme Wochen findet.
func DemoSeason(now time.Time) []Week {
	out := make([]Week, 0, demoSeasonWeeks)
	for k := demoSeasonWeeks; k >= 1; k-- {
		temp := demoWeekTempC[k-1]
		out = append(out, Week{
			Key:      isoWeekKey(now.AddDate(0, 0, -7*k)),
			Source:   seasonSourceDays,
			Days:     7,
			TempC:    &temp,
			Triggers: map[string]float64{demoRuleID: 5},
		})
	}
	return out
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

// demoHouseStateDays ist die Zahl der Vergleichstage des Demo-Hauszustands.
const demoHouseStateDays = 14

// WithDemoHouseState ergänzt die Lernwerte der Demo um einen ruhigen Hauszustand,
// solange der echte Erkenner keinen liefert. Die Demo-Tage enden gestern, für das
// Fenster der letzten drei Stunden fehlen also die Daten. So zeigen Editor und
// Regelkarte auch in der Demo einen gelernten Hauszustand.
func WithDemoHouseState(vals []Value) []Value {
	for _, v := range vals {
		if v.Key == "site/house_state" {
			return vals
		}
	}
	out := make([]Value, 0, len(vals)+1)
	out = append(out, vals...)
	return append(out, Value{Key: "site/house_state", Kind: ValueState, Group: "habit", Text: "quiet", N: demoHouseStateDays})
}
