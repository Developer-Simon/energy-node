package hostapitest

import (
	"context"
	"regexp"

	"github.com/Developer-Simon/energy-node-webui/hostapi"
)

// mediaHostname ist die Form, die ein Rechnername haben muss.
var mediaHostname = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)

// mediaPiStepIDs ist die Reihenfolge der Schritte eines Pi-Schreibauftrags.
var mediaPiStepIDs = []string{"download", "checksum", "rights", "write", "verify", "customize", "eject"}

// mediaDebianStepIDs ist die Reihenfolge eines Debian-Auftrags. Statt des
// Anpassens wird das ISO gepatcht.
var mediaDebianStepIDs = []string{"download", "checksum", "patch", "rights", "write", "verify", "eject"}

// mediaStepIDsFor liefert die Schrittfolge fuer ein Debian- oder Pi-Geraet.
func mediaStepIDsFor(debian bool) []string {
	if debian {
		return mediaDebianStepIDs
	}
	return mediaPiStepIDs
}

// mediaSteps markiert die Schritte vor active als erledigt. active gleich der
// Anzahl der Schritte heisst: alle erledigt.
func mediaSteps(ids []string, active int) []hostapi.MediaStep {
	steps := make([]hostapi.MediaStep, len(ids))
	for i, id := range ids {
		state := "pending"
		switch {
		case i < active:
			state = "done"
		case i == active:
			state = "active"
		}
		steps[i] = hostapi.MediaStep{ID: id, State: state}
	}
	return steps
}

// mediaStepIndex ist die Position eines Schritts in ids.
func mediaStepIndex(ids []string, id string) int {
	for i, v := range ids {
		if v == id {
			return i
		}
	}
	return -1
}

// mediaSequence ist der feste Verlauf nach MediaWrite: Download aktiv 40 %,
// Schreiben aktiv 62 %, Pruefen 100 %, danach fertig. ids gibt die Schritte
// vor; die Aktivstellen folgen ihren Namen.
func mediaSequence(ids []string) []hostapi.MediaJob {
	write, verify := mediaStepIndex(ids, "write"), mediaStepIndex(ids, "verify")
	return []hostapi.MediaJob{
		{
			State: "running", Steps: mediaSteps(ids, 0), Percent: 40,
			BytesDone: 200_000_000, BytesTotal: 500_000_000, Rate: 12_000_000,
			Log: []string{"Image wird geladen"},
		},
		{
			State: "running", Steps: mediaSteps(ids, write), Percent: 62,
			BytesDone: 1_600_000_000, BytesTotal: 2_700_000_000, Rate: 18_000_000,
			Log: []string{"Image wird geschrieben"},
		},
		{
			State: "running", Steps: mediaSteps(ids, verify), Percent: 100,
			Log: []string{"Geschriebene Daten werden geprueft"},
		},
		{
			State: "done", Steps: mediaSteps(ids, len(ids)), Percent: 100,
			Log: []string{"Fertig"},
			Target: &hostapi.MediaTarget{
				Host: "energy-node.local", User: "node", KeyPath: "/home/dev/.energy-node/id_ed25519",
			},
		},
	}
}

// mediaStepIDs liefert die Schrittfolge des letzten Auftrags. Ein Geraet der
// Gruppe debian bekommt den patch-Schritt und keinen customize-Schritt.
func (f *FakeBackend) mediaStepIDs() []string {
	for _, d := range f.MediaCatalogView.Devices {
		if d.ID == f.LastMediaWrite.Device {
			return mediaStepIDsFor(d.Group == "debian")
		}
	}
	return mediaStepIDsFor(false)
}

func mediaStatusFixture() hostapi.MediaStatus {
	return hostapi.MediaStatus{
		Supported: true,
		Platform:  "linux",
		Imager:    hostapi.MediaImagerStatus{Found: true, Kind: "flatpak", Version: "2.0.12"},
		Links: []hostapi.MediaLink{
			{Kind: "discover", URL: "appstream://org.raspberrypi.rpi-imager"},
			{Kind: "website", URL: "https://www.raspberrypi.com/software/"},
		},
	}
}

