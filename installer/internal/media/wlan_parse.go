package media

import (
	"encoding/xml"
	"sort"
)

// maxWifiNetworks begrenzt die Netze in Reichweite.
const maxWifiNetworks = 10

// wifiSample ist ein Access Point aus einer Scanliste, vor dem Zusammenfassen.
type wifiSample struct {
	SSID    string
	FreqMHz int
	Signal  int
	Active  bool
}

// mergeWifi fasst Access Points mit gleichem Namen zu einem Netz zusammen.
// Leere Namen fallen weg, das staerkste Signal gewinnt, und das Band ergibt
// sich aus der Frequenz (ab 5000 MHz 5 GHz). Die Liste ist nach Signal und
// dann nach Name sortiert.
func mergeWifi(samples []wifiSample) (active string, activeFreq int, nets []WifiNetwork) {
	byName := map[string]*WifiNetwork{}
	var order []*WifiNetwork
	for _, s := range samples {
		if s.SSID == "" {
			continue
		}
		if s.Active && active == "" {
			active, activeFreq = s.SSID, s.FreqMHz
		}
		n := byName[s.SSID]
		if n == nil {
			n = &WifiNetwork{SSID: s.SSID}
			byName[s.SSID] = n
			order = append(order, n)
		}
		if s.Signal > n.Signal {
			n.Signal = s.Signal
		}
		switch {
		case s.FreqMHz >= 5000:
			n.Band5 = true
		case s.FreqMHz > 0:
			n.Band24 = true
		}
	}
	sort.SliceStable(order, func(a, b int) bool {
		if order[a].Signal != order[b].Signal {
			return order[a].Signal > order[b].Signal
		}
		return order[a].SSID < order[b].SSID
	})
	if len(order) > maxWifiNetworks {
		order = order[:maxWifiNetworks]
	}
	nets = make([]WifiNetwork, len(order))
	for k, n := range order {
		nets[k] = *n
	}
	return active, activeFreq, nets
}

// wlanProfileXML ist der Teil eines WLAN-Profils, den parseWlanProfile liest.
// Ohne Namensraum im Tag passt das Element unabhaengig von der Version des
// Profils.
type wlanProfileXML struct {
	Name        string `xml:"SSIDConfig>SSID>name"`
	KeyType     string `xml:"MSM>security>sharedKey>keyType"`
	Protected   string `xml:"MSM>security>sharedKey>protected"`
	KeyMaterial string `xml:"MSM>security>sharedKey>keyMaterial"`
}

// parseWlanProfile liest den Namen und, wenn er im Klartext vorliegt, das
// Passwort aus einem WLAN-Profil von WlanGetProfile. Verschluesselte Schluessel
// bleiben leer. ok ist false bei kaputtem XML oder ohne Namen.
func parseWlanProfile(data []byte) (storedWifi, bool) {
	var p wlanProfileXML
	if err := xml.Unmarshal(data, &p); err != nil || p.Name == "" {
		return storedWifi{}, false
	}
	s := storedWifi{SSID: p.Name}
	if p.KeyType == "passPhrase" && p.Protected == "false" {
		s.PSK = p.KeyMaterial
	}
	return s, true
}
