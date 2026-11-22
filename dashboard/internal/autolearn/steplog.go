package autolearn

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"regexp"
	"sort"
	"sync"
	"time"
)

// Why sind die Gruende eines Reglerschritts. Der Automations-Dienst meldet sie
// als Text, im Speicher stehen sie als Zahl.
const (
	StepSent uint8 = iota
	StepDeadband
	StepUnchanged
	StepLimit
)

// stepFutureSlack ist die Zeit, die ein Schritt vorab liegen darf, etwa bei
// einer nachgehenden Uhr des Dienstes.
const stepFutureSlack = 300 * time.Second

// stepRecordSize ist die Laenge eines Satzes der Schrittdatei.
const stepRecordSize = 24

var stepScenarioRE = regexp.MustCompile(`^[a-z0-9_-]{1,64}$`)

// Step ist ein berechneter Reglerschritt eines Szenarios. From und To sind nil,
// wenn kein Stellwert vorliegt. Err und Net sind in Watt.
type Step struct {
	At       int64
	Err, Net float32
	From, To *float32
	Why      uint8
}

// StepDay sind die Tageskennzahlen eines Szenarios. OutWhImport und OutWhExport
// sind die Energie ausserhalb der Totzone, Fehler mal Abstand zum naechsten
// Schritt. Netz ueber null zaehlt als Bezug, unter null als Einspeisung.
type StepDay struct {
	N           int     `json:"n"`
	Changes     int     `json:"changes"`
	SignFlips   int     `json:"flips"`
	OutWhImport float32 `json:"out_imp"`
	OutWhExport float32 `json:"out_exp"`
}

// StepLog haelt die Reglerschritte je Szenario im RAM. Die Aufbewahrung folgt
// days, dir liefert das Basisverzeichnis der Schrittdateien. Leer heisst nur RAM.
type StepLog struct {
	days func() int
	dir  func() string
	now  func() time.Time

	mu    sync.Mutex
	steps map[string][]Step
}

// NewStepLog legt einen leeren Schrittspeicher an. days und dir werden bei
// jedem Aufruf gelesen.
func NewStepLog(days func() int, dir func() string) *StepLog {
	return &StepLog{days: days, dir: dir, now: time.Now, steps: map[string][]Step{}}
}

// stepPayloadJSON ist die Form des Schritts auf dem Topic. Pflichtfelder sind
// Zeiger, damit ein fehlendes Feld auffaellt.
type stepPayloadJSON struct {
	At   *float64 `json:"at"`
	Err  *float64 `json:"err"`
	Net  *float64 `json:"net"`
	From *float64 `json:"from"`
	To   *float64 `json:"to"`
	Why  *string  `json:"why"`
}

// Add nimmt einen Schritt des Szenarios entgegen. Unlesbare Payloads, ein at
// mehr als 300 s in der Zukunft und Szenario-IDs ausserhalb von [a-z0-9_-]{1,64}
// werden abgelehnt.
func (l *StepLog) Add(scenario string, payload []byte) error {
	if !stepScenarioRE.MatchString(scenario) {
		return fmt.Errorf("szenario-id %q ungueltig", scenario)
	}
	var p stepPayloadJSON
	if err := json.Unmarshal(payload, &p); err != nil {
		return fmt.Errorf("schritt unlesbar: %w", err)
	}
	if p.At == nil || p.Err == nil || p.Net == nil || p.Why == nil {
		return errors.New("schritt unvollstaendig")
	}
	why, ok := stepWhy(*p.Why)
	if !ok {
		return fmt.Errorf("unbekannter grund %q", *p.Why)
	}
	now := l.now()
	if *p.At <= 0 || *p.At >= math.MaxUint32 || *p.At > float64(now.Add(stepFutureSlack).Unix()) {
		return fmt.Errorf("zeitstempel %v ausserhalb des erlaubten bereichs", *p.At)
	}
	st := Step{At: int64(*p.At), Err: float32(*p.Err), Net: float32(*p.Net), Why: why}
	if st.Err != st.Err || st.Net != st.Net || math.IsInf(float64(st.Err), 0) || math.IsInf(float64(st.Net), 0) {
		return errors.New("fehler oder netz nicht endlich")
	}
	st.From = optFloat(p.From)
	st.To = optFloat(p.To)
	if !finitePtr(st.From) || !finitePtr(st.To) {
		return errors.New("stellwert nicht endlich")
	}

	l.mu.Lock()
	defer l.mu.Unlock()
	l.steps[scenario] = append(l.steps[scenario], st)
	return nil
}

// Steps liefert die Schritte des Szenarios mit einem Zeitpunkt von from bis to,
// beide Enden einschliesslich, in Ankunftsreihenfolge.
func (l *StepLog) Steps(scenario string, from, to time.Time) []Step {
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []Step
	for _, st := range l.steps[scenario] {
		if st.At >= from.Unix() && st.At <= to.Unix() {
			out = append(out, st)
		}
	}
	return out
}