// mediaCatalogFixture hat die fuenf Pi-Geraete und die drei Debian-Geraete aus
// der Geraeteliste. Die Pi-Images stammen aus testdata/os_list.json.
func mediaCatalogFixture() hostapi.MediaCatalog {
	lite64 := func() *hostapi.MediaImage {
		return &hostapi.MediaImage{
			Name: "Raspberry Pi OS Lite (64-bit)", Arch: "arm64",
			DownloadSize: 500000000, ExtractSize: 2700000000, ReleaseDate: "2026-10-06",
		}
	}
	lite32 := func() *hostapi.MediaImage {
		return &hostapi.MediaImage{
			Name: "Raspberry Pi OS Lite (32-bit)", Arch: "armv6",
			DownloadSize: 480000000, ExtractSize: 2600000000, ReleaseDate: "2026-10-06",
		}
	}
	legacy := func() *hostapi.MediaImage {
		return &hostapi.MediaImage{
			Name: "Raspberry Pi OS (Legacy, 32-bit) Lite", Arch: "armv6",
			DownloadSize: 520503180, ExtractSize: 2524971008, ReleaseDate: "2026-10-06",
		}
	}
	debianAmd64 := func() *hostapi.MediaImage {
		return &hostapi.MediaImage{
			Name: "debian-13.7.0-amd64-netinst.iso", Arch: "amd64",
			DownloadSize: 750000000, ExtractSize: 750000000, ReleaseDate: "2026-11-01",
		}
	}
	debianArm64 := func() *hostapi.MediaImage {
		return &hostapi.MediaImage{
			Name: "debian-13.7.0-arm64-netinst.iso", Arch: "arm64",
			DownloadSize: 700000000, ExtractSize: 700000000, ReleaseDate: "2026-11-01",
		}
	}
	return hostapi.MediaCatalog{
		Devices: []hostapi.MediaDevice{
			{ID: "pi5", Group: "pi", Experimental: true, Image: lite64()},
			{ID: "pi4", Group: "pi", Experimental: true, Image: lite64()},
			{ID: "pi3", Group: "pi", Experimental: false, Image: lite64()},
			{ID: "pi2", Group: "pi", Experimental: true, Image: lite32()},
			{ID: "pi1", Group: "pi", Experimental: false, Image: legacy()},
			{ID: "deb-amd64", Group: "debian", Experimental: false, Image: debianAmd64()},
			{ID: "deb-arm64", Group: "debian", Experimental: true, Image: debianArm64()},
			{ID: "deb-i386", Group: "debian", Experimental: false, Unsupported: true},
		},
		Defaults: hostapi.MediaDefaults{
			Hostname:    "energy-node",
			User:        "node",
			Timezone:    "Europe/Berlin",
			Keyboard:    "de",
			WifiCountry: "DE",
			WifiSSID:    "Werkstatt",
			WifiFreqMHz: 2437,
			WifiNetworks: []hostapi.MediaWifiNetwork{
				{SSID: "Werkstatt", Signal: 82, Band24: true, Band5: true},
				{SSID: "Keller 5G", Signal: 55, Band5: true},
				{SSID: "Nachbar", Signal: 40, Band24: true},
			},
			ZoneCountries: map[string]string{
				"Europe/Berlin": "DE", "Europe/Vienna": "AT", "Europe/Zurich": "CH",
			},
			Timezones: []string{"Europe/Berlin", "Europe/Vienna", "Europe/Zurich", "UTC"},
			Keyboards: []string{"de", "us", "gb", "fr", "ch"},
		},
	}
}

// mediaDriveFixture: ein Wechselmedium und eine interne SSD.
func mediaDriveFixture() []hostapi.MediaDrive {
	return []hostapi.MediaDrive{
		{
			Path: "/dev/sdb", Model: "SanDisk 32 GB", Size: 32000000000, Transport: "usb", Removable: true,
			Partitions: []hostapi.MediaPartition{
				{Name: "/dev/sdb1", Label: "bootfs", FSType: "vfat", Used: 60000000},
				{Name: "/dev/sdb2", Label: "rootfs", FSType: "ext4", Used: 2500000000},
			},
		},
		{
			Path: "/dev/sda", Model: "Samsung SSD 870", Size: 500107862016, Transport: "sata", Removable: false,
			Partitions: []hostapi.MediaPartition{
				{Name: "/dev/sda1", Label: "Windows", FSType: "ntfs", Used: 120000000000},
			},
		},
	}
}

// MediaStatus liefert die Vorgaben der Attrappe.
func (f *FakeBackend) MediaStatus(ctx context.Context) (hostapi.MediaStatus, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.MediaStatusView, nil
}

// MediaCatalog liefert die Geraeteliste der Attrappe.
func (f *FakeBackend) MediaCatalog(ctx context.Context) (hostapi.MediaCatalog, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.MediaCatalogView, nil
}

