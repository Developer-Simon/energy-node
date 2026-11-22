package learnrec

import (
	"errors"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// MinuteDirs sind die Basisverzeichnisse der Laufwerke. Leer heisst nicht verfuegbar.
type MinuteDirs struct {
	Drive  string
	System string
}

// MinuteUsage beschreibt den Stand der Minutenaufzeichnung. Bytes sind die seit
// Since geschriebenen Bytes. Since ist der Zeitpunkt der ersten Minute in Unix-Sekunden.
type MinuteUsage struct {
	Bytes         int64
	WrittenPerDay int64
	Since         int64
	Keys          int
	RAMDays       int
	FileDays      int
	Location      MinuteLocation
	DriveDown     bool
}

const minuteDayLayout = "2006-01-02"

// minuteDay ist ein Tag im RAM. observed sind die Minuten mit Werten, pending
// die, die noch nicht in der Datei des Ziels (target) stehen.
type minuteDay struct {
	values   map[string]*[minutesPerDay]float32
	observed [minutesPerDay]bool
	pending  [minutesPerDay]bool
	target   string
	onDisk   bool
}

// minuteFileState ist die zwischengespeicherte Lage einer Tagesdatei.
type minuteFileState struct {
	known map[string]int
	have  [minutesPerDay]bool
}

type minuteJob struct {
	day  string
	path string
	d    *minuteDay
}

type minuteRun struct {
	block MinuteBlock
	count int
}

// MinuteStore sammelt die Minutenwerte im RAM und schreibt sie beim Flush an
// den gewaehlten Ort. Der Minutenweg beruehrt nur das RAM. Dateien entstehen
// nur in Flush und Reset, ausserhalb des Hub-Mutex.
type MinuteStore struct {
	flushMu sync.Mutex // serialisiert Flush und Reset, schuetzt files
	loc     *time.Location
	plan    func() MinutePlan
	dirs    func() MinuteDirs
	driveUp func() bool
	limits  func() Limits
	logf    func(string, ...any)
	files   map[string]*minuteFileState

	mu      sync.Mutex // schuetzt die Felder darunter
	days    map[string]*minuteDay
	keys    map[string]bool
	since   time.Time
	written int64
	lastNow time.Time
	curKey  string
	curDir  string
	prevDir string
}

var _ MinuteConsumer = (*MinuteStore)(nil)

// NewMinuteStore legt einen Store an. plan, dirs, driveUp und limits werden bei
// jedem Aufruf gelesen.
func NewMinuteStore(loc *time.Location, plan func() MinutePlan, dirs func() MinuteDirs, driveUp func() bool, limits func() Limits, logf func(string, ...any)) *MinuteStore {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	return &MinuteStore{
		loc:     loc,
		plan:    plan,
		dirs:    dirs,
		driveUp: driveUp,
		limits:  limits,
		logf:    logf,
		files:   map[string]*minuteFileState{},
		days:    map[string]*minuteDay{},
		keys:    map[string]bool{},
	}
}

// ObserveMinute legt die Werte des Rahmens im RAM ab. Schluessel sind e:<unique_id>
// fuer Leistung, b:<feld> fuer die Bilanz und t:outdoor fuer die Aussentemperatur.
func (s *MinuteStore) ObserveMinute(f Frame) {
	if !s.plan().On {
		return
	}
	vals := map[string]float64{}
	for _, dev := range f.Devices {
		for uid, w := range dev.Power {
			vals["e:"+uid] = w
		}
	}
	for field, v := range f.Balance {
		vals["b:"+field] = v
	}
	if f.OutdoorC != nil {
		vals["t:outdoor"] = *f.OutdoorC
	}
	if len(vals) == 0 {
		return
	}

	local := f.Minute.In(s.loc)
	day := local.Format(minuteDayLayout)
	m := local.Hour()*60 + local.Minute()

	s.mu.Lock()
	defer s.mu.Unlock()
	d := s.days[day]
	if d == nil {
		d = &minuteDay{values: map[string]*[minutesPerDay]float32{}}
		s.days[day] = d
	}
	for key, v := range vals {
		arr := d.values[key]
		if arr == nil {
			arr = new([minutesPerDay]float32)
			for i := range arr {
				arr[i] = minuteGap
			}
			d.values[key] = arr
			s.keys[key] = true
		}
		arr[m] = float32(v)
	}
	d.observed[m] = true
	d.pending[m] = true
	if s.since.IsZero() {
		s.since = local
	}
}

// place liefert den Schluessel des Orts, das Basisverzeichnis und ob der Ort
// jetzt beschreibbar ist. Der Ort ram hat kein Verzeichnis.
func (s *MinuteStore) place(pl MinutePlan, dirs MinuteDirs, up bool) (key, dir string, writable bool) {
	switch pl.Location {
	case LocDrive:
		dir = dirs.Drive
		writable = dir != "" && up
	case LocSystem:
		dir = dirs.System
		writable = dir != ""
	default:
		return "ram|", "", false
	}
	return string(pl.Location) + "|" + dir, dir, writable
}

// Flush schreibt die ausstehenden Minuten an den Ort. Ist das Laufwerk nicht
// da, bleibt alles im RAM. Danach kommen Tage aus dem RAM, die aelter als die
// Aufbewahrung sind oder ueber die RAM-Tage hinausgehen.
func (s *MinuteStore) Flush(now time.Time) error {
	s.flushMu.Lock()
	defer s.flushMu.Unlock()

	pl := s.plan()
	dirs := s.dirs()
	up := s.driveUp()
	lim := s.limits()
	key, dir, writable := s.place(pl, dirs, up)
	cutoff := now.In(s.loc).AddDate(0, 0, -pl.Days).Format(minuteDayLayout)

	s.mu.Lock()
	s.lastNow = now
	if key != s.curKey {
		s.curKey = key
		if s.curDir != "" && s.curDir != dir {
			s.prevDir = s.curDir
		}
		s.curDir = dir
		s.files = map[string]*minuteFileState{}
	}
	if !writable {
		s.files = map[string]*minuteFileState{}
	}

	// RAM-Tage: aelter als die Aufbewahrung oder hinter den neuesten RAM-Tagen.
	keep := RAMDays(lim, int64(len(s.keys))*minutesPerDay*4)
	names := make([]string, 0, len(s.days))
	for k := range s.days {
		names = append(names, k)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(names)))
	for i, k := range names {
		if i >= keep || k <= cutoff {
			delete(s.days, k)
		}
	}

	days := make([]string, 0, len(s.days))
	for k := range s.days {
		days = append(days, k)
	}
	sort.Strings(days)
	var jobs []minuteJob
	for _, k := range days {
		d := s.days[k]
		path := ""
		if writable {
			path = minuteFileName(dir, k)
		}
		tgt := key + "|" + path
		if d.target != tgt {
			d.target = tgt
			d.pending = d.observed
		}
		if path != "" {
			jobs = append(jobs, minuteJob{day: k, path: path, d: d})
		}
	}
	s.mu.Unlock()

	if !writable {
		return nil
	}
	var errs []error
	for _, j := range jobs {
		if err := s.writeDay(j); err != nil {
			errs = append(errs, err)
		}
	}
	if err := s.pruneFiles(dir, cutoff); err != nil {
		errs = append(errs, err)
	}
	for _, err := range errs {
		s.logf("learnrec: Minuten schreiben fehlgeschlagen: %v", err)
	}
	return errors.Join(errs...)
}

