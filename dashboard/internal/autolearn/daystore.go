package autolearn

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/learnrec"
)

const (
	dayFileVersion = 1
	dayQuarters    = 96
	dayCounterMax  = 15
	dayLayout      = "2006-01-02"
)

// siteKeys sind die Balance-Schluessel der Haus-Reihen. Die Reihenfolge passt
// zu SiteDay.series.
var siteKeys = [6]string{"load_total", "grid_import", "grid_export", "pv", "battery_charge", "battery_discharge"}

// DayFile ist eine Tagesdatei (Version 1). Sie wird gzip-komprimiert als JSON
// geschrieben.
type DayFile struct {
	V       int                   `json:"v"`
	Day     string                `json:"day"`
	Devices map[string]*DeviceDay `json:"devices"`
	Site    SiteDay               `json:"site"`
	Events  []EventRec            `json:"events"`
	Actions []ActionRec           `json:"actions"`
}

// DeviceDay sind die Viertelstunden eines Geraets. Zaehler saettigen bei 15.
type DeviceDay struct {
	Seen  [dayQuarters]uint8   `json:"seen"`
	Reach [dayQuarters]uint8   `json:"reach"`
	OnMin [dayQuarters]uint8   `json:"on_min"`
	AvgW  [dayQuarters]float32 `json:"avg_w"`
	Wh    [dayQuarters]float32 `json:"wh"`
	Hand  [dayQuarters]uint8   `json:"hand"`
	Ext   [dayQuarters]uint8   `json:"ext"`
	Rule  [dayQuarters]uint8   `json:"rule"`
}

// SiteDay sind die Haus-Reihen einer Tagesdatei.
type SiteDay struct {
	Seen             [dayQuarters]uint8   `json:"seen"`
	LoadTotal        [dayQuarters]float32 `json:"load_total"`
	GridImport       [dayQuarters]float32 `json:"grid_import"`
	GridExport       [dayQuarters]float32 `json:"grid_export"`
	PV               [dayQuarters]float32 `json:"pv"`
	BatteryCharge    [dayQuarters]float32 `json:"battery_charge"`
	BatteryDischarge [dayQuarters]float32 `json:"battery_discharge"`
}

// EventRec ist ein Schaltvorgang im Tagesprotokoll.
type EventRec struct {
	At      int64   `json:"at"`
	Device  string  `json:"d"`
	Entity  string  `json:"e"`
	On      bool    `json:"on"`
	Origin  string  `json:"o"`
	Rule    string  `json:"r"`
	House   float64 `json:"house"`
	Surplus float64 `json:"surplus"`
}

// ActionRec ist eine Regelaktion im Tagesprotokoll. Want fehlt bei toggle oder
// unbekanntem Payload.
type ActionRec struct {
	At       int64   `json:"at"`
	Rule     string  `json:"r"`
	Device   string  `json:"d"`
	Entity   string  `json:"e"`
	Want     *bool   `json:"want,omitempty"`
	There    bool    `json:"there"`
	OK       bool    `json:"ok"`
	Reaction float64 `json:"rt"`
}

// series liefert die Haus-Reihen in der Reihenfolge von siteKeys.
func (d *SiteDay) series() [6]*[dayQuarters]float32 {
	return [6]*[dayQuarters]float32{&d.LoadTotal, &d.GridImport, &d.GridExport, &d.PV, &d.BatteryCharge, &d.BatteryDischarge}
}

// DayStore sammelt je Viertelstunde die Profile der Geraete und des Hauses.
// Er ist sicher fuer gleichzeitigen Zugriff.
type DayStore struct {
	mu       sync.Mutex
	dir      string
	loc      *time.Location
	keepDays func() int
	logf     func(string, ...any)
	enabled  bool
	excluded map[string]bool

	day   string  // lokaler Tag der laufenden Datei
	file  DayFile // Zustand des laufenden Tages
	dirty bool
	// n sind die wahren Minutenzahlen je Geraet und Viertelstunde fuer die
	// Mittelwerte. Anders als Seen saettigen sie nicht bei 15.
	n     map[string]*[dayQuarters]uint16
	siteN [6][dayQuarters]uint16
}

// OpenDayStore liefert einen Speicher in dir. Die Tagesdatei von heute wird
// geladen, wenn sie existiert. Eine kaputte Datei wird geloggt und ignoriert.
// keepDays legt fest, wie viele Tage bleiben. Der Speicher ist anfangs aktiv.
func OpenDayStore(dir string, loc *time.Location, keepDays func() int, logf func(string, ...any)) *DayStore {
	if loc == nil {
		loc = time.Local
	}
	if logf == nil {
		logf = log.Printf
	}
	s := &DayStore{dir: dir, loc: loc, keepDays: keepDays, logf: logf, enabled: true,
		excluded: map[string]bool{}}
	s.startDayLocked(time.Now().In(loc).Format(dayLayout))
	return s
}

