package energystats

import (
	"errors"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/energy"
)

var ErrInvalidPeriod = errors.New("energystats: ungueltiger Zeitraum")

var periodLayouts = map[string]string{"day": "2006-01-02", "week": "2006-01-02", "month": "2006-01", "year": "2006"}

type HourCounts struct {
	HA   int `json:"ha"`
	Own  int `json:"own"`
	None int `json:"none"`
}

// Totals sind die Summen eines Zeitraums. Autarky und SelfConsumption sind
// nil, wenn der Nenner fehlt: nie NaN, sonst scheitert json.Marshal.
type Totals struct {
	KWh             map[string]float64 `json:"kwh"`
	Hours           HourCounts         `json:"hours"`
	Autarky         *float64           `json:"autarky"`
	SelfConsumption *float64           `json:"self_consumption"`
}

type Bucket struct {
	Start time.Time `json:"start"`
	// Local ist der Beginn in Ortszeit des Pi als Text. Der Browser
	// beschriftet damit, statt Start in seiner eigenen Zeitzone zu lesen.
	Local string `json:"local"`
	Totals
}

type Result struct {
	Period   string    `json:"period"`
	At       string    `json:"at"`
	Prev     string    `json:"prev"`
	Next     string    `json:"next"`
	From     time.Time `json:"from"`
	To       time.Time `json:"to"`
	Timezone string    `json:"timezone"`
	Buckets  []Bucket  `json:"buckets"`
	Total    Totals    `json:"total"`
}

// Query summiert die Stunden des Zeitraums. Stunden ab der laufenden
// zaehlen nicht, sie haben noch keinen Satz. Gespeichert wird je
// UTC-Stunde: Zeitzonen mit halbstuendigem Versatz bekommen deshalb
// Buckets, die eine halbe Stunde verschoben sind. Das wird bewusst nicht
// ausgeglichen.
func Query(store *Store, period, at string, loc *time.Location, now time.Time) (Result, error) {
	from, to, err := bounds(period, at, loc, now)
	if err != nil {
		return Result{}, err
	}
	starts := bucketStarts(period, from, to)
	res := Result{Period: period, At: from.Format(periodLayouts[period]), From: from, To: to, Timezone: loc.String(), Buckets: make([]Bucket, len(starts)), Total: newTotals()}
	res.Prev, res.Next = neighbours(period, from, now)
	for i, start := range starts {
		res.Buckets[i] = Bucket{Start: start, Local: start.Format("2006-01-02T15:04"), Totals: newTotals()}
	}
	cutoff := now.UTC().Truncate(time.Hour)
	end := to
	if cutoff.Before(end) {
		end = cutoff
	}
	b := 0
	err = store.Each(from, end, func(hour time.Time, m *Month, i int) {
		for b+1 < len(starts) && !hour.Before(starts[b+1]) {
			b++
		}
		addHour(&res.Buckets[b].Totals, m, i)
		addHour(&res.Total, m, i)
	})
	if err != nil {
		return Result{}, err
	}
	for i := range res.Buckets {
		res.Buckets[i].finish()
	}
	res.Total.finish()
	return res, nil
}

func bounds(period, at string, loc *time.Location, now time.Time) (time.Time, time.Time, error) {
	layout, ok := periodLayouts[period]
	if !ok {
		return time.Time{}, time.Time{}, ErrInvalidPeriod
	}
	var from time.Time
	if at == "" {
		local := now.In(loc)
		switch period {
		case "day":
			from = time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc)
		case "week":
			from = mondayOf(local)
		case "month":
			from = time.Date(local.Year(), local.Month(), 1, 0, 0, 0, 0, loc)
		default:
			from = time.Date(local.Year(), 1, 1, 0, 0, 0, 0, loc)
		}
	} else {
		parsed, err := time.ParseInLocation(layout, at, loc)
		if err != nil {
			return time.Time{}, time.Time{}, ErrInvalidPeriod
		}
		from = parsed
		if period == "week" {
			from = mondayOf(from)
		}
	}
	if from.Year() < 2000 || from.Year() > 2100 {
		return time.Time{}, time.Time{}, ErrInvalidPeriod
	}
	switch period {
	case "day":
		return from, from.AddDate(0, 0, 1), nil
	case "week":
		return from, from.AddDate(0, 0, 7), nil
	case "month":
		return from, from.AddDate(0, 1, 0), nil
	default:
		return from, from.AddDate(1, 0, 0), nil
	}
}

