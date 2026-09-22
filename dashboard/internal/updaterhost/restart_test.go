package updaterhost

import (
	"encoding/json"
	"os"
	"testing"
)

// The bash/Python rule (scripts/bootstrap/lib/restart_rule.py) and this Go
// copy must agree; both read the same table.
func TestRestartReasonMatchesTheSharedTable(t *testing.T) {
	raw, err := os.ReadFile("../../../scripts/bootstrap/testdata/restart_cases.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name       string             `json:"name"`
		Candidate  *candidateManifest `json:"candidate"`
		Installed  *installedManifest `json:"installed"`
		StepID     string             `json:"step_id"`
		RestartAll bool               `json:"restart_all"`
		Want       string             `json:"want"`
	}
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	for _, c := range cases {
		if got := restartReason(c.Installed, c.Candidate, c.StepID, c.RestartAll); got != c.Want {
			t.Errorf("%s: got %q, want %q", c.Name, got, c.Want)
		}
	}
}
