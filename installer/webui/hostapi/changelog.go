package hostapi

import (
	"context"
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

// ChangelogView ist die Antwort von GET /api/changelog: was das Paket, das
// installiert werden soll, an Aenderungen mitbringt, und was auf dem Node
// schon installiert ist. Die Oberflaeche schneidet daraus "seit deiner Version".
type ChangelogView struct {
	BundleVersion string `json:"bundle_version"`
	// Installed bildet Komponenten-Id auf Version ab (siehe InstalledVersions).
	// Leer, aber nie null, wenn auf dem Node noch nichts aufgezeichnet ist.
	Installed map[string]string `json:"installed"`
	// Document ist die changelog.json des Pakets, unveraendert.
	Document json.RawMessage `json:"document"`
}

// ChangelogProvider ist eine optionale Faehigkeit eines Backends. Der Server
// prueft sie per Typzusicherung, damit weder jedes Backend noch jede Attrappe
// die Methode tragen muss; ein Backend ohne sie antwortet mit 501.
type ChangelogProvider interface {
	Changelog(ctx context.Context) (*ChangelogView, error)
}

func (s *Server) handleChangelog(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", r.Method)
		return
	}
	if !s.requireConnection(w) {
		return
	}
	provider, ok := s.opts.Backend.(ChangelogProvider)
	if !ok {
		writeError(w, http.StatusNotImplemented, "NOT_SUPPORTED", "")
		return
	}
	view, err := provider.Changelog(r.Context())
	if err != nil {
		writeBackendError(w, err)
		return
	}
	if view == nil {
		writeError(w, http.StatusNotFound, "NO_CHANGELOG", "")
		return
	}
	if view.Installed == nil {
		view.Installed = map[string]string{}
	}
	writeJSON(w, http.StatusOK, view)
}
