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
	logf    func(format string, args ...any)
}

func NewStore(dir string) *Store {
	return &Store{dir: dir, months: map[string]*Month{}, missing: map[string]bool{}, dirty: map[string]bool{}, logf: log.Printf}
}

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
