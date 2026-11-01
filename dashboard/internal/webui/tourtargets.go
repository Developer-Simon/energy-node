package webui

import (
	"io/fs"
	"regexp"
	"strings"
)

// tourTargetAttr matches a literal data-tour="<id>" in a template or script.
var tourTargetAttr = regexp.MustCompile(`data-tour="([a-z0-9_-]+)"`)

// tourTargetCall matches the ids that onboarding-steps.js passes to its data()
// helper, data('<id>') or data("<id>").
var tourTargetCall = regexp.MustCompile(`data\(['"]([a-z0-9_-]+)['"]\)`)

const onboardingStepsScript = "static/js/onboarding-steps.js"

// TourTargets returns every data-tour target the embedded web UI carries: the
// literal data-tour attributes of the templates and the static JS, and the ids
// onboarding-steps.js names. The files are embedded, so the set does not
// change while the dashboard runs. Callers read it once at startup.
func TourTargets() map[string]bool {
	targets := map[string]bool{}
	_ = fs.WalkDir(templateFS, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		inTemplates := strings.HasPrefix(path, "templates/") && strings.HasSuffix(path, ".html")
		inScripts := strings.HasPrefix(path, "static/js/") && strings.HasSuffix(path, ".js")
		if !inTemplates && !inScripts {
			return nil
		}
		raw, err := fs.ReadFile(templateFS, path)
		if err != nil {
			return nil
		}
		for _, m := range tourTargetAttr.FindAllSubmatch(raw, -1) {
			targets[string(m[1])] = true
		}
		if path == onboardingStepsScript {
			for _, m := range tourTargetCall.FindAllSubmatch(raw, -1) {
				targets[string(m[1])] = true
			}
		}
		return nil
	})
	return targets
}
