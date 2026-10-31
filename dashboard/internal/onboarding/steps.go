package onboarding

import (
	"slices"
	"strings"

	"github.com/Developer-Simon/energy-node-dashboard/internal/auth"
	"github.com/Developer-Simon/energy-node-dashboard/internal/settings"
)

// Step beschreibt einen Schritt der Einrichtungsführung. Installer markiert die
// Schritte, die auch im Installer vorkommen. Needs nennt den Dienst aus
// installed_services, ohne den der Schritt entfällt.
type Step struct {
	ID        string
	Area      string
	Installer bool
	Needs     string
}

// Catalog liefert die Schritte in Anzeigereihenfolge. Die Schritte tailscale
// und automations entfallen, wenn ihr Dienst nicht installiert ist.
func Catalog(installed map[string]bool) []Step {
	steps := []Step{
		{ID: "users", Area: "access", Installer: true},
		{ID: "guest", Area: "access", Installer: true},
	}
	if installed["tailscale"] {
		steps = append(steps, Step{ID: "tailscale", Area: "access", Needs: "tailscale"})
	}
	steps = append(steps,
		Step{ID: "companion", Area: "access"},
		Step{ID: "theme", Area: "display", Installer: true},
		Step{ID: "title", Area: "display", Installer: true},
		Step{ID: "statusbar", Area: "display", Installer: true},
		Step{ID: "tabwidth", Area: "display", Installer: true},
		Step{ID: "tiles", Area: "display"},
		Step{ID: "updates", Area: "system", Installer: true},
		Step{ID: "metrics", Area: "system", Installer: true},
		Step{ID: "configservice", Area: "devices"},
		Step{ID: "config", Area: "devices"},
		Step{ID: "devices", Area: "devices"},
		Step{ID: "roles", Area: "devices"},
		Step{ID: "devicemap", Area: "devices"},
	)
	if installed["automation"] {
		steps = append(steps, Step{ID: "automations", Area: "devices", Needs: "automation"})
	}
	return steps
}

// Detect liefert StepDone für die Schritte, deren Einstellungen vom Standard
// abweichen. Offene Schritte fehlen in der Karte. Geräte einrichten, Geräte,
// Gerätekarte und Kacheln werden nie automatisch erledigt.
func Detect(set settings.Settings, energy settings.EnergyConfig, users []auth.User) map[string]StepState {
	defaults := settings.Default()
	detected := make(map[string]StepState)
	mark := func(id string, done bool) {
		if done {
			detected[id] = StepDone
		}
	}
	mark("theme", set.Theme != defaults.Theme)
	mark("title", strings.TrimSpace(set.DashboardTitle) != "" ||
		strings.TrimSpace(set.DashboardSubtitle) != "" ||
		set.DashboardTitleHidden || set.DashboardSubtitleHidden)
	mark("statusbar", !slices.Equal(set.StatusBarItems, defaults.StatusBarItems))
	mark("tabwidth", !slices.Equal(set.WidePanels, defaults.WidePanels))
	mark("updates", set.UpdateCheckDisabled)
	mark("roles", hasAssignedRole(energy))
	mark("users", hasNonAdminAccount(users))
	return detected
}

// Merge legt die erkannten Zustände unter die gespeicherten. Gespeichert
// (later, installer, done) gewinnt, erkannt füllt die Lücken.
func Merge(detected map[string]StepState, stored map[string]StepState) map[string]StepState {
	merged := make(map[string]StepState, len(detected)+len(stored))
	for id, state := range detected {
		merged[id] = state
	}
	for id, state := range stored {
		merged[id] = state
	}
	return merged
}

func hasAssignedRole(energy settings.EnergyConfig) bool {
	for _, assignment := range energy.Assignments {
		if assignment.Role != "" {
			return true
		}
	}
	return false
}

// hasNonAdminAccount zählt lokale Konten ohne user_management. Gastkonten
// gehören nicht dazu.
func hasNonAdminAccount(users []auth.User) bool {
	for _, user := range users {
		if !user.Guest && !auth.HasRole(user, auth.RoleUserManagement) {
			return true
		}
	}
	return false
}
