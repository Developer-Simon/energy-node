package hostapi

import (
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
)

// ChangelogFile ist der Name des Changelogs im Bundle und im Zustandsverzeichnis
// des Node. Er entsteht in scripts/build/make_changelog_json.py.
const ChangelogFile = "changelog.json"

// ReadChangelogDocument liest <dir>/changelog.json unveraendert. Fehlt die Datei,
// kommt NO_CHANGELOG (404): aeltere Bundles haben keinen Changelog, und das ist
// kein Fehler des Betriebs, nur ein Zustand, den die Oberflaeche benennen kann.
func ReadChangelogDocument(dir string) (json.RawMessage, error) {
	raw, err := os.ReadFile(filepath.Join(dir, ChangelogFile))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, &Error{Code: "NO_CHANGELOG", Status: http.StatusNotFound}
		}
		return nil, &Error{Code: "CHANGELOG_UNREADABLE", Detail: err.Error()}
	}
	if !json.Valid(raw) {
		return nil, &Error{Code: "CHANGELOG_UNREADABLE", Detail: "not valid JSON"}
	}
	return raw, nil
}

// InstalledVersions liest aus einem Bundle-Manifest (manifest.json bzw. der
// installed-manifest.json-Kopie), welche Version jede Komponente hat. Die
// Schluessel sind die Komponenten-Ids des Changelogs: die Namen aus
// "components" (dashboard, services, bootstrap, ...) und "service:<dir>" fuer
// die Version, die ein Dienst-Schritt unter "steps[].version" traegt.
func InstalledVersions(manifest []byte) (map[string]string, error) {
	var parsed struct {
		Components map[string]string `json:"components"`
		Steps      []struct {
			Dir     string `json:"dir"`
			Version string `json:"version"`
		} `json:"steps"`
	}
	if err := json.Unmarshal(manifest, &parsed); err != nil {
		return nil, err
	}
	out := make(map[string]string, len(parsed.Components)+len(parsed.Steps))
	for name, version := range parsed.Components {
		out[name] = version
	}
	for _, step := range parsed.Steps {
		if step.Dir != "" && step.Version != "" {
			out["service:"+step.Dir] = step.Version
		}
	}
	return out, nil
}
