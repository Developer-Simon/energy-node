package webui

import (
	"html/template"
	"strings"

	"github.com/Developer-Simon/energy-node-dashboard/internal/registry"
)

// DeviceIcon is one entry of the catalogue the Devices tab's icon picker
// offers. Markup, not a single path like iconPaths in icons.go: an entity
// icon is one filled Material-Design path picked by Home Assistant, while a
// device symbol needs several stroked elements to stay readable at 24px.
// Both live side by side on purpose - the entity icons keep coming from
// discovery, these are chosen by hand.
type DeviceIcon struct {
	Name   string        `json:"name"`
	Label  string        `json:"label"`
	Markup template.HTML `json:"markup"`
}

// deviceIconFallbackName is the chip outline both the compact card and the
// modal hard-coded before this catalogue existed. A device without a
// preference therefore looks exactly like it did.
const deviceIconFallbackName = "mdi:chip-outline"

// deviceIconSVGAttrs are shared by the server-rendered icon and by the
// picker in the browser (device-prefs.js builds the same wrapper around
// DeviceIcon.Markup), so both render identically.
const deviceIconSVGAttrs = `viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true" focusable="false"`

var deviceIconCatalogue = []DeviceIcon{
	{Name: deviceIconFallbackName, Label: "Standard", Markup: `<rect x="6" y="6" width="12" height="12" rx="2"/><line x1="9" y1="3" x2="9" y2="6"/><line x1="15" y1="3" x2="15" y2="6"/><line x1="9" y1="18" x2="9" y2="21"/><line x1="15" y1="18" x2="15" y2="21"/><line x1="3" y1="9" x2="6" y2="9"/><line x1="3" y1="15" x2="6" y2="15"/><line x1="18" y1="9" x2="21" y2="9"/><line x1="18" y1="15" x2="21" y2="15"/>`},
	{Name: "mdi:solar-panel", Label: "Solarpanel", Markup: `<path d="M3.5 16 7 6h13.5L17 16Z"/><path d="M5.25 11h13.5"/><path d="M11.5 6 8 16"/><path d="M16 6l-3.5 10"/><path d="M10.2 16v4"/><path d="M7 20h6.5"/>`},
	{Name: "mdi:current-ac", Label: "Wechselrichter", Markup: `<rect x="3" y="3.5" width="18" height="17" rx="2"/><path d="M12.2 6.5 9 11.6h2.8l-.3 3.4 4.7-5.1h-2.8z"/><path d="M7 18c1.667-1.9 3.333-1.9 5 0s3.333 1.9 5 0"/>`},
	{Name: "mdi:power-plug", Label: "Smarte Steckdose", Markup: `<g transform="rotate(-28 12 12)"><path d="M16.2 6.6H9.4A3.4 3.4 0 0 0 6 10v4a3.4 3.4 0 0 0 3.4 3.4h6.8"/><ellipse cx="16.2" cy="12" rx="2.4" ry="5.4"/><circle cx="16.5" cy="10.1" r="0.8"/><circle cx="16.5" cy="13.9" r="0.8"/><path d="M2.6 9.9h3.4M2.6 14.1h3.4"/></g>`},
	{Name: "mdi:meter-electric", Label: "3-Phasen-Energiezähler", Markup: `<rect x="3.5" y="3" width="17" height="18" rx="2"/><rect x="6" y="5.8" width="12" height="5" rx="1"/><path d="M8.4 7.4v1.8M10.4 7.4v1.8M12.4 7.4v1.8M14.4 7.4v1.8"/><path d="M15.9 9.1h.01"/><path d="M12.1 12.4 9.6 16.4h2.3l-.3 2.6 2.5-4h-2.3z"/>`},
	{Name: "mdi:pipe-valve", Label: "Heizungsrohr-Ventil", Markup: `<path d="M2 15h5M17 15h5"/><path d="M7 11.5 17 18.5v-7L7 18.5Z"/><path d="M12 15V9.5"/><path d="M9 9.5h6"/>`},
	{Name: "mdi:raspberry-pi", Label: "Raspberry Pi", Markup: `<rect x="2.5" y="4" width="19" height="16" rx="3"/><path d="M5 8.6h2.6M5 11.1h2.6M5 13.6h2.6"/><path d="M12.9 10.2C11.5 10 10.5 8.9 10.5 7.5c1.5-.1 2.5 1.1 2.5 2.7Z"/><path d="M13.1 10.2C14.5 10 15.5 8.9 15.5 7.5c-1.5-.1-2.5 1.1-2.5 2.7Z"/><circle cx="11.7" cy="12.2" r="1.15"/><circle cx="14.3" cy="12.2" r="1.15"/><circle cx="13" cy="14.4" r="1.15"/><rect x="18.2" y="8.6" width="2.8" height="2.2" rx="0.5"/><rect x="18.2" y="13.2" width="2.8" height="2.2" rx="0.5"/>`},
	{Name: "mdi:home-battery", Label: "Batterie", Markup: `<rect x="5" y="2.5" width="14" height="18" rx="2"/><path d="M5 6.2h14"/><rect x="10.9" y="8.2" width="2.2" height="1.3" rx="0.45"/><rect x="9.3" y="9.5" width="5.4" height="8" rx="1.2"/><path d="M10.7 15.2h2.6M10.7 12.9h2.6"/><path d="M8 20.5V22M16 20.5V22"/>`},
	{Name: "mdi:battery-charging", Label: "Ladegerät", Markup: `<rect x="3" y="7" width="18" height="10" rx="2.5"/><path d="M6 10.4h5"/><path d="M6 13.6h.01M8.5 13.6h.01"/><path d="M16.6 9.5 14.6 13h1.7l-.3 2.4 2.1-3.6h-1.7z"/><path d="M3 12H1.2M21 12h1.8"/>`},
	{Name: "mdi:thermometer", Label: "Thermometer", Markup: `<path d="M10 13.6V5.5a2 2 0 1 1 4 0v8.1a4.2 4.2 0 1 1-4 0Z"/><path d="M8.2 6.5H6M8.2 9H7M8.2 11.5H6"/><circle cx="12" cy="17.3" r="1.7"/>`},
	{Name: "mdi:power-socket-de", Label: "Unterputz-Steckdose", Markup: `<rect x="2.5" y="2.5" width="19" height="19" rx="3"/><circle cx="12" cy="12" r="6"/><circle cx="9.5" cy="12" r="1.1"/><circle cx="14.5" cy="12" r="1.1"/><path d="M18.3 19.3h.01"/><path d="M16.9 18.1a2.2 2.2 0 0 1 2.9 0"/>`},
	{Name: "mdi:gas-burner", Label: "Heizung (Öl/Gas)", Markup: `<path d="M7.5 5.5V3.2a1.2 1.2 0 0 1 1.2-1.2h2.3"/><rect x="3.5" y="5.5" width="17" height="14" rx="2"/><path d="M12 9c1.9 1.9 2.8 3 2.8 4.2a2.8 2.8 0 0 1-5.6 0C9.2 12 10.1 10.9 12 9Z"/><path d="M7 19.5v2M17 19.5v2"/>`},
	{Name: "mdi:water-boiler", Label: "Wasserboiler", Markup: `<rect x="3.5" y="3.5" width="11.5" height="17" rx="4"/><path d="M5 8.4c1.42-1.3 2.83-1.3 4.25 0s2.83 1.3 4.25 0"/><path d="M5 11.4c1.42-1.3 2.83-1.3 4.25 0s2.83 1.3 4.25 0"/><path d="M9.5 14.4 7.4 17.8h1.8l-.2 2.2 1.8-3.4H9.3z"/><path d="M19.9 14.1V8.3a1.6 1.6 0 1 0-3.2 0v5.8a3.3 3.3 0 1 0 3.2 0Z"/>`},
	{Name: "mdi:ev-station", Label: "Wallbox", Markup: `<rect x="2.5" y="3" width="11" height="14" rx="2.5"/><path d="M8.1 6 5.8 10.8h2.1l-.2 3.2 2.3-4.8H7.9z"/><path d="M13.5 13h2a3 3 0 0 1 3 3v1"/><path d="M16 17a3 3 0 1 0 5 0Z"/>`},
	{Name: "mdi:transmission-tower", Label: "Stromnetz", Markup: `<path d="M7.8 21 10.9 5M16.2 21 13.1 5"/><path d="M10.9 5h2.2"/><path d="M3.5 8h17"/><path d="M5.5 8v2.4M12 8v2.4M18.5 8v2.4"/><path d="M8.8 16h6.4"/>`},
	{Name: "mdi:sitemap", Label: "Automationen", Markup: `<circle cx="5.5" cy="6" r="2.5"/><circle cx="5.5" cy="18" r="2.5"/><circle cx="18.5" cy="12" r="2.5"/><path d="M8 6h5a3 3 0 0 1 3 3v.8"/><path d="M8 18h5a3 3 0 0 0 3-3v-.8"/>`},
	{Name: "mdi:home", Label: "Gebäude", Markup: `<path d="M6 11 12 4 18 11"/><path d="M6 11v8.6h12v-8.6"/><rect x="10.2" y="14.7" width="3.6" height="4.9" rx="0.4"/>`},
	{Name: "mdi:heat-pump", Label: "Wärmepumpe", Markup: `<rect x="3" y="7" width="15" height="10" rx="1.5"/><path d="M5.5 9.6h3M5.5 12h3M5.5 14.4h3"/><circle cx="13.4" cy="12" r="3.3"/><path d="M13.4 12 13.4 9.4M13.4 12 11.15 13.3M13.4 12 15.65 13.3"/>`},
	{Name: "mdi:flash-circle", Label: "Verbrauch", Markup: `<circle cx="12" cy="12" r="8.4"/><path d="M12.7 6.6 9.3 12.1h2.6l-.3 4.3 4.4-5.3h-2.6z"/>`},
}

var deviceIconByName = func() map[string]DeviceIcon {
	index := make(map[string]DeviceIcon, len(deviceIconCatalogue))
	for _, icon := range deviceIconCatalogue {
		index[icon.Name] = icon
	}
	return index
}()

// DeviceIconCatalogue returns the pickable icons in catalogue order. The
// picker in the browser reads it over GET /api/v1/device/icons.
func DeviceIconCatalogue() []DeviceIcon {
	return append([]DeviceIcon(nil), deviceIconCatalogue...)
}

// deviceIcon renders the device's chosen symbol. An unset or unknown name
// falls back to the chip outline rather than failing - a catalogue entry can
// be renamed or dropped without breaking a saved device-prefs.json, the same
// tolerance iconFor has for unknown Home Assistant icon names.
func deviceIcon(dev registry.DeviceView) template.HTML {
	icon, ok := deviceIconByName[strings.TrimSpace(dev.IconName)]
	if !ok {
		icon = deviceIconByName[deviceIconFallbackName]
	}
	return template.HTML(`<svg ` + deviceIconSVGAttrs + `>` + string(icon.Markup) + `</svg>`)
}
