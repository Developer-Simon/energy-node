package autolearn

import (
	"bytes"
	"fmt"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/learnrec"
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
	// demoDryer ist der Wäschetrockner, der mittags aus der Sonne läuft, ohne Regel.
	demoDryer = "waeschetrockner"
	// demoHeatPump ist die Wärmepumpe, deren Modus die Demo-Regel schaltet.
	demoHeatPump = "waermepumpe"
	// demoRuleID ist die Regel „Akku leer, WP aus“ der Demo. Sie schaltet die
	// Wärmepumpe aus, werktags schaltet der Nutzer sie nach einer Minute wieder ein.
	demoRuleID = "regel_akku_leer_wp_aus"
	// demoPVRuleID ist die Regel „PV-Überschuss, WP an“ der Demo. Sie schaltet die
	// Wärmepumpe an, der Temperaturvorschlag der Demo gilt für sie.
	demoPVRuleID = "regel_pv_ueberschuss_wp_an"
	// demoRuleWashOn ist die Regel „Waschmaschine an bei Überschuss“ der Demo. Sie
	// steht mit der Wärmepumpenregel auf derselben Schwelle, die Aus-Regel bildet
	// mit ihr die Rückkopplung.
	demoRuleWashOn = "regel_wasch_ueberschuss_an"
	// demoCompetitionMin ist die Minute, zu der die Regeln der Konkurrenz an den
	// Konkurrenztagen gleichzeitig schalten, 11:00.
	demoCompetitionMin = 660.0
	// demoRiseFrom ist der Tagesindex, ab dem ein nicht gemessener Verbraucher
	// die Grundlast um demoRiseW hebt. TUNABLE
	demoRiseFrom = 12
	// demoRiseW ist der Anstieg der Grundlast in W. TUNABLE
	demoRiseW = 200.0
	// demoPriorWeeks ist die Zahl der Wochen vor now, für die das Vorjahr einen
	// Anteil leerer Morgen trägt. Sie deckt die 21 Demo-Tage plus die Nachbarwoche ab.
	demoPriorWeeks = 5
	// demoSeasonBaseW ist die Grundlast der Vorjahreswochen in W, so hoch wie die
	// Nächte der Demo vor dem Anstieg. TUNABLE
	demoSeasonBaseW = 290.0

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
	// demoSeasonWeeks ist die Zahl der Wochen im Saisongedächtnis der Demo, ein
	// halbes Jahr, damit die Jahreslinie eines gelernten Werts sichtbar ist.
	demoSeasonWeeks = 20
	// demoShareDays ist die Zahl der Tage mit Tagesanteil der Lernlast im Lernstand
	// der Demo, und demoShare der Anteil je Tag. Unter der Hälfte des Ziels der
	// Stufe eco (0,05), damit die nächsthöhere Stufe vorgeschlagen wird.
	demoShareDays = 14
	demoShare     = 0.02
	// demoBedtimeMin ist die mittlere Abschaltzeit der Balkonsteckdose im
	// Saisongedächtnis, sie schwankt je Woche um wenige Minuten.
	demoBedtimeMin = 1350.0
	// demoBedtimeKey ist der gelernte Wert, dessen Jahreslinie die Demo zeigt.
	demoBedtimeKey = "device/" + demoPlugBalkon + "/usual_off"
)

// demoWeekTempC sind die Wochenmittel der Außentemperatur im Saisongedächtnis,
// eine Woche je Eintrag, die jüngste zuerst. Mehr als 15 °C sind warme Wochen,
// die Reihe läuft vom Sommer in den Herbst.
var demoWeekTempC = []float64{
	3.5, 4.0, 6.5, 5.5, 6.0, 7.5, 8.0, 9.0, 10.0, 11.5,
	12.0, 13.0, 14.0, 16.5, 15.5, 17.0, 16.0, 17.5, 19.0, 18.5,
}

// demoHeatKneeC ist der Heizknick der Wärmepumpe der Demo.
const demoHeatKneeC = 15.0

// demoEntities ist die unique_id der Schalter je Gerät, wie sie die Registry hat.
var demoEntities = map[string]string{
	demoPlugBalkon:   "plug_balkon_steckdose",
	demoPlugDry:      "plug_entfeuchter_steckdose",
	demoRelais:       "relais_keller_switch_1",
	demoHeatPump:     "waermepumpe_mode",
	demoWasherDevice: "waschmaschine_steckdose",
	demoDryer:        "waeschetrockner_steckdose",
}

