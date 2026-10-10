package webui

import (
	"html/template"
	"strings"

	"github.com/Developer-Simon/energy-node-dashboard/internal/deviceiconname"
	"github.com/Developer-Simon/energy-node-dashboard/internal/registry"
)

// DeviceIcon is one entry of the catalogue the Devices tab's icon picker
// offers. Markup, not a single path like iconPaths in icons.go: an entity
// icon is one filled Material-Design path picked by Home Assistant, while a
// device symbol needs several stroked elements to stay readable at 24px.
//
// Name is the energy-node: name the dashboard stores and Home Assistant
// shows (energy-node:wallbox). MDI is the Material Design icon that means
// the same thing, for every place that announces an icon through Home
// Assistant discovery, which only knows mdi: names.
type DeviceIcon struct {
	Name        string        `json:"name"`
	MDI         string        `json:"mdi"`
	Category    string        `json:"category"`
	CategoryKey string        `json:"categoryKey"`
	Label       string        `json:"label"`
	LabelKey    string        `json:"labelKey"`
	Markup      template.HTML `json:"markup"`
}

// deviceIconFallbackName is the chip a device shows without a saved icon
// or a suggestion.
const deviceIconFallbackName = deviceiconname.Prefix + "chip"

// DeviceIconStrokeWidth is the stroke every device icon is drawn with. The
// Home Assistant generator (scripts/icons/flatten_icons.py) buffers each
// stroke by half this value to turn the drawing into a filled outline - a
// change here without regenerating is caught by flatten_icons.py --check.
const DeviceIconStrokeWidth = "1.6"

const deviceIconSVGAttrs = `viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="` + DeviceIconStrokeWidth + `" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true" focusable="false"`

// catalogueEntry builds a catalogue entry from the short name: name "battery-level"
// becomes energy-node:battery-level with label key device_icon.battery_level.
func catalogueEntry(category, name, mdi, label string, markup string) DeviceIcon {
	return DeviceIcon{
		Name:        deviceiconname.Prefix + name,
		MDI:         "mdi:" + mdi,
		Category:    category,
		CategoryKey: "device_icon_category." + category,
		Label:       label,
		LabelKey:    "device_icon." + strings.ReplaceAll(name, "-", "_"),
		Markup:      template.HTML(markup),
	}
}

