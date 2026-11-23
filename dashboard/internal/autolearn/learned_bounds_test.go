package autolearn

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// learnedVectorsPath is the shared vector file of the service and the dashboard.
const learnedVectorsPath = "../../../services/automation/tests/learned_vectors.json"

type boundsVector struct {
	Name   string         `json:"name"`
	Kind   string         `json:"kind"`
	Ref    map[string]any `json:"ref"`
	Errors []string       `json:"errors"`
}

// TestLearnedBoundsVectors runs the bounds section that the service also runs.
// Each error is compared without its path, the path is the rule location.
func TestLearnedBoundsVectors(t *testing.T) {
	data, err := os.ReadFile(filepath.FromSlash(learnedVectorsPath))
	if err != nil {
		t.Fatal(err)
	}
	var file struct {
		Bounds []boundsVector `json:"bounds"`
	}
	if err := json.Unmarshal(data, &file); err != nil {
		t.Fatal(err)
	}
	if len(file.Bounds) == 0 {
		t.Fatal("no bounds vectors")
	}
	for _, v := range file.Bounds {
		t.Run(v.Name, func(t *testing.T) {
			doc := map[string]any{"rules": []any{map[string]any{
				"id": "r", "conditions": []any{map[string]any{"type": "time_window", "start": v.Ref}},
			}}}
			raw, err := json.Marshal(doc)
			if err != nil {
				t.Fatal(err)
			}
			got := ValidateLearnedBounds(raw)
			if len(got) != len(v.Errors) {
				t.Fatalf("errors = %q, want %q", got, v.Errors)
			}
			for i, msg := range got {
				_, text, found := strings.Cut(msg, ": ")
				if !found || text != v.Errors[i] {
					t.Fatalf("error %d = %q, want text %q", i, msg, v.Errors[i])
				}
			}
		})
	}
}

// TestLearnedBoundsWalksScenariosAndPath checks that scenario references are
// found too and that the error names the location in the document.
func TestLearnedBoundsWalksScenariosAndPath(t *testing.T) {
	raw := []byte(`{"rules":[],"scenarios":[{"id":"s","while":{"regulator":{"threshold":{
		"learned":"site/pv_reserve_w","min":100,"max":0,"fallback":50}}}}]}`)
	got := ValidateLearnedBounds(raw)
	if len(got) != 1 {
		t.Fatalf("errors = %q, want one", got)
	}
	if !strings.HasPrefix(got[0], "scenarios[0].while.regulator.threshold: ") ||
		!strings.HasSuffix(got[0], "min must not be above max") {
		t.Fatalf("error = %q", got[0])
	}
}

func TestLearnedBoundsAcceptsValidDocument(t *testing.T) {
	raw := []byte(`{"rules":[{"conditions":[{"start":{"learned":"site/day_start","min":"23:00","max":"01:00","fallback":"00:00"}}]}]}`)
	if got := ValidateLearnedBounds(raw); len(got) != 0 {
		t.Fatalf("errors = %q, want none", got)
	}
}

func TestCheckLearnedBoundsOnSaveOnlyForAutomationRules(t *testing.T) {
	bad := []byte(`{"rules":[{"conditions":[{"start":{"learned":"site/day_start","min":"23:00","max":"01:00","fallback":"12:00"}}]}]}`)
	if err := CheckLearnedBoundsOnSave("shelly_presets", bad); err != nil {
		t.Fatalf("other document rejected: %v", err)
	}
	if err := CheckLearnedBoundsOnSave("automation_rules", bad); err == nil {
		t.Fatal("automation_rules with fallback outside the arc was accepted")
	}
}

// TestLearnedStoreSkipsNonFinite checks that NaN and Inf are not stored or
// published, and that the file stays writable afterwards.
func TestLearnedStoreSkipsNonFinite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "learned.json")
	s := LoadLearnedStore(path, func(string, ...any) {})
	now := time.Unix(1794000000, 0)
	for _, num := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		vals := []Value{{Key: "device/pump_1/usual_runtime_s", Kind: ValueNumber, Num: num, N: 4}}
		if out := s.Update(vals, now, nil); len(out) != 0 {
			t.Fatalf("Update(%v) messages = %v, want none", num, out)
		}
	}
	if entries := s.Entries(); len(entries) != 0 {
		t.Fatalf("entries = %v, want none", entries)
	}
	if err := s.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	ok := []Value{{Key: "device/pump_1/usual_runtime_s", Kind: ValueNumber, Num: 42.5, N: 4}}
	if out := s.Update(ok, now, nil); len(out) != 1 || out[0].Payload == "" {
		t.Fatalf("finite value messages = %v, want one with payload", out)
	}
}