// demoWatts ist die Leistung der Demo-Geräte, wenn sie laufen.
var demoWatts = map[string]float64{demoPlugBalkon: 60, demoPlugDry: 300, demoRelais: 500, demoHeatPump: 1500, demoWasherDevice: 2000, demoDryer: 500}

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

// demoDays erzeugt die 21 Tage vor dem heutigen Tag mit dem Grundlastanstieg.
func demoDays(now time.Time, loc *time.Location) []DayFile {
	return demoDaysWith(now, loc, true)
}

// demoDaysWith erzeugt die 21 Tage vor dem heutigen Tag. rise hebt die Grundlast
// der letzten Nächte um demoRiseW (demoRiseFrom). Die Zeiten sind zufällig
// nur innerhalb ihrer Grenzen, der Zufall hat den festen Seed demoSeed.
func demoDaysWith(now time.Time, loc *time.Location, rise bool) []DayFile {
	rng := rand.New(rand.NewSource(demoSeed))
	// Die Waschmaschine hat einen eigenen Zufallsstrom, damit die Geräte oben
	// dieselben Bytes behalten.
	wash := rand.New(rand.NewSource(demoSeed + 1))
	// Die Wärmepumpenzyklen haben einen eigenen Zufallsstrom aus demselben Grund.
	heat := rand.New(rand.NewSource(demoSeed + 3))
	// Der Trockner hat einen eigenen Zufallsstrom.
	dry := rand.New(rand.NewSource(demoSeed + 5))
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

		// Wärmepumpe: die Regel schaltet sie aus. Zwei von drei Tagen schaltet sie
		// nach Sonnenuntergang ab, dann schaltet der Nutzer nach einer Minute wieder
		// ein (überstimmt). Der dritte Tag schaltet die Regel tagsüber ab, der
		// Nutzer nimmt das hin und schaltet erst nach zweieinhalb Stunden wieder
		// ein (keine Überstimmung). So teilt die Überstimmung die Zeit nach der Sonne.
		// Ohne Sonnenstandort gilt die alte Regel: werktags überstimmt, am Wochenende nicht.
		off := demoMinute(rng, 600, 1020)
		override := weekday
		if _, set, ok := sun.Times(day, demoLat, demoLon); ok {
			setMin := minuteOfDay(set.Unix(), loc)
			override = i%3 != 0
			// Die Uhrzeiten beider Seiten überlappen, nur die Sonne trennt sie sauber.
			if override {
				off = setMin + 5 + 35*rng.Float64()
			} else {
				off = setMin - 40 + 30*rng.Float64()
			}
		}
		b.emit(off, demoHeatPump, false, OriginRule, demoRuleID)
		b.action(off, demoHeatPump, false)
		if override {
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

		b.solar(i)
		b.washer(i, wash)
		b.battery()
		if i < demoAwayFrom || i > demoAwayTo {
			b.heatCycle(heat)
			b.dryer(dry)
		}
		if demoCompetitionDays[i] {
			b.competition()
		}
		b.fill(rng)
		if rise && i >= demoRiseFrom {
			b.riseBase()
		}
		out = append(out, b.f)
	}
	return out
}

// Die Sonne der Demo: ein Süddach mit 5 kW unter 30 Grad. Der Mittagsbereich
// ist gedrosselt, ein Schatten liegt im Westen über dem Horizont. Der
// Wetterfaktor k ist 0,9 an klaren Tagen und 0,4 an bedeckten.
const (
	demoPVPeakW     = 6500.0
	demoPVTiltDeg   = 30.0
	demoPVAzDeg     = 180.0
	demoPVAzCellDeg = 5.0
	demoPVElCellDeg = 3.0
	demoKClear      = 0.9
	demoKCloudy     = 0.4
	// demoNoonAz0 und demoNoonAz1 begrenzen den gedrosselten Mittagsbereich.
	demoNoonAz0, demoNoonAz1 = 150.0, 210.0
	demoNoonFactor           = 0.5
	// demoShade* begrenzt den Schatten, je zwei Zellen in Azimut und Höhe.
	demoShadeAz0, demoShadeAz1 = 220.0, 230.0
	demoShadeEl0, demoShadeEl1 = 12.0, 18.0
	demoShadeFactor            = 0.4
	// demoWasherDevice ist die Waschmaschine, deren Zyklen die Demo schreibt.
	demoWasherDevice = "waschmaschine"
)

// demoCloudyDays sind die Tagesindizes mit bedecktem Himmel.
var demoCloudyDays = map[int]bool{4: true, 11: true, 18: true}

