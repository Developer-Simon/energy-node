package media

// Defaults are the host values the installer form starts with.
type Defaults struct {
	Timezone    string
	Keyboard    string
	WifiCountry string
	Timezones   []string
	Keyboards   []string
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
