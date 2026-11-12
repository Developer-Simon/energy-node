//go:build linux

package media

import (
	"bufio"
	"bytes"
	"context"
	"os/exec"
	"strings"
	"time"
)

// nmcliOutput fuehrt nmcli aus. Tests ersetzen es.
var nmcliOutput = func(ctx context.Context, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, "nmcli", args...).Output()
}

// CheckWifiKey vergleicht ssid und password mit den WLAN-Profilen des
// NetworkManager. Das gespeicherte Passwort verlaesst diese Funktion nie.
func CheckWifiKey(parent context.Context, ssid, password string) string {
	if ssid == "" || password == "" {
		return WifiKeyUnknown
	}
	ctx, cancel := context.WithTimeout(parent, 3*time.Second)
	defer cancel()
	out, err := nmcliOutput(ctx, "-t", "-f", "NAME,TYPE", "connection", "show")
	if err != nil {
		return WifiKeyUnknown
	}
	var stored []storedWifi
	for _, name := range wifiConnectionNames(out) {
		ssidOut, err := nmcliOutput(ctx, "-g", "802-11-wireless.ssid", "connection", "show", "id", name)
		if err != nil || unescapeTerse(strings.TrimRight(string(ssidOut), "\r\n")) != ssid {
			continue
		}
		pskOut, err := nmcliOutput(ctx, "-s", "-g", "802-11-wireless-security.psk", "connection", "show", "id", name)
		if err != nil {
			continue
		}
		stored = append(stored, storedWifi{SSID: ssid, PSK: unescapeTerse(strings.TrimRight(string(pskOut), "\r\n"))})
	}
	return matchWifiKey(stored, ssid, password)
}

// wifiConnectionNames liest die Namen der WLAN-Profile aus
// `nmcli -t -f NAME,TYPE connection show`. TYPE enthaelt keinen Doppelpunkt,
// darum trennt der letzte Doppelpunkt.
func wifiConnectionNames(data []byte) []string {
	var names []string
	sc := bufio.NewScanner(bytes.NewReader(data))
	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), "\r")
		i := strings.LastIndex(line, ":")
		if i < 0 || line[i+1:] != "802-11-wireless" {
			continue
		}
		names = append(names, unescapeTerse(line[:i]))
	}
	return names
}