// deviceIconCatalogue is grouped by category; the picker shows one heading
// per block, in this order.
var deviceIconCatalogue = []DeviceIcon{
	catalogueEntry("generation", "solar-panel", "solar-panel", "Solarpanel",
		`<path d="M3.5 15 7 5h13.5L17 15Z"/><path d="M5.25 10h13.5"/><path d="M11.5 5 8 15"/><path d="M16 5l-3.5 10"/><path d="M10.2 15v4"/><path d="M7 19h6.5"/>`),
	catalogueEntry("generation", "sun", "weather-sunny", "Sonne, Helligkeit",
		`<circle cx="12" cy="12" r="4"/><path d="M12 2v2M12 20v2M2 12h2M20 12h2M5 5l1.5 1.5M17.5 17.5 19 19M19 5l-1.5 1.5M6.5 17.5 5 19"/>`),

	catalogueEntry("storage", "battery", "battery-outline", "Batterie", batteryBody),
	catalogueEntry("storage", "battery-level", "battery-medium", "Batterie mit Füllstand",
		batteryBody+`<path d="M6 10v4M9.25 10v4M12.5 10v4"/>`),
	catalogueEntry("storage", "battery-bolt", "battery-charging-outline", "Batterie mit Blitz",
		batteryBody+bolt(10.75, 12, 7.2)),
	catalogueEntry("storage", "charger", "battery-charging", "Ladegerät",
		`<rect x="4.5" y="7" width="15" height="10" rx="2.5"/><path d="M7.5 10.4h4"/><path d="M7.5 13.6h.01M10 13.6h.01"/>`+bolt(15.6, 12, 5.6)+`<path d="M4.5 12h-2M19.5 12h2"/>`),
	catalogueEntry("storage", "battery-home", "home-battery", "Heimspeicher",
		`<rect x="5" y="2.5" width="14" height="18" rx="2"/><path d="M5 6.2h14"/><rect x="10.9" y="8.2" width="2.2" height="1.3" rx="0.45"/><rect x="9.3" y="9.5" width="5.4" height="8" rx="1.2"/><path d="M10.7 15.2h2.6M10.7 12.9h2.6"/><path d="M8 20.5V22M16 20.5V22"/>`),

	catalogueEntry("grid", "grid", "transmission-tower", "Stromnetz",
		`<path d="M7.6 21 12 3l4.4 18"/><path d="M3.5 7.5h17"/><path d="M5.5 7.5v2.4M18.5 7.5v2.4"/><path d="M9.8 12h4.4M8.6 17h6.8"/>`),
	catalogueEntry("grid", "meter-electric", "meter-electric", "3-Phasen-Energiezähler",
		`<rect x="3.5" y="3" width="17" height="18" rx="2"/><rect x="6" y="5.8" width="12" height="5" rx="1"/><path d="M8.4 7.4v1.8M10.4 7.4v1.8M12.4 7.4v1.8M14.4 7.4v1.8"/><path d="M15.9 9.1h.01"/>`+bolt(12, 15.7, 6.6)),
	catalogueEntry("grid", "inverter", "current-ac", "Wechselrichter",
		`<rect x="3" y="3.5" width="18" height="17" rx="2"/>`+bolt(12, 9.4, 6.6)+`<path d="M7 16.5c1.667-1.9 3.333-1.9 5 0s3.333 1.9 5 0"/>`),
	catalogueEntry("grid", "sensor", "pulse", "Sensor allgemein",
		`<path d="M3 12h3.5l2-5.5 4 11 2.5-5.5H21"/>`),

	catalogueEntry("switching", "plug-smart", "power-plug", "Smarte Steckdose",
		`<g transform="translate(1.1 .25) rotate(-28 12 12)"><path d="M16.2 6.6H9.4A3.4 3.4 0 0 0 6 10v4a3.4 3.4 0 0 0 3.4 3.4h6.8"/><ellipse cx="16.2" cy="12" rx="2.4" ry="5.4"/><circle cx="16.5" cy="10.1" r="0.8"/><circle cx="16.5" cy="13.9" r="0.8"/><path d="M2.6 9.9h3.4M2.6 14.1h3.4"/></g>`),
	catalogueEntry("switching", "socket-wall", "power-socket-de", "Unterputz-Steckdose",
		`<rect x="2.5" y="2.5" width="19" height="19" rx="3"/><circle cx="12" cy="12" r="6"/><circle cx="9.5" cy="12" r="1.1"/><circle cx="14.5" cy="12" r="1.1"/><path d="M18.3 19.3h.01"/><path d="M16.9 18.1a2.2 2.2 0 0 1 2.9 0"/>`),
	catalogueEntry("switching", "switch", "toggle-switch-outline", "Schalter, Relais",
		`<rect x="2.5" y="7" width="19" height="10" rx="5"/><circle cx="16.5" cy="12" r="3"/>`),
	catalogueEntry("switching", "timer", "timer-outline", "Zeitschaltuhr",
		`<circle cx="12" cy="12" r="9"/><path d="M12 7v5l3.5 2"/>`),

	catalogueEntry("heating", "heat-pump", "heat-pump", "Wärmepumpe",
		`<rect x="3" y="6.5" width="18" height="11" rx="1.5"/><path d="M5.5 9.6h3M5.5 12h3M5.5 14.4h3"/><circle cx="15.2" cy="12" r="3.7"/>`+rotor(15.2, 12, 3.1)),
	catalogueEntry("heating", "fan", "fan", "Lüfter, Klimagerät",
		`<circle cx="12" cy="12" r="8.6"/>`+rotor(12, 12, 6.2)),
	catalogueEntry("heating", "heater", "gas-burner", "Heizung (Öl/Gas)",
		`<path d="M7.5 5.5V3.2a1.2 1.2 0 0 1 1.2-1.2h2.3"/><rect x="3.5" y="5.5" width="17" height="14" rx="2"/><path d="M12 9c1.9 1.9 2.8 3 2.8 4.2a2.8 2.8 0 0 1-5.6 0C9.2 12 10.1 10.9 12 9Z"/><path d="M7 19.5v2M17 19.5v2"/>`),
	catalogueEntry("heating", "water-boiler", "water-boiler", "Wasserboiler",
		`<rect x="3.5" y="3.5" width="11.5" height="17" rx="4"/><path d="M5 8c1.42-1.3 2.83-1.3 4.25 0s2.83 1.3 4.25 0"/><path d="M5 11c1.42-1.3 2.83-1.3 4.25 0s2.83 1.3 4.25 0"/>`+bolt(9.25, 16.2, 4.2)+`<path d="M19.9 14.1V8.3a1.6 1.6 0 1 0-3.2 0v5.8a3.3 3.3 0 1 0 3.2 0Z"/>`),
	catalogueEntry("heating", "thermometer", "thermometer", "Thermometer",
		`<path d="M10.9 13.1V5a2 2 0 1 1 4 0v8.1a4.2 4.2 0 1 1-4 0Z"/><path d="M9.1 6H6.9M9.1 8.5H7.9M9.1 11H6.9"/><circle cx="12.9" cy="16.8" r="1.7"/>`),
	catalogueEntry("heating", "pipe-valve", "pipe-valve", "Heizungsrohr-Ventil",
		`<path d="M2.5 13.5H7M17 13.5h4.5"/><path d="M7 10 17 17v-7L7 17Z"/><path d="M12 13.5V6.5"/><path d="M9 6.5h6"/>`),

	catalogueEntry("mobility", "wallbox", "ev-station", "Wallbox",
		wallboxBody+bolt(8.5, 10.5, 7.5)+`<path d="M14 13h2a2.5 2.5 0 0 1 2.5 2.5v1"/><path d="M16 16.5h5V18a2.5 2.5 0 0 1-5 0z"/>`),
	catalogueEntry("mobility", "wallbox-compact", "ev-plug-type2", "Wallbox mit Dose",
		`<rect x="5" y="3" width="14" height="18" rx="3"/>`+bolt(12, 10, 8)+`<circle cx="12" cy="17" r="1.8"/>`),
	catalogueEntry("mobility", "wallbox-plug", "power-plug-outline", "Ladestecker",
		plugHead+`<path d="M12 16v5"/>`),

	catalogueEntry("building", "home", "home", "Gebäude",
		houseBody+`<path d="M10 20.5v-5h4v5"/>`),
	catalogueEntry("building", "home-bolt", "home-lightning-bolt-outline", "Hausverbrauch",
		houseBody+bolt(12, 15, 6)),
	catalogueEntry("building", "bolt-circle", "lightning-bolt-circle", "Verbrauch",
		`<circle cx="12" cy="12" r="8.4"/>`+bolt(12, 12, 10)),

	catalogueEntry("control", "chip", "chip", "Standard",
		`<rect x="6" y="6" width="12" height="12" rx="2"/><line x1="9" y1="3" x2="9" y2="6"/><line x1="15" y1="3" x2="15" y2="6"/><line x1="9" y1="18" x2="9" y2="21"/><line x1="15" y1="18" x2="15" y2="21"/><line x1="3" y1="9" x2="6" y2="9"/><line x1="3" y1="15" x2="6" y2="15"/><line x1="18" y1="9" x2="21" y2="9"/><line x1="18" y1="15" x2="21" y2="15"/>`),
	catalogueEntry("control", "device-generic", "package-variant-closed", "Gerät allgemein",
		`<rect x="4" y="3.5" width="16" height="17" rx="2"/><path d="M8 8h8M8 12h8M8 16h5"/>`),
	catalogueEntry("control", "raspberry-pi", "raspberry-pi", "Raspberry Pi",
		`<rect x="2.5" y="4" width="19" height="16" rx="3"/><path d="M5 8.6h2.6M5 11.1h2.6M5 13.6h2.6"/><path d="M12.9 10.2C11.5 10 10.5 8.9 10.5 7.5c1.5-.1 2.5 1.1 2.5 2.7Z"/><path d="M13.1 10.2C14.5 10 15.5 8.9 15.5 7.5c-1.5-.1-2.5 1.1-2.5 2.7Z"/><circle cx="11.7" cy="12.2" r="1.15"/><circle cx="14.3" cy="12.2" r="1.15"/><circle cx="13" cy="14.4" r="1.15"/><rect x="18.2" y="8.6" width="2.8" height="2.2" rx="0.5"/><rect x="18.2" y="13.2" width="2.8" height="2.2" rx="0.5"/>`),
	catalogueEntry("control", "automations", "sitemap", "Automationen",
		`<circle cx="5.5" cy="6" r="2.5"/><circle cx="5.5" cy="18" r="2.5"/><circle cx="18.5" cy="12" r="2.5"/><path d="M8 6h5a3 3 0 0 1 3 3v.8"/><path d="M8 18h5a3 3 0 0 0 3-3v-.8"/>`),
}

