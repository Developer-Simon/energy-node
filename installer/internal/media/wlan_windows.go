//go:build windows

package media

import (
	"context"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// wlanapi.dll wird erst beim Aufruf geladen. Fehlt sie, bleiben alle Ergebnisse leer.
var (
	wlanapi                   = windows.NewLazySystemDLL("wlanapi.dll")
	procWlanOpenHandle        = wlanapi.NewProc("WlanOpenHandle")
	procWlanCloseHandle       = wlanapi.NewProc("WlanCloseHandle")
	procWlanEnumInterfaces    = wlanapi.NewProc("WlanEnumInterfaces")
	procWlanQueryInterface    = wlanapi.NewProc("WlanQueryInterface")
	procWlanScan              = wlanapi.NewProc("WlanScan")
	procWlanGetNetworkBssList = wlanapi.NewProc("WlanGetNetworkBssList")
	procWlanGetProfileList    = wlanapi.NewProc("WlanGetProfileList")
	procWlanGetProfile        = wlanapi.NewProc("WlanGetProfile")
	procWlanFreeMemory        = wlanapi.NewProc("WlanFreeMemory")
)

const (
	wlanClientVersion          = 2
	wlanOpcodeCurrentConn      = 7 // wlan_intf_opcode_current_connection
	wlanBssTypeAny             = 3 // dot11_BSS_type_any
	wlanProfileGetPlaintextKey = 4 // WLAN_PROFILE_GET_PLAINTEXT_KEY
	wlanListHeader             = 8 // Zaehler und Index bzw. Gesamtgroesse und Zaehler vor den Eintraegen
	wlanMaxSSIDLen             = 32
	wlanScanWait               = 4 * time.Second
	wlanCallTimeout            = 3 * time.Second  // HostDefaults und CheckWifiKey
	wlanRangeTimeout           = 15 * time.Second // WifiInRange
)

// wlanReading ist das Ergebnis einer Abfrage der WLAN-Adapter.
type wlanReading struct {
	active string
	freq   int
	nets   []WifiNetwork
}

// wlanKeyResult ist das Ergebnis von CheckWifiKey.
type wlanKeyResult struct {
	result, via string
}

// CheckWifiKey vergleicht ssid und password mit den gespeicherten WLAN-Profilen
// dieses Rechners. Ohne Rechte, ohne Profile oder bei einem Fehler kommt
// WifiKeyUnknown heraus.
func CheckWifiKey(ctx context.Context, ssid, password string) (result, via string) {
	if ssid == "" || password == "" {
		return WifiKeyUnknown, ""
	}
	ctx, cancel := context.WithTimeout(ctx, wlanCallTimeout)
	defer cancel()
	res, ok := wlanRun(ctx, func() wlanKeyResult {
		r, v := checkWlanKey(ssid, password)
		return wlanKeyResult{r, v}
	})
	if !ok {
		return WifiKeyUnknown, ""
	}
	return res.result, res.via
}

// WifiInRange scannt die WLAN-Adapter und liefert die Netze in Reichweite.
// Ohne Ortungsfreigabe oder Adapter kommt eine leere Liste heraus.
func WifiInRange(ctx context.Context) []WifiNetwork {
	ctx, cancel := context.WithTimeout(ctx, wlanRangeTimeout)
	defer cancel()
	_, _, nets := windowsWifi(ctx, true)
	return nets
}

// windowsWifi liefert das aktive Netz, seine Frequenz und die Netze in Reichweite.
// Mit scan wird vorher ein Scan angestossen und kurz gewartet.
func windowsWifi(ctx context.Context, scan bool) (active string, freq int, nets []WifiNetwork) {
	r, ok := wlanRun(ctx, func() wlanReading { return readWlan(ctx, scan) })
	if !ok {
		return "", 0, nil
	}
	return r.active, r.freq, r.nets
}

// wlanRun fuehrt work in einer eigenen Goroutine aus, die ihre Handles selbst
// schliesst. Endet ctx vorher, kommt ok false zurueck. Die Goroutine schreibt
// dann in einen gepufferten Kanal und blockiert nicht.
func wlanRun[T any](ctx context.Context, work func() T) (T, bool) {
	done := make(chan T, 1)
	go func() { done <- work() }()
	var zero T
	select {
	case v := <-done:
		return v, true
	case <-ctx.Done():
		return zero, false
	}
}

// wlanReady laedt wlanapi.dll und alle benoetigten Funktionen.
func wlanReady() bool {
	for _, p := range []*windows.LazyProc{
		procWlanOpenHandle, procWlanCloseHandle, procWlanEnumInterfaces,
		procWlanQueryInterface, procWlanScan, procWlanGetNetworkBssList,
		procWlanGetProfileList, procWlanGetProfile, procWlanFreeMemory,
	} {
		if p.Find() != nil {
			return false
		}
	}
	return true
}

// wlanOpen oeffnet einen WLAN-Handle. Der Aufrufer schliesst ihn.
func wlanOpen() (uintptr, bool) {
	var negotiated uint32
	var h uintptr
	r, _, _ := procWlanOpenHandle.Call(wlanClientVersion, 0,
		uintptr(unsafe.Pointer(&negotiated)), uintptr(unsafe.Pointer(&h)))
	return h, r == 0
}

// readWlan liest Verbindung und Netze aller Adapter. Der Handle gehoert dieser Goroutine.
func readWlan(ctx context.Context, scan bool) wlanReading {
	if !wlanReady() {
		return wlanReading{}
	}
	h, ok := wlanOpen()
	if !ok {
		return wlanReading{}
	}
	defer procWlanCloseHandle.Call(h, 0)

	guids := wlanInterfaceGUIDs(h)
	if scan {
		for _, g := range guids {
			procWlanScan.Call(h, uintptr(unsafe.Pointer(&g)), 0, 0, 0)
		}
		timer := time.NewTimer(wlanScanWait)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return wlanReading{}
		case <-timer.C:
		}
	}

	var samples []wifiSample
	for _, g := range guids {
		conn, connected := wlanConnection(h, &g)
		for _, e := range wlanBssList(h, &g) {
			n := min(int(e.SSIDLen), wlanMaxSSIDLen)
			samples = append(samples, wifiSample{
				SSID:    string(e.SSID[:n]),
				FreqMHz: int(e.ChCenterFrequency / 1000),
				Signal:  int(e.LinkQuality),
				Active:  connected && e.BSSID == conn.BSSID,
			})
		}
	}
	active, freq, nets := mergeWifi(samples)
	return wlanReading{active: active, freq: freq, nets: nets}
}

