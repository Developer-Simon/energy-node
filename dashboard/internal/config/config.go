// Package config will scan DASHBOARD_DEVICES_DIR for *.json files that have
// a matching *.schema.json, validate against that schema, and provide
// atomic writes plus revisioning under
// <dashboard-data-dir>/revisions/<config-name>/. It never edits *.schema.json
// files themselves.
//
// This package is intentionally empty in Phase 0 (Grundgerüst und Betrieb).
// It is implemented in Phase 2 - Gerätemanager und Layout.
package config

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"
)

type Document struct {
	Name          string    `json:"name"`
	Label         string    `json:"label"`
	Path          string    `json:"path"`
	SchemaPath    string    `json:"schema_path"`
	RevisionCount int       `json:"revision_count"`
	ModifiedAt    time.Time `json:"modified_at"`
	Checksum      string    `json:"checksum"`
	ReloadFailed  bool      `json:"reload_failed"`
	ReloadError   string    `json:"reload_error,omitempty"`
}

// displayNames ordnet bekannten Konfigurationsdateien einen sprechenden
// Servicenamen fuer die Dropdown-Anzeige zu. Unbekannte Dateien (z. B. neue
// Geraetetypen ohne Eintrag hier) fallen auf ihren Dateinamen zurueck.
var displayNames = map[string]string{
	"apsystems_devices":   "APsystems Wechselrichter",
	"battery_soc_devices": "Batterie-Ladezustand (SoC)",
	"shelly_devices":      "Shelly Geräte",
	"tuya_devices":        "Tuya Geräte",
	"trucki_devices":      "Trucki GPS-Tracker",
	"automation_rules":    "Automatisierungsregeln",
}

func displayName(name string) string {
	if label, ok := displayNames[name]; ok {
		return label
	}
	return name
}

type Revision struct {
	Name      string    `json:"name"`
	Path      string    `json:"path"`
	Checksum  string    `json:"checksum"`
	CreatedAt time.Time `json:"created_at"`
}

type ReferenceQuery struct {
	DeviceID           string
	DiscoveryTopics    []string
	StateTopics        []string
	AvailabilityTopics []string
	UniqueIDs          []string
	ObjectIDs          []string
}

type Reference struct {
	Configuration    string `json:"configuration"`
	Path             string `json:"path"`
	Value            string `json:"value"`
	URL              string `json:"url"`
	ConfigurationURL string `json:"configuration_url"`
}

// ReloadFunc applies a successfully written configuration to the running
// service. The manager keeps the file and runtime reload steps separate so a
// failed reload can be reported without hiding the already persisted file.
type ReloadFunc func(name string, data []byte) error

// reloadStatus haelt fest, ob der letzte Reload-Versuch fuer ein Dokument
// fehlgeschlagen ist. Bewusst nur im Speicher: nach einem Neustart des
// Dashboards wird der Reload nicht nachgeholt, ein ueber den Neustart
// hinweg konservierter Fehlerzustand waere also irrefuehrend.
type reloadStatus struct {
	failed bool
	err    string
}

type Manager struct {
	mu          sync.Mutex
	dir         string
	reload      ReloadFunc
	excluded    map[string]bool
	reloadState map[string]reloadStatus
}

func NewManager(dir string) *Manager {
	return &Manager{
		dir:         filepath.Clean(dir),
		excluded:    make(map[string]bool),
		reloadState: make(map[string]reloadStatus),
	}
}

// Dir returns the directory this Manager scans for device configuration
// pairs. Used by handlers that need to read a file the Manager itself does
// not own (e.g. the automation service's persisted trigger history) from
// the same shared directory.
func (m *Manager) Dir() string { return m.dir }

// recordReloadLocked merkt sich das Ergebnis eines Reload-Versuchs. Ein
// geglueckter Versuch loescht den Eintrag wieder.
func (m *Manager) recordReloadLocked(name string, err error) {
	if err == nil {
		delete(m.reloadState, name)
		return
	}
	m.reloadState[name] = reloadStatus{failed: true, err: err.Error()}
}

func (m *Manager) applyReloadStateLocked(doc *Document) {
	if status, ok := m.reloadState[doc.Name]; ok {
		doc.ReloadFailed = status.failed
		doc.ReloadError = status.err
	}
}

// ExcludeName removes name (without the .json suffix) from Scan results and
// from the normal configuration document API (Read, Save, Revisions, ...).
// It is used for documents such as shelly_presets.json that live alongside
// device configuration but are served through their own dedicated endpoint
// instead of the generic device configuration form.
func (m *Manager) ExcludeName(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.excluded[name] = true
}

