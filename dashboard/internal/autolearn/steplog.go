package autolearn

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
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

// stepGapCap begrenzt den Abstand zum naechsten Schritt bei der Energierechnung,
// damit eine Luecke im Schrittstrom nicht als Dauerbetrieb zaehlt.
const stepGapCap = 300

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

// StepDay sind die Tageskennzahlen eines Szenarios. Schritte mit dem Grund
// deadband zaehlen nicht. OutWhImport und OutWhExport sind die Energie
// ausserhalb der Totzone, Fehler mal Abstand zum naechsten Schritt, hoechstens
// 300 s. Netz ueber null zaehlt als Bezug, unter null als Einspeisung.
type StepDay struct {
	N           int     `json:"n"`
	Changes     int     `json:"changes"`
	SignFlips   int     `json:"flips"`
	OutWhImport float32 `json:"out_imp"`
	OutWhExport float32 `json:"out_exp"`
}

// StepLog haelt die Reglerschritte je Szenario im RAM. Die Aufbewahrung folgt
// days. dir liefert das Basisverzeichnis: dort liegt je Szenario und Tag die
// Datei learnrec/minutes/JJJJ-MM-TT.<szenario>.steps. Leer heisst nur RAM.
type StepLog struct {
	days func() int
	dir  func() string
	now  func() time.Time

	flushMu sync.Mutex // serialisiert Flush, damit Schreiben und Zaehler zusammenpassen

	mu      sync.Mutex // schuetzt die Felder darunter
	steps   map[string][]Step
	written map[string]int // je Szenario: die ersten so vielen Schritte stehen in der Datei
}

