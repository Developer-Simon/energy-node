//go:build windows

package media

import (
	"fmt"
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

// LocateWindowsImager finds the installed Raspberry Pi Imager. It reads the version from the
// file's version resource and never starts the imager, since that could trigger UAC.
func LocateWindowsImager() (Imager, bool) {
	return FindWindowsImager(WinEnv{
		Getenv:      os.Getenv,
		Exists:      fileExists,
		FileVersion: fileVersion,
	})
}

// fileVersion returns the four part file version of path, such as "2.0.12.0".
func fileVersion(path string) (string, error) {
	size, err := windows.GetFileVersionInfoSize(path, nil)
	if err != nil {
		return "", err
	}
	buf := make([]byte, size)
	if err := windows.GetFileVersionInfo(path, 0, size, unsafe.Pointer(&buf[0])); err != nil {
		return "", err
	}
	var block unsafe.Pointer
	var n uint32
	if err := windows.VerQueryValue(unsafe.Pointer(&buf[0]), `\`, unsafe.Pointer(&block), &n); err != nil {
		return "", err
	}
	info := (*windows.VS_FIXEDFILEINFO)(block)
	ms, ls := info.FileVersionMS, info.FileVersionLS
	return fmt.Sprintf("%d.%d.%d.%d", ms>>16, ms&0xffff, ls>>16, ls&0xffff), nil
}