// writeDay haengt die ausstehenden Minuten eines Tages an seine Datei an. Die
// Minuten, die die Datei schon hat, werden uebersprungen.
func (s *MinuteStore) writeDay(j minuteJob) error {
	st, err := s.fileState(j.path)
	if err != nil {
		return err
	}
	runs := s.pendingRuns(j.d, st)
	if len(runs) == 0 {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(j.path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(j.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	for _, r := range runs {
		n, err := AppendBlock(f, j.day, r.block, st.known)
		if err != nil {
			delete(s.files, j.path)
			return err
		}
		s.mu.Lock()
		s.written += n
		j.d.onDisk = true
		for m := r.block.First; m < r.block.First+r.count; m++ {
			j.d.pending[m] = false
			st.have[m] = true
		}
		s.mu.Unlock()
	}
	return nil
}

// pendingRuns sammelt die ausstehenden Minuten zu zusammenhaengenden Bloecken.
// Minuten, die die Datei schon hat, gelten als erledigt.
func (s *MinuteStore) pendingRuns(d *minuteDay, st *minuteFileState) []minuteRun {
	s.mu.Lock()
	defer s.mu.Unlock()
	var runs []minuteRun
	for m := 0; m < minutesPerDay; {
		if !d.pending[m] {
			m++
			continue
		}
		if st.have[m] {
			d.pending[m] = false
			m++
			continue
		}
		start := m
		for m < minutesPerDay && d.pending[m] && !st.have[m] {
			m++
		}
		count := m - start
		b := MinuteBlock{First: start}
		for key, arr := range d.values {
			vals := make([]float32, count)
			hasValue := false
			for i := range vals {
				vals[i] = arr[start+i]
				if !math.IsNaN(float64(vals[i])) {
					hasValue = true
				}
			}
			if hasValue {
				b.Keys = append(b.Keys, key)
				b.Values = append(b.Values, vals)
			}
		}
		if len(b.Keys) == 0 {
			for i := start; i < m; i++ {
				d.pending[i] = false
			}
			continue
		}
		runs = append(runs, minuteRun{block: b, count: count})
	}
	return runs
}

// fileState liefert die Lage der Datei aus dem Cache oder liest sie ein. Die
// Datei wird dabei auf ihren letzten gueltigen Block gekuerzt.
func (s *MinuteStore) fileState(path string) (*minuteFileState, error) {
	if st := s.files[path]; st != nil {
		return st, nil
	}
	known, _, err := OpenAppend(path)
	if err != nil {
		return nil, err
	}
	st := &minuteFileState{known: known}
	if len(known) > 0 {
		f, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		_, values, err := ReadDay(f)
		f.Close()
		if err != nil {
			return nil, err
		}
		for _, arr := range values {
			for m := range arr {
				if !math.IsNaN(float64(arr[m])) {
					st.have[m] = true
				}
			}
		}
	}
	s.files[path] = st
	return st, nil
}

// pruneFiles loescht die Tagesdateien im Verzeichnis dir, deren Tag nicht nach cutoff liegt.
func (s *MinuteStore) pruneFiles(dir, cutoff string) error {
	sub := minuteDirOf(dir)
	entries, err := os.ReadDir(sub)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var errs []error
	for _, e := range entries {
		day, ok := strings.CutSuffix(e.Name(), ".enm")
		if !ok || e.IsDir() || !isMinuteDay(day) || day > cutoff {
			continue
		}
		path := filepath.Join(sub, e.Name())
		delete(s.files, path)
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// Usage liefert den Stand der Aufzeichnung.
func (s *MinuteStore) Usage() MinuteUsage {
	pl := s.plan()
	up := s.driveUp()

	s.mu.Lock()
	defer s.mu.Unlock()
	u := MinuteUsage{
		Bytes:     s.written,
		Keys:      len(s.keys),
		RAMDays:   len(s.days),
		Location:  pl.Location,
		DriveDown: pl.Location == LocDrive && !up,
	}
	if !s.since.IsZero() {
		u.Since = s.since.Unix()
	}
	days := 1.0
	if !s.since.IsZero() && s.lastNow.After(s.since) {
		if d := s.lastNow.Sub(s.since).Hours() / 24; d > days {
			days = d
		}
	}
	u.WrittenPerDay = int64(float64(s.written) / days)
	for _, d := range s.days {
		if d.onDisk {
			u.FileDays++
		}
	}
	return u
}

// Reset leert das RAM und loescht die Tagesdateien des aktuellen und des vorigen Orts.
func (s *MinuteStore) Reset() error {
	s.flushMu.Lock()
	defer s.flushMu.Unlock()

	_, placeDir, _ := s.place(s.plan(), s.dirs(), true)

	s.mu.Lock()
	dirSet := map[string]bool{}
	for _, d := range []string{s.curDir, s.prevDir, placeDir} {
		if d != "" {
			dirSet[d] = true
		}
	}
	s.days = map[string]*minuteDay{}
	s.keys = map[string]bool{}
	s.written = 0
	s.since = time.Time{}
	s.lastNow = time.Time{}
	s.prevDir = ""
	s.files = map[string]*minuteFileState{}
	s.mu.Unlock()

	var errs []error
	for d := range dirSet {
		sub := minuteDirOf(d)
		entries, err := os.ReadDir(sub)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			errs = append(errs, err)
			continue
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".enm") {
				continue
			}
			if err := os.Remove(filepath.Join(sub, e.Name())); err != nil && !errors.Is(err, os.ErrNotExist) {
				errs = append(errs, err)
			}
		}
	}
	return errors.Join(errs...)
}

// Day liefert die Werte eines Tages, aus dem RAM oder sonst aus der Datei am Ort.
// Die Werte sind Kopien. Luecken sind NaN.
func (s *MinuteStore) Day(day string) (map[string]*[minutesPerDay]float32, bool) {
	if !isMinuteDay(day) {
		return nil, false
	}
	s.mu.Lock()
	if d := s.days[day]; d != nil {
		out := make(map[string]*[minutesPerDay]float32, len(d.values))
		for k, arr := range d.values {
			c := *arr
			out[k] = &c
		}
		s.mu.Unlock()
		return out, true
	}
	s.mu.Unlock()

	_, dir, writable := s.place(s.plan(), s.dirs(), s.driveUp())
	if !writable {
		return nil, false
	}
	f, err := os.Open(minuteFileName(dir, day))
	if err != nil {
		return nil, false
	}
	defer f.Close()
	_, values, err := ReadDay(f)
	if err != nil {
		return nil, false
	}
	return values, true
}

func minuteDirOf(base string) string {
	return filepath.Join(base, "learnrec", "minutes")
}

func minuteFileName(base, day string) string {
	return filepath.Join(minuteDirOf(base), day+".enm")
}

func isMinuteDay(day string) bool {
	_, err := time.Parse(minuteDayLayout, day)
	return err == nil
}
