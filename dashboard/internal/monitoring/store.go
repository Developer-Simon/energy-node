package monitoring

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// absentGrace ist die Zeit, die eine fehlende ID im Speicher bleibt.
const absentGrace = time.Hour

// ErrUnknown meldet eine ID, die gerade keine Meldung ist.
var ErrUnknown = errors.New("monitoring: unknown notice")

// entry ist der gespeicherte Zustand einer ID.
type entry struct {
	FirstSeen int64 `json:"first_seen"`
	LastSeen  int64 `json:"last_seen"`
	Ack       *Ack  `json:"ack,omitempty"`
}

// storeFile ist das Dateiformat des Speichers.
type storeFile struct {
	Version int               `json:"version"`
	Entries map[string]*entry `json:"entries"`
}

// Store merkt je Meldung das Erstsehen und die Quittung. Er ist sicher für
// parallelen Zugriff.
type Store struct {
	mu      sync.Mutex
	path    string
	entries map[string]*entry
	present map[string]bool // IDs aus dem letzten Apply
	saved   []byte          // zuletzt geschriebener oder geladener Inhalt
	logf    func(string, ...any)
}

// OpenStore lädt den Speicher aus path. Fehlt die Datei oder ist sie kaputt,
// startet der Speicher leer. Ein leerer Pfad hält den Speicher nur im Speicher.
func OpenStore(path string, logf func(string, ...any)) *Store {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	s := &Store{
		path:    path,
		entries: map[string]*entry{},
		present: map[string]bool{},
		logf:    logf,
	}
	if path == "" {
		return s
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			logf("monitoring: store not read: %v", err)
		}
		return s
	}
	var f storeFile
	if err := json.Unmarshal(data, &f); err != nil {
		logf("monitoring: store broken, starting empty: %v", err)
		return s
	}
	if f.Entries != nil {
		s.entries = f.Entries
	}
	s.saved = data
	return s
}

// Apply setzt FirstSeen und Ack in notices (an Ort und Stelle), merkt neue
// IDs, frischt LastSeen auf und entfernt IDs, die länger als absentGrace fehlen.
func (s *Store) Apply(notices []Notice, now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	nowUnix := now.Unix()
	s.present = map[string]bool{}
	for i := range notices {
		n := &notices[i]
		s.present[n.ID] = true
		e, ok := s.entries[n.ID]
		if !ok {
			e = &entry{FirstSeen: nowUnix}
			s.entries[n.ID] = e
		}
		if !n.Since.IsZero() && n.Since.Before(now) {
			if since := n.Since.Unix(); since < e.FirstSeen {
				e.FirstSeen = since
			}
		}
		e.LastSeen = nowUnix
		n.FirstSeen = e.FirstSeen
		n.Ack = nil
		if e.Ack != nil {
			ack := *e.Ack
			n.Ack = &ack
		}
	}
	for id, e := range s.entries {
		if s.present[id] {
			continue
		}
		if now.Sub(time.Unix(e.LastSeen, 0)) > absentGrace {
			delete(s.entries, id)
		}
	}
}

// SetAck quittiert (ack true) oder nimmt zurück. Nur für IDs, die beim
// letzten Apply da waren, sonst ErrUnknown.
func (s *Store) SetAck(id string, ack bool, user string, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.present[id] {
		return ErrUnknown
	}
	e, ok := s.entries[id]
	if !ok {
		return ErrUnknown
	}
	if ack {
		e.Ack = &Ack{At: now.Unix(), User: user}
	} else {
		e.Ack = nil
	}
	return nil
}

// Save schreibt atomar (Temp-Datei, Rename), nur bei Änderung.
func (s *Store) Save() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.path == "" {
		return nil
	}
	if len(s.entries) == 0 && s.saved == nil {
		return nil
	}
	data, err := json.MarshalIndent(storeFile{Version: 1, Entries: s.entries}, "", "  ")
	if err != nil {
		return fmt.Errorf("monitoring: store could not be encoded: %w", err)
	}
	if bytes.Equal(data, s.saved) {
		return nil
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.path), ".monitoring-*.tmp")
	if err != nil {
		return fmt.Errorf("monitoring: save store: %w", err)
	}
	tmpName := tmp.Name()
	_, err = tmp.Write(data)
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmpName, s.path)
	}
	if err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("monitoring: save store: %w", err)
	}
	s.saved = data
	return nil
}