// MediaDrives liefert ohne internal nur Wechselmedien.
func (f *FakeBackend) MediaDrives(ctx context.Context, internal bool) ([]hostapi.MediaDrive, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	drives := []hostapi.MediaDrive{}
	for _, d := range f.MediaDriveList {
		if internal || d.Removable {
			drives = append(drives, d)
		}
	}
	return drives, nil
}

// MediaCheckName prueft die Form des Namens und ob er belegt ist.
func (f *FakeBackend) MediaCheckName(ctx context.Context, name string) (hostapi.MediaNameCheck, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	check := hostapi.MediaNameCheck{Valid: mediaHostname.MatchString(name), Taken: f.MediaTaken[name]}
	if check.Taken {
		check.Suggestion = name + "-2"
	}
	return check, nil
}

// mediaWifiPassword ist das Passwort, das der Fake fuer "Werkstatt" kennt.
const mediaWifiPassword = "werkstatt-wlan"

// MediaCheckWifi kennt nur das Netz "Werkstatt" mit mediaWifiPassword. Fuer
// "Werkstatt 5G" vergleicht es mit "Werkstatt", wie der echte Host mit dem
// Gegenstueck auf dem anderen Band.
func (f *FakeBackend) MediaCheckWifi(ctx context.Context, req hostapi.MediaWifiCheck) (hostapi.MediaWifiCheckResult, error) {
	ssid, via := req.SSID, ""
	if ssid == "Werkstatt 5G" {
		ssid, via = "Werkstatt", "Werkstatt"
	}
	switch {
	case ssid != "Werkstatt" || req.Password == "":
		return hostapi.MediaWifiCheckResult{Result: "unknown"}, nil
	case req.Password == mediaWifiPassword:
		return hostapi.MediaWifiCheckResult{Result: "match", Via: via}, nil
	default:
		return hostapi.MediaWifiCheckResult{Result: "mismatch", Via: via}, nil
	}
}

// MediaWifiNetworks liefert die Netze des Katalogs und dazu eines, das erst
// der neue Suchlauf findet.
func (f *FakeBackend) MediaWifiNetworks(ctx context.Context) ([]hostapi.MediaWifiNetwork, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	nets := append([]hostapi.MediaWifiNetwork{}, f.MediaCatalogView.Defaults.WifiNetworks...)
	return append(nets, hostapi.MediaWifiNetwork{SSID: "Garten", Signal: 20, Band24: true}), nil
}

// MediaWrite nimmt den Auftrag an und startet den festen Verlauf. Ein
// Fehler aus MediaWriteErr lehnt ihn ab, ohne etwas zu starten.
func (f *FakeBackend) MediaWrite(ctx context.Context, req hostapi.MediaWriteRequest) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.MediaWriteErr != nil {
		return f.MediaWriteErr
	}
	f.LastMediaWrite = req
	f.mediaWriting = true
	f.mediaCancelled = false
	f.mediaPos = 0
	return nil
}

// MediaJob liefert den naechsten Zustand des Verlaufs. Nach dem letzten
// bleibt er stehen. Ohne Auftrag ist der Zustand idle.
func (f *FakeBackend) MediaJob(ctx context.Context) (hostapi.MediaJob, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.mediaWriting {
		return hostapi.MediaJob{State: "idle", Steps: []hostapi.MediaStep{}, Log: []string{}}, nil
	}
	ids := f.mediaStepIDs()
	if f.mediaCancelled {
		return hostapi.MediaJob{
			State: "cancelled",
			Steps: mediaSteps(ids, 0),
			Log:   []string{},
			Error: &hostapi.ErrorView{Code: hostapi.MediaErrCancelled},
		}, nil
	}
	seq := mediaSequence(ids)
	job := seq[f.mediaPos]
	if f.mediaPos < len(seq)-1 {
		f.mediaPos++
	}
	return job, nil
}

// MediaCancel bricht einen laufenden Auftrag ab. Ohne Auftrag geschieht
// nichts.
func (f *FakeBackend) MediaCancel(ctx context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.mediaWriting {
		f.mediaCancelled = true
	}
	return nil
}

// MediaReachable meldet den Node als erreichbar mit passendem Schluessel.
func (f *FakeBackend) MediaReachable(ctx context.Context, host string) (hostapi.MediaReachability, error) {
	return hostapi.MediaReachability{Reachable: true, KeyMatches: true}, nil
}
