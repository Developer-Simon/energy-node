package diag

import (
	"fmt"
	"sort"
	"strconv"

	"github.com/Developer-Simon/energy-node-installer/internal/bundle"
)

// Check is one line of the operator-facing checklist Plan C's Diagnose
// screen renders (and this plan's `installer diagnose` prints as text).
type Check struct {
	Name        string
	OK          bool
	Detail      string
	RetryStepID string // step id that would fix this if rerun; "" if none applies
	// Group names the card the UI shows the check on: "services" for a
	// Python service unit, "system" for the fixed units, ports and Tailscale,
	// "config" for files.
	Group string
	// Subject is what was checked, without the Name prefix.
	Subject string
}

// fixedUnitSteps names the retry step for the four units whose step id never
// changes across bundles -- 10-60 are core steps, never optional, so unlike
// a device service's unit their id cannot come from the manifest.
var fixedUnitSteps = map[string]string{
	"mosquitto.service":             "20",
	"energy-node-dashboard.service": "60",
	"tailscaled.service":            "40",
	"caddy.service":                 "70",
}

// fixedPortSteps names the retry step for the three ports the spec fixes
// (Komponente A): 1883 is Mosquitto's, 8080 and 443 are the dashboard's
// plain and Caddy's HTTPS listener.
var fixedPortSteps = map[string]string{
	"1883": "20",
	"8080": "60",
	"443":  "70",
}

// Checklist turns a Report into an ordered, deterministic list of pass/fail
// checks. steps resolves a device service's unit to its step id (device
// units have no fixed id -- see this task's rationale); pass nil if that
// resolution is not needed, such as when steps.Manifest is unavailable.
func (r *Report) Checklist(steps []bundle.StepEntry) []Check {
	unitStep := map[string]string{}
	for _, s := range steps {
		if s.Unit != "" {
			unitStep[s.Unit] = s.ID
		}
	}

	var checks []Check

	unitNames := make([]string, 0, len(r.Units))
	for name := range r.Units {
		unitNames = append(unitNames, name)
	}
	sort.Strings(unitNames)
	for _, name := range unitNames {
		state := r.Units[name]
		retry := fixedUnitSteps[name]
		if retry == "" {
			retry = unitStep[name]
		}
		checks = append(checks, Check{
			Name:        "unit " + name,
			OK:          state == "active",
			Detail:      state,
			RetryStepID: retry,
			Group:       unitGroup(name),
			Subject:     name,
		})
	}

	portNames := make([]string, 0, len(r.Ports))
	for name := range r.Ports {
		portNames = append(portNames, name)
	}
	sort.Slice(portNames, func(i, j int) bool {
		a, errA := strconv.Atoi(portNames[i])
		b, errB := strconv.Atoi(portNames[j])
		if errA != nil || errB != nil {
			return portNames[i] < portNames[j]
		}
		return a < b
	})
	for _, name := range portNames {
		open := r.Ports[name]
		detail := "closed"
		if open {
			detail = "open"
		}
		checks = append(checks, Check{
			Name:        "port " + name,
			OK:          open,
			Detail:      detail,
			RetryStepID: fixedPortSteps[name],
			Group:       "system",
			Subject:     name,
		})
	}

	checks = append(checks, Check{
		Name:        "config.json",
		OK:          r.Config.ConfigJSON,
		Detail:      fmt.Sprintf("present=%v", r.Config.ConfigJSON),
		RetryStepID: "60",
		Group:       "config",
		Subject:     "config.json",
	})

	checks = append(checks, Check{
		Name:        "tailscale login",
		OK:          r.Tailscale.Angemeldet,
		Detail:      fmt.Sprintf("angemeldet=%v", r.Tailscale.Angemeldet),
		RetryStepID: "40",
		Group:       "system",
		Subject:     "tailscale",
	})

	return checks
}

// unitGroup puts the four fixed units on the system card; every other unit
// belongs to a Python service.
func unitGroup(name string) string {
	if _, fixed := fixedUnitSteps[name]; fixed {
		return "system"
	}
	return "services"
}
