// Package onboarding speichert den Fortschritt der Einrichtungsführung: welche
// Schritte der Installation erledigt sind und welche Nutzer die Begrüßung schon
// gesehen haben. Die Datei onboarding.json liegt im Datenverzeichnis neben
// settings.json und wird wie dort atomar geschrieben.
package onboarding

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
)

// StepState beschreibt, wie ein Einrichtungsschritt steht.
type StepState string

const (
	StepDone      StepState = "done"
	StepLater     StepState = "later"
	StepInstaller StepState = "installer"
)

// Run ist ein laufender Durchgang der Führung für einen Nutzer.
type Run struct {
	Mode  string `json:"mode"`
	From  string `json:"from"`
	Index int    `json:"index"`
}

// UserProgress hält den Fortschritt eines einzelnen Nutzers.
type UserProgress struct {
	Seen     []string `json:"seen"`
	Welcomed bool     `json:"welcomed"`
	Run      *Run     `json:"run"`
	Queue    []string `json:"queue"`
}

// Progress ist der gesamte Fortschritt einer Installation.
type Progress struct {
	SetupDone  bool                    `json:"setup_done"`
	SetupSteps map[string]StepState    `json:"setup_steps"`
	Users      map[string]UserProgress `json:"users"`
}

const fileName = "onboarding.json"

// Store liest und schreibt onboarding.json. Alle Zugriffe laufen über mu, damit
// parallele Änderungen einander nicht überschreiben.
type Store struct {
	mu  sync.Mutex
	dir string
}

func NewStore(dir string) *Store { return &Store{dir: filepath.Clean(dir)} }

// Load liefert den gespeicherten Fortschritt. Eine fehlende Datei ergibt
// leeren Fortschritt, eine kaputte oder nicht lesbare Datei ebenfalls, dazu
// eine Warnung im Log. Load gibt nie einen Fehler zurück.
func (s *Store) Load() Progress {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loadLocked()
}

// SetStep setzt den Zustand eines Einrichtungsschritts.
func (s *Store) SetStep(id string, state StepState) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	progress := s.loadLocked()
	progress.SetupSteps[id] = state
	return s.saveLocked(progress)
}

// CompleteSetup merkt sich, dass die Einrichtung dieser Installation fertig ist.
func (s *Store) CompleteSetup() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	progress := s.loadLocked()
	progress.SetupDone = true
	return s.saveLocked(progress)
}

// UpdateUser ändert den Fortschritt eines Nutzers. Andere Nutzer bleiben
// unberührt.
func (s *Store) UpdateUser(user string, mutate func(*UserProgress)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	progress := s.loadLocked()
	entry := progress.Users[user]
	mutate(&entry)
	progress.Users[user] = entry
	return s.saveLocked(progress)
}

func (s *Store) loadLocked() Progress {
	empty := Progress{SetupSteps: map[string]StepState{}, Users: map[string]UserProgress{}}
	data, err := os.ReadFile(filepath.Join(s.dir, fileName))
	if errors.Is(err, os.ErrNotExist) {
		return empty
	}
	if err != nil {
		log.Printf("onboarding: %s nicht lesbar, Fortschritt bleibt leer: %v", fileName, err)
		return empty
	}
	var loaded Progress
	if err := json.Unmarshal(data, &loaded); err != nil {
		log.Printf("onboarding: %s ist kein gültiges JSON, Fortschritt bleibt leer: %v", fileName, err)
		return empty
	}
	if loaded.SetupSteps == nil {
		loaded.SetupSteps = map[string]StepState{}
	}
	if loaded.Users == nil {
		loaded.Users = map[string]UserProgress{}
	}
	return loaded
}

func (s *Store) saveLocked(progress Progress) error {
	data, err := json.MarshalIndent(progress, "", "  ")
	if err != nil {
		return fmt.Errorf("onboarding: %w", err)
	}
	data = append(data, '\n')
	return atomicWrite(filepath.Join(s.dir, fileName), data)
}

// atomicWrite schreibt über eine Temp-Datei und benennt sie danach um, damit
// ein Absturz nie eine halb geschriebene Datei hinterlässt.
func atomicWrite(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0750); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".onboarding-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if _, err = tmp.Write(data); err == nil {
		err = tmp.Sync()
	}
	closeErr := tmp.Close()
	if err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Chmod(name, 0640)
	}
	if err != nil {
		return err
	}
	return os.Rename(name, path)
}