// checkWlanKey sammelt die Profile, die zu ssid passen, und vergleicht das Passwort.
func checkWlanKey(ssid, password string) (result, via string) {
	if !wlanReady() {
		return WifiKeyUnknown, ""
	}
	h, ok := wlanOpen()
	if !ok {
		return WifiKeyUnknown, ""
	}
	defer procWlanCloseHandle.Call(h, 0)

	var stored []storedWifi
	for _, g := range wlanInterfaceGUIDs(h) {
		for _, p := range wlanProfiles(h, &g) {
			s, ok := wlanProfileData(h, &g, p)
			if !ok || (s.SSID != ssid && !twins(ssid, s.SSID)) {
				continue
			}
			stored = append(stored, s)
		}
	}
	return matchWifiKey(stored, ssid, password)
}

// wlanInterfaceGUIDs liest die GUIDs der WLAN-Adapter. Die Werte werden kopiert,
// bevor der Speicher der Liste frei geht.
func wlanInterfaceGUIDs(h uintptr) [][16]byte {
	var list unsafe.Pointer
	r, _, _ := procWlanEnumInterfaces.Call(h, 0, uintptr(unsafe.Pointer(&list)))
	if r != 0 || list == nil {
		return nil
	}
	defer procWlanFreeMemory.Call(uintptr(list))
	count := int(*(*uint32)(list))
	infos := unsafe.Slice((*wlanInterfaceInfo)(unsafe.Add(list, wlanListHeader)), count)
	guids := make([][16]byte, 0, count)
	for i := range infos {
		guids = append(guids, infos[i].GUID)
	}
	return guids
}

