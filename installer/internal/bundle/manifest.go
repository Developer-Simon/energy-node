package bundle

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// StepEntry mirrors one entry of manifest.json's "steps" array (Vertrag 2).
type StepEntry struct {
	ID        string `json:"id"`
	Optional  bool   `json:"optional"`
	Default   bool   `json:"default,omitempty"`
	ServiceID string `json:"service_id,omitempty"`
	Dir       string `json:"dir,omitempty"`
	Unit      string `json:"unit,omitempty"`
	Kind      string `json:"kind,omitempty"`
	// Version ist die Version genau dieses Dienstes ("vX.Y.Z"), aus dem
	// "version"-Feld seiner services/<dir>/manifest.json. Leer bei Schritten
	// ohne Dienst und bei Bundles, die vor der Umstellung gebaut wurden.
	Version string `json:"version,omitempty"`
	// DashboardKey ist, falls gesetzt, der Schluessel, unter dem
	// 65-dashboard-config.sh diesen Dienst in installed_services vermerkt
	// (Installer-Spec, Komponente A, E7). Leer heisst: dieser Schritt hat
	// keinen Dashboard-Tab, der aus- oder eingeblendet werden muesste.
	DashboardKey string `json:"dashboard_key,omitempty"`
}

// CaddyInfo mirrors manifest.json's "caddy" object, present only when the
// bundle carries an HTTPS side-package (E13).
type CaddyInfo struct {
	Version string `json:"version"`
	File    string `json:"file"`
	SHA256  string `json:"sha256"`
}

// Manifest mirrors manifest.json exactly as Plan A-II's make_bundle.sh
// writes it (Vertrag 2). Field names and JSON tags are not renamed
// independently of that contract.
type Manifest struct {
	Version      string            `json:"version"`
	BuiltAt      string            `json:"built_at"`
	Arch         string            `json:"arch"`
	UnameMachine []string          `json:"uname_machine"`
	PythonMinor  string            `json:"python_minor"`
	PythonABI    string            `json:"python_abi"`
	TargetUser   string            `json:"target_user"`
	TargetBase   string            `json:"target_base"`
	Components   map[string]string `json:"components"`
	Wheels       map[string]string `json:"wheels"`
	Caddy        *CaddyInfo        `json:"caddy,omitempty"`
	Steps        []StepEntry       `json:"steps"`
	Files        map[string]string `json:"files"`
}

// LoadManifest reads manifest.json from a bundle directory. A missing file
// is reported as FaultManifestMissing -- the same code verify_bundle.sh
// uses for the identical situation on the node.
func LoadManifest(bundleDir string) (*Manifest, error) {
	path := filepath.Join(bundleDir, "manifest.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, &Error{Code: FaultManifestMissing, Message: path + " does not exist"}
		}
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	var m Manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	return &m, nil
}

// StepByID returns the step entry with the given id, or false if the
// manifest has none.
func (m *Manifest) StepByID(id string) (StepEntry, bool) {
	for _, s := range m.Steps {
		if s.ID == id {
			return s, true
		}
	}
	return StepEntry{}, false
}
