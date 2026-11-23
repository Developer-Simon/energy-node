package monitoring

import (
	"fmt"
	"sort"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/autolearn"
	"github.com/Developer-Simon/energy-node-dashboard/internal/energy"
	"github.com/Developer-Simon/energy-node-dashboard/internal/settings"
)

// Step ist ein empfohlener Schritt. Code ist der Katalogschlüssel
// monitoring.step.<code>, Target das Sprungziel (leer: nur Text).
type Step struct {
	Code   string   `json:"code"`
	Target string   `json:"target,omitempty"`
	IDs    []string `json:"ids,omitempty"`
	Chain  string   `json:"chain,omitempty"`
}

// Ack ist eine Quittung: Zeitpunkt in Unix-Sekunden und Nutzername.
type Ack struct {
	At   int64  `json:"at"`
	User string `json:"user"`
}

// Notice ist eine Meldung. Since ist der früheste bekannte Beginn aus der
// Quelle (Kettenbefund), sonst null. FirstSeen und Ack setzt der Store.
type Notice struct {
	ID       string              `json:"id"`
	Source   string              `json:"source"`   // energy, chain, learn
	Severity string              `json:"severity"` // warn, info
	Code     string              `json:"code"`
	Params   map[string]any      `json:"params,omitempty"`
	Evidence *autolearn.Evidence `json:"evidence,omitempty"`
	// Facts are the figures of a learned finding, shown under its text.
	Facts     []autolearn.Fact `json:"facts,omitempty"`
	Steps     []Step           `json:"steps"`
	FirstSeen int64            `json:"first_seen"`
	Ack       *Ack             `json:"ack"`
	Since     time.Time        `json:"-"`
}

// Input sind die Quellen einer Sammlung.
type Input struct {
	Snapshot energy.Snapshot
	Config   settings.EnergyConfig
	Findings []autolearn.Finding
	// Suggestions are the suggestions of the band as the automations page shows
	// them. Each one becomes a notice that opens it there.
	Suggestions []autolearn.Finding
}

// Collect baut die Meldungen in fester Reihenfolge: energy, chain, learn.
func Collect(in Input) []Notice {
	out := []Notice{}
	out = append(out, energyNotices(in)...)
	out = append(out, chainNotices(in.Snapshot)...)
	out = append(out, learnNotices(in.Findings)...)
	out = append(out, suggestionNotices(in.Suggestions)...)
	return out
}

func hasLoadRole(assignments map[string]energy.Assignment) bool {
	for _, a := range assignments {
		if a.Role == energy.RoleLoad {
			return true
		}
	}
	return false
}

func energyNotices(in Input) []Notice {
	snap := in.Snapshot
	var out []Notice

	if in.Config.Interpretation.LoadMode == energy.LoadModeMeasured && !hasLoadRole(in.Config.Assignments) {
		out = append(out, Notice{
			ID:       "energy:measured_no_meter",
			Source:   "energy",
			Severity: "warn",
			Code:     "measured_no_meter",
			Steps: []Step{
				{Code: "set_load_role", Target: "energy"},
				{Code: "load_mode_auto", Target: "energy"},
			},
		})
	}

	if snap.BatterySoCWithoutCapacity > 0 {
		out = append(out, Notice{
			ID:       "energy:soc_capacity",
			Source:   "energy",
			Severity: "warn",
			Code:     "soc_capacity",
			Params:   map[string]any{"count": snap.BatterySoCWithoutCapacity},
			Steps: []Step{
				{Code: "enter_capacity", Target: "energy"},
			},
		})
	}

	if snap.UnassignedCount > 0 {
		names := []string{}
		ids := []string{}
		for _, e := range snap.Unassigned {
			ids = append(ids, e.EntityID)
			if len(names) < 4 {
				name := e.Name
				if name == "" {
					name = e.EntityID
				}
				names = append(names, fmt.Sprintf("%s: %.0f %s", name, e.Value, e.Unit))
			}
		}
		out = append(out, Notice{
			ID:       "energy:unassigned",
			Source:   "energy",
			Severity: "info",
			Code:     "unassigned",
			Params: map[string]any{
				"count": snap.UnassignedCount,
				"names": names,
				"more":  snap.UnassignedCount - len(names),
			},
			Steps: []Step{
				{Code: "assign_roles", Target: "energy.focus", IDs: ids},
				{Code: "hide_values", Target: "energy"},
			},
		})
	}

	if len(in.Config.RoleNeeded) > 0 {
		ids := append([]string{}, in.Config.RoleNeeded...)
		sort.Strings(ids)
		out = append(out, Notice{
			ID:       "energy:role_needed",
			Source:   "energy",
			Severity: "info",
			Code:     "role_needed",
			Params:   map[string]any{"ids": ids},
			Steps: []Step{
				{Code: "set_role", Target: "energy.focus", IDs: ids},
				{Code: "role_keep", Target: "action.role_keep", IDs: ids},
			},
		})
	}

	return out
}

