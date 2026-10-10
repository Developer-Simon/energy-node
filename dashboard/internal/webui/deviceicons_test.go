package webui

import (
	"os"
	"strings"
	"testing"

	"github.com/Developer-Simon/energy-node-dashboard/internal/deviceiconname"
	"github.com/Developer-Simon/energy-node-dashboard/internal/registry"
)

func TestDeviceIconCatalogueIsComplete(t *testing.T) {
	catalogue := DeviceIconCatalogue()
	if len(catalogue) != 31 {
		t.Fatalf("catalogue has %d icons, want 31", len(catalogue))
	}
	seen := make(map[string]bool, len(catalogue))
	for _, icon := range catalogue {
		if !strings.HasPrefix(icon.Name, deviceiconname.Prefix) || !deviceiconname.Valid(icon.Name) {
			t.Errorf("icon %q is not an energy-node: catalogue name", icon.Name)
		}
		if seen[icon.Name] {
			t.Errorf("icon %q appears twice in the catalogue", icon.Name)
		}
		seen[icon.Name] = true
		markup := string(icon.Markup)
		if strings.TrimSpace(markup) == "" || icon.Label == "" || icon.Category == "" {
			t.Errorf("icon %q has empty markup, label or category", icon.Name)
		}
		if strings.Contains(markup, "<svg") || strings.Contains(markup, "fill=") || strings.Contains(markup, "stroke") {
			t.Errorf("icon %q must be inner, stroke-only markup without paint attributes", icon.Name)
		}
		if icon.CategoryKey != "device_icon_category."+icon.Category {
			t.Errorf("icon %q has category key %q", icon.Name, icon.CategoryKey)
		}
	}
	if !seen[deviceIconFallbackName] {
		t.Errorf("catalogue is missing the fallback %q", deviceIconFallbackName)
	}
}

func TestDeviceIconCategoriesAreContiguous(t *testing.T) {
	order := []string{"generation", "storage", "grid", "switching", "heating", "mobility", "building", "control"}
	var got []string
	for _, icon := range DeviceIconCatalogue() {
		if len(got) == 0 || got[len(got)-1] != icon.Category {
			got = append(got, icon.Category)
		}
	}
	if strings.Join(got, ",") != strings.Join(order, ",") {
		t.Errorf("category order = %v, want %v (each category in one block)", got, order)
	}
}

func TestEveryDeviceIconHasAnExistingMDIName(t *testing.T) {
	data, err := os.ReadFile("testdata/mdi-names-7.4.47.txt")
	if err != nil {
		t.Fatal(err)
	}
	known := map[string]bool{}
	for _, name := range strings.Fields(string(data)) {
		known[name] = true
	}
	for _, icon := range DeviceIconCatalogue() {
		if !strings.HasPrefix(icon.MDI, "mdi:") || !known[strings.TrimPrefix(icon.MDI, "mdi:")] {
			t.Errorf("icon %s: %q is not an MDI 7.4.47 icon", icon.Name, icon.MDI)
		}
	}
}

func TestEveryLegacyNameMapsToACatalogueIcon(t *testing.T) {
	for old, renamed := range deviceiconname.Legacy() {
		if _, ok := deviceIconByName[renamed]; !ok {
			t.Errorf("legacy %q maps to %q, which is not in the catalogue", old, renamed)
		}
	}
}

func TestEveryDeviceIconLabelKeyIsInTheCatalog(t *testing.T) {
	de := readCatalogs(t)["de"]
	for _, icon := range DeviceIconCatalogue() {
		if text, ok := de[icon.LabelKey]; !ok || text != icon.Label {
			t.Errorf("icon %s: de.json[%q] = %q, want %q", icon.Name, icon.LabelKey, text, icon.Label)
		}
		if _, ok := de[icon.CategoryKey]; !ok {
			t.Errorf("icon %s: de.json has no %q", icon.Name, icon.CategoryKey)
		}
	}
}

