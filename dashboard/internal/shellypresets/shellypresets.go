// Package shellypresets provides read-only access to shelly_presets.json, a
// document of reusable Shelly technical capability templates for the
// dashboard's shelly_devices configuration editor. It is intentionally kept
// separate from internal/config: presets are not a device configuration
// document and are never scanned, edited, or revisioned like one.
package shellypresets

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/Developer-Simon/energy-node-dashboard/internal/config"
)

// Preset is one reusable Shelly technical capability template. Properties
// only ever contains technical shelly_devices.json fields (generation,
// switch_channels, has_power, ...); it never carries device identity (id,
// name, host) or authentication.
type Preset struct {
	ID         string         `json:"id"`
	Name       string         `json:"name"`
	Properties map[string]any `json:"properties"`
}

// Store reads and validates the preset document from disk on every List
// call, mirroring how internal/config.Manager reads device configuration
// fresh rather than caching it.
type Store struct {
	path       string
	schemaPath string
}

// NewStore builds a Store for the preset document at path. The schema is
// expected alongside it, named by replacing the .json suffix with
// .schema.json.
func NewStore(path string) *Store {
	return &Store{path: path, schemaPath: schemaPathFor(path)}
}

func schemaPathFor(path string) string {
	return strings.TrimSuffix(path, ".json") + ".schema.json"
}

// List reads, schema-validates, and returns the configured presets. It
// fails if any preset id is not unique.
func (s *Store) List() ([]Preset, error) {
	data, err := os.ReadFile(s.path)
	if err != nil {
		return nil, err
	}
	schemaData, err := os.ReadFile(s.schemaPath)
	if err != nil {
		return nil, err
	}
	if err := config.ValidateDocument(data, schemaData); err != nil {
		return nil, err
	}
	var presets []Preset
	if err := json.Unmarshal(data, &presets); err != nil {
		return nil, fmt.Errorf("invalid preset document: %w", err)
	}
	seen := make(map[string]bool, len(presets))
	for _, preset := range presets {
		if seen[preset.ID] {
			return nil, fmt.Errorf("duplicate preset id %q in %s", preset.ID, s.path)
		}
		seen[preset.ID] = true
	}
	return presets, nil
}