func (m *Manager) SetReloadFunc(reload ReloadFunc) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.reload = reload
}

func (m *Manager) Scan() ([]Document, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	entries, err := os.ReadDir(m.dir)
	if errors.Is(err, fs.ErrNotExist) {
		return []Document{}, nil
	}
	if err != nil {
		return nil, err
	}
	result := []Document{}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") || strings.HasSuffix(entry.Name(), ".schema.json") {
			continue
		}
		name := strings.TrimSuffix(entry.Name(), ".json")
		if m.excluded[name] {
			continue
		}
		path, schema := filepath.Join(m.dir, entry.Name()), filepath.Join(m.dir, name+".schema.json")
		if _, err := os.Stat(schema); err != nil {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return nil, err
		}
		revisions, err := m.revisionsLocked(name)
		if err != nil {
			return nil, err
		}
		document := Document{Name: name, Label: displayName(name), Path: path, SchemaPath: schema, RevisionCount: len(revisions), ModifiedAt: info.ModTime()}
		if data, readErr := os.ReadFile(path); readErr == nil {
			document.Checksum = checksum(data)
		}
		m.applyReloadStateLocked(&document)
		result = append(result, document)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result, nil
}

func (m *Manager) Read(name string) (json.RawMessage, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	doc, err := m.documentLocked(name)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(doc.Path)
}

func (m *Manager) FindReferences(query ReferenceQuery) ([]Reference, error) {
	documents, err := m.Scan()
	if err != nil {
		return nil, err
	}
	values := make(map[string]bool)
	for _, value := range append([]string{query.DeviceID}, query.DiscoveryTopics...) {
		if value != "" {
			values[value] = true
		}
	}
	for _, value := range append(append(query.StateTopics, query.AvailabilityTopics...), query.UniqueIDs...) {
		if value != "" {
			values[value] = true
		}
	}
	for _, value := range query.ObjectIDs {
		if value != "" {
			values[value] = true
		}
	}
	result := make([]Reference, 0)
	for _, document := range documents {
		data, err := os.ReadFile(document.Path)
		if err != nil {
			return nil, err
		}
		var value any
		if err := json.Unmarshal(data, &value); err != nil {
			return nil, fmt.Errorf("configuration %q: %w", document.Name, err)
		}
		configurationURL := "/?panel=config&config=" + url.QueryEscape(document.Name)
		findReferences(value, "$", document.Name, configurationURL, values, &result)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Configuration != result[j].Configuration {
			return result[i].Configuration < result[j].Configuration
		}
		if result[i].Path != result[j].Path {
			return result[i].Path < result[j].Path
		}
		return result[i].Value < result[j].Value
	})
	return result, nil
}

func findReferences(value any, path, configuration, configurationURL string, wanted map[string]bool, result *[]Reference) {
	switch typed := value.(type) {
	case string:
		if wanted[typed] {
			*result = append(*result, Reference{Configuration: configuration, Path: path, Value: typed, URL: configurationURL, ConfigurationURL: configurationURL})
		}
	case []any:
		for index, item := range typed {
			findReferences(item, fmt.Sprintf("%s[%d]", path, index), configuration, configurationURL, wanted, result)
		}
	case map[string]any:
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			findReferences(typed[key], path+"."+key, configuration, configurationURL, wanted, result)
		}
	}
}

// Reload re-applies the current on-disk configuration to the running service
// without changing the file or creating a revision.
func (m *Manager) Reload(name string) (Document, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	doc, err := m.documentLocked(name)
	if err != nil {
		return Document{}, err
	}
	data, err := os.ReadFile(doc.Path)
	if err != nil {
		return Document{}, err
	}
	doc.Checksum = checksum(data)
	if err := Validate(data, doc.SchemaPath); err != nil {
		return Document{}, err
	}
	if m.reload == nil {
		return doc, errors.New("configuration reload is not configured")
	}
	reloadErr := m.reload(name, data)
	m.recordReloadLocked(name, reloadErr)
	if reloadErr != nil {
		doc.ReloadFailed = true
		doc.ReloadError = reloadErr.Error()
		return doc, reloadErr
	}
	return doc, nil
}

func (m *Manager) ReadSchema(name string) (json.RawMessage, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	doc, err := m.documentLocked(name)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(doc.SchemaPath)
}

