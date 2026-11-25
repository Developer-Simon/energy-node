//go:build windows

package media

import (
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows/registry"
)

// localeNameMaxLength entspricht LOCALE_NAME_MAX_LENGTH der Windows-API.
const localeNameMaxLength = 85

var procGetUserDefaultLocaleName = syscall.NewLazyDLL("kernel32.dll").NewProc("GetUserDefaultLocaleName")

// HostDefaults reads timezone, keyboard and country from Windows. Missing values
// fall back to UTC, us and an empty country.
func HostDefaults() Defaults {
	d := Defaults{Timezone: "UTC", Keyboard: "us"}
	zone := windowsZoneToIANA(registryString(registry.LOCAL_MACHINE, `SYSTEM\CurrentControlSet\Control\TimeZoneInformation`, "TimeZoneKeyName"))
	if zone != "" {
		d.Timezone = zone
	}
	d.Timezones, d.ZoneCountries = embeddedZones()
	if kb := keyboardFromKLID(registryString(registry.CURRENT_USER, `Keyboard Layout\Preload`, "1")); kb != "" {
		d.Keyboard = kb
	}
	d.Keyboards = keyboardChoices(d.Keyboard)
	d.WifiCountry = countryFromLocale(userLocaleName())
	return d
}

// registryString returns a string value, or "" when the key or value cannot be read.
func registryString(root registry.Key, path, name string) string {
	key, err := registry.OpenKey(root, path, registry.QUERY_VALUE)
	if err != nil {
		return ""
	}
	defer key.Close()
	value, _, err := key.GetStringValue(name)
	if err != nil {
		return ""
	}
	return value
}

// userLocaleName returns the user's default locale name such as de-DE, or "".
func userLocaleName() string {
	buf := make([]uint16, localeNameMaxLength)
	n, _, _ := procGetUserDefaultLocaleName.Call(uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	if n == 0 {
		return ""
	}
	return syscall.UTF16ToString(buf)
}
