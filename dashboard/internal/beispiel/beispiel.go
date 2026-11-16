// Package beispiel übernimmt beim Start den Beispiel-Seed aus dem Bootstrap:
// Zuordnungen, Simulation und Kacheln für die Beispiel-Geräte.
package beispiel

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"

	"github.com/Developer-Simon/energy-node-dashboard/internal/energy"
	"github.com/Developer-Simon/energy-node-dashboard/internal/settings"
)

// SeedFileName ist der Seed des Bootstrap-Schritts, liegt im Datenverzeichnis
// und wird beim Start einmal übernommen.
const SeedFileName = "beispiel-seed.json"

// NoteFileName ist die Notiz, zu der der Seed nach der Übernahme wird. Sie
// bleibt liegen, damit die Beispiel-Geräte später zuzuordnen sind.
const NoteFileName = "beispiel.json"

// Note entspricht dem Schema aus scripts/bootstrap/lib/beispiel.json.
type Note struct {
	Version       int                          `json:"version"`
	Devices       map[string][]json.RawMessage `json:"devices"`
	Scenario      json.RawMessage              `json:"scenario"`
	PublishPrefix string                       `json:"publish_prefix"`
	Assignments   map[string]energy.Assignment `json:"assignments"`
}

// ApplySeed übernimmt den Seed einmal und benennt ihn danach in NoteFileName
// um, auch wenn Teile scheitern, damit der Start nicht immer wieder anwendet.
// Eine fehlende Datei ist kein Fehler. Fehler werden gesammelt zurückgegeben.
func ApplySeed(dataDir string, st *settings.Store) (err error) {
	seedPath := filepath.Join(dataDir, SeedFileName)
	if _, statErr := os.Stat(seedPath); errors.Is(statErr, os.ErrNotExist) {
		return nil
	} else if statErr != nil {
		return fmt.Errorf("%s nicht lesbar: %w", SeedFileName, statErr)
	}
	defer func() {
		notePath := filepath.Join(dataDir, NoteFileName)
		if rnErr := os.Rename(seedPath, notePath); rnErr != nil {
			err = errors.Join(err, fmt.Errorf("%s nicht umbenannt: %w", SeedFileName, rnErr))
		}
	}()

	note, readErr := readNote(seedPath)
	if readErr != nil {
		return readErr
	}

	var errs []error
	if err := applyAssignments(note, st); err != nil {
		errs = append(errs, err)
	}
	if err := applySimulation(st); err != nil {
		errs = append(errs, err)
	}
	if err := applyTiles(note, st); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

// LoadNote liest die Notiz aus dem Datenverzeichnis. Fehlt sie, ist das kein
// Fehler, dann liefert sie nil.
func LoadNote(dataDir string) (*Note, error) {
	notePath := filepath.Join(dataDir, NoteFileName)
	if _, err := os.Stat(notePath); errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	note, err := readNote(notePath)
	if err != nil {
		return nil, err
	}
	return &note, nil
}

func readNote(path string) (Note, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Note{}, fmt.Errorf("%s nicht lesbar: %w", filepath.Base(path), err)
	}
	var note Note
	if err := json.Unmarshal(data, &note); err != nil {
		return Note{}, fmt.Errorf("%s kein gültiges JSON: %w", filepath.Base(path), err)
	}
	return note, nil
}

// applyAssignments setzt die Rollen nur für Entitäten ohne Zuordnung. Eine
// vorhandene Zuordnung bleibt, auch wenn der Seed etwas anderes nennt.
func applyAssignments(note Note, st *settings.Store) error {
	if len(note.Assignments) == 0 {
		return nil
	}
	_, err := st.PatchEnergy(func(cfg *settings.EnergyConfig) error {
		if cfg.Assignments == nil {
			cfg.Assignments = map[string]energy.Assignment{}
		}
		for id, assignment := range note.Assignments {
			if _, taken := cfg.Assignments[id]; !taken {
				cfg.Assignments[id] = assignment
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("zuordnungen speichern: %w", err)
	}
	return nil
}

// applySimulation schaltet die Simulation ein, damit die Beispiel-Geräte
// Werte liefern.
func applySimulation(st *settings.Store) error {
	cfg, err := st.LoadMQTT()
	if err != nil {
		return fmt.Errorf("mqtt laden: %w", err)
	}
	if cfg.SimulationActive {
		return nil
	}
	cfg.SimulationActive = true
	if err := st.SaveMQTT(cfg); err != nil {
		return fmt.Errorf("mqtt speichern: %w", err)
	}
	return nil
}

// applyTiles hängt je Beispiel-Gerät eine Kachel an die erste Gruppe der
// ersten Seite. Das gilt nur für ein gespeichertes Layout, das Standardraster
// zeigt die Geräte ohnehin.
func applyTiles(note Note, st *settings.Store) error {
	if !st.LayoutConfigured() {
		return nil
	}
	layout, err := st.LoadLayout()
	if err != nil {
		return fmt.Errorf("layout laden: %w", err)
	}
	if len(layout.Pages) == 0 || len(layout.Pages[0].Groups) == 0 {
		return nil
	}
	present := map[string]bool{}
	for _, page := range layout.Pages {
		for _, group := range page.Groups {
			for _, item := range group.Items {
				present[item.ID] = true
			}
		}
	}

	changed := false
	group := &layout.Pages[0].Groups[0]
	for _, id := range deviceIDs(note) {
		itemID := "device:" + id
		if present[itemID] {
			continue
		}
		group.Items = append(group.Items, settings.Item{
			ID: itemID, Type: "device", Ref: id, Span: "1", Visible: true,
		})
		changed = true
	}
	if !changed {
		return nil
	}
	if err := st.SaveLayout(layout); err != nil {
		return fmt.Errorf("layout speichern: %w", err)
	}
	return nil
}

// deviceIDs liefert die IDs der Seed-Geräte, nach Kategorie sortiert, damit
// die Kacheln in fester Reihenfolge landen.
func deviceIDs(note Note) []string {
	categories := make([]string, 0, len(note.Devices))
	for category := range note.Devices {
		categories = append(categories, category)
	}
	sort.Strings(categories)
	var ids []string
	for _, category := range categories {
		for _, raw := range note.Devices[category] {
			var device struct {
				ID string `json:"id"`
			}
			if err := json.Unmarshal(raw, &device); err != nil || device.ID == "" {
				slog.Warn("beispiel-seed: Gerät ohne ID wird übersprungen", "kategorie", category)
				continue
			}
			ids = append(ids, device.ID)
		}
	}
	return ids
}
