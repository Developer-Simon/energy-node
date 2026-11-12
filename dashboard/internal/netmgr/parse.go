package netmgr

import (
	"encoding/json"
	"errors"
	"sort"
	"strconv"
	"strings"
)

// ParseStatus parses the output of `energy-node-dashboard-system-action network-status`.
func ParseStatus(raw string) (Status, error) {
	secs := splitSections(raw)
	if _, ok := secs["managed"]; !ok {
		return Status{}, errors.New("network status: missing ##managed section")
	}

	managed := strings.TrimSpace(secs["managed"]) == "yes"
	country := strings.TrimSpace(secs["country"])
	wifiDev := firstLine(secs["wifidev"])
	hasWifi := wifiDev != ""

	rfkillBlocked := false
	for _, l := range lines(secs["rfkill"]) {
		for _, f := range strings.Fields(l) {
			if f == "1" {
				rfkillBlocked = true
			}
		}
	}

	addrs := parseAddrs(secs["addr"])
	routes := parseRoutes(secs["route"])
	dns := lines(secs["dns"])
	speeds := parseSpeeds(secs["speed"])

	devStates := map[string]string{}
	lanDev := ""
	for _, l := range lines(secs["devices"]) {
		f := splitTerse(l)
		if len(f) < 3 {
			continue
		}
		devStates[f[0]] = f[2]
		if lanDev == "" && f[1] == "ethernet" {
			lanDev = f[0]
		}
	}
	if !managed {
		lanDev = firstAddrDevice(addrs, wifiDev)
	}

	hotspotActive := false
	clients := 0
	for _, l := range lines(secs["hotspot"]) {
		f := strings.Fields(l)
		if len(f) != 2 {
			continue
		}
		switch f[0] {
		case "active":
			hotspotActive = f[1] == "yes"
		case "clients":
			clients, _ = strconv.Atoi(f[1])
		}
	}

	settings := Settings{Fallback: "parallel", Hotspot: true}
	if body := strings.TrimSpace(secs["settings"]); body != "" {
		if err := json.Unmarshal([]byte(body), &settings); err != nil {
			return Status{}, errors.New("network status: ##settings: " + err.Error())
		}
	}

	var sw *SwitchProgress
	if body := strings.TrimSpace(secs["switch"]); body != "" && body != "{}" {
		sw = &SwitchProgress{}
		if err := json.Unmarshal([]byte(body), sw); err != nil {
			return Status{}, errors.New("network status: ##switch: " + err.Error())
		}
	}

	networks := parseNetworks(secs["wifi"], parseProfiles(secs["profiles"]))
	var current *Network
	for i := range networks {
		if networks[i].Current {
			current = &networks[i]
			break
		}
	}

	lan := Link{ID: "lan", Device: lanDev, Address: addrOf(addrs, lanDev)}
	if managed {
		lan.State = "down"
		if devStates[lanDev] == "connected" {
			lan.State = "up"
		}
	} else if lan.Address != "" {
		lan.State = "up"
	} else {
		lan.State = "down"
	}
	if sp, ok := speeds[lanDev]; ok {
		lan.SpeedMbps = sp
	}

	wifi := Link{ID: "wifi", Device: wifiDev, Address: addrOf(addrs, wifiDev)}
	wifi.State = wifiState(wifiDev, rfkillBlocked, managed, hotspotActive, devStates[wifiDev], wifi.Address, sw)
	if current != nil {
		wifi.SSID = current.SSID
		wifi.Band = current.Band
		wifi.Channel = current.Channel
		dbm := current.SignalDBm
		wifi.SignalDBm = &dbm
	}
	if sw != nil && sw.Result == "fail" {
		wifi.LastError = sw.Error
		wifi.LastErrorSSID = sw.SSID
		wifi.LastErrorAt = sw.FinishedAt
	}

	routeDev := applyRoutes(routes, []*Link{&lan, &wifi})
	if routeDev != "" {
		for _, l := range []*Link{&lan, &wifi} {
			if l.Device == routeDev {
				l.Route = true
				l.DNS = dns
			}
		}
	}

	return Status{
		Managed:  managed,
		HasWifi:  hasWifi,
		Country:  country,
		Links:    []Link{lan, wifi},
		Networks: networks,
		Settings: settings,
		Hotspot: HotspotState{
			Available: managed && hasWifi,
			Active:    hotspotActive,
			Clients:   clients,
		},
		Switch: sw,
	}, nil
}