// demoCompetitionDays sind die Tagesindizes, an denen die Regeln der Wärmepumpe
// und der Waschmaschine um elf Uhr gleichzeitig auslösen. Sechs Tage in den letzten
// vierzehn reichen für die Konkurrenz.
var demoCompetitionDays = map[int]bool{10: true, 12: true, 14: true, 16: true, 18: true, 20: true}

// demoEcoCycleDays und demoShortCycleDays sind die Tagesindizes mit einem
// Eco-Zyklus (12 Tage) oder einem kurzen Zyklus (4 Tage) der Waschmaschine.
var (
	demoEcoCycleDays   = map[int]bool{0: true, 1: true, 3: true, 5: true, 6: true, 7: true, 11: true, 12: true, 15: true, 18: true, 19: true, 20: true}
	demoShortCycleDays = map[int]bool{4: true, 9: true, 13: true, 17: true}
)

// demoKWeather ist der Wetterfaktor k des Tagesindex i.
func demoKWeather(i int) float64 {
	if demoCloudyDays[i] {
		return demoKCloudy
	}
	return demoKClear
}

// demoPVFactor ist der Faktor der Drosselung und des Schattens an einer Himmelsposition.
func demoPVFactor(az, el float64) float64 {
	f := 1.0
	if az >= demoNoonAz0 && az < demoNoonAz1 {
		f *= demoNoonFactor
	}
	if az >= demoShadeAz0 && az < demoShadeAz1 && el >= demoShadeEl0 && el < demoShadeEl1 {
		f *= demoShadeFactor
	}
	return f
}

// demoPVW ist die PV-Leistung in W des Demo-Dachs an einer Himmelsposition bei
// dem Wetterfaktor k. Unter drei Grad Sonnenhöhe ist sie 0.
func demoPVW(az, el, k float64) float64 {
	if el < pvRasterMinElDeg {
		return 0
	}
	return k * demoPVPeakW * pvGeom(az, el, demoPVTiltDeg, demoPVAzDeg) * demoPVFactor(az, el)
}

// solar setzt den Wetterfaktor und die PV-Leistung des Tages je Viertelstunde.
func (b *demoBuilder) solar(i int) {
	k := demoKWeather(i)
	for q := range b.f.Site.K {
		kv := float32(k)
		b.f.Site.K[q] = &kv
		az, el := sun.Position(shadeQuarterTime(b.day, q), demoLat, demoLon)
		b.f.Site.PV[q] = float32(demoPVW(az, el, k))
	}
}

// washer schreibt die Zyklen der Waschmaschine des Tagesindex i: ein Eco-Zyklus
// an den meisten Tagen, an vier Tagen ein kurzer Zyklus.
func (b *demoBuilder) washer(i int, rng *rand.Rand) {
	var rec CycleRec
	switch {
	case demoShortCycleDays[i]:
		wh := 500 + 20*rng.NormFloat64()
		rec = CycleRec{Wh: float32(wh), OpW: float32(1500 + 50*rng.NormFloat64())}
	case demoEcoCycleDays[i]:
		wh := 900 + 30*rng.NormFloat64()
		rec = CycleRec{Wh: float32(wh), OpW: float32(2000 + 100*rng.NormFloat64())}
	default:
		return
	}
	rec.Device = demoWasherDevice
	rec.Start = b.at(540 + float64(i%5)*30).Unix()
	rec.DurS = int(float64(rec.Wh) / float64(rec.OpW) * 3600)
	rec.End = "self"
	b.f.Cycles = append(b.f.Cycles, rec)
}

// heatCycle schreibt den Zyklus der Wärmepumpe des Tages, 1400 Wh bei 1500 W, um
// acht Uhr. Die Konkurrenz lernt daraus die Betriebsleistung der Wärmepumpe.
func (b *demoBuilder) heatCycle(rng *rand.Rand) {
	wh := 1400 + 50*rng.NormFloat64()
	opW := 1500 + 50*rng.NormFloat64()
	rec := CycleRec{Device: demoHeatPump, Wh: float32(wh), OpW: float32(opW)}
	rec.Start = b.at(480).Unix()
	rec.DurS = int(wh / opW * 3600)
	rec.End = "self"
	b.f.Cycles = append(b.f.Cycles, rec)
}