func (m *Manager) Save(name string, data []byte) (Document, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	doc, err := m.documentLocked(name)
	if err != nil {
		return Document{}, err
	}
	if err := Validate(data, doc.SchemaPath); err != nil {
		return Document{}, err
	}
	old, err := os.ReadFile(doc.Path)
	if err != nil {
		return Document{}, err
	}
	if err := m.writeRevisionLocked(name, old); err != nil {
		return Document{}, err
	}
	if err := AtomicWrite(doc.Path, data, 0640); err != nil {
		return Document{}, err
	}
	doc.Checksum = checksum(data)
	if m.reload != nil {
		reloadErr := m.reload(name, data)
		m.recordReloadLocked(name, reloadErr)
		if reloadErr != nil {
			doc.ReloadFailed = true
			doc.ReloadError = reloadErr.Error()
		}
	}
	revisions, _ := m.revisionsLocked(name)
	doc.RevisionCount = len(revisions)
	if info, statErr := os.Stat(doc.Path); statErr == nil {
		doc.ModifiedAt = info.ModTime()
	}
	return doc, nil
}

func (m *Manager) Revisions(name string) ([]Revision, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, err := m.documentLocked(name); err != nil {
		return nil, err
	}
	return m.revisionsLocked(name)
}

func (m *Manager) ReadRevision(name, revision string) (json.RawMessage, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, err := m.documentLocked(name); err != nil {
		return nil, err
	}
	if revision == "" || filepath.Base(revision) != revision || !strings.HasSuffix(revision, ".json") {
		return nil, errors.New("invalid revision name")
	}
	return os.ReadFile(filepath.Join(m.dir, "revisions", name, revision))
}

func (m *Manager) Restore(name, revision string) (Document, error) {
	data, err := m.ReadRevision(name, revision)
	if err != nil {
		return Document{}, err
	}
	return m.Save(name, data)
}

func (m *Manager) documentLocked(name string) (Document, error) {
	if name == "" || filepath.Base(name) != name || strings.Contains(name, string(filepath.Separator)) {
		return Document{}, errors.New("invalid configuration name")
	}
	if m.excluded[name] {
		return Document{}, fmt.Errorf("configuration %q is not available", name)
	}
	path, schema := filepath.Join(m.dir, name+".json"), filepath.Join(m.dir, name+".schema.json")
	if _, err := os.Stat(path); err != nil {
		return Document{}, fmt.Errorf("configuration %q: %w", name, err)
	}
	if _, err := os.Stat(schema); err != nil {
		return Document{}, fmt.Errorf("configuration %q has no matching schema", name)
	}
	return Document{Name: name, Path: path, SchemaPath: schema}, nil
}

func (m *Manager) revisionsLocked(name string) ([]Revision, error) {
	dir := filepath.Join(m.dir, "revisions", name)
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return []Revision{}, nil
	}
	if err != nil {
		return nil, err
	}
	result := make([]Revision, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		info, err := entry.Info()
		if err != nil {
			return nil, err
		}
		result = append(result, Revision{Name: entry.Name(), Path: path, Checksum: checksum(data), CreatedAt: info.ModTime()})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result, nil
}

// revisionLimit begrenzt, wie viele Revisionen je Konfigurationsdokument
// aufbewahrt werden - dieselbe Grenze wie im settings-Paket. Ohne sie
// wuechse revisions/ auf der SD-Karte des Pi unbegrenzt weiter.
const revisionLimit = 20

func (m *Manager) writeRevisionLocked(name string, data []byte) error {
	dir := filepath.Join(m.dir, "revisions", name)
	if err := os.MkdirAll(dir, 0750); err != nil {
		return err
	}
	stamp := time.Now().UTC().Format("2006-01-02T15-04-05.000000000Z")
	if err := AtomicWrite(filepath.Join(dir, stamp+".json"), data, 0640); err != nil {
		return err
	}
	return m.pruneRevisionsLocked(name)
}

