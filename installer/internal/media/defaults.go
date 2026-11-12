package media

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
