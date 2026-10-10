package deviceiconname

import (
	"reflect"
	"testing"
)

func TestCanonicalMapsEveryLegacyName(t *testing.T) {
	cases := map[string]string{
		"mdi:chip-outline":       "energy-node:chip",
		"mdi:solar-panel":        "energy-node:solar-panel",
		"mdi:current-ac":         "energy-node:inverter",
		"mdi:power-plug":         "energy-node:plug-smart",
		"mdi:meter-electric":     "energy-node:meter-electric",
		"mdi:pipe-valve":         "energy-node:pipe-valve",
		"mdi:raspberry-pi":       "energy-node:raspberry-pi",
		"mdi:home-battery":       "energy-node:battery-home",
		"mdi:battery-charging":   "energy-node:charger",
		"mdi:thermometer":        "energy-node:thermometer",
		"mdi:power-socket-de":    "energy-node:socket-wall",
		"mdi:gas-burner":         "energy-node:heater",
		"mdi:water-boiler":       "energy-node:water-boiler",
		"mdi:ev-station":         "energy-node:wallbox",
		"mdi:transmission-tower": "energy-node:grid",
		"mdi:sitemap":            "energy-node:automations",
		"mdi:home":               "energy-node:home",
		"mdi:heat-pump":          "energy-node:heat-pump",
		"mdi:flash-circle":       "energy-node:bolt-circle",
	}
	for old, want := range cases {
		if got := Canonical(old); got != want {
			t.Errorf("Canonical(%q) = %q, want %q", old, got, want)
		}
	}
	if len(Legacy()) != len(cases) {
		t.Errorf("Legacy() has %d entries, want %d", len(Legacy()), len(cases))
	}
}

func TestCanonicalKeepsOtherNamesAndTrims(t *testing.T) {
	for in, want := range map[string]string{
		"energy-node:wallbox": "energy-node:wallbox",
		"  mdi:ev-station ":   "energy-node:wallbox",
		"mdi:does-not-exist":  "mdi:does-not-exist",
		"":                    "",
	} {
		if got := Canonical(in); got != want {
			t.Errorf("Canonical(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestValidAcceptsNewAndLegacyShapes(t *testing.T) {
	for _, name := range []string{"energy-node:wallbox-compact", "mdi:ev-station", "mdi:does-not-exist"} {
		if !Valid(name) {
			t.Errorf("Valid(%q) = false, want true", name)
		}
	}
	for _, name := range []string{"", "wallbox", "energy-node:", "energy-node:Wallbox", "energy-node:a b", `energy-node:"><script>`, "hass:home"} {
		if Valid(name) {
			t.Errorf("Valid(%q) = true, want false", name)
		}
	}
}

func TestHAAliasesListOnlyChangedNamesSorted(t *testing.T) {
	want := []Alias{
		{From: "battery-charging", To: "charger"},
		{From: "chip-outline", To: "chip"},
		{From: "current-ac", To: "inverter"},
		{From: "ev-station", To: "wallbox"},
		{From: "flash-circle", To: "bolt-circle"},
		{From: "gas-burner", To: "heater"},
		{From: "home-battery", To: "battery-home"},
		{From: "power-plug", To: "plug-smart"},
		{From: "power-socket-de", To: "socket-wall"},
		{From: "sitemap", To: "automations"},
		{From: "transmission-tower", To: "grid"},
	}
	if got := HAAliases(); !reflect.DeepEqual(got, want) {
		t.Errorf("HAAliases() = %v, want %v", got, want)
	}
}

func TestLegacyReturnsACopy(t *testing.T) {
	Legacy()["mdi:home"] = "energy-node:broken"
	if Canonical("mdi:home") != "energy-node:home" {
		t.Error("mutating the Legacy() result changed the package table")
	}
}
