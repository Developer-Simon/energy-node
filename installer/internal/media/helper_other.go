//go:build !windows

package media

import (
	"os"
	"os/exec"
)

// hideWindow ist auf Nicht-Windows-Systemen ein No-op.
func hideWindow(cmd *exec.Cmd) {}

func startImager(log *os.File, name string, args []string) (imagerProcess, error) {
	return startExec(log, name, args)
}
