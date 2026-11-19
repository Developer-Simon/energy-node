// Package example übernimmt beim Start den Beispiel-Seed aus dem Bootstrap:
// Zuordnungen, Simulation und Kacheln für die Beispiel-Geräte.
package example

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/netip"
	"os"
	"path/filepath"
	"reflect"
	"sort"

	"github.com/Developer-Simon/energy-node-dashboard/internal/config"
	"github.com/Developer-Simon/energy-node-dashboard/internal/energy"
	"github.com/Developer-Simon/energy-node-dashboard/internal/settings"
)

// SeedFileName ist der Seed des Bootstrap-Schritts, liegt im Datenverzeichnis
// und wird beim Start einmal übernommen.
const SeedFileName = "example-seed.json"

// NoteFileName ist die Notiz, zu der der Seed nach der Übernahme wird. Sie
// bleibt liegen, damit die Beispiel-Geräte später zuzuordnen sind.
const NoteFileName = "example.json"

// Note entspricht dem Schema aus scripts/bootstrap/lib/example.json.
type Note struct {
	Version       int                          `json:"version"`
	Devices       map[string][]json.RawMessage `json:"devices,omitempty"`
	Scenario      json.RawMessage              `json:"scenario,omitempty"`
	PublishPrefix string                       `json:"publish_prefix,omitempty"`
	Assignments   map[string]energy.Assignment `json:"assignments,omitempty"`
}

// empty meldet, ob von der Notiz nichts mehr übrig ist.
func (n Note) empty() bool {
	return len(n.Devices) == 0 && len(n.Scenario) == 0 && n.PublishPrefix == "" && len(n.Assignments) == 0
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
				slog.Warn("example-seed: Gerät ohne ID wird übersprungen", "kategorie", category)
				continue
			}
			ids = append(ids, device.ID)
		}
	}
	return ids
}

// Result nennt die Beispiel-Teile, die Stop entfernt hat, und die, die stehen
// geblieben sind. Die Einträge heißen "device:<id>", "scenario:<id>",
// "publish_prefix:<topic>" oder "assignment:<entity>".
type Result struct {
	Removed []string `json:"removed"`
	Kept    []string `json:"kept"`
}

// exampleNet ist das Dokumentationsnetz der Beispiel-Geräte. Ein Gerät mit
// anderer Adresse hat der Nutzer geändert, es bleibt stehen.
var exampleNet = netip.MustParsePrefix("192.0.2.0/24")

// scenarioKeys bestimmen, ob ein Szenario dem Beispiel noch gleicht.
var scenarioKeys = []string{"when", "until", "while", "on_start", "on_end"}

// Stop schaltet die Beispiel-Simulation aus. Mit remove entfernt es danach die
// Beispiel-Teile, die der Nutzer nicht verändert hat. Alles andere bleibt in
// der Notiz, damit es später entschieden werden kann.
func Stop(dataDir string, remove bool, configs *config.Manager, st *settings.Store, publish func(bool)) (Result, error) {
	cfg, err := st.LoadMQTT()
	if err != nil {
		return Result{}, fmt.Errorf("mqtt laden: %w", err)
	}
	cfg.SimulationActive = false
	if err := st.SaveMQTT(cfg); err != nil {
		return Result{}, fmt.Errorf("mqtt speichern: %w", err)
	}
	publish(false)
	if !remove {
		return Result{}, nil
	}

	note, err := LoadNote(dataDir)
	if err != nil {
		return Result{}, err
	}
	if note == nil {
		return Result{}, nil
	}

	var res Result
	restDevices, errs := stopDevices(*note, configs, &res)
	restScenario, restPrefix, automationErrs := stopAutomation(*note, configs, &res)
	errs = append(errs, automationErrs...)
	restAssignments, err := stopAssignments(*note, st, &res)
	if err != nil {
		errs = append(errs, err)
	}

	rest := Note{
		Version:       note.Version,
		Devices:       restDevices,
		Scenario:      restScenario,
		PublishPrefix: restPrefix,
		Assignments:   restAssignments,
	}
	notePath := filepath.Join(dataDir, NoteFileName)
	if rest.empty() {
		if err := os.Remove(notePath); err != nil && !errors.Is(err, os.ErrNotExist) {
			errs = append(errs, fmt.Errorf("notiz entfernen: %w", err))
		}
	} else if data, err := json.MarshalIndent(rest, "", "  "); err != nil {
		errs = append(errs, fmt.Errorf("notiz kodieren: %w", err))
	} else if err := config.AtomicWrite(notePath, data, 0640); err != nil {
		errs = append(errs, fmt.Errorf("notiz schreiben: %w", err))
	}

	if res.Removed == nil {
		res.Removed = []string{}
	}
	if res.Kept == nil {
		res.Kept = []string{}
	}
	return res, errors.Join(errs...)
}

