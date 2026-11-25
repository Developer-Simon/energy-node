package host

import (
	"context"
	"io"
	"os"

	"github.com/Developer-Simon/energy-node-installer/internal/media"
)

// helperExe liefert den Pfad des eigenen Programms, das den erhoehten Helfer startet (Testnaht).
var helperExe = os.Executable

// shellExecuteInfo entspricht SHELLEXECUTEINFOW auf 64 bit (112 Bytes).
type shellExecuteInfo struct {
	cbSize       uint32
	fMask        uint32
	hwnd         uintptr
	lpVerb       uintptr
	lpFile       uintptr
	lpParameters uintptr
	lpDirectory  uintptr
	nShow        int32
	hInstApp     uintptr
	lpIDList     uintptr
	lpClass      uintptr
	hkeyClass    uintptr
	dwHotKey     uint32
	hIconOrMon   uintptr
	hProcess     uintptr
}

const (
	seeMaskNoCloseProcess = 0x00000040
	seeMaskNoAsync        = 0x00000100
	seeMaskFlagNoUI       = 0x00000400
)

// launcher startet exe mit args erhoeht und liefert eine Wartefunktion fuer den Exit-Code.
type launcher func(exe string, args []string) (wait func() (int, error), err error)

// helperArgs baut die Argumente fuer den Helfer: Dateien, dann der Imager nach dem Trenner.
func helperArgs(f media.HelperFiles, name string, args []string) []string {
	out := []string{
		media.HelperName,
		"--log", f.Log,
		"--cancel", f.Cancel,
		"--status", f.Status,
		"--", name,
	}
	return append(out, args...)
}

// runElevated startet den Imager ueber launch und liefert dessen Log und Exit-Code.
// Wird ctx abgebrochen, schreibt es die Abbruchdatei, auf die der Helfer den Imager beendet.
func runElevated(ctx context.Context, launch launcher, exe, dir, name string, args []string) (io.ReadCloser, func() (int, error), error) {
	files, err := media.PrepareHelperFiles(dir)
	if err != nil {
		return nil, nil, err
	}
	wait, err := launch(exe, helperArgs(files, name, args))
	if err != nil {
		return nil, nil, err
	}
	if ctx.Err() != nil {
		writeCancelFile(files.Cancel)
	}

	var code int
	var waitErr error
	done := make(chan struct{})
	go func() {
		code, waitErr = wait()
		close(done)
	}()
	go func() {
		select {
		case <-ctx.Done():
			writeCancelFile(files.Cancel)
		case <-done:
		}
	}()

	return media.TailLog(files.Log, done), func() (int, error) {
		<-done
		return code, waitErr
	}, nil
}

// writeCancelFile legt die Abbruchdatei an. Fehler bleiben folgenlos, der Helfer endet ohnehin mit dem Imager.
func writeCancelFile(path string) {
	if f, err := os.Create(path); err == nil {
		_ = f.Close()
	}
}
