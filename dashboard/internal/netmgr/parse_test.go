package netmgr

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func readFixture(t *testing.T, name string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", name+".txt"))
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func parseFixture(t *testing.T, name string) Status {
	t.Helper()
	st, err := ParseStatus(readFixture(t, name))
	if err != nil {
		t.Fatalf("ParseStatus(%s): %v", name, err)
	}
	return st
}

func linkByID(t *testing.T, st Status, id string) Link {
	t.Helper()
	for _, l := range st.Links {
		if l.ID == id {
			return l
		}
	}
	t.Fatalf("no link %q in %+v", id, st.Links)
	return Link{}
}

func TestParseStatusTable(t *testing.T) {
	cases := []struct {
		name      string
		managed   bool
		hasWifi   bool
		lanState  string
		lanRoute  bool
		wifiState string
		wifiRoute bool
	}{
		{"lan_only", true, true, "up", true, "off", false},
		{"both", true, true, "up", true, "up", false},
		{"wifi_only", true, true, "down", false, "up", true},
		{"wifi_error", true, true, "up", true, "error", false},
		{"hotspot", true, true, "up", true, "hotspot", false},
		{"no_wifi", true, false, "up", true, "none", false},
		{"unmanaged", false, true, "up", true, "off", false},
		{"no_country", true, true, "up", true, "up", false},
		{"fake_lan", true, true, "up", true, "off", false},
		{"fake_wifi", true, true, "down", false, "up", true},
		{"fake_error", true, true, "up", true, "error", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			st := parseFixture(t, tc.name)
			if st.Managed != tc.managed {
				t.Errorf("Managed = %v, want %v", st.Managed, tc.managed)
			}
			if st.HasWifi != tc.hasWifi {
				t.Errorf("HasWifi = %v, want %v", st.HasWifi, tc.hasWifi)
			}
			if len(st.Links) != 2 || st.Links[0].ID != "lan" || st.Links[1].ID != "wifi" {
				t.Fatalf("Links = %+v, want [lan, wifi]", st.Links)
			}
			lan := linkByID(t, st, "lan")
			if lan.State != tc.lanState || lan.Route != tc.lanRoute {
				t.Errorf("lan State/Route = %s/%v, want %s/%v", lan.State, lan.Route, tc.lanState, tc.lanRoute)
			}
			wifi := linkByID(t, st, "wifi")
			if wifi.State != tc.wifiState || wifi.Route != tc.wifiRoute {
				t.Errorf("wifi State/Route = %s/%v, want %s/%v", wifi.State, wifi.Route, tc.wifiState, tc.wifiRoute)
			}
		})
	}
}

func TestParseStatusLinkDetails(t *testing.T) {
	st := parseFixture(t, "both")
	lan := linkByID(t, st, "lan")
	if lan.Device != "eth0" || lan.Address != "192.168.1.20" || lan.Gateway != "192.168.1.1" {
		t.Errorf("lan = %+v", lan)
	}
	if lan.SpeedMbps != 1000 {
		t.Errorf("lan SpeedMbps = %d, want 1000", lan.SpeedMbps)
	}
	if strings.Join(lan.DNS, ",") != "192.168.1.1,1.1.1.1" {
		t.Errorf("lan DNS = %v", lan.DNS)
	}
	wifi := linkByID(t, st, "wifi")
	if wifi.Device != "wlan0" || wifi.SSID != "Haus:Netz" || wifi.Address != "192.168.1.31" {
		t.Errorf("wifi = %+v", wifi)
	}
	if wifi.SignalDBm == nil || *wifi.SignalDBm != -59 {
		t.Errorf("wifi SignalDBm = %v, want -59", wifi.SignalDBm)
	}
	if wifi.Band != "2.4" || wifi.Channel != 1 {
		t.Errorf("wifi band/channel = %s/%d, want 2.4/1", wifi.Band, wifi.Channel)
	}
}

func TestParseStatusWifiOnlyRoute(t *testing.T) {
	st := parseFixture(t, "wifi_only")
	wifi := linkByID(t, st, "wifi")
	if wifi.Gateway != "192.168.1.1" || wifi.Band != "5" || wifi.Channel != 36 {
		t.Errorf("wifi = %+v", wifi)
	}
	lan := linkByID(t, st, "lan")
	if lan.Device != "eth0" || lan.Address != "" {
		t.Errorf("lan without cable = %+v", lan)
	}
}

func TestParseStatusWifiError(t *testing.T) {
	st := parseFixture(t, "wifi_error")
	wifi := linkByID(t, st, "wifi")
	if wifi.LastError != "auth" || wifi.LastErrorSSID != "Haus:Netz" || wifi.LastErrorAt != "2026-11-11T20:13:02+01:00" {
		t.Errorf("wifi last error = %+v", wifi)
	}
	if st.Switch == nil || st.Switch.Result != "fail" || len(st.Switch.Steps) != 3 {
		t.Errorf("Switch = %+v", st.Switch)
	}
}

func TestParseStatusHotspot(t *testing.T) {
	st := parseFixture(t, "hotspot")
	if !st.Hotspot.Available || !st.Hotspot.Active || st.Hotspot.Clients != 2 {
		t.Errorf("Hotspot = %+v", st.Hotspot)
	}
	if !st.Settings.Hotspot || st.Settings.Fallback != "parallel" {
		t.Errorf("Settings = %+v", st.Settings)
	}
}

