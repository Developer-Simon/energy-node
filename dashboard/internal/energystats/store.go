package energystats

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
)

// Store haelt die Monatsdateien <dir>/<jjjj-mm>.json. Monate werden erst
// beim ersten Zugriff geladen. Gecacht werden nur Monate, die eine Datei
// haben oder gerade beschrieben werden, damit Abfragen fremder Jahre den
// Speicher nicht fuellen.
type Store struct {
	dir    string
	mu     sync.Mutex
	months map[string]*Month
	// missing merkt Monate ohne Datei, damit eine Jahresabfrage nicht
	// 8760-mal vergeblich liest. Wird beim Anlegen des Monats geloescht.
	missing map[string]bool
	dirty   map[string]bool
	corrupt []string
	logf    func(format string, args ...any)
	// rev zaehlt jede Aenderung, die eine Abfrage anders ausfallen liesse.
	// Das SSE-Thema energy_stats traegt ihn, Kachel und Seite laden nur bei
	// einem neuen Wert nach.
	rev atomic.Uint64
}

func NewStore(dir string) *Store {
	return &Store{dir: dir, months: map[string]*Month{}, missing: map[string]bool{}, dirty: map[string]bool{}, logf: log.Printf}
}

// Revision ist der Stand der Daten. Er steigt bei jeder Aenderung und bleibt
// beim Lesen gleich.
func (s *Store) Revision() uint64 { return s.rev.Load() }

func (s *Store) bump() { s.rev.Add(1) }

func monthKey(start time.Time) string { return start.Format("2006-01") }

func (s *Store) path(key string) string { return filepath.Join(s.dir, key+".json") }

// monthLocked liefert den Monat zu t. create=false liefert nil fuer einen
// Monat ohne Datei. Ein Lesefehler, der nicht "fehlt" bedeutet, wird
// gemeldet: die Datei darf dann nicht ueberschrieben werden.
func (s *Store) monthLocked(t time.Time, create bool) (*Month, error) {
	start := monthStart(t)
	key := monthKey(start)
	if m, ok := s.months[key]; ok {
		return m, nil
	}
	if !create && s.missing[key] {
		return nil, nil
	}
	path := s.path(key)
	data, err := os.ReadFile(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		if !create {
			s.missing[key] = true
			return nil, nil
		}
		delete(s.missing, key)
		m := NewMonth(start)
		s.months[key] = m
		return m, nil
	case err != nil:
		return nil, fmt.Errorf("energystats: %s nicht lesbar: %w", path, err)
	}
	m, err := decodeMonth(data)
	if err == nil && !m.Start.Equal(start) {
		err = fmt.Errorf("Monat %s statt %s", m.Start.Format("2006-01"), key)
	}
	if err != nil {
		s.logf("energystats: %s ist beschaedigt und wird nach .corrupt verschoben: %v", path, err)
		if renameErr := os.Rename(path, path+".corrupt"); renameErr != nil {
			return nil, fmt.Errorf("energystats: %s nicht verschiebbar: %w", path, renameErr)
		}
		s.corrupt = append(s.corrupt, key)
		s.bump()
		m = NewMonth(start)
	}
	s.months[key] = m
	return m, nil
}

// AddOwn traegt eine eigene Stunde ein und schreibt alle geaenderten
// Monate. Scheitert das Schreiben, bleiben die Daten im Speicher und werden
// beim naechsten Aufruf erneut geschrieben.
func (s *Store) AddOwn(r HourRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, err := s.monthLocked(r.Hour, true)
	if err != nil {
		return err
	}
	if m.AddOwn(r.Hour, r.KWh, r.Covered) {
		s.dirty[monthKey(m.Start)] = true
		s.bump()
	}
	return s.writeDirtyLocked()
}

func (s *Store) writeDirtyLocked() error {
	var firstErr error
	for key := range s.dirty {
		if err := s.writeLocked(key, s.months[key]); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		delete(s.dirty, key)
	}
	return firstErr
}

// writeLocked schreibt ueber Temp-Datei und Rename, damit ein Stromausfall
// keine halbe Datei hinterlaesst (wie runtimecache).
func (s *Store) writeLocked(key string, m *Month) error {
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(m)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(s.dir, ".month-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	_, err = tmp.Write(data)
	if err == nil {
		err = tmp.Sync()
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(tmpName, s.path(key))
	}
	if err != nil {
		os.Remove(tmpName)
	}
	return err
}

// Each ruft fn fuer jede volle Stunde in [from, to) auf. m ist nil fuer
// Monate ohne Datei.
func (s *Store) Each(from, to time.Time, fn func(hour time.Time, m *Month, i int)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for hour := from.UTC().Truncate(time.Hour); hour.Before(to); hour = hour.Add(time.Hour) {
		m, err := s.monthLocked(hour, false)
		if err != nil {
			return err
		}
		i := 0
		if m != nil {
			i, _ = m.Index(hour)
		}
		fn(hour, m, i)
	}
	return nil
}

// HARecord ist eine Stunde aus Home Assistant fuer eine Rolle.
type HARecord struct {
	Hour time.Time
	Role string
	KWh  float64
}

// SetHA uebernimmt Stunden aus Home Assistant. Sie ersetzen eigene
// Messungen derselben Stunde. Geschrieben wird einmal fuer alle
// geaenderten Monate.
func (s *Store) SetHA(records []HARecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	changed := false
	for _, r := range records {
		m, err := s.monthLocked(r.Hour, true)
		if err != nil {
			return err
		}
		if m.SetHA(r.Hour, r.Role, r.KWh) {
			s.dirty[monthKey(m.Start)] = true
			changed = true
		}
	}
	if changed {
		s.bump()
	}
	return s.writeDirtyLocked()
}

// HADays zaehlt je UTC-Tag ab from die Stunden, die fuer role aus Home
// Assistant stammen. Grundlage des Abgleichs mit dem Angebot des Companion.
func (s *Store) HADays(role string, from time.Time, days int) ([]int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	counts := make([]int, days)
	start := from.UTC().Truncate(time.Hour)
	for d := 0; d < days; d++ {
		for h := 0; h < 24; h++ {
			hour := start.Add(time.Duration(d*24+h) * time.Hour)
			m, err := s.monthLocked(hour, false)
			if err != nil {
				return nil, err
			}
			if m == nil {
				continue
			}
			series := m.Roles[role]
			if series == nil {
				continue
			}
			if i, ok := m.Index(hour); ok && series.Source[i] == SourceHA {
				counts[d]++
			}
		}
	}
	return counts, nil
}

// SourceAt liefert die Quelle einer Stunde, SourceNone ohne Daten.
func (s *Store) SourceAt(role string, hour time.Time) (byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, err := s.monthLocked(hour, false)
	if err != nil || m == nil {
		return SourceNone, err
	}
	series := m.Roles[role]
	i, ok := m.Index(hour)
	if series == nil || !ok {
		return SourceNone, nil
	}
	return series.Source[i], nil
}

// Corrupt liefert die Monate, deren Datei seit dem Start beschaedigt war und
// beiseitegelegt wurde. Home Assistant liefert sie beim naechsten Abgleich
// erneut, die eigene Aufzeichnung dieses Monats ist verloren.
func (s *Store) Corrupt() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.corrupt...)
}
