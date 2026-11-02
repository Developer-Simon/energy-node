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
	"time"
)

// StepState beschreibt, wie ein Einrichtungsschritt steht.
type StepState string

const (
	StepDone      StepState = "done"
	StepLater     StepState = "later"
	StepInstaller StepState = "installer"
)

// Run ist ein laufender Durchgang der Führung für einen Nutzer. All steht,
// wenn die Checkliste den ganzen Durchgang wiederholt, auch erledigte Schritte.
type Run struct {
	Mode  string `json:"mode"`
	From  string `json:"from"`
	Index int    `json:"index"`
	All   bool   `json:"all,omitempty"`
}

// UserProgress hält den Fortschritt eines einzelnen Nutzers. Welcome ist
// gesetzt, solange die Begrüßungstour aussteht, Baseline, wenn Seen beim
// Anlegen mit allen Highlights des Updates gefüllt wurde.
type UserProgress struct {
	Seen     []string `json:"seen"`
	Welcomed bool     `json:"welcomed"`
	Run      *Run     `json:"run"`
	Queue    []string `json:"queue"`
	Welcome  bool     `json:"welcome,omitempty"`
	Baseline bool     `json:"baseline,omitempty"`
}

// Progress ist der gesamte Fortschritt einer Installation. Since ist der
// Zeitpunkt, ab dem die Einrichtungsführung lief. Konten, die danach angelegt
// wurden, gelten als neu.
type Progress struct {
	SetupDone  bool                    `json:"setup_done"`
	SetupSteps map[string]StepState    `json:"setup_steps"`
	Users      map[string]UserProgress `json:"users"`
	Since      time.Time               `json:"since"`
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
	progress, err := s.loadLocked()
	if err != nil {
		log.Printf("onboarding: Fortschritt bleibt leer: %v", err)
		return emptyProgress()
	}
	return progress
}

// SetStep setzt den Zustand eines Einrichtungsschritts.
func (s *Store) SetStep(id string, state StepState) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	progress, err := s.loadLocked()
	if err != nil {
		return err
	}
	progress.SetupSteps[id] = state
	return s.saveLocked(progress)
}

// CompleteSetup merkt sich, dass die Einrichtung dieser Installation fertig ist.
func (s *Store) CompleteSetup() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	progress, err := s.loadLocked()
	if err != nil {
		return err
	}
	progress.SetupDone = true
	return s.saveLocked(progress)
}

// UpdateUser ändert den Fortschritt eines Nutzers. Andere Nutzer bleiben
// unberührt.
func (s *Store) UpdateUser(user string, mutate func(*UserProgress)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	progress, err := s.readLocked()
	switch {
	case errors.Is(err, errCorrupt):
		// Eine kaputte Datei wird durch den Stand dieser Änderung ersetzt.
		log.Printf("onboarding: %v, wird ersetzt", err)
		progress.Since = time.Now().UTC()
	case err != nil:
		return err
	}
	entry := progress.Users[user]
	mutate(&entry)
	progress.Users[user] = entry
	return s.saveLocked(progress)
}

// EnsureUser setzt beim ersten Aufruf Since auf now und legt für user einen
// Eintrag aus seed an, wenn er noch keinen hat. seed bekommt Since. Liefert den
// Fortschritt danach. Scheitert Lesen oder Schreiben, gilt der Stand trotzdem
// für diese Anfrage und das Log bekommt eine Warnung, wie bei Load.
func (s *Store) EnsureUser(user string, now time.Time, seed func(since time.Time) UserProgress) Progress {
	s.mu.Lock()
	defer s.mu.Unlock()
	progress, changed := s.ensureLocked(user, now, seed)
	if changed {
		if err := s.saveLocked(progress); err != nil {
			log.Printf("onboarding: Fortschritt nicht gespeichert: %v", err)
		}
	}
	return progress
}

// Preview is EnsureUser without the write: the progress for this answer only,
// used while the seed would be wrong to keep, for example without a readable
// changelog. The file is never changed.
func (s *Store) Preview(user string, now time.Time, seed func(since time.Time) UserProgress) Progress {
	s.mu.Lock()
	defer s.mu.Unlock()
	progress, _ := s.ensureLocked(user, now, seed)
	return progress
}

// ensureLocked computes the progress EnsureUser returns and whether it differs
// from the file. An unreadable file gives an in-memory seed and no change.
func (s *Store) ensureLocked(user string, now time.Time, seed func(since time.Time) UserProgress) (Progress, bool) {
	progress, err := s.readLocked()
	if err != nil {
		// Kaputte oder unlesbare Datei: der Stand gilt nur für diese Anfrage.
		// Die Datei bleibt unberührt, damit ein GET nie schreibt.
		log.Printf("onboarding: Fortschritt bleibt leer: %v", err)
		progress = emptyProgress()
		progress.Since = now
		progress.Users[user] = seed(now)
		return progress, false
	}
	changed := false
	if progress.Since.IsZero() {
		progress.Since = now
		changed = true
	}
	if _, ok := progress.Users[user]; !ok {
		progress.Users[user] = seed(progress.Since)
		changed = true
	}
	return progress, changed
}

func emptyProgress() Progress {
	return Progress{SetupSteps: map[string]StepState{}, Users: map[string]UserProgress{}}
}

// errCorrupt meldet eine Datei, die kein gültiges JSON ist.
var errCorrupt = errors.New("onboarding: kaputtes JSON")

// loadLocked liefert den Fortschritt für Schreibvorgänge. Eine fehlende Datei
// und kaputtes JSON ergeben leeren Fortschritt (kaputtes JSON wird beim
// nächsten Schreiben ersetzt). Jeder andere Lesefehler wird zurückgegeben,
// damit ein vorübergehender Fehler nicht den gespeicherten Stand überschreibt.
func (s *Store) loadLocked() (Progress, error) {
	progress, err := s.readLocked()
	if errors.Is(err, errCorrupt) {
		log.Printf("onboarding: %v, Fortschritt bleibt leer", err)
		return emptyProgress(), nil
	}
	return progress, err
}

// readLocked liest onboarding.json. Kaputtes JSON liefert leeren Fortschritt
// zusammen mit errCorrupt, ein anderer Lesefehler liefert den Fehler.
func (s *Store) readLocked() (Progress, error) {
	data, err := os.ReadFile(filepath.Join(s.dir, fileName))
	if errors.Is(err, os.ErrNotExist) {
		return emptyProgress(), nil
	}
	if err != nil {
		return Progress{}, fmt.Errorf("onboarding: %s nicht lesbar: %w", fileName, err)
	}
	var loaded Progress
	if err := json.Unmarshal(data, &loaded); err != nil {
		return emptyProgress(), fmt.Errorf("%w: %s: %v", errCorrupt, fileName, err)
	}
	if loaded.SetupSteps == nil {
		loaded.SetupSteps = map[string]StepState{}
	}
	if loaded.Users == nil {
		loaded.Users = map[string]UserProgress{}
	}
	return loaded, nil
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