// SetEnabled schaltet das Sammeln ein oder aus. Ausgeschaltet nimmt
// ObserveMinute keine Rahmen mehr auf.
func (s *DayStore) SetEnabled(on bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.enabled = on
}

// SetExcluded legt die Geraete fest, die kuenftig nicht mehr gesammelt werden.
// Vorhandene Eintraege bleiben erhalten.
func (s *DayStore) SetExcluded(deviceIDs []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.excluded = make(map[string]bool, len(deviceIDs))
	for _, id := range deviceIDs {
		s.excluded[id] = true
	}
}

// ObserveMinute zaehlt einen abgeschlossenen Minutenrahmen in seine Viertelstunde
// der lokalen Wanduhr. Beim Tageswechsel wird der alte Tag zuerst geschrieben.
func (s *DayStore) ObserveMinute(f learnrec.Frame) {
	if f.Minute.IsZero() {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.enabled {
		return
	}
	t := f.Minute.In(s.loc)
	day := t.Format(dayLayout)
	if day != s.day {
		s.finishDayLocked()
		s.startDayLocked(day)
	}
	q := (t.Hour()*60 + t.Minute()) / 15
	for _, dm := range f.Devices {
		if s.excluded[dm.ID] {
			continue
		}
		s.observeDeviceLocked(dm, q)
	}
	s.observeSiteLocked(f.Balance, q)
	s.dirty = true
}

// Save schreibt die laufende Datei, wenn sich seit dem letzten Schreiben etwas
// geaendert hat.
func (s *DayStore) Save() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.writeLocked()
}

// Days liefert die Tagesdateien von from bis to (lokale Tage, beide eingeschlossen),
// sortiert. Fehlende Tage fehlen in der Liste. Der laufende Tag kommt aus dem Speicher.
func (s *DayStore) Days(from, to time.Time) ([]DayFile, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	first := from.In(s.loc)
	last := to.In(s.loc)
	start := time.Date(first.Year(), first.Month(), first.Day(), 0, 0, 0, 0, s.loc)
	end := time.Date(last.Year(), last.Month(), last.Day(), 0, 0, 0, 0, s.loc)
	var out []DayFile
	for d := start; !d.After(end); d = d.AddDate(0, 0, 1) {
		day := d.Format(dayLayout)
		if day == s.day {
			out = append(out, s.snapshotLocked())
			continue
		}
		f, err := s.readDay(day)
		if err != nil {
			s.logf("autolearn: %s unreadable, skipped: %v", s.path(day), err)
			continue
		}
		if f != nil {
			out = append(out, *f)
		}
	}
	return out, nil
}

// startDayLocked macht day zum laufenden Tag und laedt seine Datei, falls
// vorhanden. Eine kaputte Datei wird geloggt, der Tag beginnt dann leer.
func (s *DayStore) startDayLocked(day string) {
	s.day = day
	s.file = DayFile{V: dayFileVersion, Day: day, Devices: map[string]*DeviceDay{},
		Events: []EventRec{}, Actions: []ActionRec{}}
	s.n = map[string]*[dayQuarters]uint16{}
	s.siteN = [6][dayQuarters]uint16{}
	s.dirty = false

	f, err := s.readDay(day)
	if err != nil {
		s.logf("autolearn: %s unreadable, starting day %s empty: %v", s.path(day), day, err)
		return
	}
	if f == nil {
		return
	}
	s.file = *f
	if s.file.Devices == nil {
		s.file.Devices = map[string]*DeviceDay{}
	}
	for id, dd := range s.file.Devices {
		if dd == nil {
			dd = &DeviceDay{}
			s.file.Devices[id] = dd
		}
		n := new([dayQuarters]uint16)
		for q := range n {
			n[q] = uint16(dd.Seen[q])
		}
		s.n[id] = n
	}
	for i := range s.siteN {
		for q := range s.siteN[i] {
			s.siteN[i][q] = uint16(s.file.Site.Seen[q])
		}
	}
}

// finishDayLocked schreibt den laufenden Tag, bevor ein anderer beginnt.
func (s *DayStore) finishDayLocked() {
	if err := s.writeLocked(); err != nil {
		s.logf("autolearn: %v", err)
	}
}