func TestDeviceIconFallsBackForUnknownAndEmptyNames(t *testing.T) {
	fallback := string(deviceIcon(registry.DeviceView{ID: "node"}))
	unknown := string(deviceIcon(registry.DeviceView{ID: "node", IconName: "mdi:does-not-exist"}))
	chosen := string(deviceIcon(registry.DeviceView{ID: "node", IconName: "energy-node:solar-panel"}))
	legacy := string(deviceIcon(registry.DeviceView{ID: "node", IconName: "mdi:solar-panel"}))

	if !strings.HasPrefix(fallback, "<svg viewBox=\"0 0 24 24\"") || !strings.HasSuffix(fallback, "</svg>") {
		t.Fatalf("deviceIcon returned %q, want a complete svg element", fallback)
	}
	if unknown != fallback {
		t.Errorf("an unknown icon name rendered %q, want the fallback", unknown)
	}
	if chosen == fallback {
		t.Error("a catalogue name rendered the fallback")
	}
	if legacy != chosen {
		t.Error("a legacy mdi: name must render the same icon as its energy-node: name")
	}
}

func TestDeviceIconTemplateFuncs(t *testing.T) {
	symbol := string(deviceIconSymbol("ico-sun", "energy-node:sun"))
	if !strings.HasPrefix(symbol, `<symbol id="ico-sun" viewBox="0 0 24 24">`) || !strings.HasSuffix(symbol, "</symbol>") {
		t.Errorf("deviceIconSymbol = %s", symbol)
	}
	if !strings.Contains(symbol, `<circle cx="12" cy="12" r="4"/>`) {
		t.Errorf("deviceIconSymbol did not embed the sun markup: %s", symbol)
	}
	if got := string(deviceIconMarkup("mdi:nope")); got != string(deviceIconByName[deviceIconFallbackName].Markup) {
		t.Errorf("deviceIconMarkup of an unknown name = %s, want the chip markup", got)
	}
}

func TestSuggestedDeviceIconMatchesKnownModels(t *testing.T) {
	cases := []struct {
		manufacturer, model, want string
	}{
		{"APsystems", "EZ1", "energy-node:solar-panel"},
		{"Trucki (Community-Firmware)", "T2SG", "energy-node:inverter"},
		{"DIY", "LiFePO4 Dual-Bank Coulomb-Counter", "energy-node:battery-home"},
		{"Raspberry Pi Foundation", "Raspberry Pi 1 (ARMv6)", "energy-node:raspberry-pi"},
		{"Energy Node", "", "energy-node:automations"},
		{"Tuya", "Tuya valve", "energy-node:pipe-valve"},
		{"Shelly", "SNPL-00112EU", "energy-node:plug-smart"},
		{"Shelly", "SHPLG-S", "energy-node:plug-smart"},
		{"Shelly", "S3PL-00112EU", "energy-node:plug-smart"},
		{"Shelly", "SHSW-1", "energy-node:socket-wall"},
		{"Shelly", "SHSW-25", "energy-node:socket-wall"},
		{"Shelly", "SNSW-102P16EU", "energy-node:socket-wall"},
		{"Shelly", "S3SW-001X16EU", "energy-node:socket-wall"},
		{"Shelly", "SHEM-3", "energy-node:meter-electric"},
		{"Shelly", "SPEM-003CEBEU", "energy-node:meter-electric"},
		{"Shelly", "SHHT-1", "energy-node:thermometer"},
		{"Shelly", "SNSN-0013A", "energy-node:thermometer"},
		{"shelly", "snpl-00112eu", "energy-node:plug-smart"},
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
	if suggested != string(deviceIcon(registry.DeviceView{IconName: "energy-node:plug-smart"})) {
		t.Errorf("a Shelly Plug S without a saved icon rendered %q, want the plug-smart suggestion", suggested)
	}
	plug.IconName = "energy-node:chip"
	if got := string(deviceIcon(plug)); got != string(deviceIcon(registry.DeviceView{})) {
		t.Errorf("a saved chip icon rendered %q, want the saved choice to beat the suggestion", got)
	}
}
