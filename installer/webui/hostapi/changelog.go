package hostapi

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
)

// ChangelogFile ist der Name des Changelogs im Bundle und im Zustandsverzeichnis
// des Node. Er entsteht in scripts/build/make_changelog_json.py.
const ChangelogFile = "changelog.json"

// ChangelogDir enthaelt je Bundle-Version eine Datei <version>.json
// (scripts/build/make_changelog_json.py --out-dir). Im Bundle die letzten 20,
// auf dem Node alles, was je ankam: der Updater legt nur dazu.
const ChangelogDir = "changelog"

// ChangelogHistory ist der zusammengefuehrte Changelog. Total zaehlt die
// lesbaren Bundle-Versionen, Shown die davon zusammengefuehrten.
type ChangelogHistory struct {
	Document json.RawMessage
	Total    int
	Shown    int
}

var historyName = regexp.MustCompile(`^v?(\d+)\.(\d+)\.(\d+)\.json$`)

type historyFile struct {
	parts [3]int
	top   map[string]json.RawMessage
}

// ReadChangelogHistory fuehrt die neuesten <limit> Dateien aus changelog/
// zu einem Dokument im Schema von changelog.json zusammen (limit <= 0: alle).
// Komponenten in der Reihenfolge der neuesten Datei, Kopfdaten aus der
// neuesten Datei mit der Komponente, Releases neueste zuerst, doppelte
// Versionen gewinnt die neuere Datei. Ohne changelog/ gilt changelog.json
// allein (Bundles und Updater von vor der Aufteilung). Unlesbare Dateien
// fallen weg.
func ReadChangelogHistory(dir string, limit int) (*ChangelogHistory, error) {
	entries, err := os.ReadDir(filepath.Join(dir, ChangelogDir))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, &Error{Code: "CHANGELOG_UNREADABLE", Detail: err.Error()}
	}
	var files []historyFile
	for _, entry := range entries {
		match := historyName.FindStringSubmatch(entry.Name())
		if match == nil || entry.IsDir() {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, ChangelogDir, entry.Name()))
		if err != nil {
			continue
		}
		var top map[string]json.RawMessage
		if json.Unmarshal(raw, &top) != nil {
			continue
		}
		var file historyFile
		for i := range file.parts {
			file.parts[i], _ = strconv.Atoi(match[i+1])
		}
		file.top = top
		files = append(files, file)
	}
	if len(files) == 0 {
		raw, err := readSingleChangelog(dir)
		if err != nil {
			return nil, err
		}
		return &ChangelogHistory{Document: raw, Total: 1, Shown: 1}, nil
	}
	sort.Slice(files, func(i, j int) bool {
		for k := 0; k < 3; k++ {
			if files[i].parts[k] != files[j].parts[k] {
				return files[i].parts[k] > files[j].parts[k]
			}
		}
		return false
	})
	shown := files
	if limit > 0 && limit < len(files) {
		shown = files[:limit]
	}
	doc, err := mergeHistory(shown)
	if err != nil {
		return nil, &Error{Code: "CHANGELOG_UNREADABLE", Detail: err.Error()}
	}
	return &ChangelogHistory{Document: doc, Total: len(files), Shown: len(shown)}, nil
}

func mergeHistory(files []historyFile) (json.RawMessage, error) {
	var order []string
	heads := map[string]map[string]json.RawMessage{}
	releases := map[string][]json.RawMessage{}
	seen := map[string]map[string]bool{}
	for _, file := range files {
		var components []map[string]json.RawMessage
		if json.Unmarshal(file.top["components"], &components) != nil {
			continue
		}
		for _, component := range components {
			var id string
			if json.Unmarshal(component["id"], &id) != nil || id == "" {
				continue
			}
			if _, known := heads[id]; !known {
				order = append(order, id)
				heads[id] = component
				seen[id] = map[string]bool{}
			}
			var list []json.RawMessage
			_ = json.Unmarshal(component["releases"], &list)
			for _, release := range list {
				var head struct {
					Version string `json:"version"`
				}
				if json.Unmarshal(release, &head) != nil || seen[id][head.Version] {
					continue
				}
				seen[id][head.Version] = true
				releases[id] = append(releases[id], release)
			}
		}
	}
	components := make([]map[string]json.RawMessage, 0, len(order))
	for _, id := range order {
		component := heads[id]
		list := releases[id]
		if list == nil {
			list = []json.RawMessage{}
		}
		component["releases"], _ = json.Marshal(list)
		components = append(components, component)
	}
	top := map[string]json.RawMessage{}
	for key, value := range files[0].top {
		if key != "components" && key != "previous_bundle_version" {
			top[key] = value
		}
	}
	top["components"], _ = json.Marshal(components)
	return json.Marshal(top)
}

// ReadChangelogDocument liefert den ganzen Changelog eines Verzeichnisses:
// alle Dateien aus changelog/ zusammengefuehrt, sonst changelog.json. Fehlt
// beides, kommt NO_CHANGELOG (404): aeltere Bundles haben keinen Changelog,
// und das ist kein Fehler des Betriebs, nur ein Zustand, den die Oberflaeche
// benennen kann.
func ReadChangelogDocument(dir string) (json.RawMessage, error) {
	history, err := ReadChangelogHistory(dir, 0)
	if err != nil {
		return nil, err
	}
	return history.Document, nil
}

// readSingleChangelog liest <dir>/changelog.json unveraendert.
func readSingleChangelog(dir string) (json.RawMessage, error) {
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
