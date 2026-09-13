package selection_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Developer-Simon/energy-node-installer/internal/bundle"
	"github.com/Developer-Simon/energy-node-installer/internal/selection"
)

func testManifest() *bundle.Manifest {
	return &bundle.Manifest{
		Steps: []bundle.StepEntry{
			{ID: "10", Optional: false},
			{ID: "40", Optional: true, Default: true},
			{ID: "70", Optional: true, Default: true},
			{ID: "83", Optional: true, Default: false, ServiceID: "shelly"},
		},
	}
}

func TestLoadMissingFileSelectsEverything(t *testing.T) {
	s, err := selection.Load(filepath.Join(t.TempDir(), "selection.json"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !s.Selected("40") || !s.Selected("83") {
		t.Fatalf("a missing selection.json must select every step")
	}
}

func TestLoadUnmentionedStepIsSelected(t *testing.T) {
	path := filepath.Join(t.TempDir(), "selection.json")
	if err := os.WriteFile(path, []byte(`{"steps":{"70":false}}`), 0o644); err != nil {
		t.Fatalf("writing fixture: %v", err)
	}
	s, err := selection.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !s.Selected("40") {
		t.Fatalf("a step not mentioned in selection.json must be selected")
	}
	if s.Selected("70") {
		t.Fatalf("70 was explicitly deselected")
	}
}

func TestSaveAndLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "selection.json")
	original := &selection.Selection{Steps: map[string]bool{"40": false, "70": true}}
	if err := selection.Save(path, original); err != nil {
		t.Fatalf("Save: %v", err)
	}
	loaded, err := selection.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.Selected("40") {
		t.Fatalf("round trip lost the deselection of 40")
	}
	if !loaded.Selected("70") {
		t.Fatalf("round trip lost the selection of 70")
	}
}

func TestDefaultForUsesManifestDefaults(t *testing.T) {
	s := selection.DefaultFor(testManifest())
	if !s.Selected("40") || !s.Selected("70") {
		t.Fatalf("steps defaulting to on must be selected")
	}
	if s.Selected("83") {
		t.Fatalf("a step defaulting to off must not be selected")
	}
	// Core steps never appear in Steps at all -- Selected always answers
	// true for them regardless, and nothing should ever ask.
	if _, present := s.Steps["10"]; present {
		t.Fatalf("a core step must not appear in the selection")
	}
}
