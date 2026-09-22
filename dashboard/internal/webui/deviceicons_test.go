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