func (s *DayStore) observeDeviceLocked(dm learnrec.DeviceMinute, q int) {
	dd := s.file.Devices[dm.ID]
	if dd == nil {
		dd = &DeviceDay{}
		s.file.Devices[dm.ID] = dd
	}
	n := s.n[dm.ID]
	if n == nil {
		n = new([dayQuarters]uint16)
		s.n[dm.ID] = n
	}
	var sum float64
	for _, w := range dm.Power {
		if finite(w) {
			sum += w
		}
	}
	addMean(&dd.AvgW[q], &n[q], sum)
	dd.Wh[q] += float32(sum / 60)
	bump(&dd.Seen[q])
	if dm.Reachable {
		bump(&dd.Reach[q])
	}
	for _, on := range dm.Switch {
		if on {
			bump(&dd.OnMin[q])
			break
		}
	}
}

func (s *DayStore) observeSiteLocked(balance map[string]float64, q int) {
	if len(balance) == 0 {
		return
	}
	bump(&s.file.Site.Seen[q])
	series := s.file.Site.series()
	for i, key := range siteKeys {
		v, ok := balance[key]
		if !ok || !finite(v) {
			continue
		}
		addMean(&series[i][q], &s.siteN[i][q], v)
	}
}

// writeLocked schreibt den laufenden Tag, wenn er geaendert wurde.
func (s *DayStore) writeLocked() error {
	if !s.dirty || s.dir == "" {
		return nil
	}
	if err := s.writeFile(s.day, s.snapshotLocked()); err != nil {
		return err
	}
	s.dirty = false
	return nil
}

// snapshotLocked liefert eine Kopie des laufenden Tages, auf 1 W und 0,1 Wh
// gerundet.
func (s *DayStore) snapshotLocked() DayFile {
	out := DayFile{V: dayFileVersion, Day: s.day, Devices: make(map[string]*DeviceDay, len(s.file.Devices)),
		Site: s.file.Site, Events: s.file.Events, Actions: s.file.Actions}
	for id, dd := range s.file.Devices {
		c := *dd
		for q := range c.AvgW {
			c.AvgW[q] = roundW(c.AvgW[q])
			c.Wh[q] = roundWh(c.Wh[q])
		}
		out.Devices[id] = &c
	}
	for _, ser := range out.Site.series() {
		for q := range ser {
			ser[q] = roundW(ser[q])
		}
	}
	return out
}

func (s *DayStore) path(day string) string {
	return filepath.Join(s.dir, day+".json.gz")
}

// readDay liest eine Tagesdatei. Eine fehlende Datei ergibt nil ohne Fehler.
func (s *DayStore) readDay(day string) (*DayFile, error) {
	raw, err := os.ReadFile(s.path(day))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	zr, err := gzip.NewReader(bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	data, err := io.ReadAll(zr)
	if err != nil {
		return nil, err
	}
	var f DayFile
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, err
	}
	if f.V != dayFileVersion {
		return nil, fmt.Errorf("version %d", f.V)
	}
	if f.Day != day {
		return nil, fmt.Errorf("day %q, expected %q", f.Day, day)
	}
	return &f, nil
}

// writeFile schreibt die Datei ueber eine Temp-Datei und benennt sie um.
func (s *DayStore) writeFile(day string, f DayFile) error {
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return fmt.Errorf("autolearn: create %s: %w", s.dir, err)
	}
	data, err := json.Marshal(f)
	if err != nil {
		return fmt.Errorf("autolearn: encode %s: %w", day, err)
	}
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write(data); err != nil {
		return fmt.Errorf("autolearn: compress %s: %w", day, err)
	}
	if err := zw.Close(); err != nil {
		return fmt.Errorf("autolearn: compress %s: %w", day, err)
	}
	tmp, err := os.CreateTemp(s.dir, ".day-*.tmp")
	if err != nil {
		return fmt.Errorf("autolearn: save %s: %w", day, err)
	}
	tmpName := tmp.Name()
	_, err = tmp.Write(buf.Bytes())
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmpName, s.path(day))
	}
	if err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("autolearn: save %s: %w", day, err)
	}
	return nil
}

// addMean fuegt x dem Mittel avg mit n Minuten hinzu und zaehlt n hoch.
func addMean(avg *float32, n *uint16, x float64) {
	v := (float64(*avg)*float64(*n) + x) / float64(*n+1)
	*avg = float32(v)
	*n++
}

// bump zaehlt einen Zaehler hoch, bis er bei dayCounterMax saettigt.
func bump(c *uint8) {
	if *c < dayCounterMax {
		*c++
	}
}

func finite(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0)
}

func roundW(v float32) float32 {
	return float32(math.Round(float64(v)))
}

func roundWh(v float32) float32 {
	return float32(math.Round(float64(v)*10) / 10)
}
