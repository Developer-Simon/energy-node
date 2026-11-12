package media

import "crypto/subtle"

// Ergebnisse von CheckWifiKey.
const (
	WifiKeyMatch    = "match"    // der Rechner kennt dieses Netz mit genau diesem Passwort
	WifiKeyMismatch = "mismatch" // der Rechner kennt dieses Netz mit einem anderen Passwort
	WifiKeyUnknown  = "unknown"  // der Rechner kennt das Netz oder sein Passwort nicht
)

// storedWifi ist ein gespeichertes WLAN-Profil dieses Rechners.
type storedWifi struct{ SSID, PSK string }

// matchWifiKey vergleicht ssid und password mit den gespeicherten Profilen.
// Ein Profil mit passendem Namen und gleichem Passwort gewinnt. Ein Profil
// ohne Passwort (offenes Netz, Schluessel im Schluesselbund) sagt nichts.
func matchWifiKey(stored []storedWifi, ssid, password string) string {
	result := WifiKeyUnknown
	for _, s := range stored {
		if s.SSID != ssid || s.PSK == "" {
			continue
		}
		if subtle.ConstantTimeCompare([]byte(s.PSK), []byte(password)) == 1 {
			return WifiKeyMatch
		}
		result = WifiKeyMismatch
	}
	return result
}
