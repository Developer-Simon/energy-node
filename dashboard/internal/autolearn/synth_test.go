package autolearn

import (
	"math"
	"math/rand"
	"reflect"
	"sort"
	"testing"
	"time"
)

// synthOpts beschreibt einen erzeugten Haushalt. Alles ist deterministisch,
// die Zufallszahlen kommen aus Seed. Tage werden ueber ihren Index ab dem
// Starttag angegeben, from inklusiv und to exklusiv.
type synthOpts struct {
	Seed      int64
	Devices   []string // alle Geraete, auch die ohne Gewohnheit
	Watts     float64  // Leistung eingeschalteter Geraete, Standard 100 W
	Habits    []synthHabit
	Runs      []synthRun
	Couplings []synthCoupling
	Rules     []synthRule
	Vacation  []synthSpan   // Tage ohne jede Aufzeichnung
	Outages   []synthOutage // Geraet nicht erreichbar, es schaltet nicht
	Noise     float64       // Wahrscheinlichkeit je Geraet und Tag fuer einen zufaelligen Handschaltvorgang
}

// synthSpan ist ein Bereich von Tagesindizes, from inklusiv, to exklusiv.
type synthSpan struct{ From, To int }

// synthOutage markiert ein Geraet als nicht erreichbar in den Tagen des Bereichs.
type synthOutage struct {
	Device string
	synthSpan
}

// synthHabit ist ein Schaltvorgang, der um Minute herum passiert, mit Streuung
// Spread (Minuten) und Trefferquote Hit an passenden Wochentagen. Weekdays nil
// heisst jeden Tag.
type synthHabit struct {
	Device   string
	On       bool // Richtung: true schaltet ein, false aus
	Weekdays []time.Weekday
	Minute   float64
	Spread   float64
	Hit      float64
}

// synthRun schaltet das Geraet um Minute ein und nach Minutes wieder aus.
type synthRun struct {
	Device   string
	Weekdays []time.Weekday
	Minute   float64
	Spread   float64
	Minutes  int
	Hit      float64
}

// synthCoupling schaltet den Ausloeser um Minute ein und kurz danach wieder aus.
// Der Folger schaltet Delay Minuten spaeter ein und laeuft Minutes lang.
type synthCoupling struct {
	Trigger, Follower string
	Minute            float64
	Delay, Minutes    int
	Hit               float64
}

// synthRule ist eine Regelaktion: sie schaltet das Geraet auf On und erzeugt
// einen Schaltvorgang mit Herkunft rule sowie einen Aktionseintrag.
type synthRule struct {
	RuleID string
	Device string
	On     bool
	Minute float64
	Spread float64
	Hit    float64
}