func bucketStarts(period string, from, to time.Time) []time.Time {
	next := func(t time.Time) time.Time {
		switch period {
		case "day":
			return t.Add(time.Hour)
		case "month", "week":
			return t.AddDate(0, 0, 1)
		default:
			return t.AddDate(0, 1, 0)
		}
	}
	var starts []time.Time
	for t := from; t.Before(to); t = next(t) {
		starts = append(starts, t)
	}
	return starts
}

// neighbours liefert at des vorigen und des naechsten Zeitraums. prev ist
// leer vor dem Jahr 2000 (Grenze von bounds), next ist leer, solange der
// naechste Zeitraum noch nicht begonnen hat.
func neighbours(period string, from, now time.Time) (string, string) {
	step := func(n int) time.Time {
		switch period {
		case "day":
			return from.AddDate(0, 0, n)
		case "week":
			return from.AddDate(0, 0, 7*n)
		case "month":
			return from.AddDate(0, n, 0)
		default:
			return from.AddDate(n, 0, 0)
		}
	}
	layout := periodLayouts[period]
	prev, next := step(-1), step(1)
	prevAt, nextAt := "", ""
	if prev.Year() >= 2000 {
		prevAt = prev.Format(layout)
	}
	if !next.After(now) {
		nextAt = next.Format(layout)
	}
	return prevAt, nextAt
}

// mondayOf liefert 00:00 des Montags der Woche, in der t liegt.
func mondayOf(t time.Time) time.Time {
	offset := (int(t.Weekday()) + 6) % 7
	return time.Date(t.Year(), t.Month(), t.Day()-offset, 0, 0, 0, 0, t.Location())
}

func newTotals() Totals { return Totals{KWh: map[string]float64{}} }

func addHour(t *Totals, m *Month, i int) {
	kind := SourceNone
	if m != nil {
		for role, s := range m.Roles {
			source := s.Source[i]
			if source == SourceNone {
				continue
			}
			t.KWh[role] += s.KWh[i]
			if source == SourceHA || kind == SourceNone {
				kind = source
			}
		}
	}
	switch kind {
	case SourceHA:
		t.Hours.HA++
	case SourceOwn:
		t.Hours.Own++
	default:
		t.Hours.None++
	}
}

func (t *Totals) finish() {
	if load := t.KWh["load_total"]; load > 0 {
		value := clamp01(1 - t.KWh["grid_import"]/load)
		t.Autarky = &value
	}
	if pv := t.KWh["pv"]; pv > 0 {
		value := clamp01(1 - t.KWh["grid_export"]/pv)
		t.SelfConsumption = &value
	}
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

// Service buendelt, was die API braucht.
type Service struct {
	Store    *Store
	Recorder *Recorder
	Sync     *Sync
	Location *time.Location
	Now      func() time.Time
	// Categories liefert die Verbraucher- und Erzeugerkategorien aus
	// energy.json, damit die Anzeige custom:<id> beschriften kann.
	Categories func() map[string]energy.Category
}

type Response struct {
	Result
	Status     Status                     `json:"status"`
	Categories map[string]energy.Category `json:"categories,omitempty"`
	Companion  *SyncStatus                `json:"companion,omitempty"`
}

func (s *Service) Stats(period, at string) (Response, error) {
	now := time.Now
	if s.Now != nil {
		now = s.Now
	}
	loc := s.Location
	if loc == nil {
		loc = time.Local
	}
	res, err := Query(s.Store, period, at, loc, now())
	if err != nil {
		return Response{}, err
	}
	resp := Response{Result: res}
	if s.Recorder != nil {
		resp.Status = s.Recorder.Status()
	}
	resp.Status.CorruptMonths = s.Store.Corrupt()
	if s.Categories != nil {
		resp.Categories = s.Categories()
	}
	if s.Sync != nil {
		companion := s.Sync.Status()
		resp.Companion = &companion
	}
	return resp, nil
}
