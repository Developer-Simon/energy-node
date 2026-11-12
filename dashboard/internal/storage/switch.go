package storage

import (
	"fmt"
	"os"
	"path/filepath"
)

// Switch points the runtime symlink at the location of target. The new link
// is created next to the old one and renamed over it, so the runtime path
// always resolves to a target. The fallback directories are created when
// needed. The mount is only checked, never created.
func Switch(p Paths, target Mode) error {
	dest, err := destination(p, target)
	if err != nil {
		return err
	}
	if target == ModeDrive {
		fi, err := os.Stat(dest)
		if err != nil || !fi.IsDir() {
			return fmt.Errorf("storage: drive runtime dir %s not available", dest)
		}
	} else if err := os.MkdirAll(dest, 0750); err != nil {
		return fmt.Errorf("storage: create %s: %w", dest, err)
	}

	if err := os.MkdirAll(filepath.Dir(p.Runtime), 0750); err != nil {
		return fmt.Errorf("storage: runtime dir: %w", err)
	}
	if fi, err := os.Lstat(p.Runtime); err == nil && fi.Mode()&os.ModeSymlink == 0 {
		return fmt.Errorf("storage: %s is not a symlink, refusing to replace it", p.Runtime)
	}

	tmp := p.Runtime + ".new"
	if err := os.RemoveAll(tmp); err != nil {
		return fmt.Errorf("storage: clear %s: %w", tmp, err)
	}
	if err := os.Symlink(dest, tmp); err != nil {
		return fmt.Errorf("storage: symlink: %w", err)
	}
	if err := os.Rename(tmp, p.Runtime); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("storage: switch runtime path: %w", err)
	}
	return nil
}

func destination(p Paths, target Mode) (string, error) {
	switch target {
	case ModeDrive:
		return DriveRuntimeDir(p), nil
	case ModeFallbackSystem:
		return p.SystemFallback, nil
	case ModeFallbackRAM:
		return p.RAMFallback, nil
	default:
		return "", fmt.Errorf("storage: cannot switch to mode %q", target)
	}
}

// DriveRuntimeDir is the directory on the drive that holds the runtime data.
// The mount root stays root-owned and carries only the marker, so the
// service user writes below runtime/.
func DriveRuntimeDir(p Paths) string {
	return filepath.Join(p.Mount, driveRuntimeName)
}

// driveRuntimeName is the subdirectory of the drive the helper creates for the runtime data.
const driveRuntimeName = "runtime"
