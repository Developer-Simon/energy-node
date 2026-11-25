package media

import "strings"

// Defaults are the host values the installer form starts with.
type Defaults struct {
	Timezone      string
	Keyboard      string
	WifiCountry   string
	WifiSSID      string
	WifiFreqMHz   int               // Frequenz des erkannten WLANs, 0 = unbekannt
	WifiNetworks  []WifiNetwork     // Netze in Reichweite, staerkstes zuerst
	ZoneCountries map[string]string // Zeitzone -> Laendercode aus zone1970.tab
	Timezones     []string
	Keyboards     []string
}

// fixedKeyboards are the layouts offered in the form, in display order.
var fixedKeyboards = []string{"de", "us", "gb", "fr", "ch", "it", "es", "nl", "se", "dk", "no", "fi", "pl", "cz"}

// klidLayouts ordnet die letzten vier Hex-Stellen einer Windows-KLID dem Layout zu.
var klidLayouts = map[string]string{
	"0407": "de", "0409": "us", "0809": "gb", "040c": "fr", "0807": "ch", "100c": "ch",
	"0410": "it", "040a": "es", "0c0a": "es", "0413": "nl", "0813": "nl", "041d": "se",
	"0406": "dk", "0414": "no", "040b": "fi", "0415": "pl", "0405": "cz",
}

// keyboardFromKLID returns the layout of a Windows keyboard layout ID such as "00000407", or "".
func keyboardFromKLID(klid string) string {
	if len(klid) < 4 {
		return ""
	}
	return klidLayouts[strings.ToLower(klid[len(klid)-4:])]
}

// countryFromLocale returns the upper-case country of a locale such as de_DE.UTF-8 or de-DE, or "".
func countryFromLocale(locale string) string {
	if i := strings.IndexAny(locale, ".@"); i >= 0 {
		locale = locale[:i]
	}
	parts := strings.FieldsFunc(locale, func(r rune) bool { return r == '_' || r == '-' })
	if len(parts) < 2 {
		return ""
	}
	last := parts[len(parts)-1]
	if len(last) != 2 {
		return ""
	}
	for _, r := range last {
		if (r < 'A' || r > 'Z') && (r < 'a' || r > 'z') {
			return ""
		}
	}
	return strings.ToUpper(last)
}

// keyboardChoices returns the fixed layouts with the detected one first when it is missing.
func keyboardChoices(detected string) []string {
	for _, k := range fixedKeyboards {
		if k == detected {
			return append([]string(nil), fixedKeyboards...)
		}
	}
	return append([]string{detected}, fixedKeyboards...)
}

// WifiNetwork ist ein Netz in Reichweite. Ein Name kann auf beiden Baendern funken.
type WifiNetwork struct {
	SSID   string
	Signal int // 0..100, das staerkste des Namens
	Band24 bool
	Band5  bool
}
