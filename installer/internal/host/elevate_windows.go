//go:build windows

package host

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"
	"unsafe"

	"github.com/Developer-Simon/energy-node-installer/internal/media"
	"golang.org/x/sys/windows"
)

var procShellExecuteExW = windows.NewLazySystemDLL("shell32.dll").NewProc("ShellExecuteExW")

// shellExecuteRunas startet exe erhoeht (UAC) ohne Fenster und liefert eine Wartefunktion.
func shellExecuteRunas(exe string, args []string) (func() (int, error), error) {
	quoted := make([]string, len(args))
	for i, a := range args {
		quoted[i] = windows.EscapeArg(a)
	}
	verb, _ := windows.UTF16PtrFromString("runas")
	file, _ := windows.UTF16PtrFromString(exe)
	params, err := windows.UTF16PtrFromString(strings.Join(quoted, " "))
	if err != nil {
		return nil, err
	}
	// Die Zeiger werden nur als uintptr gehalten, daher gepinnt.
	var pin runtime.Pinner
	defer pin.Unpin()
	pin.Pin(verb)
	pin.Pin(file)
	pin.Pin(params)
	info := &shellExecuteInfo{
		fMask:        seeMaskNoCloseProcess | seeMaskNoAsync | seeMaskFlagNoUI,
		lpVerb:       uintptr(unsafe.Pointer(verb)),
		lpFile:       uintptr(unsafe.Pointer(file)),
		lpParameters: uintptr(unsafe.Pointer(params)),
		nShow:        windows.SW_HIDE,
	}
	info.cbSize = uint32(unsafe.Sizeof(*info))
	r1, _, e1 := procShellExecuteExW.Call(uintptr(unsafe.Pointer(info)))
	runtime.KeepAlive(verb)
	runtime.KeepAlive(file)
	runtime.KeepAlive(params)
	if r1 == 0 {
		if errors.Is(e1, windows.ERROR_CANCELLED) {
			return nil, fmt.Errorf("uac: %w", media.ErrRightsDenied)
		}
		return nil, fmt.Errorf("ShellExecuteExW: %w", e1)
	}
	process := windows.Handle(info.hProcess)
	return func() (int, error) {
		defer windows.CloseHandle(process)
		if _, err := windows.WaitForSingleObject(process, windows.INFINITE); err != nil {
			return -1, err
		}
		var code uint32
		if err := windows.GetExitCodeProcess(process, &code); err != nil {
			return -1, err
		}
		return int(code), nil
	}, nil
}

// elevatedRunner startet den Imager ueber den erhoehten Helfer des Installers.
// Der Job-Ordner wird nach dem Warten geloescht, Windows kann offene Dateien nicht loeschen.
func elevatedRunner(ctx context.Context, name string, args ...string) (io.ReadCloser, func() (int, error), error) {
	dir, err := os.MkdirTemp("", "energy-node-media-")
	if err != nil {
		return nil, nil, err
	}
	exe, err := helperExe()
	if err != nil {
		_ = os.RemoveAll(dir)
		return nil, nil, fmt.Errorf("own executable: %w", err)
	}
	out, wait, err := runElevated(ctx, shellExecuteRunas, exe, dir, name, args)
	if err != nil {
		_ = os.RemoveAll(dir)
		return nil, nil, err
	}
	return out, func() (int, error) {
		defer os.RemoveAll(dir)
		defer out.Close()
		return wait()
	}, nil
}
