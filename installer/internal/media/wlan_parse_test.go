package media

import (
	"os"
	"reflect"
	"strings"
	"testing"
	"unsafe"
)

func TestWlanStructLayouts(t *testing.T) {
	sizes := []struct {
		name      string
		got, want uintptr
	}{
		{"wlanInterfaceInfo", unsafe.Sizeof(wlanInterfaceInfo{}), 532},
		{"wlanBssEntry", unsafe.Sizeof(wlanBssEntry{}), 360},
		{"wlanProfileInfo", unsafe.Sizeof(wlanProfileInfo{}), 516},
		{"wlanBssEntry.BssType", unsafe.Offsetof(wlanBssEntry{}.BssType), 48},
		{"wlanBssEntry.Timestamp", unsafe.Offsetof(wlanBssEntry{}.Timestamp), 72},
		{"wlanBssEntry.ChCenterFrequency", unsafe.Offsetof(wlanBssEntry{}.ChCenterFrequency), 92},
		{"wlanBssEntry.IeSize", unsafe.Offsetof(wlanBssEntry{}.IeSize), 356},
		{"wlanConnectionHead.SSIDLen", unsafe.Offsetof(wlanConnectionHead{}.SSIDLen), 520},
		{"wlanConnectionHead.BSSID", unsafe.Offsetof(wlanConnectionHead{}.BSSID), 560},
	}
	for _, s := range sizes {
		if s.got != s.want {
			t.Errorf("%s = %d, want %d", s.name, s.got, s.want)
		}
	}
}

func TestMergeWifiMatchesTheNmcliBehaviour(t *testing.T) {
	samples := []wifiSample{
		{SSID: "", FreqMHz: 2412, Signal: 90},
		{SSID: "WelcomeHome 5 GHz", FreqMHz: 5500, Signal: 64, Active: true},
		{SSID: "WelcomeHome", FreqMHz: 2437, Signal: 70},
		{SSID: "WelcomeHome", FreqMHz: 5180, Signal: 50},
		{SSID: "My:Net", FreqMHz: 2462, Signal: 30},
		{SSID: "Back\\slash", FreqMHz: 2412, Signal: 30},
	}
	active, freq, nets := mergeWifi(samples)
	if active != "WelcomeHome 5 GHz" || freq != 5500 {
		t.Errorf("active = %q, %d", active, freq)
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

func TestParseWlanProfileReadsThePassphrase(t *testing.T) {
	data, err := os.ReadFile("testdata/windows/wlan-profile-psk.xml")
	if err != nil {
		t.Fatal(err)
	}
	got, ok := parseWlanProfile(data)
	want := storedWifi{SSID: "Beispielnetz1", PSK: "beispielschluessel"}
	if !ok || got != want {
		t.Errorf("parseWlanProfile() = %+v, %v, want %+v, true", got, ok, want)
	}
}

func TestParseWlanProfileProtectedKeyIsUnknown(t *testing.T) {
	data, err := os.ReadFile("testdata/windows/wlan-profile-protected.xml")
	if err != nil {
		t.Fatal(err)
	}
	got, ok := parseWlanProfile(data)
	want := storedWifi{SSID: "Beispielnetz1", PSK: ""}
	if !ok || got != want {
		t.Errorf("parseWlanProfile() = %+v, %v, want %+v, true", got, ok, want)
	}
}

func TestParseWlanProfileUnescapesTheName(t *testing.T) {
	data := `<?xml version="1.0"?>
<WLANProfile xmlns="http://www.microsoft.com/networking/WLAN/profile/v1">
	<SSIDConfig><SSID><name>Caf&#233; &amp; Co 5G</name></SSID></SSIDConfig>
</WLANProfile>`
	got, ok := parseWlanProfile([]byte(data))
	if !ok || got.SSID != "Café & Co 5G" {
		t.Errorf("parseWlanProfile() = %+v, %v, want SSID %q", got, ok, "Café & Co 5G")
	}
}

func TestParseWlanProfileRejectsBrokenProfiles(t *testing.T) {
	for name, data := range map[string]string{
		"broken xml":   `<WLANProfile><SSIDConfig><SSID><name>Haus</SSID>`,
		"without ssid": `<WLANProfile><connectionType>ESS</connectionType></WLANProfile>`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, ok := parseWlanProfile([]byte(data)); ok {
				t.Errorf("parseWlanProfile() ok = true, want false")
			}
		})
	}
}

func TestParseWlanProfileKeepsSSIDAndKeyUntrimmed(t *testing.T) {
	data := `<WLANProfile><SSIDConfig><SSID><name> Haus </name></SSID></SSIDConfig>` +
		`<MSM><security><sharedKey><keyType>passPhrase</keyType><protected>false</protected>` +
		`<keyMaterial> geheim123 </keyMaterial></sharedKey></security></MSM></WLANProfile>`
	got, ok := parseWlanProfile([]byte(strings.TrimSpace(data)))
	want := storedWifi{SSID: " Haus ", PSK: " geheim123 "}
	if !ok || got != want {
		t.Errorf("parseWlanProfile() = %+v, %v, want %+v, true", got, ok, want)
	}
}
