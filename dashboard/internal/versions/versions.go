// Package versions answers "what is installed on this node?": the bundle it
// came from, the version of every component in it, and what changed. It only
// reads the files the updater and the installer leave in the installer state
// directory (installed-manifest.json, selection.json, changelog.json); it
// never writes and never talks to GitHub.
package versions

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Developer-Simon/energy-node-webui/hostapi"
)

// DefaultStateDir is where installer and updater record what they applied.
const DefaultStateDir = "/var/lib/energy-node-installer"

// fallbackComponents names the bundle's components when the bundle carries no
// changelog.json (every bundle before plan 2). It mirrors the "bundle": true
// rows of scripts/version/components.json;
// TestFallbackComponentsMatchTheComponentTable fails when the two drift.
var fallbackComponents = map[string]struct{ label, kind string }{
	"dashboard":             {"Dashboard", "app"},
	"services":              {"Shared services", "shared"},
	"service:apsystems_ez1": {"APsystems EZ1", "service"},
	"service:automation":    {"Automation", "service"},
	"service:battery_soc":   {"Battery SoC", "service"},
	"service:shelly":        {"Shelly", "service"},
	"service:trucki":        {"Trucki", "service"},
	"service:tuya_mqtt":     {"Tuya", "service"},
	"energy_node_common":    {"energy_node_common", "library"},
	"battery_soc_core":      {"battery_soc_core", "library"},
	"bootstrap":             {"Bootstrap", "tool"},
}

// fallbackMeta returns label and kind for id. A service the table does not
// know yet (a bundle newer than this dashboard) is still a service, named by
// its directory; anything else unknown keeps its id and no kind.
func fallbackMeta(id string) (label, kind string) {
	if meta, ok := fallbackComponents[id]; ok {
		return meta.label, meta.kind
	}
	if dir, ok := strings.CutPrefix(id, "service:"); ok {
		return dir, "service"
	}
	return id, ""
}

// Bundle describes the package the node was installed from.
type Bundle struct {
	Version string `json:"version"`
	BuiltAt string `json:"built_at,omitempty"`
	Arch    string `json:"arch,omitempty"`
}

// Running is what this very process reports, which can differ from the bundle
// after a dashboard self-update whose package has not been rebuilt.
type Running struct {
	Dashboard string `json:"dashboard"`
}

// Component is one row of the versions page.
type Component struct {
	ID        string `json:"id"`
	Label     string `json:"label"`
	Kind      string `json:"kind"`
	Version   string `json:"version"`
	Installed bool   `json:"installed"`
}

// Snapshot is the answer of GET /api/v1/versions. Bundle is nil and Components
// empty on a node the installer never touched (a development checkout).
type Snapshot struct {
	Bundle       *Bundle     `json:"bundle"`
	Running      Running     `json:"running"`
	Components   []Component `json:"components"`
	HasChangelog bool        `json:"has_changelog"`
}

type manifestFile struct {
	Version    string            `json:"version"`
	BuiltAt    string            `json:"built_at"`
	Arch       string            `json:"arch"`
	Components map[string]string `json:"components"`
	Steps      []struct {
		ID       string `json:"id"`
		Optional bool   `json:"optional"`
		Dir      string `json:"dir"`
	} `json:"steps"`
}

type document struct {
	Components []struct {
		ID      string `json:"id"`
		Label   string `json:"label"`
		Kind    string `json:"kind"`
		Version string `json:"version"`
	} `json:"components"`
}

// Load builds the snapshot from stateDir. running is the dashboard's own
// build version. A missing installed-manifest.json is not an error.
func Load(stateDir, running string) (*Snapshot, error) {
	snap := &Snapshot{Running: Running{Dashboard: running}, Components: []Component{}}

	raw, err := os.ReadFile(filepath.Join(stateDir, "installed-manifest.json"))
	if errors.Is(err, fs.ErrNotExist) {
		return snap, nil
	}
	if err != nil {
		return nil, err
	}
	var manifest manifestFile
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return nil, err
	}
	snap.Bundle = &Bundle{Version: manifest.Version, BuiltAt: manifest.BuiltAt, Arch: manifest.Arch}

	deselected := deselectedDirs(stateDir, manifest)

	if doc, err := readDocument(stateDir); err == nil {
		snap.HasChangelog = true
		for _, c := range doc.Components {
			snap.Components = append(snap.Components, Component{
				ID: c.ID, Label: c.Label, Kind: c.Kind, Version: c.Version,
				Installed: !isDeselected(c.ID, deselected),
			})
		}
		return snap, nil
	}

	// A bundle from before changelog.json existed: list what the manifest knows,
	// named through fallbackComponents.
	installed, err := hostapi.InstalledVersions(raw)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(installed))
	for id := range installed {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		label, kind := fallbackMeta(id)
		snap.Components = append(snap.Components, Component{
			ID: id, Label: label, Kind: kind, Version: installed[id], Installed: !isDeselected(id, deselected),
		})
	}
	return snap, nil
}

func readDocument(stateDir string) (*document, error) {
	raw, err := hostapi.ReadChangelogDocument(stateDir)
	if err != nil {
		return nil, err
	}
	var doc document
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}
	return &doc, nil
}

// deselectedDirs returns the service directories the operator switched off: a
// step counts only when it is optional and selection.json says false for it.
// No selection.json means everything is on.
func deselectedDirs(stateDir string, manifest manifestFile) map[string]bool {
	out := map[string]bool{}
	raw, err := os.ReadFile(filepath.Join(stateDir, "selection.json"))
	if err != nil {
		return out
	}
	var selection struct {
		Steps map[string]bool `json:"steps"`
	}
	if json.Unmarshal(raw, &selection) != nil {
		return out
	}
	for _, step := range manifest.Steps {
		if step.Dir == "" || !step.Optional {
			continue
		}
		if on, known := selection.Steps[step.ID]; known && !on {
			out[step.Dir] = true
		}
	}
	return out
}

func isDeselected(id string, deselected map[string]bool) bool {
	const prefix = "service:"
	return len(id) > len(prefix) && id[:len(prefix)] == prefix && deselected[id[len(prefix):]]
}

// Changelog returns the installed changelog.json. With no ids it is the file
// verbatim; with ids only those components stay (same schema).
func Changelog(stateDir string, ids []string) (json.RawMessage, error) {
	raw, err := hostapi.ReadChangelogDocument(stateDir)
	if err != nil || len(ids) == 0 {
		return raw, err
	}
	want := make(map[string]bool, len(ids))
	for _, id := range ids {
		want[id] = true
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil {
		return nil, &hostapi.Error{Code: "CHANGELOG_UNREADABLE", Detail: err.Error()}
	}
	var components []json.RawMessage
	if err := json.Unmarshal(top["components"], &components); err != nil {
		return nil, &hostapi.Error{Code: "CHANGELOG_UNREADABLE", Detail: err.Error()}
	}
	kept := make([]json.RawMessage, 0, len(components))
	for _, component := range components {
		var head struct {
			ID string `json:"id"`
		}
		if json.Unmarshal(component, &head) == nil && want[head.ID] {
			kept = append(kept, component)
		}
	}
	top["components"], _ = json.Marshal(kept)
	return json.Marshal(top)
}
