package webui

import (
	"html/template"
	"strings"

	"github.com/Developer-Simon/energy-node-dashboard/internal/registry"
)

type iconData struct {
	SVG      template.HTML
	Label    string
	Fallback bool
}

var iconPaths = map[string]string{
	"mdi:alert-circle":                   "M12 2a10 10 0 1 0 0 20 10 10 0 0 0 0-20zm1 15h-2v-2h2v2zm0-4h-2V7h2v6z",
	"mdi:battery":                        "M16 7V5h-2V3h-4v2H8v2H6v12h12V7h-2zm0 10H8V9h8v8z",
	"mdi:battery-heart-variant":          "M16 7V5h-2V3h-4v2H8v2H6v12h12V7h-2zm-1 8-3 3-3-3c-2-2 .5-5 3-3 2.5-2 5 1 3 3zm1 2H8V9h8v8z",
	"mdi:chart-line":                     "M3 17h2.5l3.5-6 4 4 5-8H21v-2h-4v2h2.1l-3.2 5.1-4-4-4.5 7.9H3v2z",
	"mdi:checkbox-marked-circle-outline": "M12 2a10 10 0 1 0 0 20 10 10 0 0 0 0-20zm-2 15-4-4 1.4-1.4L10 14.2l6.6-6.6L18 9l-8 8z",
	"mdi:chip":                           "M9 3h6v2h2v2h2v6h-2v2h-2v2H9v-2H7v-2H5V7h2V5h2V3zm0 4v6h6V7H9z",
	"mdi:clock-outline":                  "M12 2a10 10 0 1 0 0 20 10 10 0 0 0 0-20zm1 10.4 3.5 2.1-1 1.7-4.5-2.8V6h2v6.4z",
	"mdi:flash":                          "M13 2 4 14h6l-1 8 9-12h-6l1-8z",
	"mdi:harddisk":                       "M5 4h14v16H5V4zm2 2v4h10V6H7zm0 8v4h10v-4H7zm2-7h2v2H9V7zm0 8h2v2H9v-2z",
	"mdi:ip-network":                     "M11 3h2v3h3v2h-3v3h3v2h-3v3h-2v-3H8v-2h3V8H8V6h3V3zM4 5h4v4H4V5zm12 10h4v4h-4v-4zM4 15h4v4H4v-4z",
	"mdi:lan-connect":                    "M4 4h6v6H4V4zm10 0h6v6h-6V4zM9 14h6v6H9v-6zM7 10v2h10v-2h2v4h-2v2h-2v-2H9v2H7v-2H5v-4h2z",
	"mdi:lightning-bolt":                 "M13 2 4 14h6l-1 8 9-12h-6l1-8z",
	"mdi:memory":                         "M6 4h12v16H6V4zm2 3v10h8V7H8zm2 2h4v2h-4V9zm0 4h4v2h-4v-2z",
	"mdi:numeric":                        "M7 4h2l-1 6h3l1-6h2l-1 6h2v2h-2l-1 6h-2l1-6H8l-1 6H5l1-6H4v-2h2l1-6z",
	"mdi:package-up":                     "M12 3 4 7v10l8 4 8-4V7l-8-4zm0 2.2 5.5 2.7L12 10.6 6.5 7.9 12 5.2zM6 9.5l5 2.5v6.2l-5-2.5V9.5zm7 8.7V12l5-2.5v6.2l-5 2.5zM12 7l-2 2h1v3h2V9h1l-2-2z",
	"mdi:play-circle":                    "M12 2a10 10 0 1 0 0 20 10 10 0 0 0 0-20zm-2 5 6 5-6 5V7z",
	"mdi:scale-unbalanced":               "M12 3v3H7l-3 5h6l-3-5h10l-3 5h6l-3-5h-3V3h-2zm-6 10h12v2H6v-2z",
	"mdi:shape-outline":                  "M4 4h7v7H4V4zm9 0h7v7h-7V4zM4 13h7v7H4v-7zm9 0h7v7h-7v-7z",
	"mdi:signal":                         "M4 18h3v2H4v-2zm4-4h3v6H8v-6zm4-4h3v10h-3V10zm4-4h3v14h-3V6z",
	"mdi:sine-wave":                      "M3 12c2-8 4 8 6 0s4-8 6 0 4 8 6 0v3c-2 8-4-8-6 0s-4 8-6 0-4-8-6 0v-3z",
	"mdi:thermometer":                    "M14 14.8V5a2 2 0 0 0-4 0v9.8a4 4 0 1 0 4 0zM12 21a2 2 0 0 1-1-3.7V5a1 1 0 0 1 2 0v12.3A2 2 0 0 1 12 21z",
	"mdi:toggle-switch":                  "M7 7h10a5 5 0 0 1 0 10H7A5 5 0 0 1 7 7zm0 2a3 3 0 1 0 0 6h10a3 3 0 1 0 0-6H7zM7 10a2 2 0 1 0 0 4 2 2 0 0 0 0-4z",
}

func iconFor(entity registry.EntityView) iconData {
	name := strings.TrimSpace(entity.Icon)
	if name != "" {
		if path, ok := iconPaths[name]; ok {
			return makeIcon(name, path, false)
		}
		return makeIcon(name, iconPaths["mdi:shape-outline"], true)
	}

	name = fallbackIconName(entity)
	return makeIcon(name, iconPaths[name], true)
}

func fallbackIconName(entity registry.EntityView) string {
	byClass := map[string]string{
		"battery":         "mdi:battery",
		"connectivity":    "mdi:lan-connect",
		"energy":          "mdi:lightning-bolt",
		"power":           "mdi:flash",
		"problem":         "mdi:alert-circle",
		"running":         "mdi:play-circle",
		"signal_strength": "mdi:signal",
		"temperature":     "mdi:thermometer",
		"timestamp":       "mdi:clock-outline",
		"voltage":         "mdi:sine-wave",
	}
	if name, ok := byClass[entity.DeviceClass]; ok {
		return name
	}

	byComponent := map[string]string{
		"binary_sensor": "mdi:checkbox-marked-circle-outline",
		"number":        "mdi:numeric",
		"sensor":        "mdi:chart-line",
		"switch":        "mdi:toggle-switch",
	}
	if name, ok := byComponent[entity.Component]; ok {
		return name
	}
	return "mdi:shape-outline"
}

func makeIcon(name, path string, fallback bool) iconData {
	label := "Home Assistant icon: " + name
	if fallback {
		label = "Fallback icon: " + name
	}
	markup := `<svg viewBox="0 0 24 24" aria-hidden="true" focusable="false"><path d="` + path + `"></path></svg>`
	return iconData{SVG: template.HTML(markup), Label: label, Fallback: fallback}
}