// Flush wirft die Schritte ab, die ausserhalb der Aufbewahrung liegen. Die
// Schreibstufe in die Datei am Ort kommt spaeter dazu.
func (l *StepLog) Flush(now time.Time) error {
	cutoff := l.cutoff(now)
	l.mu.Lock()
	defer l.mu.Unlock()
	for id, list := range l.steps {
		keep := list[:0]
		for _, st := range list {
			if st.At >= cutoff {
				keep = append(keep, st)
			}
		}
		if len(keep) == 0 {
			delete(l.steps, id)
		} else {
			l.steps[id] = keep
		}
	}
	return nil
}

// DayFigures rechnet die Tageskennzahlen je Szenario fuer den Tag day (JJJJ-MM-TT,
// Ortszeit) aus den Schritten im RAM.
func (l *StepLog) DayFigures(day string) map[string]*StepDay {
	start, err := time.ParseInLocation(dayLayout, day, time.Local)
	if err != nil {
		return nil
	}
	end := start.Add(24 * time.Hour)
	l.mu.Lock()
	defer l.mu.Unlock()
	out := map[string]*StepDay{}
	for id, list := range l.steps {
		var inDay []Step
		for _, st := range list {
			if st.At >= start.Unix() && st.At < end.Unix() {
				inDay = append(inDay, st)
			}
		}
		if len(inDay) == 0 {
			continue
		}
		sort.SliceStable(inDay, func(i, j int) bool { return inDay[i].At < inDay[j].At })
		out[id] = stepFigures(inDay)
	}
	return out
}

// cutoff ist der Anfang des ersten Tages, den der Ring behaelt.
func (l *StepLog) cutoff(now time.Time) int64 {
	n := max(1, l.days())
	local := now.In(time.Local)
	first := time.Date(local.Year(), local.Month(), local.Day()-(n-1), 0, 0, 0, 0, time.Local)
	return first.Unix()
}

// stepFigures rechnet die Kennzahlen eines Tages aus den nach Zeit sortierten Schritten.
func stepFigures(steps []Step) *StepDay {
	fig := &StepDay{N: len(steps)}
	var prevSign int
	var impWh, expWh float64
	for i, st := range steps {
		if st.From != nil && st.To != nil && *st.From != *st.To {
			fig.Changes++
		}
		if st.Err != 0 {
			sign := 1
			if st.Err < 0 {
				sign = -1
			}
			if prevSign != 0 && sign != prevSign {
				fig.SignFlips++
			}
			prevSign = sign
		}
		if st.Why == StepDeadband || i+1 >= len(steps) {
			continue
		}
		dt := float64(steps[i+1].At - st.At)
		wh := math.Abs(float64(st.Err)) * dt / 3600
		switch {
		case st.Net > 0:
			impWh += wh
		case st.Net < 0:
			expWh += wh
		}
	}
	fig.OutWhImport = float32(impWh)
	fig.OutWhExport = float32(expWh)
	return fig
}

func stepWhy(s string) (uint8, bool) {
	switch s {
	case "sent":
		return StepSent, true
	case "deadband":
		return StepDeadband, true
	case "unchanged":
		return StepUnchanged, true
	case "limit":
		return StepLimit, true
	}
	return 0, false
}

func optFloat(p *float64) *float32 {
	if p == nil {
		return nil
	}
	v := float32(*p)
	return &v
}

func finitePtr(p *float32) bool {
	return p == nil || (!math.IsNaN(float64(*p)) && !math.IsInf(float64(*p), 0))
}

// encodeStep schreibt einen Schritt als 24 Byte: Zeit als uint32, Fehler,
// Netz, Stellwert von und bis als float32 (NaN fehlt), Grund, drei Fuellbytes.
// Der Szenario-Schluessel gehoert nicht zum Satz.
func encodeStep(st Step) []byte {
	buf := make([]byte, stepRecordSize)
	binary.LittleEndian.PutUint32(buf[0:], uint32(st.At))
	binary.LittleEndian.PutUint32(buf[4:], math.Float32bits(st.Err))
	binary.LittleEndian.PutUint32(buf[8:], math.Float32bits(st.Net))
	binary.LittleEndian.PutUint32(buf[12:], math.Float32bits(ptrOrNaN(st.From)))
	binary.LittleEndian.PutUint32(buf[16:], math.Float32bits(ptrOrNaN(st.To)))
	buf[20] = st.Why
	return buf
}

// decodeStep liest einen Satz aus encodeStep. NaN wird wieder zu nil.
func decodeStep(buf []byte) Step {
	st := Step{
		At:   int64(binary.LittleEndian.Uint32(buf[0:])),
		Err:  math.Float32frombits(binary.LittleEndian.Uint32(buf[4:])),
		Net:  math.Float32frombits(binary.LittleEndian.Uint32(buf[8:])),
		From: nanToNil(math.Float32frombits(binary.LittleEndian.Uint32(buf[12:]))),
		To:   nanToNil(math.Float32frombits(binary.LittleEndian.Uint32(buf[16:]))),
		Why:  buf[20],
	}
	return st
}

func ptrOrNaN(p *float32) float32 {
	if p == nil {
		return float32(math.NaN())
	}
	return *p
}

func nanToNil(v float32) *float32 {
	if math.IsNaN(float64(v)) {
		return nil
	}
	return &v
}
