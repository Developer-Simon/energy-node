// Package selection reads, writes and applies selection.json (Vertrag 3):
// which optional steps of a bundle are active on a given node.
package selection

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Developer-Simon/energy-node-installer/internal/bundle"
)

// Selection mirrors selection.json. A step not mentioned counts as
// selected -- default "on" per E7 -- and Load returns an empty Selection
// for a missing file, which answers true for every step through the same
// rule. Core steps (Optional: false in the manifest) never consult this.
type Selection struct {
	Steps map[string]bool `json:"steps"`
}

// Load reads selection.json from path. A missing file is not an error.
func Load(path string) (*Selection, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &Selection{Steps: map[string]bool{}}, nil
		}
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	var s Selection
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	if s.Steps == nil {
		s.Steps = map[string]bool{}
	}
	return &s, nil
}

// Save writes the selection to path, creating its parent directory if
// needed.
func Save(path string, s *Selection) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("creating %s: %w", filepath.Dir(path), err)
	}
	raw, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding selection: %w", err)
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}

// Selected reports whether the given step id should run. A step not
// mentioned is selected -- the same rule bootstrap's own step_selected
// applies (scripts/bootstrap/lib/step.sh, Plan A-II Task 6).
func (s *Selection) Selected(stepID string) bool {
	if v, ok := s.Steps[stepID]; ok {
		return v
	}
	return true
}

// DefaultFor builds the initial selection for a fresh install from the
// manifest's own per-step defaults. Plan C's configuration screen starts
// from this and lets the operator change it before the first run.
func DefaultFor(manifest *bundle.Manifest) *Selection {
	s := &Selection{Steps: map[string]bool{}}
	for _, step := range manifest.Steps {
		if step.Optional {
			s.Steps[step.ID] = step.Default
		}
	}
	return s
}