// dryer schreibt den Lauf des Wäschetrockners: zwei Stunden um Mittag, 1000 Wh
// bei 500 W, aus der Sonne. Der Lauf ist eine Messung, keine Schaltung, deshalb
// entstehen dafür keine Ereignisse und keine Befunde der Gewohnheiten.
func (b *demoBuilder) dryer(rng *rand.Rand) {
	wh := 1000 + 30*rng.NormFloat64()
	opW := 500 + 10*rng.NormFloat64()
	start := 720 + 60*rng.Float64() - 30
	rec := CycleRec{Device: demoDryer, Wh: float32(wh), OpW: float32(opW), PV: 1}
	rec.Start = b.at(start).Unix()
	rec.DurS = int(wh / opW * 3600)
	rec.End = "self"
	b.f.Cycles = append(b.f.Cycles, rec)
	on := int(math.Round(start))
	b.changes[demoDryer] = append(b.changes[demoDryer],
		demoChange{minute: on, on: true},
		demoChange{minute: on + rec.DurS/60, on: false})
}

// competition schreibt die Konkurrenz des Tages: die Regel der Wärmepumpe und die
// Regel der Waschmaschine schalten um elf Uhr im selben Viertel ein, die
// Waschmaschine läuft dann eine Stunde.
func (b *demoBuilder) competition() {
	b.actionFor(demoCompetitionMin, demoPVRuleID, demoHeatPump, true)
	b.actionFor(demoCompetitionMin, demoRuleWashOn, demoWasherDevice, true)
	b.emit(demoCompetitionMin, demoWasherDevice, true, OriginRule, demoRuleWashOn)
	b.emit(demoCompetitionMin+60, demoWasherDevice, false, OriginRule, demoRuleWashOn)
}

// demoSoC ist der Ladestand des Hausakkus in Prozent zur Minute m ab Mitternacht.
// Der Akku sinkt bis 05:00 von 30 auf 6 Prozent und bleibt dort bis 08:00, also
// leer vor Sonnenaufgang. Er lädt bis 10:00 auf 97 Prozent, hält sie bis 17:00
// und sinkt bis Mitternacht wieder auf 30 Prozent.
func demoSoC(m float64) float64 {
	switch {
	case m < 300:
		return 30 - 24*m/300
	case m < 480:
		return 6
	case m < 600:
		return 6 + 91*(m-480)/120
	case m < 1020:
		return 97
	default:
		return 97 - 67*(m-1020)/420
	}
}

// battery setzt den Ladestand des Tages je Viertelstunde.
func (b *demoBuilder) battery() {
	for q := range b.f.Site.SoC {
		v := float32(demoSoC(float64(q) * baseQuarterMin))
		b.f.Site.SoC[q] = &v
	}
}

// riseBase hebt die Last des ganzen Tages um demoRiseW, ein Verbraucher ohne
// eigene Messung. Netzbezug folgt der Last.
func (b *demoBuilder) riseBase() {
	for q := range b.f.Site.LoadTotal {
		b.f.Site.LoadTotal[q] += float32(demoRiseW)
		b.f.Site.GridImport[q] = b.f.Site.LoadTotal[q]
	}
}

// demoTodayC ist die Außentemperatur des angefangenen Demo-Tages, so kalt wie
// die letzten Demo-Tage.
const demoTodayC = 9.0

// demoToday baut den angefangenen heutigen Tag bis vor die Viertelstunde von now:
// nur die Werte des Hauses (Last, PV, Wetterfaktor eines klaren Tages,
// Außentemperatur), keine Geräte und keine Schaltvorgänge. ok ist false in der
// ersten Viertelstunde des Tages.
func demoToday(now time.Time, loc *time.Location) (DayFile, bool) {
	local := now.In(loc)
	day := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc)
	q0 := (local.Hour()*60 + local.Minute()) / shadeQuarterMin
	if q0 == 0 {
		return DayFile{}, false
	}
	rng := rand.New(rand.NewSource(demoSeed + 2))
	f := DayFile{V: dayFileVersion, Day: day.Format(dayLayout), Fake: true,
		Devices: map[string]*DeviceDay{}, Events: []EventRec{}, Actions: []ActionRec{}}
	for q := 0; q < q0; q++ {
		k := float32(demoKClear)
		temp := float32(demoTodayC)
		az, el := sun.Position(shadeQuarterTime(day, q), demoLat, demoLon)
		f.Site.Seen[q] = dayCounterMax
		f.Site.K[q] = &k
		f.Site.TempC[q] = &temp
		f.Site.PV[q] = float32(demoPVW(az, el, demoKClear))
		f.Site.LoadTotal[q] = float32(300 + 10*rng.NormFloat64())
		f.Site.GridImport[q] = f.Site.LoadTotal[q]
	}
	return f, true
}

