//go:build linux

package media

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestMatchWifiKey(t *testing.T) {
	stored := []storedWifi{
		{SSID: "Haus", PSK: "geheim123"},
		{SSID: "Offen", PSK: ""},
		{SSID: "Garten 5G", PSK: "blume123"},
	}
	cases := map[string]struct{ ssid, pw, want, via string }{
		"same password":          {"Haus", "geheim123", WifiKeyMatch, ""},
		"other password":         {"Haus", "falsch123", WifiKeyMismatch, ""},
		"unknown net":            {"Fremd", "egal1234", WifiKeyUnknown, ""},
		"no stored key":          {"Offen", "egal1234", WifiKeyUnknown, ""},
		"twin 5 GHz, same":       {"Haus 5 GHz", "geheim123", WifiKeyMatch, "Haus"},
		"twin 5 GHz, other":      {"Haus_5G", "falsch123", WifiKeyMismatch, "Haus"},
		"twin 2.4 GHz, same":     {"Garten", "blume123", WifiKeyMatch, "Garten 5G"},
		"twin 2.4 suffix, other": {"Garten-2.4GHz", "falsch123", WifiKeyMismatch, "Garten 5G"},
		"no twin by prefix":      {"Hausboot", "geheim123", WifiKeyUnknown, ""},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got, via := matchWifiKey(stored, tc.ssid, tc.pw)
			if got != tc.want || via != tc.via {
				t.Errorf("matchWifiKey(%q, %q) = %q, %q, want %q, %q", tc.ssid, tc.pw, got, via, tc.want, tc.via)
			}
		})
	}
}

func TestExactProfileWinsOverTwin(t *testing.T) {
	stored := []storedWifi{{SSID: "Haus", PSK: "alt12345"}, {SSID: "Haus 5 GHz", PSK: "neu12345"}}
	if got, via := matchWifiKey(stored, "Haus 5 GHz", "alt12345"); got != WifiKeyMismatch || via != "" {
		t.Errorf("got %q, %q, want mismatch against the exact profile", got, via)
	}
}

func TestWifiConnectionNames(t *testing.T) {
	data := []byte("Haus 5 GHz:802-11-wireless\nlo:loopback\nMy\\:Net:802-11-wireless\nWired connection 1:802-3-ethernet\n")
	got := strings.Join(wifiConnectionNames(data), "|")
	if got != "Haus 5 GHz|My:Net" {
		t.Errorf("wifiConnectionNames() = %q", got)
	}
}

func TestCheckWifiKeyAsksNetworkManager(t *testing.T) {
	old := nmcliOutput
	t.Cleanup(func() { nmcliOutput = old })
	nmcliOutput = func(ctx context.Context, args ...string) ([]byte, error) {
		joined := strings.Join(args, " ")
		switch {
		case joined == "-t -f NAME,TYPE connection show":
			return []byte("Profil A:802-11-wireless\nProfil B:802-11-wireless\n"), nil
		case joined == "-g 802-11-wireless.ssid connection show id Profil A":
			return []byte("Nachbar\n"), nil
		case joined == "-g 802-11-wireless.ssid connection show id Profil B":
			return []byte("Haus\n"), nil
		case joined == "-s -g 802-11-wireless-security.psk connection show id Profil B":
			return []byte("geheim123\n"), nil
		}
		return nil, errors.New("unexpected: " + joined)
	}
	if got, via := CheckWifiKey(context.Background(), "Haus", "geheim123"); got != WifiKeyMatch || via != "" {
		t.Errorf("match case = %q, %q", got, via)
	}
	if got, _ := CheckWifiKey(context.Background(), "Haus", "falsch123"); got != WifiKeyMismatch {
		t.Errorf("mismatch case = %q", got)
	}
	if got, via := CheckWifiKey(context.Background(), "Haus 5 GHz", "geheim123"); got != WifiKeyMatch || via != "Haus" {
		t.Errorf("twin case = %q, %q", got, via)
	}
	if got, _ := CheckWifiKey(context.Background(), "", "x"); got != WifiKeyUnknown {
		t.Errorf("empty ssid = %q", got)
	}
}
