package media

import (
	"strings"
	"testing"
)

func TestWindowsZoneToIANA(t *testing.T) {
	cases := map[string]string{
		"W. Europe Standard Time": "Europe/Berlin",
		"UTC":                     "UTC",
		"Gibt es nicht":           "",
	}
	for in, want := range cases {
		if got := windowsZoneToIANA(in); got != want {
			t.Errorf("windowsZoneToIANA(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestEveryWindowsZoneIsKnown(t *testing.T) {
	zones, _ := embeddedZones()
	known := map[string]bool{}
	for _, z := range zones {
		known[z] = true
	}
	for _, line := range strings.Split(string(windowsZonesData()), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		id, _, _ := strings.Cut(line, "\t")
		iana := windowsZoneToIANA(id)
		if !known[iana] {
			t.Errorf("Windows zone %q maps to %q, which is not in embeddedZones()", id, iana)
		}
	}
}

func TestEmbeddedZonesContainBerlinAndUTC(t *testing.T) {
	zones, countries := embeddedZones()
	var hasBerlin, hasUTC bool
	for _, z := range zones {
		hasBerlin = hasBerlin || z == "Europe/Berlin"
		hasUTC = hasUTC || z == "UTC"
	}
	if !hasBerlin || !hasUTC {
		t.Errorf("embeddedZones() lacks Berlin (%v) or UTC (%v)", hasBerlin, hasUTC)
	}
	if countries["Europe/Berlin"] != "DE" {
		t.Errorf("countries[Europe/Berlin] = %q, want DE", countries["Europe/Berlin"])
	}
}

func TestKeyboardFromKLID(t *testing.T) {
	cases := map[string]string{
		"00000407": "de",
		"0000100C": "ch",
		"00010409": "us",
		"00000419": "",
	}
	for in, want := range cases {
		if got := keyboardFromKLID(in); got != want {
			t.Errorf("keyboardFromKLID(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCountryFromLocaleAcceptsDash(t *testing.T) {
	cases := map[string]string{
		"de-DE":       "DE",
		"de_DE.UTF-8": "DE",
		"en":          "",
		"en_001":      "",
		"sr-Latn-RS":  "RS",
	}
	for in, want := range cases {
		if got := countryFromLocale(in); got != want {
			t.Errorf("countryFromLocale(%q) = %q, want %q", in, got, want)
		}
	}
}