// Active meldet, ob das Beispiel noch besteht: die Notiz ist da und die
// Simulation läuft.
func Active(dataDir string, st *settings.Store) bool {
	note, err := LoadNote(dataDir)
	if err != nil || note == nil {
		return false
	}
	cfg, err := st.LoadMQTT()
	return err == nil && cfg.SimulationActive
}

// stopDevices entfernt die unveränderten Beispiel-Geräte aus den Geräte-
// listen. Es liefert die Geräte, die in der Notiz bleiben.
func stopDevices(note Note, configs *config.Manager, res *Result) (map[string][]json.RawMessage, []error) {
	var errs []error
	rest := map[string][]json.RawMessage{}
	for _, category := range sortedKeys(note.Devices) {
		entries := note.Devices[category]
		doc, err := readList(configs, category)
		if err != nil {
			errs = append(errs, err)
			rest[category] = entries
			for _, raw := range entries {
				id, _ := entryFields(raw)
				res.Kept = append(res.Kept, "device:"+id)
			}
			continue
		}

		var keep, pending []json.RawMessage
		var pendingIDs []string
		removeAt := map[int]bool{}
		for _, raw := range entries {
			id, _ := entryFields(raw)
			index := indexByID(doc, id)
			switch {
			case index < 0:
				// Das Gerät ist ohnehin nicht mehr da.
			case isExampleHost(doc[index]):
				removeAt[index] = true
				pending = append(pending, raw)
				pendingIDs = append(pendingIDs, id)
			default:
				keep = append(keep, raw)
				res.Kept = append(res.Kept, "device:"+id)
			}
		}

		if len(removeAt) > 0 {
			next := make([]json.RawMessage, 0, len(doc)-len(removeAt))
			for i, raw := range doc {
				if !removeAt[i] {
					next = append(next, raw)
				}
			}
			if err := saveList(configs, category, next); err != nil {
				errs = append(errs, err)
				keep = append(keep, pending...)
				for _, id := range pendingIDs {
					res.Kept = append(res.Kept, "device:"+id)
				}
			} else {
				for _, id := range pendingIDs {
					res.Removed = append(res.Removed, "device:"+id)
				}
			}
		}
		if len(keep) > 0 {
			rest[category] = keep
		}
	}
	return rest, errs
}

