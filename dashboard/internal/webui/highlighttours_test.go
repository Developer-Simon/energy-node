package webui_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/Developer-Simon/energy-node-dashboard/internal/auth"
	"github.com/Developer-Simon/energy-node-dashboard/internal/webui"
)

func TestCollectTourRefsReadsPRFilesAndArchives(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("200.json", `{"de":{"*":"x"},"tour":{"*":{"steps":[{"panel":"settings-panel","page":"users","target":"alpha","need":"view_devices","open":["alpha-tab","@beta"],"title":{},"text":{}}],"replaced_by":{"pr":53,"step":2}}}}`)
	write("archive-v0.json", `{"53":{"tour":{"dashboard":{"steps":[{"target":"beta","title":{},"text":{}},{"target":"gamma","title":{},"text":{}}]}}},"54":{"de":{"*":"y"}}}`)
	write("v0.10.8.json", `{"158":{"tour":{"*":{"exempt":"nothing to show"}}}}`)
	write("README.md", "# not json")
	refs, err := collectTourRefs(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := []tourRef{
		{File: "200.json", PR: "200", Key: "*", Step: 1, Panel: "settings-panel", Page: "users", Target: "alpha", Need: "view_devices",
			Open: []string{"alpha-tab", "@beta"}, ReplacedPR: 53, ReplacedStep: 2},
		{File: "archive-v0.json", PR: "53", Key: "dashboard", Step: 1, Target: "beta"},
		{File: "archive-v0.json", PR: "53", Key: "dashboard", Step: 2, Target: "gamma"},
	}
	if !reflect.DeepEqual(refs, want) {
		t.Fatalf("refs = %+v\nwant %+v", refs, want)
	}
}

// tourRef is one tour step as stored in changelog/highlights/.
type tourRef struct {
	File, PR, Key string
	Step          int
	Panel, Page   string
	Target, Need  string
	Open          []string
	// ReplacedPR and ReplacedStep are set on the first step of a tour with replaced_by.
	ReplacedPR, ReplacedStep int
}

// tourFile is the part of a highlight JSON file the tests read.
type tourFile struct {
	Tour map[string]struct {
		Steps []struct {
			Panel, Page, Target, Need string
			Open                      []string `json:"open"`
		}
		ReplacedBy *struct {
			PR   int `json:"pr"`
			Step int `json:"step"`
		} `json:"replaced_by"`
	}
}

var numberedHighlightFile = regexp.MustCompile(`^\d+\.json$`)

// collectTourRefs reads the tour steps of all highlight files in dir. A
// <N>.json file holds one entry for PR N, archive-v*.json and v*.json hold a
// map from PR number to entry. Other files are skipped.
func collectTourRefs(dir string) ([]tourRef, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var refs []tourRef
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".json") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return nil, err
		}
		switch {
		case numberedHighlightFile.MatchString(name):
			var entry tourFile
			if err := json.Unmarshal(raw, &entry); err != nil {
				return nil, fmt.Errorf("%s: %w", name, err)
			}
			refs = appendTourRefs(refs, name, strings.TrimSuffix(name, ".json"), entry)
		case strings.HasPrefix(name, "archive-v") || strings.HasPrefix(name, "v"):
			var byPR map[string]tourFile
			if err := json.Unmarshal(raw, &byPR); err != nil {
				return nil, fmt.Errorf("%s: %w", name, err)
			}
			for pr, entry := range byPR {
				if _, err := strconv.Atoi(pr); err != nil {
					return nil, fmt.Errorf("%s: PR key %q is not a number", name, pr)
				}
				refs = appendTourRefs(refs, name, pr, entry)
			}
		}
	}
	sort.SliceStable(refs, func(i, j int) bool {
		a, b := refs[i], refs[j]
		if a.File != b.File {
			return a.File < b.File
		}
		pa, _ := strconv.Atoi(a.PR)
		pb, _ := strconv.Atoi(b.PR)
		if pa != pb {
			return pa < pb
		}
		if a.Key != b.Key {
			return a.Key < b.Key
		}
		return a.Step < b.Step
	})
	return refs, nil
}

func appendTourRefs(refs []tourRef, file, pr string, entry tourFile) []tourRef {
	for key, tour := range entry.Tour {
		for i, s := range tour.Steps {
			ref := tourRef{
				File: file, PR: pr, Key: key, Step: i + 1,
				Panel: s.Panel, Page: s.Page, Target: s.Target, Need: s.Need, Open: s.Open,
			}
			if i == 0 && tour.ReplacedBy != nil {
				ref.ReplacedPR = tour.ReplacedBy.PR
				ref.ReplacedStep = tour.ReplacedBy.Step
			}
			refs = append(refs, ref)
		}
	}
	return refs
}

// Panels mit Reiter und die Seiten der Einstellungen, wie ?panel=&page= sie kennt.
var tourPanels = map[string]bool{
	"overview-panel": true, "devices-panel": true, "history-panel": true, "stats-panel": true,
	"energy-panel": true, "devicemap-panel": true, "config-panel": true, "automations-panel": true,
	"diagnostics-panel": true, "settings-panel": true,
}
var tourPages = map[string]bool{
	"general": true, "display": true, "history": true, "mqtt": true, "system": true,
	"tailscale": true, "tiny-tuya": true, "users": true, "versions": true, "checklist": true, "backup": true,
}

func TestHighlightToursPointAtKnownTargets(t *testing.T) {
	refs, err := collectTourRefs(filepath.Join("..", "..", "..", "changelog", "highlights"))
	if err != nil {
		t.Fatal(err)
	}
	targets := webui.TourTargets()
	roles := map[string]bool{}
	for _, r := range auth.AllRoles() {
		roles[r] = true
	}
	for _, ref := range refs {
		at := fmt.Sprintf("%s #%s %s step %d", ref.File, ref.PR, ref.Key, ref.Step)
		if !targets[ref.Target] {
			t.Errorf("%s: target %q is no literal data-tour of the web UI", at, ref.Target)
		}
		if ref.Need != "" && !roles[ref.Need] {
			t.Errorf("%s: need %q is not a role of auth.AllRoles()", at, ref.Need)
		}
		if ref.Panel != "" && !tourPanels[ref.Panel] {
			t.Errorf("%s: panel %q is unknown", at, ref.Panel)
		}
		if ref.Page != "" && (ref.Panel != "settings-panel" || !tourPages[ref.Page]) {
			t.Errorf("%s: page %q needs panel settings-panel and a known settings page", at, ref.Page)
		}
		for _, id := range ref.Open {
			if !targets[id] {
				t.Errorf("%s: open %q is no data-tour of the web UI and no action of onboarding-actions.js", at, id)
			}
		}
	}
	// stepsByPR is the number of steps of the longest tour of each PR.
	stepsByPR := map[string]int{}
	for _, ref := range refs {
		if ref.Step > stepsByPR[ref.PR] {
			stepsByPR[ref.PR] = ref.Step
		}
	}
	for _, ref := range refs {
		if ref.ReplacedPR == 0 {
			continue
		}
		pr := strconv.Itoa(ref.ReplacedPR)
		if pr == ref.PR || stepsByPR[pr] < ref.ReplacedStep {
			t.Errorf("%s #%s %s: replaced_by #%d step %d does not exist", ref.File, ref.PR, ref.Key, ref.ReplacedPR, ref.ReplacedStep)
		}
	}
}
