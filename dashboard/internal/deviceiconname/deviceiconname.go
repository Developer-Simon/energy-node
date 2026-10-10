// Package deviceiconname owns the names of the device icon catalogue: the
// energy-node: prefix and the aliases that keep names saved before the
// catalogue was reorganised (October 2026) working. It has no dependencies,
// so the settings store can migrate stored names without importing webui.
package deviceiconname

import (
	"regexp"
	"sort"
	"strings"
)

// Prefix starts every catalogue name. Home Assistant registers the same
// icon set under it (energy-node:wallbox).
const Prefix = "energy-node:"

// legacy maps every catalogue name in use before the reorganisation to its
// new name. device-prefs.json and energy.json can still hold the old ones.
var legacy = map[string]string{
	"mdi:chip-outline":       Prefix + "chip",
	"mdi:solar-panel":        Prefix + "solar-panel",
	"mdi:current-ac":         Prefix + "inverter",
	"mdi:power-plug":         Prefix + "plug-smart",
	"mdi:meter-electric":     Prefix + "meter-electric",
	"mdi:pipe-valve":         Prefix + "pipe-valve",
	"mdi:raspberry-pi":       Prefix + "raspberry-pi",
	"mdi:home-battery":       Prefix + "battery-home",
	"mdi:battery-charging":   Prefix + "charger",
	"mdi:thermometer":        Prefix + "thermometer",
	"mdi:power-socket-de":    Prefix + "socket-wall",
	"mdi:gas-burner":         Prefix + "heater",
	"mdi:water-boiler":       Prefix + "water-boiler",
	"mdi:ev-station":         Prefix + "wallbox",
	"mdi:transmission-tower": Prefix + "grid",
	"mdi:sitemap":            Prefix + "automations",
	"mdi:home":               Prefix + "home",
	"mdi:heat-pump":          Prefix + "heat-pump",
	"mdi:flash-circle":       Prefix + "bolt-circle",
}

// Canonical trims name and replaces a legacy name with its new one. Any
// other name comes back unchanged (trimmed), known or not.
func Canonical(name string) string {
	name = strings.TrimSpace(name)
	if renamed, ok := legacy[name]; ok {
		return renamed
	}
	return name
}

// Only catalogue-shaped names, no free text: the name is inserted into
// markup in the browser. Legacy mdi: names stay valid so an outdated or
// hand-edited file still loads; an unknown name renders the fallback icon.
var pattern = regexp.MustCompile(`^(energy-node|mdi):[a-z0-9-]+$`)

// Valid reports whether name has the shape of a catalogue name.
func Valid(name string) bool {
	return pattern.MatchString(name)
}

// Legacy returns a copy of the old→new table.
func Legacy() map[string]string {
	out := make(map[string]string, len(legacy))
	for old, renamed := range legacy {
		out[old] = renamed
	}
	return out
}

// Alias is one renamed Home Assistant icon name, both sides without prefix.
type Alias struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// HAAliases lists the Home Assistant names that changed, sorted by From.
// Names whose HA form stayed the same (mdi:home -> energy-node:home) are
// left out: Home Assistant always used the energy-node: prefix.
func HAAliases() []Alias {
	var out []Alias
	for old, renamed := range legacy {
		from := strings.TrimPrefix(old, "mdi:")
		to := strings.TrimPrefix(renamed, Prefix)
		if from != to {
			out = append(out, Alias{From: from, To: to})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].From < out[j].From })
	return out
}
