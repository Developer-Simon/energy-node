package versions

import (
	"cmp"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"

	"github.com/Developer-Simon/energy-node-webui/hostapi"
)

// HighlightsEntry is one highlighted change. I18n holds the text in other
// languages than English (changelog/highlights/<PR>.json in the repo), the
// page picks its own language and falls back to Text. ID is "h<PR>", the key
// the onboarding remembers a seen highlight by, empty without a PR.
type HighlightsEntry struct {
	ID       string            `json:"id,omitempty"`
	Text     string            `json:"text"`
	Scope    string            `json:"scope,omitempty"`
	PR       int               `json:"pr,omitempty"`
	Breaking bool              `json:"breaking"`
	I18n     map[string]string `json:"i18n,omitempty"`
	Details  *HighlightDetails `json:"details,omitempty"`
	Tour     *HighlightTour    `json:"tour,omitempty"`
}

// HighlightImage is the screenshot of a highlight, with the crop (x, y, width,
// height as fractions of the image) the dialog shows.
type HighlightImage struct {
	URL    string    `json:"url"`
	Width  int       `json:"width"`
	Height int       `json:"height"`
	Crop   []float64 `json:"crop,omitempty"`
}

// HighlightDetails is the optional extra part of a highlight: a longer text
// per language, an image and the doc page it links to.
type HighlightDetails struct {
	Text  map[string]string `json:"text"`
	Image *HighlightImage   `json:"image,omitempty"`
	Doc   string            `json:"doc,omitempty"`
}

// HighlightTour walks a user through a highlight in the web UI. A highlight
// with nothing to show carries the reason in Exempt and no steps.
type HighlightTour struct {
	Steps  []TourStep `json:"steps"`
	Exempt string     `json:"exempt,omitempty"`
}

// TourStep points at one data-tour target. Panel and Page say where it
// lives, Device is any, phone or desktop, Need the permission the step
// requires.
type TourStep struct {
	Panel  string            `json:"panel,omitempty"`
	Page   string            `json:"page,omitempty"`
	Target string            `json:"target"`
	Device string            `json:"device"`
	Need   string            `json:"need,omitempty"`
	Title  map[string]string `json:"title"`
	Text   map[string]string `json:"text"`
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
	Components []fullComponent `json:"components"`
}

type fullComponent struct {
	ID       string        `json:"id"`
	Label    string        `json:"label"`
	Version  string        `json:"version"`
	Releases []fullRelease `json:"releases"`
}

type fullRelease struct {
	Version string      `json:"version"`
	Date    string      `json:"date"`
	Groups  []fullGroup `json:"groups"`
}

type fullGroup struct {
	Entries []fullEntry `json:"entries"`
}

type fullEntry struct {
	Text      string            `json:"text"`
	Scope     string            `json:"scope"`
	PR        int               `json:"pr"`
	Breaking  bool              `json:"breaking"`
	Highlight *bool             `json:"highlight"`
	I18n      map[string]string `json:"i18n"`
	Details   *HighlightDetails `json:"details"`
	Tour      *HighlightTour    `json:"tour"`
}

// Highlights cuts the installed changelog.json down to the highlighted
// entries the last update brought: per component every release newer than
// its version in previous-manifest.json and not newer than the installed
// one. A component the previous manifest does not know (new in this
// package), and every component when there is no previous manifest, shows
// only the release matching its installed version. Deselected services and
// components without a highlighted entry in range are left out.
func Highlights(stateDir string) (*HighlightsView, error) {
	doc, view, deselected, err := readHighlightsInput(stateDir)
	if err != nil {
		return nil, err
	}

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
		// An unreadable optional file must not break the page: show the
		// installed release only, as without a previous manifest.
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
			appendRelease(&component, release, flagged)
		}
		if len(component.Releases) > 0 {
			view.Components = append(view.Components, component)
		}
	}
	return view, nil
}

// ExploreView is what "Neu erkunden" lists: the releases of the explore
// window, newest first, and the installed version as To.
type ExploreView struct {
	To       string
	Releases []ExploreRelease
}

// ExploreRelease is one release the explore list shows. Its entries are the
// dashboard release's own and those of the other component releases attached
// to it, the dashboard ones first.
type ExploreRelease struct {
	Version string
	Date    string
	Entries []ExploreEntry
}