// synthDays erzeugt n Tagesdateien ab dem Tag von start (in dessen Zeitzone).
// Jeder Tag mit Aufzeichnung hat Seen und Reach fuer alle Viertelstunden, pn
// fuer alle, und OnMin, AvgW und Wh aus den Schaltzeiten. Die Haus-Last ist
// 300 W plus die Leistung der eingeschalteten Geraete plus Rauschen. Ein Tag
// im Urlaub bleibt leer. Ein Geraet im Ausfall hat Reach 0, schaltet nicht und
// seine Regelaktionen sind ok false.
func synthDays(start time.Time, n int, opts synthOpts) []DayFile {
	rng := rand.New(rand.NewSource(opts.Seed))
	watts := opts.Watts
	if watts == 0 {
		watts = 100
	}
	loc := start.Location()
	out := make([]DayFile, 0, n)
	for i := 0; i < n; i++ {
		day := time.Date(start.Year(), start.Month(), start.Day()+i, 0, 0, 0, 0, loc)
		f := DayFile{V: dayFileVersion, Day: day.Format(dayLayout), Devices: map[string]*DeviceDay{},
			Events: []EventRec{}, Actions: []ActionRec{}}
		if synthIn(opts.Vacation, i) {
			for _, id := range opts.Devices {
				f.Devices[id] = &DeviceDay{}
			}
			out = append(out, f)
			continue
		}
		for _, id := range opts.Devices {
			f.Devices[id] = &DeviceDay{}
		}
		changes := map[string][]synthChange{}
		emit := func(at time.Time, device string, on bool, origin Origin, rule string) bool {
			if synthOutageOn(opts.Outages, device, i) {
				return false
			}
			dd := f.Devices[device]
			if dd == nil {
				dd = &DeviceDay{}
				f.Devices[device] = dd
			}
			q := (at.Hour()*60 + at.Minute()) / 15
			switch origin {
			case OriginHand:
				bump(&dd.Hand[q])
			case OriginRule:
				bump(&dd.Rule[q])
			}
			f.Events = append(f.Events, EventRec{At: at.Unix(), Device: device, Entity: "switch." + device,
				On: on, Origin: string(origin), Rule: rule})
			changes[device] = append(changes[device], synthChange{minute: synthMinute(at), on: on})
			return true
		}
		for _, h := range opts.Habits {
			if !synthWeekday(h.Weekdays, day) || rng.Float64() >= h.Hit {
				continue
			}
			emit(synthTime(day, h.Minute+rng.NormFloat64()*h.Spread), h.Device, h.On, OriginHand, "")
		}
		for _, r := range opts.Runs {
			if !synthWeekday(r.Weekdays, day) || rng.Float64() >= r.Hit {
				continue
			}
			at := h2m(r.Minute + rng.NormFloat64()*r.Spread)
			emit(synthTime(day, at), r.Device, true, OriginHand, "")
			emit(synthTime(day, at+float64(r.Minutes)), r.Device, false, OriginHand, "")
		}
		for _, c := range opts.Couplings {
			if rng.Float64() >= c.Hit {
				continue
			}
			emit(synthTime(day, c.Minute), c.Trigger, true, OriginHand, "")
			emit(synthTime(day, c.Minute+1), c.Trigger, false, OriginHand, "")
			emit(synthTime(day, c.Minute+float64(c.Delay)), c.Follower, true, OriginHand, "")
			emit(synthTime(day, c.Minute+float64(c.Delay+c.Minutes)), c.Follower, false, OriginHand, "")
		}
		for _, r := range opts.Rules {
			if rng.Float64() >= r.Hit {
				continue
			}
			at := synthTime(day, r.Minute+rng.NormFloat64()*r.Spread)
			want := r.On
			ok := emit(at, r.Device, r.On, OriginRule, r.RuleID)
			f.Actions = append(f.Actions, ActionRec{At: at.Unix(), Rule: r.RuleID, Device: r.Device,
				Entity: "switch." + r.Device, Want: &want, OK: ok,
				Reaction: math.Round((1.5+rng.Float64()*2)*10) / 10})
		}
		for _, id := range opts.Devices {
			if rng.Float64() < opts.Noise {
				emit(synthTime(day, rng.Float64()*1439), id, rng.Intn(2) == 0, OriginHand, "")
			}
		}
		synthFillDay(&f, changes, opts, i, watts, rng)
		out = append(out, f)
	}
	return out
}

// synthChange ist ein Schaltvorgang eines Geraets am Tag, minute ab Mitternacht.
type synthChange struct {
	minute int
	on     bool
}

