package storage

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// LegacyBatteryState is the state file battery_soc uses for an entry without
// state_file. It is the same path as LEGACY_STATE_FILE in soc_config.py.
const LegacyBatteryState = "/home/energynode/battery_soc/state.json"

// legacyOldSuffix marks the old legacy files that setup keeps until Finish.
const legacyOldSuffix = ".old"

// Legacy names the places the runtime data lives while runtime_dir is unset.
type Legacy struct {
	// DevicesDir holds automation_history.json and battery_soc_devices.json.
	DevicesDir string
	// BatteryState is the shared battery state file. Empty means LegacyBatteryState.
	BatteryState string
}

// legacyEntry is one file or directory of the runtime data at its legacy place.
// rel is its path below the runtime root, path its legacy location.
type legacyEntry struct {
	rel  string
	path string
}

func (s *Service) batteryStatePath() string {
	if s.Legacy.BatteryState != "" {
		return s.Legacy.BatteryState
	}
	return LegacyBatteryState
}

// legacyEntries lists the runtime data that moves to the drive: the runtime
// cache, the energy months, the automation history and one battery state per
// battery that uses the shared legacy file.
func (s *Service) legacyEntries() []legacyEntry {
	out := []legacyEntry{
		{rel: "runtime.json", path: filepath.Join(s.DataDir, "runtime.json")},
		{rel: "energystats", path: filepath.Join(s.DataDir, "energystats")},
		{rel: automationHistoryFile, path: filepath.Join(s.Legacy.DevicesDir, automationHistoryFile)},
	}
	for _, id := range s.legacyBatteryIDs() {
		out = append(out, legacyEntry{rel: filepath.Join("battery_soc", id+".json"), path: s.batteryStatePath()})
	}
	return out
}

// legacyBatteryIDs returns the ids of the batteries that use the legacy shared
// state file: entries without state_file, or with the legacy path. Ids that
// would leave the battery_soc directory are skipped.
func (s *Service) legacyBatteryIDs() []string {
	raw, err := os.ReadFile(filepath.Join(s.Legacy.DevicesDir, "battery_soc_devices.json"))
	if err != nil {
		return nil
	}
	var devices []struct {
		ID        string `json:"id"`
		StateFile string `json:"state_file"`
	}
	if json.Unmarshal(raw, &devices) != nil {
		return nil
	}
	var ids []string
	for _, d := range devices {
		if d.ID == "" || strings.ContainsAny(d.ID, `/\`) || d.ID == "." || d.ID == ".." {
			continue
		}
		if d.StateFile == "" || d.StateFile == LegacyBatteryState {
			ids = append(ids, d.ID)
		}
	}
	sort.Strings(ids)
	return ids
}

// uniqueByPath keeps the first entry of every legacy location. Battery ids
// that share the legacy file collapse into one entry.
func uniqueByPath(entries []legacyEntry) []legacyEntry {
	seen := map[string]bool{}
	var out []legacyEntry
	for _, e := range entries {
		if seen[e.path] {
			continue
		}
		seen[e.path] = true
		out = append(out, e)
	}
	return out
}

// oldPaths returns the .old name of every legacy location in entries.
func oldPaths(entries []legacyEntry) []string {
	var out []string
	for _, e := range entries {
		out = append(out, e.path+legacyOldSuffix)
	}
	return out
}

// checkLegacyOld refuses a setup when an old file from an earlier, unfinished
// setup is still there, so a rename cannot overwrite it.
func checkLegacyOld(entries []legacyEntry) error {
	for _, p := range oldPaths(entries) {
		if _, err := os.Lstat(p); err == nil {
			return errors.New("old legacy data still present: " + p)
		}
	}
	return nil
}

type move struct {
	from string
	to   string
}

// legacyMoveAside renames every existing legacy location in entries to its
// .old name. On an error the renames done so far are undone.
func legacyMoveAside(entries []legacyEntry) ([]move, error) {
	var done []move
	for _, e := range entries {
		if _, err := os.Lstat(e.path); errors.Is(err, fs.ErrNotExist) {
			continue
		} else if err != nil {
			undoMoves(done)
			return nil, err
		}
		m := move{from: e.path, to: e.path + legacyOldSuffix}
		if err := os.Rename(m.from, m.to); err != nil {
			undoMoves(done)
			return nil, err
		}
		done = append(done, m)
	}
	return done, nil
}

// undoMoves renames the moved files back, in reverse order.
func undoMoves(moves []move) {
	for i := len(moves) - 1; i >= 0; i-- {
		_ = os.Rename(moves[i].to, moves[i].from)
	}
}

// backupsDirName is the backup directory in data_dir. Setup renames it aside
// once its archives are on the drive, like the runtime files.
const backupsDirName = "backups"

// moveBackupsAside renames data_dir/backups to backups.old and returns the new
// name. A missing backups directory moves nothing and returns "".
func moveBackupsAside(dataDir string) (string, error) {
	dir := filepath.Join(dataDir, backupsDirName)
	if _, err := os.Lstat(dir); errors.Is(err, fs.ErrNotExist) {
		return "", nil
	} else if err != nil {
		return "", err
	}
	old := dir + legacyOldSuffix
	if err := os.Rename(dir, old); err != nil {
		return "", err
	}
	return old, nil
}

// removeOld deletes the old legacy files that setup kept.
func removeOld(paths []string) error {
	for _, p := range paths {
		if err := os.RemoveAll(p); err != nil {
			return err
		}
	}
	return nil
}
