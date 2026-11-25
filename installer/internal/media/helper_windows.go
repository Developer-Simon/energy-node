package media

import (
	"os"
	"os/exec"
	"syscall"
)

// createNoWindow ist CREATE_NO_WINDOW: Kindprozesse bekommen kein sichtbares Konsolenfenster.
const createNoWindow = 0x08000000

func hideWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
}

// startImager ist voruebergehend ohne Pseudokonsole, siehe Teilauftrag 2b.
func startImager(log *os.File, name string, args []string) (imagerProcess, error) {
	return startExec(log, name, args)
}