// synthFillDay setzt Erreichbarkeit, Zaehler, Leistung und Haus-Last aus den
// Schaltvorgaengen des Tages.
func synthFillDay(f *DayFile, changes map[string][]synthChange, opts synthOpts, i int, watts float64, rng *rand.Rand) {
	var load [dayQuarters]float64
	for id, dd := range f.Devices {
		reach := uint8(dayCounterMax)
		if synthOutageOn(opts.Outages, id, i) {
			reach = 0
		}
		list := changes[id]
		sort.SliceStable(list, func(a, b int) bool { return list[a].minute < list[b].minute })
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
		for q := 0; q < dayQuarters; q++ {
			dd.Seen[q] = dayCounterMax
			dd.Reach[q] = reach
			dd.PN[q] = dayCounterMax
			dd.OnMin[q] = uint8(onMin[q])
			dd.AvgW[q] = float32(watts * float64(onMin[q]) / 15)
			dd.Wh[q] = float32(watts * float64(onMin[q]) / 60)
			load[q] += watts * float64(onMin[q]) / 15
		}
	}
	for q := 0; q < dayQuarters; q++ {
		f.Site.Seen[q] = dayCounterMax
		f.Site.LoadTotal[q] = float32(300 + load[q] + 10*rng.NormFloat64())
		f.Site.GridImport[q] = f.Site.LoadTotal[q]
	}
}

// synthIn meldet, ob der Tagesindex i in einem der Bereiche liegt.
func synthIn(spans []synthSpan, i int) bool {
	for _, s := range spans {
		if i >= s.From && i < s.To {
			return true
		}
	}
	return false
}

// synthOutageOn meldet, ob das Geraet am Tagesindex i nicht erreichbar ist.
func synthOutageOn(outages []synthOutage, device string, i int) bool {
	for _, o := range outages {
		if o.Device == device && synthIn([]synthSpan{o.synthSpan}, i) {
			return true
		}
	}
	return false
}

// synthWeekday meldet, ob der Tag zu den Wochentagen gehoert. Nil heisst jeder Tag.
func synthWeekday(days []time.Weekday, day time.Time) bool {
	if days == nil {
		return true
	}
	for _, d := range days {
		if d == day.Weekday() {
			return true
		}
	}
	return false
}

// synthTime liefert den Wandzeit-Zeitpunkt minute ab Mitternacht von day.
// Minuten werden auf den Tag begrenzt.
func synthTime(day time.Time, minute float64) time.Time {
	return time.Date(day.Year(), day.Month(), day.Day(), 0, 0, int(math.Round(h2m(minute)*60)), 0, day.Location())
}

// synthMinute liefert die Minute des Tages einer Wandzeit.
func synthMinute(t time.Time) int {
	return t.Hour()*60 + t.Minute()
}

// h2m begrenzt eine Minute auf den Tag, 0 bis 1439.
func h2m(m float64) float64 {
	return math.Min(math.Max(m, 0), 1439)
}

func TestSynthDaysDeterministicAndSilentVacation(t *testing.T) {
	loc := berlinLoc(t)
	start := time.Date(2026, 1, 5, 0, 0, 0, 0, loc) // Montag
	opts := synthOpts{
		Seed:    7,
		Devices: []string{"light.kitchen", "plug.coffee"},
		Habits: []synthHabit{
			{Device: "light.kitchen", On: true, Weekdays: []time.Weekday{time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday}, Minute: 390, Spread: 10, Hit: 0.9},
		},
		Vacation: []synthSpan{{From: 7, To: 9}},
		Rules:    []synthRule{{RuleID: "r1", Device: "plug.coffee", On: true, Minute: 480, Spread: 5, Hit: 1}},
	}
	a := synthDays(start, 14, opts)
	b := synthDays(start, 14, opts)
	if !reflect.DeepEqual(a, b) {
		t.Fatal("same seed gave different days")
	}
	if len(a) != 14 || a[0].Day != "2026-01-05" {
		t.Fatalf("days = %d, first %s", len(a), a[0].Day)
	}
	for _, i := range []int{7, 8} {
		if len(a[i].Events) != 0 || len(a[i].Actions) != 0 || a[i].Site.Seen[0] != 0 {
			t.Errorf("vacation day %s not silent", a[i].Day)
		}
	}
	if len(a[0].Events) == 0 || a[0].Site.LoadTotal[0] <= 300 {
		t.Errorf("day 0 has no habit events or load: %+v", a[0].Events)
	}
	if len(a[0].Actions) != 1 || !a[0].Actions[0].OK {
		t.Errorf("day 0 actions = %+v, want one ok action", a[0].Actions)
	}
}