// stopAutomation entfernt das Szenario und das Präfix aus automation_rules.
// Beide liegen im selben Dokument und werden mit einem Speichern geschrieben.
// Es liefert das Szenario und das Präfix, die in der Notiz bleiben.
func stopAutomation(note Note, configs *config.Manager, res *Result) (json.RawMessage, string, []error) {
	if len(note.Scenario) == 0 && note.PublishPrefix == "" {
		return nil, "", nil
	}
	keepAll := func(err error) (json.RawMessage, string, []error) {
		var errs []error
		if err != nil {
			errs = append(errs, err)
		}
		if len(note.Scenario) > 0 {
			res.Kept = append(res.Kept, "scenario:"+rawID(note.Scenario))
		}
		if note.PublishPrefix != "" {
			res.Kept = append(res.Kept, "publish_prefix:"+note.PublishPrefix)
		}
		return note.Scenario, note.PublishPrefix, errs
	}

	doc, settingsDoc, scenarios, err := readAutomation(configs)
	if err != nil {
		return keepAll(err)
	}

	var keptScenario json.RawMessage
	var keptPrefix string
	dropScenario := -1
	if len(note.Scenario) > 0 {
		id := rawID(note.Scenario)
		index := indexByID(scenarios, id)
		switch {
		case index < 0:
			// Das Szenario ist ohnehin nicht mehr da.
		case sameScenarioParts(scenarios[index], note.Scenario):
			dropScenario = index
		default:
			keptScenario = note.Scenario
			res.Kept = append(res.Kept, "scenario:"+id)
		}
	}

	dropPrefix := false
	if note.PublishPrefix != "" {
		var auto bool
		var prefixes []string
		_ = json.Unmarshal(settingsDoc["publish_allowed_prefixes_auto"], &auto)
		_ = json.Unmarshal(settingsDoc["publish_allowed_prefixes"], &prefixes)
		switch {
		case !auto:
			// Von Hand gepflegte Präfixe bleiben.
			keptPrefix = note.PublishPrefix
			res.Kept = append(res.Kept, "publish_prefix:"+note.PublishPrefix)
		case contains(prefixes, note.PublishPrefix):
			dropPrefix = true
		}
	}

	if dropScenario < 0 && !dropPrefix {
		return keptScenario, keptPrefix, nil
	}

	nextScenarios := scenarios
	if dropScenario >= 0 {
		nextScenarios = make([]json.RawMessage, 0, len(scenarios)-1)
		for i, raw := range scenarios {
			if i != dropScenario {
				nextScenarios = append(nextScenarios, raw)
			}
		}
	}
	nextPrefixes := []string{}
	if dropPrefix {
		for _, p := range stringsOf(settingsDoc["publish_allowed_prefixes"]) {
			if p != note.PublishPrefix {
				nextPrefixes = append(nextPrefixes, p)
			}
		}
	}
	if err := saveAutomation(configs, doc, settingsDoc, nextScenarios, nextPrefixes, dropPrefix); err != nil {
		errs := []error{err}
		if dropScenario >= 0 {
			keptScenario = note.Scenario
			res.Kept = append(res.Kept, "scenario:"+rawID(note.Scenario))
		}
		if dropPrefix {
			keptPrefix = note.PublishPrefix
			res.Kept = append(res.Kept, "publish_prefix:"+note.PublishPrefix)
		}
		return keptScenario, keptPrefix, errs
	}
	if dropScenario >= 0 {
		res.Removed = append(res.Removed, "scenario:"+rawID(note.Scenario))
	}
	if dropPrefix {
		res.Removed = append(res.Removed, "publish_prefix:"+note.PublishPrefix)
	}
	return keptScenario, keptPrefix, nil
}

// stopAssignments entfernt die Zuordnungen, deren Rolle noch dem Beispiel
// entspricht. Es liefert die Zuordnungen, die in der Notiz bleiben.
func stopAssignments(note Note, st *settings.Store, res *Result) (map[string]energy.Assignment, error) {
	kept := map[string]energy.Assignment{}
	var dropped []string
	ids := sortedKeys(note.Assignments)
	_, err := st.PatchEnergy(func(cfg *settings.EnergyConfig) error {
		for _, id := range ids {
			want := note.Assignments[id]
			current, ok := cfg.Assignments[id]
			if !ok {
				continue
			}
			if current.Role != want.Role {
				kept[id] = want
				continue
			}
			delete(cfg.Assignments, id)
			dropped = append(dropped, id)
		}
		return nil
	})
	if err != nil {
		for _, id := range dropped {
			kept[id] = note.Assignments[id]
		}
		dropped = nil
	}
	for _, id := range dropped {
		res.Removed = append(res.Removed, "assignment:"+id)
	}
	for _, id := range ids {
		if _, ok := kept[id]; ok {
			res.Kept = append(res.Kept, "assignment:"+id)
		}
	}
	if err != nil {
		return kept, fmt.Errorf("zuordnungen speichern: %w", err)
	}
	return kept, nil
}

// sameScenarioParts vergleicht die Teile eines Szenarios, die das Beispiel
// festlegen. Der Vergleich ist kanonisch, also unabhängig von Leerzeichen und
// Schlüsselreihenfolge.
func sameScenarioParts(a, b json.RawMessage) bool {
	var left, right map[string]any
	if json.Unmarshal(a, &left) != nil || json.Unmarshal(b, &right) != nil {
		return false
	}
	for _, key := range scenarioKeys {
		if !reflect.DeepEqual(left[key], right[key]) {
			return false
		}
	}
	return true
}

// isExampleHost meldet, ob das Gerät im Dokumentationsnetz liegt.
func isExampleHost(raw json.RawMessage) bool {
	_, host := entryFields(raw)
	addr, err := netip.ParseAddr(host)
	return err == nil && exampleNet.Contains(addr)
}