// pruneRevisionsLocked entfernt die aeltesten Revisionen, bis hoechstens
// revisionLimit uebrig sind. revisionsLocked liefert nach Namen sortiert,
// und die Namen sind UTC-Zeitstempel - die aeltesten stehen also vorn.
func (m *Manager) pruneRevisionsLocked(name string) error {
	revisions, err := m.revisionsLocked(name)
	if err != nil {
		return err
	}
	for i := 0; i+revisionLimit < len(revisions); i++ {
		if err := os.Remove(revisions[i].Path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

func Validate(data []byte, schemaPath string) error {
	schemaData, err := os.ReadFile(schemaPath)
	if err != nil {
		return err
	}
	return ValidateDocument(data, schemaData)
}

func ValidateDocument(data, schemaData []byte) error {
	var value, schema any
	if err := json.Unmarshal(data, &value); err != nil {
		return fmt.Errorf("invalid JSON: %w", err)
	}
	if err := json.Unmarshal(schemaData, &schema); err != nil {
		return fmt.Errorf("invalid schema: %w", err)
	}
	if err := checkSchemaKeywords(schema, "schema"); err != nil {
		return fmt.Errorf("invalid schema: %w", err)
	}
	return validateValue(value, schema, "$")
}

// unsupportedKeywords are JSON Schema keywords this validator does not
// evaluate. A schema using one would validate silently wrong, so it is
// refused instead of ignored. The conditional subset (if/then/else, allOf,
// const, unevaluatedProperties) is what the schema editor understands too.
var unsupportedKeywords = []string{"oneOf", "anyOf", "not", "dependentSchemas", "$ref"}

func checkSchemaKeywords(raw any, path string) error {
	schema, ok := raw.(map[string]any)
	if !ok {
		return nil
	}
	for _, keyword := range unsupportedKeywords {
		if _, found := schema[keyword]; found {
			return fmt.Errorf("%s uses unsupported keyword %s", path, keyword)
		}
	}
	_, additional := schema["additionalProperties"]
	_, unevaluated := schema["unevaluatedProperties"]
	if additional && unevaluated {
		return fmt.Errorf("%s mixes additionalProperties and unevaluatedProperties", path)
	}
	if properties, ok := schema["properties"].(map[string]any); ok {
		for key, child := range properties {
			if err := checkSchemaKeywords(child, path+".properties."+key); err != nil {
				return err
			}
		}
	}
	for _, keyword := range []string{"items", "if", "then", "else"} {
		if child, ok := schema[keyword]; ok {
			if err := checkSchemaKeywords(child, path+"."+keyword); err != nil {
				return err
			}
		}
	}
	if all, ok := schema["allOf"].([]any); ok {
		for index, child := range all {
			if err := checkSchemaKeywords(child, fmt.Sprintf("%s.allOf[%d]", path, index)); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateValue(value, rawSchema any, path string) error {
	_, err := applySchema(value, rawSchema, path)
	return err
}

// applySchema validates value against one schema. For an object it also
// returns the property names this schema evaluated, including those of its
// in-place subschemas (allOf entries, a matching if, the applied then/else).
// unevaluatedProperties needs that set: a property is only known if some
// applied branch declares it, so a field of an inactive branch is rejected.
func applySchema(value, rawSchema any, path string) (map[string]bool, error) {
	schema, ok := rawSchema.(map[string]any)
	if !ok {
		return nil, nil
	}
	if typ, ok := schema["type"].(string); ok && !matchesType(value, typ) {
		return nil, fmt.Errorf("%s must be %s", path, typ)
	}
	if min, ok := schema["minLength"].(float64); ok {
		if text, isString := value.(string); isString && float64(len(text)) < min {
			return nil, fmt.Errorf("%s must not be empty", path)
		}
	}
	if min, ok := schema["minimum"].(float64); ok && numberValue(value) < min {
		return nil, fmt.Errorf("%s is below minimum", path)
	}
	if max, ok := schema["maximum"].(float64); ok && numberValue(value) > max {
		return nil, fmt.Errorf("%s is above maximum", path)
	}
	// Guarded by the type assertion rather than numberValue(): that helper
	// returns 0 for non-numbers, which would make a string fail an
	// exclusiveMinimum of 0 with a misleading message.
	if number, isNumber := value.(float64); isNumber {
		if min, ok := schema["exclusiveMinimum"].(float64); ok && number <= min {
			return nil, fmt.Errorf("%s must be greater than %v", path, min)
		}
		if max, ok := schema["exclusiveMaximum"].(float64); ok && number >= max {
			return nil, fmt.Errorf("%s must be less than %v", path, max)
		}
		if step, ok := schema["multipleOf"].(float64); ok && step > 0 {
			if quotient := number / step; math.Abs(quotient-math.Round(quotient)) > 1e-9 {
				return nil, fmt.Errorf("%s must be a multiple of %v", path, step)
			}
		}
	}
	if enums, ok := schema["enum"].([]any); ok {
		valid := false
		for _, candidate := range enums {
			if fmt.Sprint(candidate) == fmt.Sprint(value) {
				valid = true
			}
		}
		if !valid {
			return nil, fmt.Errorf("%s has an unsupported value", path)
		}
	}
	if expected, ok := schema["const"]; ok && !reflect.DeepEqual(expected, value) {
		return nil, fmt.Errorf("%s must be %v", path, expected)
	}
	evaluated := map[string]bool{}
	object, isObject := value.(map[string]any)
	if isObject {
		properties, _ := schema["properties"].(map[string]any)
		for _, required := range stringSlice(schema["required"]) {
			if _, exists := object[required]; !exists {
				return nil, fmt.Errorf("%s.%s is required", path, required)
			}
		}
		if additional, ok := schema["additionalProperties"].(bool); ok && !additional {
			for _, key := range sortedKeys(object) {
				if _, exists := properties[key]; !exists {
					return nil, fmt.Errorf("%s.%s is not allowed", path, key)
				}
			}
		}
		for _, key := range sortedKeys(object) {
			if property, exists := properties[key]; exists {
				evaluated[key] = true
				if err := validateValue(object[key], property, path+"."+key); err != nil {
					return nil, err
				}
			}
		}
	}
	if all, ok := schema["allOf"].([]any); ok {
		for _, sub := range all {
			keys, err := applySchema(value, sub, path)
			if err != nil {
				return nil, err
			}
			mergeKeys(evaluated, keys)
		}
	}
	if condition, ok := schema["if"]; ok {
		branch := schema["else"]
		if keys, err := applySchema(value, condition, path); err == nil {
			mergeKeys(evaluated, keys)
			branch = schema["then"]
		}
		if branch != nil {
			keys, err := applySchema(value, branch, path)
			if err != nil {
				return nil, err
			}
			mergeKeys(evaluated, keys)
		}
	}
	if isObject {
		if unevaluated, ok := schema["unevaluatedProperties"].(bool); ok && !unevaluated {
			for _, key := range sortedKeys(object) {
				if !evaluated[key] {
					return nil, fmt.Errorf("%s.%s is not allowed", path, key)
				}
			}
		}
	}
	if array, ok := value.([]any); ok {
		if min, ok := schema["minItems"].(float64); ok && float64(len(array)) < min {
			return nil, fmt.Errorf("%s needs at least %v entries", path, min)
		}
		if max, ok := schema["maxItems"].(float64); ok && float64(len(array)) > max {
			return nil, fmt.Errorf("%s allows at most %v entries", path, max)
		}
		if itemSchema, exists := schema["items"]; exists {
			for index, item := range array {
				if err := validateValue(item, itemSchema, fmt.Sprintf("%s[%d]", path, index)); err != nil {
					return nil, err
				}
			}
		}
	}
	return evaluated, nil
}

func mergeKeys(into, from map[string]bool) {
	for key := range from {
		into[key] = true
	}
}

// sortedKeys keeps error messages deterministic: map order is random.
func sortedKeys(object map[string]any) []string {
	keys := make([]string, 0, len(object))
	for key := range object {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func AtomicWrite(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0750); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".dashboard-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err = tmp.Write(data); err == nil {
		err = tmp.Sync()
	}
	closeErr := tmp.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

// ServiceIDForConfig maps a configuration document to the service that
// loads it: automation_rules belongs to automation, every <x>_devices to <x>.
// Other documents (presets and the like) belong to no service.
func ServiceIDForConfig(name string) (string, bool) {
	if name == "automation_rules" {
		return "automation", true
	}
	const suffix = "_devices"
	if !strings.HasSuffix(name, suffix) {
		return "", false
	}
	return strings.TrimSuffix(name, suffix), true
}

func checksum(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }
func matchesType(value any, typ string) bool {
	switch typ {
	case "object":
		_, ok := value.(map[string]any)
		return ok
	case "array":
		_, ok := value.([]any)
		return ok
	case "string":
		_, ok := value.(string)
		return ok
	case "integer":
		n, ok := value.(float64)
		return ok && n == float64(int64(n))
	case "number":
		_, ok := value.(float64)
		return ok
	case "boolean":
		_, ok := value.(bool)
		return ok
	default:
		return true
	}
}
func numberValue(value any) float64 { n, _ := value.(float64); return n }
func stringSlice(value any) []string {
	raw, _ := value.([]any)
	result := make([]string, 0, len(raw))
	for _, item := range raw {
		if s, ok := item.(string); ok {
			result = append(result, s)
		}
	}
	return result
}