func TestParseStatusUnmanaged(t *testing.T) {
	st := parseFixture(t, "unmanaged")
	if st.Hotspot.Available || len(st.Networks) != 0 {
		t.Errorf("unmanaged: Hotspot = %+v, Networks = %+v", st.Hotspot, st.Networks)
	}
	if st.Switch != nil {
		t.Errorf("Switch = %+v, want nil for {}", st.Switch)
	}
	if linkByID(t, st, "lan").Device != "eth0" {
		t.Errorf("unmanaged lan device = %q, want eth0", linkByID(t, st, "lan").Device)
	}
}

func TestParseStatusBlockedByRfkill(t *testing.T) {
	raw := strings.Replace(readFixture(t, "both"), "##rfkill\n0 0\n", "##rfkill\n1 0\n", 1)
	st, err := ParseStatus(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got := linkByID(t, st, "wifi").State; got != "blocked" {
		t.Errorf("wifi State = %s, want blocked", got)
	}
}

func TestParseStatusNoCountry(t *testing.T) {
	st := parseFixture(t, "no_country")
	if st.Country != "" {
		t.Errorf("Country = %q, want empty", st.Country)
	}
	if parseFixture(t, "both").Country != "DE" {
		t.Errorf("both: Country want DE")
	}
}

func TestParseWifiEscapedColon(t *testing.T) {
	cases := []struct {
		name     string
		wifiRow  string
		profile  string
		wantSSID string
	}{
		{"umlaut_colon", `*:Büro\:2:82:2437:1:WPA2`, "uuid-1\tyes\tBüro\\:2", "Büro:2"},
		{"haus_colon", `*:Haus\:Netz:82:2437:1:WPA2`, "uuid-1\tyes\tHaus\\:Netz", "Haus:Netz"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw := readFixture(t, "both")
			raw = strings.Replace(raw, `*:Haus\:Netz:82:2437:1:WPA2`, tc.wifiRow, 1)
			raw = strings.Replace(raw, "uuid-1\tyes\tHaus\\:Netz", tc.profile, 1)
			st, err := ParseStatus(raw)
			if err != nil {
				t.Fatal(err)
			}
			if len(st.Networks) != 2 {
				t.Fatalf("Networks = %+v, want 2", st.Networks)
			}
			first := st.Networks[0]
			if first.SSID != tc.wantSSID || !first.Current || !first.Saved || first.ProfileUUID != "uuid-1" {
				t.Errorf("first network = %+v, want SSID %q", first, tc.wantSSID)
			}
			if st.Networks[1].SSID != "Nachbar" || st.Networks[1].Saved || st.Networks[1].Band != "5" {
				t.Errorf("second network = %+v", st.Networks[1])
			}
			if got := linkByID(t, st, "wifi").SSID; got != tc.wantSSID {
				t.Errorf("wifi link SSID = %q, want %q", got, tc.wantSSID)
			}
		})
	}
}

func TestParseSecurityMapping(t *testing.T) {
	st := parseFixture(t, "wifi_only")
	got := map[string]string{}
	for _, n := range st.Networks {
		got[n.SSID] = n.Security
	}
	if got["Haus:Netz"] != "WPA2/WPA3" || got["Nachbar"] != "WPA2" {
		t.Errorf("security = %v", got)
	}
}

func TestParseNetworksDedupAndSort(t *testing.T) {
	raw := strings.Replace(readFixture(t, "both"),
		":Nachbar:45:5180:36:WPA2\n",
		":Nachbar:45:5180:36:WPA2\n:Nachbar:70:2412:1:WPA2\n::80:2412:1:WPA2\n", 1)
	st, err := ParseStatus(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Networks) != 2 {
		t.Fatalf("Networks = %+v, want Haus:Netz and one Nachbar", st.Networks)
	}
	nachbar := st.Networks[1]
	if nachbar.SSID != "Nachbar" || nachbar.SignalDBm != -65 || nachbar.Channel != 1 {
		t.Errorf("Nachbar = %+v, want strongest entry (70 %%, channel 1)", nachbar)
	}
}

func TestParseWifiEmptyWhenUnmanaged(t *testing.T) {
	st := parseFixture(t, "unmanaged")
	if st.Hotspot.Available {
		t.Errorf("Hotspot.Available = true without NetworkManager")
	}
}

func TestSignalPercentToDBm(t *testing.T) {
	cases := map[int]int{100: -50, 72: -64, 0: -100}
	for pct, want := range cases {
		if got := signalPercentToDBm(pct); got != want {
			t.Errorf("signalPercentToDBm(%d) = %d, want %d", pct, got, want)
		}
	}
}

func TestLevel(t *testing.T) {
	cases := map[int]int{-40: 4, -55: 4, -56: 3, -65: 3, -66: 2, -75: 2, -76: 1, -100: 1}
	for dbm, want := range cases {
		if got := level(dbm); got != want {
			t.Errorf("level(%d) = %d, want %d", dbm, got, want)
		}
	}
}

func TestSplitTerse(t *testing.T) {
	got := splitTerse(`*:Haus\:Netz:82:2437:1:WPA2`)
	want := []string{"*", "Haus:Netz", "82", "2437", "1", "WPA2"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("splitTerse = %q, want %q", got, want)
	}
	if got := splitTerse(`a\\b:c`); strings.Join(got, "|") != `a\b|c` {
		t.Errorf(`splitTerse(a\\b:c) = %q`, got)
	}
}

func TestParseFreqWithUnit(t *testing.T) {
	raw := strings.Replace(readFixture(t, "both"), "*:Haus\\:Netz:82:2437:1:WPA2", "*:Haus\\:Netz:82:5180 MHz:36:WPA2", 1)
	st, err := ParseStatus(raw)
	if err != nil {
		t.Fatal(err)
	}
	if n := st.Networks[0]; n.Band != "5" || n.Channel != 36 {
		t.Errorf("network = %+v, want band 5 channel 36", n)
	}
}
