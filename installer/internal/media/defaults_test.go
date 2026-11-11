//go:build linux

package media

import (
	"reflect"
	"testing"
)

func TestLangToCountry(t *testing.T) {
	cases := map[string]string{
		"de_DE.UTF-8": "DE",
		"en_US":       "US",
		"C":           "",
	}
	for in, want := range cases {
		if got := countryFromLocale(in); got != want {
			t.Errorf("countryFromLocale(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestZone1970Parsing(t *testing.T) {
	data := []byte("# comment line\n" +
		"DE\t+5230+01322\tEurope/Berlin\n" +
		"AT\t+4813+01620\tEurope/Vienna\n")
	want := []string{"Europe/Berlin", "Europe/Vienna"}
	if got := parseZoneTab(data); !reflect.DeepEqual(got, want) {
		t.Errorf("parseZoneTab() = %v, want %v", got, want)
	}
}