// entryFields liest ID und Host aus einem Geräte- oder Szenario-Eintrag.
func entryFields(raw json.RawMessage) (id, host string) {
	var entry struct {
		ID   string `json:"id"`
		Host string `json:"host"`
	}
	_ = json.Unmarshal(raw, &entry)
	return entry.ID, entry.Host
}

// rawID liefert die ID eines Eintrags, leer wenn es keine gibt.
func rawID(raw json.RawMessage) string {
	id, _ := entryFields(raw)
	return id
}

// indexByID liefert den Index des ersten Eintrags mit der ID, sonst -1.
func indexByID(list []json.RawMessage, id string) int {
	if id == "" {
		return -1
	}
	for i, raw := range list {
		if rawID(raw) == id {
			return i
		}
	}
	return -1
}

// readList liest eine Geräteliste aus den Konfigurationen.
func readList(configs *config.Manager, name string) ([]json.RawMessage, error) {
	raw, err := configs.Read(name)
	if err != nil {
		return nil, fmt.Errorf("%s lesen: %w", name, err)
	}
	var list []json.RawMessage
	if err := json.Unmarshal(raw, &list); err != nil {
		return nil, fmt.Errorf("%s kein gültiges JSON: %w", name, err)
	}
	return list, nil
}

// saveList schreibt eine Geräteliste zurück.
func saveList(configs *config.Manager, name string, list []json.RawMessage) error {
	data, err := json.Marshal(list)
	if err != nil {
		return fmt.Errorf("%s kodieren: %w", name, err)
	}
	if _, err := configs.Save(name, data); err != nil {
		return fmt.Errorf("%s speichern: %w", name, err)
	}
	return nil
}

// readAutomation liest automation_rules in Teilen: das Dokument, seine
// settings und seine Szenarien.
func readAutomation(configs *config.Manager) (map[string]json.RawMessage, map[string]json.RawMessage, []json.RawMessage, error) {
	raw, err := configs.Read("automation_rules")
	if err != nil {
		return nil, nil, nil, fmt.Errorf("automation_rules lesen: %w", err)
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, nil, nil, fmt.Errorf("automation_rules kein gültiges JSON: %w", err)
	}
	var settingsDoc map[string]json.RawMessage
	if err := json.Unmarshal(doc["settings"], &settingsDoc); err != nil {
		return nil, nil, nil, fmt.Errorf("automation_rules settings: %w", err)
	}
	var scenarios []json.RawMessage
	if err := json.Unmarshal(doc["scenarios"], &scenarios); err != nil {
		return nil, nil, nil, fmt.Errorf("automation_rules scenarios: %w", err)
	}
	return doc, settingsDoc, scenarios, nil
}

// saveAutomation schreibt automation_rules mit den geänderten Teilen zurück.
// Mit setPrefixes ersetzt es publish_allowed_prefixes durch prefixes.
func saveAutomation(configs *config.Manager, doc, settingsDoc map[string]json.RawMessage, scenarios []json.RawMessage, prefixes []string, setPrefixes bool) error {
	if setPrefixes {
		encoded, err := json.Marshal(prefixes)
		if err != nil {
			return fmt.Errorf("publish_allowed_prefixes kodieren: %w", err)
		}
		settingsDoc["publish_allowed_prefixes"] = encoded
	}
	encodedSettings, err := json.Marshal(settingsDoc)
	if err != nil {
		return fmt.Errorf("automation_rules settings kodieren: %w", err)
	}
	encodedScenarios, err := json.Marshal(scenarios)
	if err != nil {
		return fmt.Errorf("automation_rules scenarios kodieren: %w", err)
	}
	doc["settings"] = encodedSettings
	doc["scenarios"] = encodedScenarios
	data, err := json.Marshal(doc)
	if err != nil {
		return fmt.Errorf("automation_rules kodieren: %w", err)
	}
	if _, err := configs.Save("automation_rules", data); err != nil {
		return fmt.Errorf("automation_rules speichern: %w", err)
	}
	return nil
}

// stringsOf liest eine JSON-Liste von Texten, leer bei Fehlern.
func stringsOf(raw json.RawMessage) []string {
	var out []string
	_ = json.Unmarshal(raw, &out)
	return out
}

// contains meldet, ob der Text in der Liste steht.
func contains(list []string, want string) bool {
	for _, item := range list {
		if item == want {
			return true
		}
	}
	return false
}

// sortedKeys liefert die Schlüssel einer Map sortiert, damit die Ergebnisse
// in fester Reihenfolge stehen.
func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
