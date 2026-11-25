package media

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// createNoWindow ist CREATE_NO_WINDOW: Kindprozesse bekommen kein sichtbares Konsolenfenster.
const createNoWindow = 0x08000000

func hideWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
}

// shimExe liefert den Pfad des eigenen Programms, das den Zwischenprozess startet.
var shimExe = os.Executable

// startImager startet den Imager ueber den Shim in einer Pseudokonsole, siehe conpty_windows.go.
// Der Imager haengt sich an die Konsole seines Elternprozesses, deshalb ist die Pipe allein zu wenig.
func startImager(log *os.File, name string, args []string) (imagerProcess, error) {
	exe, err := shimExe()
	if err != nil {
		return nil, fmt.Errorf("own executable: %w", err)
	}
	cmdLine, err := windows.UTF16PtrFromString(windows.ComposeCommandLine(append([]string{exe, ShimName, "--", name}, args...)))
	if err != nil {
		return nil, err
	}

	w := &conptyImager{log: log}
	ok := false
	defer func() {
		if !ok {
			if w.proc != 0 {
				// Gestartet, aber nicht zugeordnet oder nicht fortgesetzt: sofort beenden.
				_ = windows.TerminateProcess(w.proc, 1)
			}
			w.closeAll()
		}
	}()

	// Pipes: der Installer schreibt nach inW, die Konsole liest inR; die Konsole schreibt nach outW, wir lesen outR.
	if err := windows.CreatePipe(&w.inR, &w.inW, nil, 0); err != nil {
		return nil, fmt.Errorf("CreatePipe in: %w", err)
	}
	if err := windows.CreatePipe(&w.outR, &w.outW, nil, 0); err != nil {
		return nil, fmt.Errorf("CreatePipe out: %w", err)
	}
	if err := windows.CreatePseudoConsole(windows.Coord{X: 1000, Y: 30}, w.inR, w.outW, 0, &w.hpc); err != nil {
		return nil, fmt.Errorf("CreatePseudoConsole: %w", err)
	}
	// Die Konsole besitzt diese Enden jetzt.
	_ = windows.CloseHandle(w.inR)
	_ = windows.CloseHandle(w.outW)
	w.inR, w.outW = 0, 0

	attrs, err := windows.NewProcThreadAttributeList(1)
	if err != nil {
		return nil, fmt.Errorf("NewProcThreadAttributeList: %w", err)
	}
	defer attrs.Delete()

	// Der PSEUDOCONSOLE-Attributwert ist der HPCON-Wert selbst, nicht ein Zeiger darauf.
	hpcValue := *(*unsafe.Pointer)(unsafe.Pointer(&w.hpc))
	if err := attrs.Update(windows.PROC_THREAD_ATTRIBUTE_PSEUDOCONSOLE, hpcValue, unsafe.Sizeof(w.hpc)); err != nil {
		return nil, fmt.Errorf("UpdateProcThreadAttribute: %w", err)
	}

	si := new(windows.StartupInfoEx)
	si.Cb = uint32(unsafe.Sizeof(*si))
	si.ProcThreadAttributeList = attrs.List()

	w.job, err = newKillOnCloseJob()
	if err != nil {
		return nil, err
	}

	// Gestartet wird angehalten: kein Kindprozess entsteht, bevor der Prozess im Job liegt.
	var pi windows.ProcessInformation
	flags := uint32(windows.EXTENDED_STARTUPINFO_PRESENT | windows.CREATE_SUSPENDED)
	if err := windows.CreateProcess(nil, cmdLine, nil, nil, false, flags, nil, nil, &si.StartupInfo, &pi); err != nil {
		return nil, fmt.Errorf("CreateProcess: %w", err)
	}
	w.proc, w.thread = pi.Process, pi.Thread
	if err := windows.AssignProcessToJobObject(w.job, w.proc); err != nil {
		return nil, fmt.Errorf("AssignProcessToJobObject: %w", err)
	}
	if _, err := windows.ResumeThread(w.thread); err != nil {
		return nil, fmt.Errorf("ResumeThread: %w", err)
	}

	// Die Konsolenausgabe laeuft ins Log, bis die Konsole geschlossen wird.
	out := os.NewFile(uintptr(w.outR), "conpty-out")
	w.out = out
	w.outR = 0
	done := make(chan struct{})
	w.copyDone = done
	go func() {
		defer close(done)
		_, _ = io.Copy(log, out)
	}()

	ok = true
	return w, nil
}
