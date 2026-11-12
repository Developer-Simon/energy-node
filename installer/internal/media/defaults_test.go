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

func TestParseActiveWifi(t *testing.T) {
	cases := map[string]struct {
		in   string
		ssid string
		freq int
	}{
		"no active line":    {in: "no:Other:2412 MHz\n", ssid: "", freq: 0},
		"active line":       {in: "no:Other:2412 MHz\nyes:Werkstatt:5500 MHz\n", ssid: "Werkstatt", freq: 5500},
		"spaces in name":    {in: "yes:WelcomeHome 5 GHz:5500 MHz\n", ssid: "WelcomeHome 5 GHz", freq: 5500},
		"escaped colon":     {in: "yes:My\\:Net:2437 MHz\n", ssid: "My:Net", freq: 2437},
		"escaped slash":     {in: "yes:Back\\\\slash:2412 MHz\n", ssid: "Back\\slash", freq: 2412},
		"empty input":       {in: "", ssid: "", freq: 0},
		"bad frequency":     {in: "yes:Werkstatt:?\n", ssid: "Werkstatt", freq: 0},
		"first active wins": {in: "yes:First:2412 MHz\nyes:Second:5180 MHz\n", ssid: "First", freq: 2412},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			ssid, freq := parseActiveWifi([]byte(tc.in))
			if ssid != tc.ssid || freq != tc.freq {
				t.Errorf("parseActiveWifi(%q) = %q, %d, want %q, %d", tc.in, ssid, freq, tc.ssid, tc.freq)
			}
		})
	}
}

func TestParseZoneCountries(t *testing.T) {
	data := []byte("# comment\nDE,DK,NO,SE,SJ\t+5230+01322\tEurope/Berlin\nCH,DE,LI\t+4723+00832\tEurope/Zurich\nbroken line\n")
	got := parseZoneCountries(data)
	if got["Europe/Berlin"] != "DE" || got["Europe/Zurich"] != "CH" || len(got) != 2 {
		t.Errorf("parseZoneCountries() = %v", got)
	}
}
