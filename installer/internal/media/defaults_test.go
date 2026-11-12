//go:build linux

package media

import (
	"fmt"
	"reflect"
	"strings"
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

func TestParseWifiList(t *testing.T) {
	data := []byte(strings.Join([]string{
		"no::2412 MHz:90",
		"yes:WelcomeHome 5 GHz:5500 MHz:64",
		"no:WelcomeHome:2437 MHz:70",
		"no:WelcomeHome:5180 MHz:50",
		"no:My\\:Net:2462 MHz:30",
		"no:Back\\\\slash:2412 MHz:30",
		"broken",
	}, "\n") + "\n")
	ssid, freq, nets := parseWifiList(data)
	if ssid != "WelcomeHome 5 GHz" || freq != 5500 {
		t.Errorf("active = %q, %d", ssid, freq)
	}
	want := []WifiNetwork{
		{SSID: "WelcomeHome", Signal: 70, Band24: true, Band5: true},
		{SSID: "WelcomeHome 5 GHz", Signal: 64, Band5: true},
		{SSID: "Back\\slash", Signal: 30, Band24: true},
		{SSID: "My:Net", Signal: 30, Band24: true},
	}
	if !reflect.DeepEqual(nets, want) {
		t.Errorf("nets = %+v\nwant %+v", nets, want)
	}
}

func TestParseWifiListLimitsTheList(t *testing.T) {
	var lines []string
	for i := 0; i < maxWifiNetworks+5; i++ {
		lines = append(lines, fmt.Sprintf("no:Netz%02d:2412 MHz:%d", i, i))
	}
	_, _, nets := parseWifiList([]byte(strings.Join(lines, "\n")))
	if len(nets) != maxWifiNetworks || nets[0].SSID != fmt.Sprintf("Netz%02d", maxWifiNetworks+4) {
		t.Errorf("got %d nets, first %+v", len(nets), nets[0])
	}
}

func TestParseWifiListWithoutActive(t *testing.T) {
	ssid, freq, nets := parseWifiList([]byte(""))
	if ssid != "" || freq != 0 || len(nets) != 0 {
		t.Errorf("empty input = %q, %d, %v", ssid, freq, nets)
	}
}

func TestParseZoneCountries(t *testing.T) {
	data := []byte("# comment\nDE,DK,NO,SE,SJ\t+5230+01322\tEurope/Berlin\nCH,DE,LI\t+4723+00832\tEurope/Zurich\nbroken line\n")
	got := parseZoneCountries(data)
	if got["Europe/Berlin"] != "DE" || got["Europe/Zurich"] != "CH" || len(got) != 2 {
		t.Errorf("parseZoneCountries() = %v", got)
	}
}
