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
	"strings"
	"time"
)

const zone1970Path = "/usr/share/zoneinfo/zone1970.tab"

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
	return d
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

// timezoneList returns the sorted zone names from zone1970.tab plus UTC.
func timezoneList() []string {
	data, err := os.ReadFile(zone1970Path)
	if err != nil {
		return []string{"UTC"}
	}
	zones := parseZoneTab(data)
	for _, z := range zones {
		if z == "UTC" {
			return zones
		}
	}
	zones = append(zones, "UTC")
	sort.Strings(zones)
	return zones
}

// parseZoneTab returns the sorted zone names (third column) of a zone1970.tab file.
func parseZoneTab(data []byte) []string {
	var zones []string
	sc := bufio.NewScanner(bytes.NewReader(data))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) < 3 {
			continue
		}
		zones = append(zones, strings.TrimSpace(fields[2]))
	}
	sort.Strings(zones)
	return zones
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

// countryFromLocale returns the upper-case country of a locale such as de_DE.UTF-8, or "".
func countryFromLocale(locale string) string {
	if i := strings.IndexAny(locale, ".@"); i >= 0 {
		locale = locale[:i]
	}
	parts := strings.Split(locale, "_")
	if len(parts) != 2 {
		return ""
	}
	return strings.ToUpper(parts[1])
}