// SeedDemoToday schreibt den angefangenen heutigen Tag nach dir, damit der
// Rest-Überschuss von heute in der Demo gleich nach dem Start einen Vergleich
// hat. Der Tagesspeicher übernimmt die Datei beim Start und schreibt live weiter.
// In der ersten Viertelstunde des Tages schreibt sie nichts.
func SeedDemoToday(dir string, now time.Time, loc *time.Location) error {
	if loc == nil {
		loc = time.Local
	}
	f, ok := demoToday(now, loc)
	if !ok {
		return nil
	}
	store := &DayStore{dir: dir}
	if err := store.writeFile(f.Day, f); err != nil {
		return fmt.Errorf("autolearn: demo today: %w", err)
	}
	return nil
}

// DemoPVRaster liefert das Himmelsraster des Demo-Dachs: je Viertelstunde der
// 21 Demo-Tage mit Sonnenstand die PV-Leistung, die auch die Tagesdateien tragen.
// now bestimmt die Tage, lat und lon den Standort der Sonne.
func DemoPVRaster(now time.Time, lat, lon float64) *Raster {
	r := NewRaster(demoPVAzCellDeg, demoPVElCellDeg)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	for i := 0; i < demoSeedDays; i++ {
		day := today.AddDate(0, 0, i-demoSeedDays)
		k := demoKWeather(i)
		for q := 0; q < dayQuarters; q++ {
			az, el := sun.Position(shadeQuarterTime(day, q), lat, lon)
			if el < pvRasterMinElDeg {
				continue
			}
			r.Add(az, el, demoPVW(az, el, k), day.Format(dayLayout))
		}
	}
	return r
}

// demoYearStep ist der Abstand in Tagen der klaren Tage vor den 21 Demo-Tagen. TUNABLE
const demoYearStep = 3

// DemoPVRasterYear ist DemoPVRaster mit dem Jahr davor: an jedem demoYearStep-ten
// Tag bis 365 Tage zurück ein klarer Tag. So kennt der Raster die Sommersonne,
// wie bei einem Node, der ein Jahr gelaufen ist.
func DemoPVRasterYear(now time.Time, lat, lon float64) *Raster {
	r := DemoPVRaster(now, lat, lon)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	for back := demoSeedDays + 1; back <= 365; back += demoYearStep {
		day := today.AddDate(0, 0, -back)
		for q := 0; q < dayQuarters; q++ {
			az, el := sun.Position(shadeQuarterTime(day, q), lat, lon)
			if el < pvRasterMinElDeg {
				continue
			}
			r.Add(az, el, demoPVW(az, el, demoKClear), day.Format(dayLayout))
		}
	}
	return r
}

// DemoPVModel passt das Dachmodell an das Demo-Raster an, so wie der nächtliche
// Erkenner es täte.
func DemoPVModel(r *Raster) PVModel {
	m, _ := FitPV(r, 5, 2)
	return m
}

// SeedDemoPV schreibt das Demo-Raster, das ein Jahr Himmel umfasst, nach
// rasterPath und das Dachmodell nach modelPath. Der Aufrufer schreibt sie nur
// beim ersten Start der Demo, zusammen mit den Tagesdateien.
func SeedDemoPV(rasterPath, modelPath string, now time.Time) error {
	r := DemoPVRasterYear(now, demoLat, demoLon)
	data := r.State()
	if data == nil {
		return fmt.Errorf("autolearn: demo raster could not be encoded")
	}
	if err := os.WriteFile(rasterPath, data, 0o644); err != nil {
		return fmt.Errorf("autolearn: demo raster: %w", err)
	}
	store := NewPVModelStore(modelPath)
	store.Set(DemoPVModel(r))
	if err := store.Save(); err != nil {
		return fmt.Errorf("autolearn: demo model: %w", err)
	}
	return nil
}