// wlanConnection liest die aktuelle Verbindung eines Adapters. ok ist false ohne
// Verbindung oder wenn Windows den Zugriff verweigert.
func wlanConnection(h uintptr, guid *[16]byte) (conn wlanConnectionHead, ok bool) {
	var size uint32
	var data unsafe.Pointer
	r, _, _ := procWlanQueryInterface.Call(h, uintptr(unsafe.Pointer(guid)), wlanOpcodeCurrentConn, 0,
		uintptr(unsafe.Pointer(&size)), uintptr(unsafe.Pointer(&data)), 0)
	if r != 0 || data == nil {
		return wlanConnectionHead{}, false
	}
	defer procWlanFreeMemory.Call(uintptr(data))
	if size < uint32(unsafe.Sizeof(wlanConnectionHead{})) {
		return wlanConnectionHead{}, false
	}
	return *(*wlanConnectionHead)(data), true
}

// wlanBssList liest die Netze in Reichweite eines Adapters. Die Eintraege werden kopiert.
func wlanBssList(h uintptr, guid *[16]byte) []wlanBssEntry {
	var list unsafe.Pointer
	r, _, _ := procWlanGetNetworkBssList.Call(h, uintptr(unsafe.Pointer(guid)), 0, wlanBssTypeAny, 0, 0,
		uintptr(unsafe.Pointer(&list)))
	if r != 0 || list == nil {
		return nil
	}
	defer procWlanFreeMemory.Call(uintptr(list))
	count := int(*(*uint32)(unsafe.Add(list, 4)))
	entries := unsafe.Slice((*wlanBssEntry)(unsafe.Add(list, wlanListHeader)), count)
	out := make([]wlanBssEntry, count)
	copy(out, entries)
	return out
}

// wlanProfiles liest die gespeicherten Profile eines Adapters. Die Eintraege werden kopiert.
func wlanProfiles(h uintptr, guid *[16]byte) []wlanProfileInfo {
	var list unsafe.Pointer
	r, _, _ := procWlanGetProfileList.Call(h, uintptr(unsafe.Pointer(guid)), 0, uintptr(unsafe.Pointer(&list)))
	if r != 0 || list == nil {
		return nil
	}
	defer procWlanFreeMemory.Call(uintptr(list))
	count := int(*(*uint32)(list))
	entries := unsafe.Slice((*wlanProfileInfo)(unsafe.Add(list, wlanListHeader)), count)
	out := make([]wlanProfileInfo, count)
	copy(out, entries)
	return out
}

// wlanProfileData holt ein Profil mit Klartext-Schluessel und liest es aus.
// Das XML liegt in Speicher der Windows-API und wird danach frei gegeben.
func wlanProfileData(h uintptr, guid *[16]byte, info wlanProfileInfo) (storedWifi, bool) {
	name := info.Name
	flags := uint32(wlanProfileGetPlaintextKey)
	var xml unsafe.Pointer
	r, _, _ := procWlanGetProfile.Call(h, uintptr(unsafe.Pointer(guid)), uintptr(unsafe.Pointer(&name[0])), 0,
		uintptr(unsafe.Pointer(&xml)), uintptr(unsafe.Pointer(&flags)), 0)
	if r != 0 || xml == nil {
		return storedWifi{}, false
	}
	defer procWlanFreeMemory.Call(uintptr(xml))
	return parseWlanProfile([]byte(windows.UTF16PtrToString((*uint16)(xml))))
}
