package media

import (
	"crypto/subtle"
	"regexp"
	"strings"
)

// Ergebnisse von CheckWifiKey.
const (
	WifiKeyMatch    = "match"    // der Rechner kennt das Netz (oder sein Gegenstueck) mit genau diesem Passwort
	WifiKeyMismatch = "mismatch" // der Rechner kennt das Netz (oder sein Gegenstueck) mit einem anderen Passwort
	WifiKeyUnknown  = "unknown"  // der Rechner kennt kein Passwort fuer das Netz
)

// storedWifi ist ein gespeichertes WLAN-Profil dieses Rechners.
type storedWifi struct{ SSID, PSK string }

// bandSuffix ist ein Bandzusatz am Ende eines WLAN-Namens wie "5 GHz", "5G",
// "_5G" oder "2.4GHz". Dieselbe Regel wie BAND_SUFFIX in media-model.js.
var bandSuffix = regexp.MustCompile(`(?i)[\s_.-]*(5|2[.,]?4)\s*g(hz)?$`)

// bandless gibt den Namen ohne Bandzusatz klein geschrieben zurueck.
func bandless(ssid string) string {
	return strings.ToLower(strings.TrimSpace(bandSuffix.ReplaceAllString(ssid, "")))
}

// twins sagt, ob a und b dasselbe Netz auf verschiedenen Baendern sind,
// etwa "Haus" und "Haus 5 GHz".
func twins(a, b string) bool {
	base := bandless(a)
	return a != b && base != "" && base == bandless(b)
}

// matchWifiKey vergleicht ssid und password mit den gespeicherten Profilen.
// Zuerst zaehlen Profile mit genau diesem Namen. Kennt der Rechner nur das
// Gegenstueck auf dem anderen Band, vergleicht er damit und nennt dessen
// Namen in via, denn Router senden beide Baender meist mit demselben
// Passwort. Profile ohne Passwort (offenes Netz, Schluesselbund) sagen nichts.
func matchWifiKey(stored []storedWifi, ssid, password string) (result, via string) {
	if r, _ := compareKeys(stored, password, func(name string) bool { return name == ssid }); r != WifiKeyUnknown {
		return r, ""
	}
	return compareKeys(stored, password, func(name string) bool { return twins(ssid, name) })
}

// compareKeys vergleicht password mit den Profilen, deren Name zu match passt.
// Ein gleiches Passwort gewinnt, sonst zaehlt das erste abweichende.
func compareKeys(stored []storedWifi, password string, match func(string) bool) (string, string) {
	result, via := WifiKeyUnknown, ""
	for _, s := range stored {
		if !match(s.SSID) || s.PSK == "" {
			continue
		}
		if subtle.ConstantTimeCompare([]byte(s.PSK), []byte(password)) == 1 {
			return WifiKeyMatch, s.SSID
		}
		if result == WifiKeyUnknown {
			result, via = WifiKeyMismatch, s.SSID
		}
	}
	return result, via
}
