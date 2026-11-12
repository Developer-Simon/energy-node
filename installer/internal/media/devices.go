// Package media holds the device table for the installer media mode.
package media

// Device describes one Raspberry Pi model the installer can prepare media for.
type Device struct {
	ID           string // "pi5", "pi4", "pi3", "pi2", "pi1"
	Group        string // "pi"
	ImageName    string // exakter "name" in der OS-Liste
	Tag          string // Gerätekennung der OS-Liste, z. B. "pi4-64bit"
	Arch         string // Installationspaket: "arm64" | "armv6"
	Experimental bool
}

var devices = []Device{
	{ID: "pi5", Group: "pi", ImageName: "Raspberry Pi OS Lite (64-bit)", Tag: "pi5-64bit", Arch: "arm64", Experimental: true},
	{ID: "pi4", Group: "pi", ImageName: "Raspberry Pi OS Lite (64-bit)", Tag: "pi4-64bit", Arch: "arm64", Experimental: true},
	{ID: "pi3", Group: "pi", ImageName: "Raspberry Pi OS Lite (64-bit)", Tag: "pi3-64bit", Arch: "arm64", Experimental: false},
	{ID: "pi2", Group: "pi", ImageName: "Raspberry Pi OS Lite (32-bit)", Tag: "pi2-32bit", Arch: "armv6", Experimental: true},
	{ID: "pi1", Group: "pi", ImageName: "Raspberry Pi OS (Legacy, 32-bit) Lite", Tag: "pi1-32bit", Arch: "armv6", Experimental: false},
}

// Devices returns the device table in display order.
func Devices() []Device {
	out := make([]Device, len(devices))
	copy(out, devices)
	return out
}

// DeviceByID looks up a device by its ID.
func DeviceByID(id string) (Device, bool) {
	for _, d := range devices {
		if d.ID == id {
			return d, true
		}
	}
	return Device{}, false
}
