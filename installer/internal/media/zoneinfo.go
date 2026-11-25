package media

import (
	"bufio"
	"bytes"
	"embed"
	"sort"
	"strings"
)

// zoneinfoFS enthaelt die Zonentabelle (tzdata, gemeinfrei) und die Zuordnung der
// Windows-Zonen-IDs (CLDR, territory 001). Linux und Windows greifen darauf zurueck.
//
//go:embed zoneinfo/zone1970.tab zoneinfo/windowszones.txt
var zoneinfoFS embed.FS

// windowsZonesData returns the embedded Windows-to-IANA table.
func windowsZonesData() []byte {
	data, _ := zoneinfoFS.ReadFile("zoneinfo/windowszones.txt")
	return data
}

// embeddedZones returns the sorted zones of the embedded zone1970.tab plus UTC,
// and the zone -> country code map of the same table.
func embeddedZones() (zones []string, countries map[string]string) {
	data, _ := zoneinfoFS.ReadFile("zoneinfo/zone1970.tab")
	return zonesWithUTC(parseZoneTab(data)), parseZoneCountries(data)
}

// zonesWithUTC adds UTC to a sorted zone list when it is missing.
func zonesWithUTC(zones []string) []string {
	for _, z := range zones {
		if z == "UTC" {
			return zones
		}
	}
	zones = append(zones, "UTC")
	sort.Strings(zones)
	return zones
}

// windowsZoneToIANA maps a Windows time zone ID such as "W. Europe Standard Time"
// to its IANA zone. Etc/UTC and Etc/GMT both become "UTC", so the zone is part
// of the picker list. Unknown IDs return "".
func windowsZoneToIANA(id string) string {
	scanner := bufio.NewScanner(bytes.NewReader(windowsZonesData()))
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		winID, iana, ok := strings.Cut(line, "\t")
		if !ok || winID != id {
			continue
		}
		if iana == "Etc/UTC" || iana == "Etc/GMT" {
			return "UTC"
		}
		return iana
	}
	return ""
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

// parseZoneCountries returns zone -> first country code (first column, comma separated) of zone1970.tab.
func parseZoneCountries(data []byte) map[string]string {
	out := map[string]string{}
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
		code, _, _ := strings.Cut(strings.TrimSpace(fields[0]), ",")
		out[strings.TrimSpace(fields[2])] = code
	}
	return out
}