func chainNotices(snap energy.Snapshot) []Notice {
	var out []Notice
	for _, chain := range snap.Chains {
		for _, f := range chain.Findings {
			steps := []Step{{Code: "chain_open", Target: "energy.chain", Chain: f.ChainID}}
			if f.Suggestion != nil {
				steps = append(steps, Step{Code: "chain_suggestion", Target: "energy.chain", Chain: f.ChainID})
			}
			steps = append(steps,
				Step{Code: "efficiency_open", Target: "efficiency"},
				Step{Code: "devicemap_check", Target: "devicemap"},
			)
			out = append(out, Notice{
				ID:       fmt.Sprintf("chain:%s:%s", f.ChainID, f.Kind),
				Source:   "chain",
				Severity: "warn",
				Code:     "chain",
				Params: map[string]any{
					"kind":  f.Kind,
					"chain": f.ChainLabel,
					"from":  f.From,
					"to":    f.To,
					"since": f.Since.Format(time.RFC3339),
				},
				Steps: steps,
				Since: f.Since,
			})
		}
	}
	return out
}

func learnSeverity(s string) string {
	switch s {
	case "warn", "bad":
		return "warn"
	case "info":
		return "info"
	default:
		return "info"
	}
}

// learnNotices lists the learned findings that no rule shows. A finding with
// RuleIDs or Apply belongs to a rule, and the automations page marks that rule
// with it. A regulator finding is bound to its scenario, which has no mark, so
// it is listed here as well.
func learnNotices(findings []autolearn.Finding) []Notice {
	var out []Notice
	for _, f := range findings {
		if (len(f.RuleIDs) != 0 || len(f.Apply) != 0) && f.Kind != "regulator" {
			continue
		}
		params := make(map[string]any, len(f.Summary.Params)+1)
		for k, v := range f.Summary.Params {
			params[k] = v
		}
		params["kind"] = f.Kind

		var steps []Step
		if f.Kind == "shade" {
			steps = append(steps,
				Step{Code: "shade_look"},
				Step{Code: "shade_accept"},
			)
		}
		steps = append(steps, Step{Code: "learned_open", Target: "learned"})

		n := Notice{
			ID:       "learn:" + f.ID,
			Source:   "learn",
			Severity: learnSeverity(f.Severity),
			Code:     f.Summary.Code,
			Params:   params,
			Steps:    steps,
		}
		if f.Evidence.Shape != "" {
			ev := f.Evidence
			n.Evidence = &ev
		}
		if len(f.Facts) > 0 {
			n.Facts = append([]autolearn.Fact(nil), f.Facts...)
		}
		out = append(out, n)
	}
	return out
}

// suggestionNotices turns the visible suggestions into notices. The step opens
// the suggestion on the automations page. Their source is suggest, the page lists
// them under the filter Vorschläge.
func suggestionNotices(list []autolearn.Finding) []Notice {
	var out []Notice
	for _, f := range list {
		params := make(map[string]any, len(f.Summary.Params)+1)
		for k, v := range f.Summary.Params {
			params[k] = v
		}
		params["kind"] = f.Kind
		n := Notice{
			ID:       "suggest:" + f.ID,
			Source:   "suggest",
			Severity: learnSeverity(f.Severity),
			Code:     f.Summary.Code,
			Params:   params,
			Steps:    []Step{{Code: "suggestion_open", Target: "suggestion", IDs: []string{f.ID}}},
		}
		if f.Evidence.Shape != "" {
			ev := f.Evidence
			n.Evidence = &ev
		}
		out = append(out, n)
	}
	return out
}