var deviceIconByName = func() map[string]DeviceIcon {
	index := make(map[string]DeviceIcon, len(deviceIconCatalogue))
	for _, icon := range deviceIconCatalogue {
		index[icon.Name] = icon
	}
	return index
}()

// lookupDeviceIcon resolves a stored name, legacy mdi: names included.
func lookupDeviceIcon(name string) (DeviceIcon, bool) {
	icon, ok := deviceIconByName[deviceiconname.Canonical(name)]
	return icon, ok
}

// DeviceIconCatalogue returns the pickable icons in catalogue order. The
// picker in the browser reads it over GET /api/v1/device/icons.
func DeviceIconCatalogue() []DeviceIcon {
	return append([]DeviceIcon(nil), deviceIconCatalogue...)
}

// deviceIconSuggestion maps a discovery device block to a catalogue icon.
// Manufacturer must match exactly (case-insensitive); ModelPrefixes is empty
// for "any model of this manufacturer". The first matching rule wins, so the
// more specific rules of one manufacturer come first.
type deviceIconSuggestion struct {
	Manufacturer  string
	ModelPrefixes []string
	Icon          string
}

// deviceIconSuggestions is the default icon per device type, used until
// someone saves an icon for the device. Manufacturer and model are what the
// services put into their Home Assistant discovery device block; the Shelly
// codes are the hardware IDs from Shelly.GetDeviceInfo (gen1 SH*, Plus SN*,
// Pro SP*, gen3 S3*). Documented in docs/_internal/device-icons.md.
var deviceIconSuggestions = []deviceIconSuggestion{
	{Manufacturer: "APsystems", Icon: deviceiconname.Prefix + "solar-panel"},
	{Manufacturer: "Trucki (Community-Firmware)", Icon: deviceiconname.Prefix + "inverter"},
	{Manufacturer: "DIY", ModelPrefixes: []string{"LiFePO4"}, Icon: deviceiconname.Prefix + "battery-home"},
	{Manufacturer: "Raspberry Pi Foundation", Icon: deviceiconname.Prefix + "raspberry-pi"},
	{Manufacturer: "Energy Node", Icon: deviceiconname.Prefix + "automations"},
	{Manufacturer: "Tuya", ModelPrefixes: []string{"Tuya valve"}, Icon: deviceiconname.Prefix + "pipe-valve"},
	// Zwischenstecker: Plug, Plug S, Plus Plug S, Plug S Gen3.
	{Manufacturer: "Shelly", ModelPrefixes: []string{"SHPLG", "SNPL", "S3PL"}, Icon: deviceiconname.Prefix + "plug-smart"},
	// Unterputz-Relais: 1, 1PM, 2.5, Plus 1/1PM/2PM, Mini, Gen3.
	{Manufacturer: "Shelly", ModelPrefixes: []string{"SHSW", "SNSW", "S3SW", "SPSW"}, Icon: deviceiconname.Prefix + "socket-wall"},
	// Energiezaehler: EM, 3EM, Pro EM/3EM, Gen3 EM.
	{Manufacturer: "Shelly", ModelPrefixes: []string{"SHEM", "SPEM", "S3EM"}, Icon: deviceiconname.Prefix + "meter-electric"},
	// Temperatur/Feuchte: H&T, Plus H&T, H&T Gen3.
	{Manufacturer: "Shelly", ModelPrefixes: []string{"SHHT", "SNSN-0013A", "S3SN-0U12A"}, Icon: deviceiconname.Prefix + "thermometer"},
}

