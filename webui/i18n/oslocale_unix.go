//go:build !windows

package i18n

import "os"

// OSLocale liest die Locale aus der Umgebung. Reihenfolge wie in POSIX:
// LC_ALL schlaegt LC_MESSAGES schlaegt LANG.
func OSLocale() string {
	for _, name := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		if value := os.Getenv(name); value != "" {
			return value
		}
	}
	return ""
}