// ExploreEntry is one highlight with the component release it came from.
// ComponentVersion is that release, for the dashboard the dashboard release.
type ExploreEntry struct {
	HighlightsEntry
	Component        string
	ComponentLabel   string
	ComponentVersion string
}

const dashboardComponent = "dashboard"

// Releases builds the "Neu erkunden" list. The window is the last minors
// minor versions among the dashboard releases not newer than the installed
// one, with every dashboard release of those minors. Each release of another
// component (not newer than its installed version) is attached to the
// earliest window release dated on or after its own date, or to the newest
// window release when there is none. Without a dashboard release in the
// window every component lists its own last minors as releases of their own.
// Entries are filtered like Highlights (flagged highlights, deselected
// services left out) and previous-manifest.json is ignored.
func Releases(stateDir string, minors int) (*ExploreView, error) {
	doc, view, deselected, err := readHighlightsInput(stateDir)
	if err != nil {
		return nil, err
	}
	flagged := documentIsFlagged(doc)
	components := make([]fullComponent, 0, len(doc.Components))
	for _, c := range doc.Components {
		if !isDeselected(c.ID, deselected) {
			components = append(components, c)
		}
	}

	var releases []ExploreRelease
	if i := slices.IndexFunc(components, func(c fullComponent) bool { return c.ID == dashboardComponent }); i >= 0 {
		releases = dashboardWindow(components[i], minors, flagged)
	}
	if len(releases) == 0 {
		releases = ownReleases(components, minors, flagged)
	} else {
		for _, c := range components {
			if c.ID != dashboardComponent {
				attachComponent(releases, c, flagged)
			}
		}
	}
	return &ExploreView{To: view.To, Releases: slices.DeleteFunc(releases, func(r ExploreRelease) bool { return len(r.Entries) == 0 })}, nil
}

// dashboardWindow lists the dashboard releases of the last minors minor
// versions, newest first, with the dashboard entries of each.
func dashboardWindow(c fullComponent, minors int, flagged bool) []ExploreRelease {
	window := latestMinors(c.Releases, c.Version, minors)
	var out []ExploreRelease
	for _, r := range c.Releases {
		if !window[MinorOf(r.Version)] || compareVersions(r.Version, c.Version) > 0 {
			continue
		}
		out = append(out, ExploreRelease{Version: r.Version, Date: r.Date, Entries: tagEntries(c, r.Version, releaseEntries(r, flagged))})
	}
	slices.SortStableFunc(out, func(a, b ExploreRelease) int { return compareVersions(b.Version, a.Version) })
	return out
}

// attachComponent appends the entries of every release of c to the window
// release each one belongs to, see attachIndex.
func attachComponent(releases []ExploreRelease, c fullComponent, flagged bool) {
	for _, r := range c.Releases {
		if compareVersions(r.Version, c.Version) > 0 {
			continue
		}
		entries := releaseEntries(r, flagged)
		if len(entries) == 0 {
			continue
		}
		i := attachIndex(releases, r.Date)
		releases[i].Entries = append(releases[i].Entries, tagEntries(c, r.Version, entries)...)
	}
}

// attachIndex is the index of the release a component release dated date
// belongs to: the earliest release dated on or after date, the newest one
// (index 0, releases are newest first) when none is. Dates compare as
// strings, they are YYYY-MM-DD. Equal dates go to the lower version.
func attachIndex(releases []ExploreRelease, date string) int {
	best, found := 0, false
	for i, r := range releases {
		if r.Date < date {
			continue
		}
		if !found || r.Date < releases[best].Date || (r.Date == releases[best].Date && compareVersions(r.Version, releases[best].Version) < 0) {
			best, found = i, true
		}
	}
	return best
}

// ownReleases is the fallback without a dashboard release in the window: per
// component the releases of its last minors, each as a release of its own.
func ownReleases(components []fullComponent, minors int, flagged bool) []ExploreRelease {
	var out []ExploreRelease
	for _, c := range components {
		window := latestMinors(c.Releases, c.Version, minors)
		for _, r := range c.Releases {
			if !window[MinorOf(r.Version)] || compareVersions(r.Version, c.Version) > 0 {
				continue
			}
			if entries := releaseEntries(r, flagged); len(entries) > 0 {
				out = append(out, ExploreRelease{Version: r.Version, Date: r.Date, Entries: tagEntries(c, r.Version, entries)})
			}
		}
	}
	return out
}