// wifiState applies the WLAN state rules. Without NetworkManager only the
// address decides between up and off.
func wifiState(dev string, rfkillBlocked, managed, hotspot bool, nmState, address string, sw *SwitchProgress) string {
	switch {
	case dev == "":
		return "none"
	case rfkillBlocked:
		return "blocked"
	case !managed:
		if address != "" {
			return "up"
		}
		return "off"
	case hotspot:
		return "hotspot"
	case nmState == "connected":
		return "up"
	case sw != nil && sw.Result == "fail":
		return "error"
	default:
		return "off"
	}
}

// applyRoutes sets the Gateway of each link from the default route with the
// lowest metric on its device and returns the device carrying that route.
func applyRoutes(routes []route, links []*Link) string {
	sort.SliceStable(routes, func(i, j int) bool { return routes[i].Metric < routes[j].Metric })
	for _, r := range routes {
		for _, l := range links {
			if l.Device == r.Dev && l.Gateway == "" {
				l.Gateway = r.Gateway
			}
		}
	}
	if len(routes) == 0 {
		return ""
	}
	return routes[0].Dev
}

func parseNetworks(body string, profiles map[string]string) []Network {
	best := map[string]Network{}
	order := []string{}
	for _, l := range lines(body) {
		f := splitTerse(l)
		if len(f) < 6 || f[1] == "" {
			continue
		}
		pct, _ := strconv.Atoi(f[2])
		freq := 0
		if fs := strings.Fields(f[3]); len(fs) > 0 {
			freq, _ = strconv.Atoi(fs[0]) // nmcli prints "2437 MHz"
		}
		chn, _ := strconv.Atoi(f[4])
		n := Network{
			SSID:      f[1],
			Band:      bandOf(freq),
			Channel:   chn,
			Security:  securityOf(f[5]),
			SignalDBm: signalPercentToDBm(pct),
		}
		n.Level = level(n.SignalDBm)
		current := strings.TrimSpace(f[0]) == "*"
		prev, seen := best[n.SSID]
		if !seen {
			order = append(order, n.SSID)
			n.Current = current
			best[n.SSID] = n
			continue
		}
		// Duplicate SSID: keep the strongest entry, the current flag holds for either.
		if n.SignalDBm > prev.SignalDBm {
			n.Current = prev.Current || current
			best[n.SSID] = n
		} else {
			prev.Current = prev.Current || current
			best[n.SSID] = prev
		}
	}

	out := make([]Network, 0, len(order))
	for _, ssid := range order {
		n := best[ssid]
		if uuid, ok := profiles[ssid]; ok {
			n.Saved = true
			n.ProfileUUID = uuid
		}
		out = append(out, n)
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Current != b.Current {
			return a.Current
		}
		if a.Saved != b.Saved {
			return a.Saved
		}
		return a.SignalDBm > b.SignalDBm
	})
	return out
}

// parseProfiles maps the SSID of each WLAN profile to its UUID.
func parseProfiles(body string) map[string]string {
	out := map[string]string{}
	for _, l := range lines(body) {
		parts := strings.SplitN(l, "\t", 3)
		if len(parts) != 3 {
			continue
		}
		ssid := strings.Join(splitTerse(parts[2]), ":")
		if ssid != "" {
			out[ssid] = parts[0]
		}
	}
	return out
}

type addrEntry struct {
	Ifname string `json:"ifname"`
	Addrs  []struct {
		Family string `json:"family"`
		Local  string `json:"local"`
	} `json:"addr_info"`
}

// parseAddrs returns the first IPv4 address per interface.
func parseAddrs(body string) []addrEntry {
	var entries []addrEntry
	if strings.TrimSpace(body) == "" {
		return nil
	}
	_ = json.Unmarshal([]byte(body), &entries)
	return entries
}