// NewStepLog legt einen leeren Schrittspeicher an. days und dir werden bei
// jedem Aufruf gelesen.
func NewStepLog(days func() int, dir func() string) *StepLog {
	return &StepLog{days: days, dir: dir, now: time.Now,
		steps: map[string][]Step{}, written: map[string]int{}}
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
// beide Enden einschliesslich. Aeltere Tage liest es aus den Dateien am Ort,
// die juengeren aus dem RAM. Ohne Ort zaehlt nur das RAM.
func (l *StepLog) Steps(scenario string, from, to time.Time) []Step {
	cutoff := l.cutoff(l.now())
	dir := l.dir()
	var out []Step
	if dir != "" && from.Unix() < cutoff {
		out = l.readFiles(dir, scenario, from, to, cutoff)
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, st := range l.steps[scenario] {
		if st.At < from.Unix() || st.At > to.Unix() {
			continue
		}
		if dir != "" && st.At < cutoff {
			continue // steht in der Datei oder ist schon verfallen
		}
		out = append(out, st)
	}
	return out
}

// Flush haengt die neuen Schritte an die Dateien am Ort an und wirft die
// Schritte ab, die ausserhalb der Aufbewahrung liegen. Ein Schritt steht nach
// einem Flush genau einmal in der Datei, auch wenn Flush wiederholt laeuft.
// Ohne Ort passiert nur das Abwerfen im RAM.
func (l *StepLog) Flush(now time.Time) error {
	l.flushMu.Lock()
	defer l.flushMu.Unlock()
	cutoff := l.cutoff(now)
	dir := l.dir()

	type job struct {
		id    string
		steps []Step
	}
	var jobs []job
	l.mu.Lock()
	for id, list := range l.steps {
		w := l.written[id]
		keep := list[:0]
		dropped := 0
		for i, st := range list {
			if st.At < cutoff {
				if i < w {
					dropped++
				}
				continue
			}
			keep = append(keep, st)
		}
		w -= dropped
		if len(keep) == 0 {
			delete(l.steps, id)
			delete(l.written, id)
			continue
		}
		l.steps[id] = keep
		l.written[id] = w
		if dir != "" && w < len(keep) {
			jobs = append(jobs, job{id: id, steps: append([]Step(nil), keep[w:]...)})
		}
	}
	l.mu.Unlock()

	if dir == "" {
		return nil
	}
	var errs []error
	for _, j := range jobs {
		n, err := appendSteps(dir, j.id, j.steps)
		l.mu.Lock()
		if _, ok := l.steps[j.id]; ok {
			l.written[j.id] += n
		}
		l.mu.Unlock()
		if err != nil {
			errs = append(errs, err)
		}
	}
	if err := pruneStepFiles(dir, time.Unix(cutoff, 0).In(time.Local).Format(dayLayout)); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
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
		sort.SliceStable(inDay, func(i, j int) bool { return inDay[i].At < inDay[j].At })
		if fig := stepFigures(inDay); fig.N > 0 {
			out[id] = fig
		}
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

// readFiles liest die Schritte des Szenarios aus den Tagesdateien vor dem
// Anfang des Rings, im Bereich von from bis to.
func (l *StepLog) readFiles(dir, scenario string, from, to time.Time, cutoff int64) []Step {
	var out []Step
	first := time.Unix(from.Unix(), 0).In(time.Local)
	day := time.Date(first.Year(), first.Month(), first.Day(), 0, 0, 0, 0, time.Local)
	last := time.Unix(min(to.Unix(), cutoff-1), 0).In(time.Local)
	for ; !day.After(last); day = day.AddDate(0, 0, 1) {
		raw, err := os.ReadFile(stepFileName(dir, day.Format(dayLayout), scenario))
		if err != nil {
			continue
		}
		for off := 0; off+stepRecordSize <= len(raw); off += stepRecordSize {
			st := decodeStep(raw[off : off+stepRecordSize])
			if st.At >= from.Unix() && st.At <= to.Unix() && st.At < cutoff {
				out = append(out, st)
			}
		}
	}
	return out
}

// appendSteps haengt die Schritte an ihre Tagesdateien an, Lauf fuer Lauf. Die
// Zahl der geschriebenen Schritte kommt zurueck, auch bei einem Fehler.
func appendSteps(dir, scenario string, steps []Step) (int, error) {
	done := 0
	for start := 0; start < len(steps); {
		day := time.Unix(steps[start].At, 0).In(time.Local).Format(dayLayout)
		end := start
		for end < len(steps) && time.Unix(steps[end].At, 0).In(time.Local).Format(dayLayout) == day {
			end++
		}
		if err := appendStepFile(stepFileName(dir, day, scenario), steps[start:end]); err != nil {
			return done, err
		}
		done += end - start
		start = end
	}
	return done, nil
}

func appendStepFile(path string, steps []Step) error {
	buf := make([]byte, 0, len(steps)*stepRecordSize)
	for _, st := range steps {
		buf = append(buf, encodeStep(st)...)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.Write(buf); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// Reset leert den Speicher und loescht die Schrittdateien am aktuellen Ort und
// in den extra Basisverzeichnissen, etwa dem Ort vor einem Wechsel.
func (l *StepLog) Reset(extra ...string) error {
	l.flushMu.Lock()
	defer l.flushMu.Unlock()
	l.mu.Lock()
	l.steps = map[string][]Step{}
	l.written = map[string]int{}
	l.mu.Unlock()

	var errs []error
	seen := map[string]bool{}
	for _, dir := range append([]string{l.dir()}, extra...) {
		if dir == "" || seen[dir] {
			continue
		}
		seen[dir] = true
		if err := removeStepFiles(dir); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// removeStepFiles loescht alle Schrittdateien unter dir.
func removeStepFiles(dir string) error {
	sub := filepath.Join(dir, "learnrec", "minutes")
	entries, err := os.ReadDir(sub)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var errs []error
	for _, e := range entries {
		if _, ok := strings.CutSuffix(e.Name(), ".steps"); !ok || e.IsDir() {
			continue
		}
		if err := os.Remove(filepath.Join(sub, e.Name())); err != nil && !errors.Is(err, os.ErrNotExist) {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// pruneStepFiles loescht die Schrittdateien, deren Tag vor cutoffDay liegt.
func pruneStepFiles(dir, cutoffDay string) error {
	sub := filepath.Join(dir, "learnrec", "minutes")
	entries, err := os.ReadDir(sub)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var errs []error
	for _, e := range entries {
		name, ok := strings.CutSuffix(e.Name(), ".steps")
		if !ok || e.IsDir() {
			continue
		}
		day, _, ok := strings.Cut(name, ".")
		if !ok || len(day) != len(dayLayout) || day >= cutoffDay {
			continue
		}
		if err := os.Remove(filepath.Join(sub, e.Name())); err != nil && !errors.Is(err, os.ErrNotExist) {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// stepFileName ist die Schrittdatei eines Szenarios an einem Tag.
func stepFileName(dir, day, scenario string) string {
	return filepath.Join(dir, "learnrec", "minutes", day+"."+scenario+".steps")
}

// stepFigures rechnet die Kennzahlen eines Tages aus den nach Zeit sortierten
// Schritten. Schritte mit dem Grund deadband fallen weg, bevor gezaehlt wird.
func stepFigures(all []Step) *StepDay {
	var steps []Step
	for _, st := range all {
		if st.Why != StepDeadband {
			steps = append(steps, st)
		}
	}
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
		if i+1 >= len(steps) {
			continue
		}
		dt := min(float64(steps[i+1].At-st.At), stepGapCap)
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

// encodeStep schreibt einen Schritt als 24 Byte little endian: Zeit als uint32,
// Fehler, Netz, Stellwert von und bis als float32 (NaN fehlt), Grund, drei
// Fuellbytes. Der Szenario-Schluessel steckt im Dateinamen.
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
	return Step{
		At:   int64(binary.LittleEndian.Uint32(buf[0:])),
		Err:  math.Float32frombits(binary.LittleEndian.Uint32(buf[4:])),
		Net:  math.Float32frombits(binary.LittleEndian.Uint32(buf[8:])),
		From: nanToNil(math.Float32frombits(binary.LittleEndian.Uint32(buf[12:]))),
		To:   nanToNil(math.Float32frombits(binary.LittleEndian.Uint32(buf[16:]))),
		Why:  buf[20],
	}
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