// demoOutdoorC ist die Tagesmitteltemperatur des Tagesindex i in °C. Die Reihe
// läuft über 13 bis 19 °C, mit warmen Tagen über dem Heizknick. Die Abwesenheitstage
// sind mit demoAwayC warm (Urlaub), damit der Standby außerhalb der Heizkurve liegt.
func demoOutdoorC(i int) float64 {
	if i >= demoAwayFrom && i <= demoAwayTo {
		return demoAwayC
	}
	// Die letzten drei Tage sind kalt, 8 bis 10 °C im Mittel 9 °C. Damit liegt
	// das Mittel der letzten drei Tage unter dem Heizknick, und die Befunde der
	// Heizsaison der Wärmepumpe sind sichtbar.
	if i >= demoSeedDays-3 {
		return float64(8 + i%3)
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
	b.actionFor(minute, demoRuleID, device, on)
}

// actionFor schreibt die Regelaktion einer Regel zu einem Schaltvorgang.
func (b *demoBuilder) actionFor(minute float64, rule, device string, on bool) {
	want := on
	b.f.Actions = append(b.f.Actions, ActionRec{At: b.at(minute).Unix(), Rule: rule, Device: device,
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
		b.f.Site.GridExport[q] = float32(math.Max(0, float64(b.f.Site.PV[q]-b.f.Site.LoadTotal[q])))
	}
}

// DemoSeason liefert das Saisongedächtnis der Demo: die zwanzig Wochen vor now,
// die älteste zuerst. Jede Woche trägt ihre Außentemperatur, fünf Auslösungen
// der Demo-Regeln, damit der Temperaturvorschlag warme Wochen findet, und die
// gelernte Abschaltzeit der Balkonsteckdose, damit die Jahreslinie erscheint.
func DemoSeason(now time.Time) []Week {
	out := make([]Week, 0, demoSeasonWeeks)
	for k := demoSeasonWeeks; k >= 1; k-- {
		temp := demoWeekTempC[k-1]
		out = append(out, Week{
			Key:      isoWeekKey(now.AddDate(0, 0, -7*k)),
			Source:   seasonSourceDays,
			Days:     7,
			TempC:    &temp,
			Triggers: map[string]float64{demoRuleID: 5, demoPVRuleID: 5},
			Learned:  map[string]float64{demoBedtimeKey: demoBedtimeMin + 6*math.Sin(float64(k))},
		})
	}
	// Dieselben Wochen in den zwei Jahren davor: der Akku war damals selten leer,
	// und die Grundlast lag nachts bei demoSeasonBaseW. Damit meldet die Demo
	// „Akku ist nachts leer“ und „Grundlast steigt“ schon nach 21 Tagen.
	for back := 1; back <= 2; back++ {
		for k := 0; k <= demoPriorWeeks; k++ {
			y, w := now.AddDate(0, 0, -7*k).ISOWeek()
			week := Week{Key: fmt.Sprintf("%d-W%02d", y-back, w), Source: seasonSourceDays, Days: 7, BaseW: demoSeasonBaseW}
			if back == 1 {
				share := 0.1
				week.EmptyShare = &share
			}
			out = append(out, week)
		}
	}
	return out
}

// DemoDailyShare liefert den Tagesanteil der Lernlast für die letzten
// demoShareDays Tage vor now, den der Lernstand der Demo beim ersten Start
// bekommt. Der Anteil liegt unter der Hälfte des Ziels der Stufe eco.
func DemoDailyShare(now time.Time) map[string]float64 {
	out := make(map[string]float64, demoShareDays)
	for i := 1; i <= demoShareDays; i++ {
		out[now.AddDate(0, 0, -i).Format("2006-01-02")] = demoShare
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

// Die Minutenaufzeichnung der Demo: die letzten drei Tage vor dem heutigen Tag
// mit Leistung je Gerät, Bilanz und Außentemperatur, dazu am letzten Tag die
// Reglerschritte des Szenarios der Demo. Die Dateien tragen keine Kennzeichnung,
// sie liegen im Datenordner der Demo und verschwinden mit dessen Zurücksetzen.
const (
	demoMinuteDays = 3
	// demoStepScenario ist das Szenario der Demo, das den Regler fährt (Nulleinspeisung).
	demoStepScenario = "nulleinspeisung"
	// demoStepEveryMin ist der Abstand der Reglerschritte in Minuten, von 06:00 bis 20:00.
	demoStepEveryMin = 2
	// demoStepTargetW ist der Zielwert der Nulleinspeisung, und demoStepDeadbandW
	// die Totzone um ihn, wie in der Regel der Demo.
	demoStepTargetW   = -30.0
	demoStepDeadbandW = 20.0
	// demoStepLimitMin und demoStepLimitMax sind die Grenzen des Stellwerts in W.
	demoStepLimitMin = 50.0
	demoStepLimitMax = 800.0
	// demoStepGain ist der Verstärkungsfaktor des Reglers der Demo.
	demoStepGain = 0.8
)

// SeedDemoMinutes schreibt die Minutenaufzeichnung der Demo nach dataDir, in
// <dataDir>/learnrec/minutes: je Tag eine Datei mit den Minuten, am letzten Tag
// zusätzlich die Reglerschritte des Szenarios. Vorhandene Dateien bleiben
// unberührt, derselbe Aufruf schreibt dieselben Bytes.
func SeedDemoMinutes(dataDir string, now time.Time, loc *time.Location) error {
	if loc == nil {
		loc = time.Local
	}
	days := demoDays(now.In(loc), loc)
	rng := rand.New(rand.NewSource(demoSeed + 4))
	// Der Regler mit dem letzten Wert als Basis hat einen eigenen Zufallsstrom.
	rngLast := rand.New(rand.NewSource(demoSeed + 6))
	// Die Schritte des zweiten Reglers gibt es an allen Demo-Tagen, damit die
	// Zahl der Stellwertänderungen im Fenster des Erkenners reicht.
	for i := 0; i < demoSeedDays; i++ {
		if err := seedDemoStepsLast(dataDir, days[i], loc, rngLast); err != nil {
			return fmt.Errorf("autolearn: demo steps: %w", err)
		}
	}
	for i := demoSeedDays - demoMinuteDays; i < demoSeedDays; i++ {
		f := days[i]
		path := minuteFileName(dataDir, f.Day)
		if err := seedMinuteDay(path, f); err != nil {
			return fmt.Errorf("autolearn: demo minutes: %w", err)
		}
		if i == demoSeedDays-1 {
			if err := seedDemoSteps(dataDir, f, loc, rng); err != nil {
				return fmt.Errorf("autolearn: demo steps: %w", err)
			}
		}
	}
	return nil
}

// minuteFileName ist die Minutendatei eines Tages am Basisverzeichnis dir.
func minuteFileName(dir, day string) string {
	return filepath.Join(dir, "learnrec", "minutes", day+".enm")
}

// seedMinuteDay schreibt die Minuten eines Demo-Tages in path, wenn die Datei fehlt.
// Je Viertelstunde gilt ein Wert, für die Minuten einer Viertelstunde.
func seedMinuteDay(path string, f DayFile) error {
	if demoFileExists(path) {
		return nil
	}
	var keys []string
	var values [][]float32
	add := func(key string, quarter func(q int) float32) {
		row := make([]float32, minutesPerDayDemo)
		for m := range row {
			row[m] = quarter(m / 15)
		}
		keys = append(keys, key)
		values = append(values, row)
	}
	ids := make([]string, 0, len(demoEntities))
	for id := range demoEntities {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		dd := f.Devices[id]
		add("e:"+demoEntities[id], func(q int) float32 { return dd.AvgW[q] })
	}
	add("b:load_total", func(q int) float32 { return f.Site.LoadTotal[q] })
	add("b:grid_import", func(q int) float32 { return f.Site.GridImport[q] })
	add("b:pv", func(q int) float32 { return f.Site.PV[q] })
	add("t:outdoor", func(q int) float32 {
		if p := f.Site.TempC[q]; p != nil {
			return *p
		}
		return float32(math.NaN())
	})

	var buf bytes.Buffer
	if _, err := learnrec.AppendBlock(&buf, f.Day, learnrec.MinuteBlock{Keys: keys, Values: values}, map[string]int{}); err != nil {
		return err
	}
	return writeNewFile(path, buf.Bytes())
}

// minutesPerDayDemo ist die Zahl der Minuten eines Tages.
const minutesPerDayDemo = 1440

// seedDemoSteps schreibt die Reglerschritte des Szenarios am Tag f, wenn die
// Datei fehlt. Der Regler zieht den Netzbezug auf den Zielwert: Netz ist PV minus
// Last, der Stellwert folgt dem Fehler mit dem Verstärkungsfaktor und bleibt in
// seinen Grenzen. Gesendet wird jeder Schritt, auch ohne Stellwertänderung.
func seedDemoSteps(dataDir string, f DayFile, loc *time.Location, rng *rand.Rand) error {
	path := stepFileName(dataDir, f.Day, demoStepScenario)
	if demoFileExists(path) {
		return nil
	}
	day, err := time.ParseInLocation(dayLayout, f.Day, loc)
	if err != nil {
		return err
	}
	var steps []Step
	prev := demoStepLimitMax
	for m := 6 * 60; m < 20*60; m += demoStepEveryMin {
		q := m / 15
		net := float64(f.Site.PV[q]-f.Site.LoadTotal[q]) + 5*rng.NormFloat64()
		errW := net - demoStepTargetW
		to, why := prev, StepUnchanged
		switch {
		case math.Abs(errW) <= demoStepDeadbandW:
			why = StepDeadband
		default:
			raw := float64(prev) - demoStepGain*errW
			switch {
			case raw > demoStepLimitMax || raw < demoStepLimitMin:
				to = math.Max(demoStepLimitMin, math.Min(demoStepLimitMax, math.Round(raw/10)*10))
				why = StepLimit
			default:
				to = math.Round(raw/10) * 10
				if to != prev {
					why = StepSent
				}
			}
		}
		from32, to32 := float32(prev), float32(to)
		at := time.Date(day.Year(), day.Month(), day.Day(), m/60, m%60, 0, 0, loc)
		steps = append(steps, Step{At: at.Unix(), Err: float32(errW), Net: float32(net),
			From: &from32, To: &to32, Why: why})
		prev = to
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	var buf []byte
	for _, st := range steps {
		buf = append(buf, encodeStep(st)...)
	}
	return writeNewFile(path, buf)
}

// demoStepScenarioLast ist das zweite Szenario der Demo mit Regler, dessen
// Basis der letzte Wert ist. Seine Strecke folgt dem Stellwert mit K = 1 und
// Totzeit 1, so wie der nächtliche Erkenner sie messen kann.
const demoStepScenarioLast = "nulleinspeisung_last"

// demoStepBaseLoadW ist die feste Grundlast der Regler-Schritte in W. Sie hängt
// nicht am Lastprofil der Tage, damit Geräte der Demo die Schritte nicht ändern.
const demoStepBaseLoadW = 300.0

// demoStepJumpW ist der Sprung der Last je Viertelstunde in W.
const demoStepJumpW = 15.0

// seedDemoStepsLast schreibt die Reglerschritte des Szenarios mit der Basis
// „last“ am Tag f, wenn die Datei fehlt. Die Schritte liegen von 09:00 bis 14:00,
// dort liegt die PV über der Last, der Stellwert bestimmt also das Netz. Das Netz
// ist das kleinere von PV und dem Stellwert des vorigen Schritts, abzüglich der
// festen Grundlast und einer langsam driftenden Last. So folgt die Netzänderung der
// Stellwertänderung im nächsten Schritt, K = 1 und Totzeit 1.
func seedDemoStepsLast(dataDir string, f DayFile, loc *time.Location, rng *rand.Rand) error {
	path := stepFileName(dataDir, f.Day, demoStepScenarioLast)
	if demoFileExists(path) {
		return nil
	}
	day, err := time.ParseInLocation(dayLayout, f.Day, loc)
	if err != nil {
		return err
	}
	var buf []byte
	prev := demoStepLimitMax
	drift := 0.0
	load := demoStepBaseLoadW
	for m := 9 * 60; m < 14*60; m++ {
		q := m / 15
		if m%15 == 0 {
			// Die Last springt je Viertelstunde, wie die Verbraucher des Hauses.
			load = demoStepBaseLoadW + demoStepJumpW*rng.NormFloat64()
		}
		// Die Last driftet langsam, so ändert der Regler den Stellwert regelmäßig.
		drift += 4 * rng.NormFloat64()
		net := math.Min(float64(f.Site.PV[q]), prev) - load - drift
		errW := net - demoStepTargetW
		to, why := prev, StepUnchanged
		switch {
		case math.Abs(errW) <= demoStepDeadbandW:
			why = StepDeadband
		default:
			raw := prev - demoStepGain*errW
			switch {
			case raw > demoStepLimitMax || raw < demoStepLimitMin:
				to = math.Max(demoStepLimitMin, math.Min(demoStepLimitMax, math.Round(raw/10)*10))
				why = StepLimit
			default:
				to = math.Round(raw/10) * 10
				if to != prev {
					why = StepSent
				}
			}
		}
		from32, to32 := float32(prev), float32(to)
		at := time.Date(day.Year(), day.Month(), day.Day(), m/60, m%60, 0, 0, loc)
		buf = append(buf, encodeStep(Step{At: at.Unix(), Err: float32(errW), Net: float32(net),
			From: &from32, To: &to32, Why: why})...)
		prev = to
	}
	return writeNewFile(path, buf)
}

// demoFileExists meldet, ob path existiert.
func demoFileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// writeNewFile schreibt data nach path, nur wenn path noch nicht existiert.
func writeNewFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}