func addrOf(entries []addrEntry, dev string) string {
	if dev == "" {
		return ""
	}
	for _, e := range entries {
		if e.Ifname != dev {
			continue
		}
		for _, a := range e.Addrs {
			if a.Family == "inet" && a.Local != "" {
				return a.Local
			}
		}
	}
	return ""
}

// firstAddrDevice returns the first interface with an IPv4 address that is
// neither loopback nor the WLAN device. It stands in for the LAN device when
// NetworkManager is not available.
func firstAddrDevice(entries []addrEntry, wifiDev string) string {
	for _, e := range entries {
		if e.Ifname == "" || e.Ifname == "lo" || e.Ifname == wifiDev {
			continue
		}
		if addrOf(entries, e.Ifname) != "" {
			return e.Ifname
		}
	}
	return ""
}

type route struct {
	Dev     string `json:"dev"`
	Gateway string `json:"gateway"`
	Metric  int    `json:"metric"`
}

func parseRoutes(body string) []route {
	var routes []route
	if strings.TrimSpace(body) == "" {
		return nil
	}
	_ = json.Unmarshal([]byte(body), &routes)
	return routes
}

// parseSpeeds reads "DEV SPEED" lines from the speed section.
func parseSpeeds(body string) map[string]int {
	out := map[string]int{}
	for _, l := range lines(body) {
		f := strings.Fields(l)
		if len(f) != 2 {
			continue
		}
		if sp, err := strconv.Atoi(f[1]); err == nil {
			out[f[0]] = sp
		}
	}
	return out
}

// splitSections cuts the helper output at its ## markers.
func splitSections(raw string) map[string]string {
	secs := map[string]string{}
	name := ""
	var buf []string
	flush := func() {
		if name != "" {
			secs[name] = strings.Join(buf, "\n")
		}
	}
	for _, l := range strings.Split(raw, "\n") {
		if strings.HasPrefix(l, "##") {
			flush()
			name = strings.TrimPrefix(l, "##")
			buf = nil
			continue
		}
		buf = append(buf, l)
	}
	flush()
	return secs
}

// lines returns the non-empty lines of a section.
func lines(body string) []string {
	var out []string
	for _, l := range strings.Split(body, "\n") {
		if strings.TrimSpace(l) != "" {
			out = append(out, l)
		}
	}
	return out
}

func firstLine(body string) string {
	ls := lines(body)
	if len(ls) == 0 {
		return ""
	}
	return strings.TrimSpace(ls[0])
}

// splitTerse splits one nmcli terse line at unescaped colons. \: and \\ are
// unescaped in the fields.
func splitTerse(line string) []string {
	var fields []string
	var cur strings.Builder
	for i := 0; i < len(line); i++ {
		c := line[i]
		switch {
		case c == '\\' && i+1 < len(line) && (line[i+1] == ':' || line[i+1] == '\\'):
			cur.WriteByte(line[i+1])
			i++
		case c == ':':
			fields = append(fields, cur.String())
			cur.Reset()
		default:
			cur.WriteByte(c)
		}
	}
	return append(fields, cur.String())
}

func bandOf(freqMHz int) string {
	switch {
	case freqMHz <= 0:
		return ""
	case freqMHz < 3000:
		return "2.4"
	case freqMHz < 5925:
		return "5"
	default:
		return "6"
	}
}

// securityOf maps the nmcli SECURITY column to the values the UI shows.
func securityOf(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "--" {
		return ""
	}
	has2 := strings.Contains(s, "WPA2")
	has3 := strings.Contains(s, "WPA3")
	switch {
	case has2 && has3:
		return "WPA2/WPA3"
	case has3:
		return "WPA3"
	case has2:
		return "WPA2"
	default:
		return s
	}
}

// signalPercentToDBm converts the nmcli signal percentage to dBm.
func signalPercentToDBm(pct int) int {
	return pct/2 - 100
}

// level maps dBm to the 1..4 bars of the UI.
func level(dbm int) int {
	switch {
	case dbm >= -55:
		return 4
	case dbm >= -65:
		return 3
	case dbm >= -75:
		return 2
	default:
		return 1
	}
}
