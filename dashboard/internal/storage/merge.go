package storage

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/Developer-Simon/energy-node-dashboard/internal/config"
	"github.com/Developer-Simon/energy-node-dashboard/internal/energystats"
)

// automationHistoryFile is the automation history in the runtime root.
const automationHistoryFile = "automation_history.json"

// Merge brings the runtime data of the fallback folder onto the drive. Both
// arguments are runtime roots, the layout is the one of Paths.Runtime.
//
// Energy months go through energystats.MergeDir. The automation history is
// kept whole: the newer file wins, because the service holds the complete
// history in memory. Battery states: a state the fallback wrote after a fresh
// start (fresh_start) never replaces the drive, otherwise the newer saved_at
// wins.
//
// A month file that cannot be read is reported as *energystats.MergeSkipped
// (wrapped). The other data is merged in that case as well.
func Merge(fallbackDir, driveDir string) error {
	skipped := mergeEnergyStats(fallbackDir, driveDir)
	var ms *energystats.MergeSkipped
	if skipped != nil && !errors.As(skipped, &ms) {
		return skipped
	}
	if err := mergeBatteryStates(fallbackDir, driveDir); err != nil {
		return err
	}
	if err := mergeHistory(fallbackDir, driveDir); err != nil {
		return err
	}
	return skipped
}

func mergeEnergyStats(fallbackDir, driveDir string) error {
	src := filepath.Join(fallbackDir, "energystats")
	if _, err := os.Stat(src); errors.Is(err, fs.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	dst := filepath.Join(driveDir, "energystats")
	if err := os.MkdirAll(dst, 0750); err != nil {
		return err
	}
	return energystats.MergeDir(dst, src)
}

// batteryMeta is the part of a battery state file that decides the merge.
type batteryMeta struct {
	SavedAt    float64 `json:"saved_at"`
	FreshStart bool    `json:"fresh_start"`
}

func mergeBatteryStates(fallbackDir, driveDir string) error {
	srcDir := filepath.Join(fallbackDir, "battery_soc")
	entries, err := os.ReadDir(srcDir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	dstDir := filepath.Join(driveDir, "battery_soc")
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		src, err := os.ReadFile(filepath.Join(srcDir, entry.Name()))
		if err != nil {
			return err
		}
		var srcMeta batteryMeta
		if json.Unmarshal(src, &srcMeta) != nil {
			// A state that does not parse is not worth keeping.
			continue
		}
		dstPath := filepath.Join(dstDir, entry.Name())
		dst, err := os.ReadFile(dstPath)
		if errors.Is(err, fs.ErrNotExist) {
			if err := config.AtomicWrite(dstPath, src, 0o600); err != nil {
				return err
			}
			continue
		}
		if err != nil {
			return err
		}
		var dstMeta batteryMeta
		if json.Unmarshal(dst, &dstMeta) != nil {
			if err := config.AtomicWrite(dstPath, src, 0o600); err != nil {
				return err
			}
			continue
		}
		if srcMeta.FreshStart || srcMeta.SavedAt <= dstMeta.SavedAt {
			continue
		}
		if err := config.AtomicWrite(dstPath, src, 0o600); err != nil {
			return err
		}
	}
	return nil
}

func mergeHistory(fallbackDir, driveDir string) error {
	srcPath := filepath.Join(fallbackDir, automationHistoryFile)
	srcInfo, err := os.Stat(srcPath)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	dstPath := filepath.Join(driveDir, automationHistoryFile)
	dstInfo, err := os.Stat(dstPath)
	if err == nil && !srcInfo.ModTime().After(dstInfo.ModTime()) {
		return nil
	}
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	data, err := os.ReadFile(srcPath)
	if err != nil {
		return err
	}
	return config.AtomicWrite(dstPath, data, 0o600)
}

// mergeLeftoverEnergyStats brings the energy months that the old dashboard
// wrote to the legacy folder after the restart (its SIGTERM flush of the last
// partial hour) into the drive copy, then deletes the folder. A month that
// cannot be read is skipped, as Merge does, so the folder never blocks Finish.
func mergeLeftoverEnergyStats(dataDir, driveRuntime string) error {
	src := filepath.Join(dataDir, "energystats")
	if _, err := os.Lstat(src); errors.Is(err, fs.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	dst := filepath.Join(driveRuntime, "energystats")
	if err := os.MkdirAll(dst, 0750); err != nil {
		return err
	}
	var skipped *energystats.MergeSkipped
	if err := energystats.MergeDir(dst, src); err != nil && !errors.As(err, &skipped) {
		return err
	}
	return os.RemoveAll(src)
}

// clearDir removes everything inside dir and keeps dir itself.
func clearDir(dir string) error {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if err := os.RemoveAll(filepath.Join(dir, entry.Name())); err != nil {
			return err
		}
	}
	return nil
}
