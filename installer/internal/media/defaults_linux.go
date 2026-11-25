//go:build linux

package media

import (
	"bufio"
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

const zone1970Path = "/usr/share/zoneinfo/zone1970.tab"

// maxWifiNetworks begrenzt die Netze in Reichweite.
const maxWifiNetworks = 10

// HostDefaults reads timezone, keyboard, and WLAN country from this computer.
func HostDefaults() Defaults {
	d := Defaults{Timezone: "UTC", Keyboard: "us"}
	if tz := localTimezone(); tz != "" {
		d.Timezone = tz
	}
	d.Timezones = timezoneList()
	if kb := localKeyboard(); kb != "" {
		d.Keyboard = kb
	}
	d.Keyboards = keyboardChoices(d.Keyboard)
	d.WifiCountry = countryFromLocale(localeFromEnv())
	d.WifiSSID, d.WifiFreqMHz, d.WifiNetworks = scanWifi(context.Background())
	d.ZoneCountries = zoneCountries()
	return d
}

// scanWifi returns the connected network, its frequency and the networks in
// range from the cached scan list. --rescan no answers at once; a rescan takes
// several seconds and runs past the timeout.
func scanWifi(parent context.Context) (string, int, []WifiNetwork) {
	return nmcliWifiList(parent, "no", 2*time.Second)
}

// WifiInRange scans again when the cached list is old (--rescan auto) and
// returns the networks in range. While connected, NetworkManager rarely scans
// on its own, so the cached list often holds only the own network.
func WifiInRange(parent context.Context) []WifiNetwork {
	_, _, nets := nmcliWifiList(parent, "auto", 15*time.Second)
	return nets
}

// nmcliWifiList runs `nmcli dev wifi list` with the given rescan mode.
func nmcliWifiList(parent context.Context, rescan string, timeout time.Duration) (string, int, []WifiNetwork) {
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "nmcli", "-t", "-f", "ACTIVE,SSID,FREQ,SIGNAL", "dev", "wifi", "list", "--rescan", rescan).Output()
	if err != nil {
		return "", 0, nil
	}
	return parseWifiList(out)
}

// parseWifiList reads `nmcli -t -f ACTIVE,SSID,FREQ,SIGNAL dev wifi list`, e.g.
// "yes:Werkstatt:5500 MHz:82". FREQ and SIGNAL never contain a colon, so the
// last two colons separate them from the SSID. Terse mode escapes ":" as "\:"
// and "\" as "\\" in the SSID. Hidden networks (empty SSID) are skipped, access
// points with the same name are merged.
func parseWifiList(data []byte) (string, int, []WifiNetwork) {
	var active string
	var activeFreq int
	byName := map[string]*WifiNetwork{}
	var order []*WifiNetwork
	sc := bufio.NewScanner(bytes.NewReader(data))
	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), "\r")
		activeField, rest, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		i := strings.LastIndex(rest, ":")
		if i < 0 {
			continue
		}
		signal, _ := strconv.Atoi(strings.TrimSpace(rest[i+1:]))
		rest = rest[:i]
		j := strings.LastIndex(rest, ":")
		if j < 0 {
			continue
		}
		freqField, _, _ := strings.Cut(strings.TrimSpace(rest[j+1:]), " ")
		freq, _ := strconv.Atoi(freqField)
		ssid := unescapeTerse(rest[:j])
		if ssid == "" {
			continue
		}
		if activeField == "yes" && active == "" {
			active, activeFreq = ssid, freq
		}
		n := byName[ssid]
		if n == nil {
			n = &WifiNetwork{SSID: ssid}
			byName[ssid] = n
			order = append(order, n)
		}
		if signal > n.Signal {
			n.Signal = signal
		}
		switch {
		case freq >= 5000:
			n.Band5 = true
		case freq > 0:
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
	nets := make([]WifiNetwork, len(order))
	for k, n := range order {
		nets[k] = *n
	}
	return active, activeFreq, nets
}

// zoneCountries maps each zone of zone1970.tab to its first country code.
func zoneCountries() map[string]string {
	data, err := os.ReadFile(zone1970Path)
	if err != nil {
		_, countries := embeddedZones()
		return countries
	}
	return parseZoneCountries(data)
}

// unescapeTerse reverses the backslash escaping of nmcli terse output.
func unescapeTerse(s string) string {
	var b strings.Builder
	escaped := false
	for _, r := range s {
		if !escaped && r == '\\' {
			escaped = true
			continue
		}
		escaped = false
		b.WriteRune(r)
	}
	return b.String()
}

// localTimezone returns the zone named by the /etc/localtime symlink, or "".
func localTimezone() string {
	target, err := os.Readlink("/etc/localtime")
	if err != nil {
		return ""
	}
	const marker = "zoneinfo/"
	i := strings.Index(target, marker)
	if i < 0 {
		return ""
	}
	return target[i+len(marker):]
}

// timezoneList returns the sorted zone names from zone1970.tab plus UTC, or the embedded table.
func timezoneList() []string {
	data, err := os.ReadFile(zone1970Path)
	if err != nil {
		zones, _ := embeddedZones()
		return zones
	}
	return zonesWithUTC(parseZoneTab(data))
}

// localKeyboard returns the X11 layout from localectl, falling back to the console keymap.
func localKeyboard() string {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if out, err := exec.CommandContext(ctx, "localectl", "status").Output(); err == nil {
		sc := bufio.NewScanner(bytes.NewReader(out))
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if rest, ok := strings.CutPrefix(line, "X11 Layout:"); ok {
				layout := strings.TrimSpace(strings.Split(strings.TrimSpace(rest), ",")[0])
				if layout != "" {
					return layout
				}
			}
		}
	}
	if data, err := os.ReadFile(filepath.Join("/etc", "vconsole.conf")); err == nil {
		sc := bufio.NewScanner(bytes.NewReader(data))
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if rest, ok := strings.CutPrefix(line, "KEYMAP="); ok {
				keymap := strings.TrimSpace(strings.SplitN(strings.TrimSpace(rest), "-", 2)[0])
				if keymap != "" {
					return keymap
				}
			}
		}
	}
	return ""
}

// localeFromEnv returns LC_ALL, or LANG when LC_ALL is unset.
func localeFromEnv() string {
	if v := os.Getenv("LC_ALL"); v != "" {
		return v
	}
	return os.Getenv("LANG")
}
