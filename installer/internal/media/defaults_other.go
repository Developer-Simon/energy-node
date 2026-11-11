//go:build !linux

package media

// HostDefaults returns neutral defaults on systems without the Linux host settings.
func HostDefaults() Defaults {
	return Defaults{
		Timezone:  "UTC",
		Keyboard:  "us",
		Timezones: []string{"UTC"},
		Keyboards: keyboardChoices("us"),
	}
}