// suggestedDeviceIcon returns the catalogue icon for the device's type, or
// "" when no rule matches.
func suggestedDeviceIcon(dev registry.DeviceView) string {
	manufacturer := strings.TrimSpace(dev.Manufacturer)
	model := strings.ToUpper(strings.TrimSpace(dev.Model))
	for _, rule := range deviceIconSuggestions {
		if !strings.EqualFold(rule.Manufacturer, manufacturer) {
			continue
		}
		if len(rule.ModelPrefixes) == 0 {
			return rule.Icon
		}
		for _, prefix := range rule.ModelPrefixes {
			if strings.HasPrefix(model, strings.ToUpper(prefix)) {
				return rule.Icon
			}
		}
	}
	return ""
}

// deviceIcon renders the device's symbol: the saved choice, else the
// suggestion for its type, else the chip. Legacy mdi: names resolve through
// deviceiconname; an unknown saved name falls back the same way rather than
// failing - the same tolerance iconFor has for unknown Home Assistant names.
func deviceIcon(dev registry.DeviceView) template.HTML {
	icon, ok := lookupDeviceIcon(dev.IconName)
	if !ok {
		icon, ok = deviceIconByName[suggestedDeviceIcon(dev)]
	}
	if !ok {
		icon = deviceIconByName[deviceIconFallbackName]
	}
	return template.HTML(`<svg ` + deviceIconSVGAttrs + `>` + string(icon.Markup) + `</svg>`)
}

// deviceIconMarkup returns the inner markup of a catalogue icon, the chip
// for an unknown name. For templates that need the drawing inside an <svg>
// they style themselves.
func deviceIconMarkup(name string) template.HTML {
	icon, ok := lookupDeviceIcon(name)
	if !ok {
		icon = deviceIconByName[deviceIconFallbackName]
	}
	return icon.Markup
}

// deviceIconSymbol renders a catalogue icon as a sprite <symbol>, so the
// sprite in base.html draws device-like glyphs (ico-sun, ico-battery, ...)
// from the same source as the device cards.
func deviceIconSymbol(id, name string) template.HTML {
	return template.HTML(`<symbol id="` + template.HTMLEscapeString(id) + `" viewBox="0 0 24 24">` + string(deviceIconMarkup(name)) + `</symbol>`)
}
