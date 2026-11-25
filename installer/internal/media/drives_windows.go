//go:build windows

package media

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
)

// ListDrives fragt Windows nach den Platten. Das Skript laeuft ohne Konsolenfenster
// und ohne erhoehte Rechte, weil Get-Disk und Get-Volume das fuer Lesezugriffe nicht brauchen.
func ListDrives(ctx context.Context) ([]Drive, error) {
	cmd := exec.CommandContext(ctx, "powershell.exe",
		"-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", windowsDiskScript)
	hideWindow(cmd)
	out, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return nil, fmt.Errorf("media: powershell disk list: %w: %s", err, bytes.TrimSpace(exitErr.Stderr))
		}
		return nil, fmt.Errorf("media: powershell disk list: %w", err)
	}
	return ParseWindowsDisks(out)
}
