package webui

import (
	"io/fs"
	"regexp"
	"strings"
)

// tourTargetAttr matches a literal data-tour="<id>" in a template or script.
var tourTargetAttr = regexp.MustCompile(`data-tour="([a-z0-9_-]+)"`)

// tourTargetBind matches an x-bind:data-tour="<expression>". The expression
// may pick one of several ids with quoted literals, see tourTargetQuoted.
var tourTargetBind = regexp.MustCompile(`x-bind:data-tour="([^"]*)"`)

// tourTargetQuoted matches a quoted id inside an x-bind expression, 'own-account'.
var tourTargetQuoted = regexp.MustCompile(`'([a-z0-9_-]+)'`)

// tourTargetCall matches the ids that onboarding-steps.js passes to its data()
// helper, data('<id>') or data("<id>").
var tourTargetCall = regexp.MustCompile(`data\(['"]([a-z0-9_-]+)['"]\)`)

// tourActionCall matches the named actions that onboarding-actions.js
// registers, action('<name>', ...).
var tourActionCall = regexp.MustCompile(`action\('([a-z0-9_-]+)'`)

const onboardingStepsScript = "static/js/onboarding-steps.js"
const onboardingActionsScript = "static/js/onboarding-actions.js"

// tourTargetsIn returns the ids one file carries: the literal data-tour
// attributes and the quoted ids of x-bind:data-tour expressions.
func tourTargetsIn(raw []byte) map[string]bool {
	ids := map[string]bool{}
	for _, m := range tourTargetAttr.FindAllSubmatch(raw, -1) {
		ids[string(m[1])] = true
	}
	for _, m := range tourTargetBind.FindAllSubmatch(raw, -1) {
		for _, q := range tourTargetQuoted.FindAllSubmatch(m[1], -1) {
			ids[string(q[1])] = true
		}
	}
	return ids
}

// TourTargets returns every data-tour target the embedded web UI carries: the
// literal data-tour attributes and x-bind:data-tour ids of the templates and
// the static JS, the ids onboarding-steps.js names, and the named actions of
// onboarding-actions.js, written as "@name". The files are embedded, so the
// set does not change while the dashboard runs. Callers read it once at startup.
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
		for id := range tourTargetsIn(raw) {
			targets[id] = true
		}
		if path == onboardingStepsScript {
			for _, m := range tourTargetCall.FindAllSubmatch(raw, -1) {
				targets[string(m[1])] = true
			}
		}
		if path == onboardingActionsScript {
			for _, m := range tourActionCall.FindAllSubmatch(raw, -1) {
				targets["@"+string(m[1])] = true
			}
		}
		return nil
	})
	return targets
}
