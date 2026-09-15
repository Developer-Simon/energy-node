//go:build windows

package i18n

import (
	"os"
	"syscall"
	"unsafe"
)

// OSLocale liest unter Windows die Benutzer-Locale ueber
// GetUserDefaultLocaleName. Die Umgebungsvariablen der POSIX-Welt sind dort
// in aller Regel leer; ohne diesen Aufruf startete der Installer auf einem
// deutschen Windows auf Englisch. Reines Go ueber syscall - kein cgo, damit
// die Cross-Compile-Matrix aus E6 traegt.
func OSLocale() string {
	if value := os.Getenv("LANG"); value != "" {
		return value
	}
	const localeNameMaxLength = 85
	proc := syscall.NewLazyDLL("kernel32.dll").NewProc("GetUserDefaultLocaleName")
	buf := make([]uint16, localeNameMaxLength)
	n, _, _ := proc.Call(uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	if n <= 1 {
		return ""
	}
	return syscall.UTF16ToString(buf[:n-1])
}
