package webui

import (
	"strings"
	"testing"

	"github.com/Developer-Simon/energy-node-dashboard/internal/registry"
)

func TestDeviceIconCatalogueIsComplete(t *testing.T) {
	catalogue := DeviceIconCatalogue()
	if len(catalogue) < 18 {
		t.Fatalf("catalogue has %d icons, want at least the 18 device types", len(catalogue))
	}
	seen := make(map[string]bool, len(catalogue))
	for _, icon := range catalogue {
		if !strings.HasPrefix(icon.Name, "mdi:") {
			t.Errorf("icon %q does not use an mdi: name - settings.validateDevicePrefs would reject it", icon.Name)
		}
		if seen[icon.Name] {
			t.Errorf("icon %q appears twice in the catalogue", icon.Name)
		}
		seen[icon.Name] = true
		if strings.TrimSpace(string(icon.Markup)) == "" || icon.Label == "" {
			t.Errorf("icon %q has empty markup or label", icon.Name)
		}
		if strings.Contains(string(icon.Markup), "<svg") {
			t.Errorf("icon %q carries a whole <svg> element, want inner markup only", icon.Name)
		}
	}
	if !seen[deviceIconFallbackName] {
		t.Errorf("catalogue is missing the fallback %q", deviceIconFallbackName)
	}
}

func TestDeviceIconFallsBackForUnknownAndEmptyNames(t *testing.T) {
	fallback := string(deviceIcon(registry.DeviceView{ID: "node"}))
	unknown := string(deviceIcon(registry.DeviceView{ID: "node", IconName: "mdi:does-not-exist"}))
	chosen := string(deviceIcon(registry.DeviceView{ID: "node", IconName: "mdi:solar-panel"}))

	if !strings.HasPrefix(fallback, "<svg viewBox=\"0 0 24 24\"") || !strings.HasSuffix(fallback, "</svg>") {
		t.Fatalf("deviceIcon returned %q, want a complete svg element", fallback)
	}
	if unknown != fallback {
		t.Errorf("an unknown icon name rendered %q, want the fallback", unknown)
	}
	if chosen == fallback {
		t.Error("a catalogue name rendered the fallback")
	}
}

func TestSuggestedDeviceIconMatchesKnownModels(t *testing.T) {
	cases := []struct {
		manufacturer, model, want string
	}{
		{"APsystems", "EZ1", "mdi:solar-panel"},
		{"Trucki (Community-Firmware)", "T2SG", "mdi:current-ac"},
		{"DIY", "LiFePO4 Dual-Bank Coulomb-Counter", "mdi:home-battery"},
		{"Raspberry Pi Foundation", "Raspberry Pi 1 (ARMv6)", "mdi:raspberry-pi"},
		{"Energy Node", "", "mdi:sitemap"},
		{"Tuya", "Tuya valve", "mdi:pipe-valve"},
		{"Shelly", "SNPL-00112EU", "mdi:power-plug"},
		{"Shelly", "SHPLG-S", "mdi:power-plug"},
		{"Shelly", "S3PL-00112EU", "mdi:power-plug"},
		{"Shelly", "SHSW-1", "mdi:power-socket-de"},
		{"Shelly", "SHSW-25", "mdi:power-socket-de"},
		{"Shelly", "SNSW-102P16EU", "mdi:power-socket-de"},
		{"Shelly", "S3SW-001X16EU", "mdi:power-socket-de"},
		{"Shelly", "SHEM-3", "mdi:meter-electric"},
		{"Shelly", "SPEM-003CEBEU", "mdi:meter-electric"},
		{"Shelly", "SHHT-1", "mdi:thermometer"},
		{"Shelly", "SNSN-0013A", "mdi:thermometer"},
		{"shelly", "snpl-00112eu", "mdi:power-plug"},
		{"Shelly", "SNXX-unknown", ""},
		{"Acme", "Widget", ""},
		{"", "", ""},
	}
	for _, tc := range cases {
		got := suggestedDeviceIcon(registry.DeviceView{Manufacturer: tc.manufacturer, Model: tc.model})
		if got != tc.want {
			t.Errorf("suggestedDeviceIcon(%q, %q) = %q, want %q", tc.manufacturer, tc.model, got, tc.want)
		}
		if got != "" {
			if _, ok := deviceIconByName[got]; !ok {
				t.Errorf("suggestion %q for %q/%q is not in the catalogue", got, tc.manufacturer, tc.model)
			}
		}
	}
}

func TestDeviceIconPrefersSavedOverSuggestedOverFallback(t *testing.T) {
	plug := registry.DeviceView{ID: "p", Manufacturer: "Shelly", Model: "SNPL-00112EU"}
	suggested := string(deviceIcon(plug))
	if suggested != string(deviceIcon(registry.DeviceView{IconName: "mdi:power-plug"})) {
		t.Errorf("a Shelly Plug S without a saved icon rendered %q, want the power-plug suggestion", suggested)
	}
	plug.IconName = "mdi:chip-outline"
	if got := string(deviceIcon(plug)); got != string(deviceIcon(registry.DeviceView{})) {
		t.Errorf("a saved chip icon rendered %q, want the saved choice to beat the suggestion", got)
	}
}