// tagEntries names the component and the component release the entries came
// from.
func tagEntries(c fullComponent, version string, entries []HighlightsEntry) []ExploreEntry {
	out := make([]ExploreEntry, 0, len(entries))
	for _, e := range entries {
		out = append(out, ExploreEntry{HighlightsEntry: e, Component: c.ID, ComponentLabel: c.Label, ComponentVersion: version})
	}
	return out
}

// readHighlightsInput reads the merged changelog and the installed manifest.
// The view carries To and an empty component list, deselected holds the
// services the user turned off.
func readHighlightsInput(stateDir string) (fullDocument, *HighlightsView, map[string]bool, error) {
	raw, err := hostapi.ReadChangelogDocument(stateDir)
	if err != nil {
		return fullDocument{}, nil, nil, err
	}
	var doc fullDocument
	if err := json.Unmarshal(raw, &doc); err != nil {
		return fullDocument{}, nil, nil, &hostapi.Error{Code: "CHANGELOG_UNREADABLE", Detail: err.Error()}
	}

	view := &HighlightsView{Components: []HighlightsComponent{}}
	var manifest manifestFile
	if installedRaw, err := os.ReadFile(filepath.Join(stateDir, "installed-manifest.json")); err == nil {
		if json.Unmarshal(installedRaw, &manifest) == nil {
			view.To = manifest.Version
		}
	}
	return doc, view, deselectedDirs(stateDir, manifest), nil
}

// appendRelease adds the entries of release to component. A release without
// entries is skipped.
func appendRelease(component *HighlightsComponent, release fullRelease, flagged bool) {
	entries := releaseEntries(release, flagged)
	if len(entries) > 0 {
		component.Releases = append(component.Releases, HighlightsRelease{Version: release.Version, Date: release.Date, Entries: entries})
	}
}

// releaseEntries lists the entries of release, the highlighted ones only when
// the document is flagged.
func releaseEntries(release fullRelease, flagged bool) []HighlightsEntry {
	var out []HighlightsEntry
	for _, group := range release.Groups {
		for _, e := range group.Entries {
			if flagged && (e.Highlight == nil || !*e.Highlight) {
				continue
			}
			out = append(out, HighlightsEntry{ID: highlightID(e.PR), Text: e.Text, Scope: e.Scope, PR: e.PR, Breaking: e.Breaking, I18n: e.I18n, Details: e.Details, Tour: e.Tour})
		}
	}
	return out
}

// latestMinors returns the major.minor keys (see MinorOf) of the count highest
// minor versions among releases not newer than installed.
func latestMinors(releases []fullRelease, installed string, count int) map[string]bool {
	seen := map[[2]int]bool{}
	for _, r := range releases {
		if compareVersions(r.Version, installed) > 0 {
			continue
		}
		p := versionParts(r.Version)
		seen[[2]int{p[0], p[1]}] = true
	}
	minors := make([][2]int, 0, len(seen))
	for m := range seen {
		minors = append(minors, m)
	}
	slices.SortFunc(minors, func(a, b [2]int) int {
		if a[0] != b[0] {
			return cmp.Compare(b[0], a[0])
		}
		return cmp.Compare(b[1], a[1])
	})
	out := map[string]bool{}
	for i := 0; i < len(minors) && i < count; i++ {
		out[strconv.Itoa(minors[i][0])+"."+strconv.Itoa(minors[i][1])] = true
	}
	return out
}

// MinorOf is the major.minor part of a version without a leading "v", as
// "0.10" for "v0.10.8". A version without numbers gives "0.0".
func MinorOf(version string) string {
	p := versionParts(version)
	return strconv.Itoa(p[0]) + "." + strconv.Itoa(p[1])
}

// highlightID is "h<PR>", empty for an entry without a PR number.
func highlightID(pr int) string {
	if pr == 0 {
		return ""
	}
	return "h" + strconv.Itoa(pr)
}

// FreshInstall reports whether the node has no previous-manifest.json, which
// means a first install: its highlights count as seen. An unreadable file
// counts as an update, so nothing is hidden by mistake.
func FreshInstall(stateDir string) bool {
	_, err := os.Stat(filepath.Join(stateDir, "previous-manifest.json"))
	return errors.Is(err, fs.ErrNotExist)
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
