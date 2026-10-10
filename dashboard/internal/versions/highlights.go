package versions

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"

	"github.com/Developer-Simon/energy-node-webui/hostapi"
)

// HighlightsEntry is one highlighted change.
type HighlightsEntry struct {
	Text     string `json:"text"`
	Scope    string `json:"scope,omitempty"`
	PR       int    `json:"pr,omitempty"`
	Breaking bool   `json:"breaking"`
}

// HighlightsRelease is one release of a component inside the range.
type HighlightsRelease struct {
	Version string            `json:"version"`
	Date    string            `json:"date,omitempty"`
	Entries []HighlightsEntry `json:"entries"`
}

// HighlightsComponent is one component that changed inside the range.
type HighlightsComponent struct {
	ID       string              `json:"id"`
	Label    string              `json:"label"`
	From     string              `json:"from,omitempty"`
	To       string              `json:"to"`
	Releases []HighlightsRelease `json:"releases"`
}

// HighlightsView is the answer of GET /api/v1/changelog/highlights: what the
// last update brought. SincePrevious is false when the node has no
// previous-manifest.json (a first install, or an update from before the
// updater kept one); every component then shows its installed release only.
type HighlightsView struct {
	From          string                `json:"from,omitempty"`
	To            string                `json:"to"`
	SincePrevious bool                  `json:"since_previous"`
	Components    []HighlightsComponent `json:"components"`
}

type fullDocument struct {
	Components []struct {
		ID       string `json:"id"`
		Label    string `json:"label"`
		Version  string `json:"version"`
		Releases []struct {
			Version string `json:"version"`
			Date    string `json:"date"`
			Groups  []struct {
				Entries []struct {
					Text      string `json:"text"`
					Scope     string `json:"scope"`
					PR        int    `json:"pr"`
					Breaking  bool   `json:"breaking"`
					Highlight *bool  `json:"highlight"`
				} `json:"entries"`
			} `json:"groups"`
		} `json:"releases"`
	} `json:"components"`
}

// Highlights cuts the installed changelog.json down to the highlighted
// entries the last update brought: per component every release newer than
// its version in previous-manifest.json and not newer than the installed
// one. A component the previous manifest does not know (new in this
// package), and every component when there is no previous manifest, shows
// only the release matching its installed version. Deselected services and
// components without a highlighted entry in range are left out.
func Highlights(stateDir string) (*HighlightsView, error) {
	raw, err := hostapi.ReadChangelogDocument(stateDir)
	if err != nil {
		return nil, err
	}
	var doc fullDocument
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, &hostapi.Error{Code: "CHANGELOG_UNREADABLE", Detail: err.Error()}
	}

	view := &HighlightsView{Components: []HighlightsComponent{}}
	var manifest manifestFile
	if installedRaw, err := os.ReadFile(filepath.Join(stateDir, "installed-manifest.json")); err == nil {
		if json.Unmarshal(installedRaw, &manifest) == nil {
			view.To = manifest.Version
		}
	}
	deselected := deselectedDirs(stateDir, manifest)

	previous := map[string]string{}
	previousRaw, err := os.ReadFile(filepath.Join(stateDir, "previous-manifest.json"))
	switch {
	case err == nil:
		var head struct {
			Version string `json:"version"`
		}
		if json.Unmarshal(previousRaw, &head) == nil {
			if versions, err := hostapi.InstalledVersions(previousRaw); err == nil {
				previous = versions
				view.From = head.Version
				view.SincePrevious = true
			}
		}
	case !errors.Is(err, fs.ErrNotExist):
		return nil, err
	}

	flagged := documentIsFlagged(doc)
	for _, c := range doc.Components {
		if isDeselected(c.ID, deselected) {
			continue
		}
		from, known := previous[c.ID]
		component := HighlightsComponent{ID: c.ID, Label: c.Label, To: c.Version}
		if view.SincePrevious && known {
			component.From = from
		}
		for _, release := range c.Releases {
			inRange := compareVersions(release.Version, c.Version) == 0
			if component.From != "" {
				inRange = compareVersions(release.Version, from) > 0 && compareVersions(release.Version, c.Version) <= 0
			}
			if !inRange {
				continue
			}
			out := HighlightsRelease{Version: release.Version, Date: release.Date, Entries: []HighlightsEntry{}}
			for _, group := range release.Groups {
				for _, e := range group.Entries {
					if flagged && (e.Highlight == nil || !*e.Highlight) {
						continue
					}
					out.Entries = append(out.Entries, HighlightsEntry{Text: e.Text, Scope: e.Scope, PR: e.PR, Breaking: e.Breaking})
				}
			}
			if len(out.Entries) > 0 {
				component.Releases = append(component.Releases, out)
			}
		}
		if len(component.Releases) > 0 {
			view.Components = append(view.Components, component)
		}
	}
	return view, nil
}

// documentIsFlagged mirrors isFlagged in versions.page.js: a changelog.json
// from before the highlight flag carries none, and then every entry counts.
func documentIsFlagged(doc fullDocument) bool {
	for _, c := range doc.Components {
		for _, r := range c.Releases {
			for _, g := range r.Groups {
				for _, e := range g.Entries {
					if e.Highlight != nil {
						return true
					}
				}
			}
		}
	}
	return false
}

var versionNumbers = regexp.MustCompile(`^v?(\d+)(?:\.(\d+))?(?:\.(\d+))?`)

// compareVersions compares the X.Y.Z part of two version strings and ignores
// a "v" prefix and any suffix ("-dev", "-branch.3"). A string without a
// number sorts as 0.0.0.
func compareVersions(a, b string) int {
	pa, pb := versionParts(a), versionParts(b)
	for i := range pa {
		switch {
		case pa[i] < pb[i]:
			return -1
		case pa[i] > pb[i]:
			return 1
		}
	}
	return 0
}

func versionParts(v string) [3]int {
	var out [3]int
	match := versionNumbers.FindStringSubmatch(v)
	for i := 1; match != nil && i <= 3; i++ {
		out[i-1], _ = strconv.Atoi(match[i])
	}
	return out
}
