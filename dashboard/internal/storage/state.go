package storage

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Fallback is where the runtime data goes while the second drive is not
// usable: on the system disk or in RAM.
type Fallback string

const (
	FallbackSystem Fallback = "system"
	FallbackRAM    Fallback = "ram"
)

// Mode is the place the runtime path currently points to.
type Mode string

const (
	// ModeNone means no runtime path has been set up yet.
	ModeNone Mode = "none"
	// ModeDrive means the runtime data lives on the second drive.
	ModeDrive Mode = "drive"
	// ModeFallbackSystem means the runtime data lives on the system disk.
	ModeFallbackSystem Mode = "fallback_system"
	// ModeFallbackRAM means the runtime data lives in RAM.
	ModeFallbackRAM Mode = "fallback_ram"
)

// Status describes the current mode, when it was entered and why.
type Status struct {
	Mode   Mode
	Since  time.Time
	Reason string
}

// Paths are the locations Switch moves the runtime symlink between. Mount is
// the drive root that carries the marker, the runtime data lives in its
// runtime subdirectory (see DriveRuntimeDir).
type Paths struct {
	Runtime        string
	Mount          string
	SystemFallback string
	RAMFallback    string
}

// Config is the drive choice the user made, stored in data_dir/storage.json.
type Config struct {
	UUID       string   `json:"uuid"`
	Name       string   `json:"name"`
	Fallback   Fallback `json:"fallback"`
	UseData    bool     `json:"use_data"`
	UseBackups bool     `json:"use_backups"`
	// Legacy is set when the runtime data came from the legacy locations and
	// runtime_dir was set by setup. Remove moves the data back there.
	Legacy bool `json:"legacy,omitempty"`
	// LegacyOld lists the old legacy files setup kept until Finish deletes them.
	LegacyOld []string `json:"legacy_old,omitempty"`
	// BackupsOld is the data_dir/backups.old that setup kept until Finish
	// deletes it. Empty when setup moved no backups.
	BackupsOld string `json:"backups_old,omitempty"`
	// ReplaceStaging is the data_dir/replace-staging that a replace kept until
	// Finish deletes it. Empty when the replace staged nothing.
	ReplaceStaging string `json:"replace_staging,omitempty"`
}

const (
	configFile = "storage.json"
	markerName = ".energy-node-volume"
)

// machineIDPath is a variable so tests can point it at a temp file.
var machineIDPath = "/etc/machine-id"

// MachineID returns the node's machine id, trimmed.
func MachineID() (string, error) {
	data, err := os.ReadFile(machineIDPath)
	if err != nil {
		return "", fmt.Errorf("machine id: %w", err)
	}
	return strings.TrimSpace(string(data)), nil
}

// VolumeOK reports whether the drive mounted at mount carries the marker of
// this node. The marker holds the machine id. An empty nodeID never matches.
func VolumeOK(mount, nodeID string) bool {
	nodeID = strings.TrimSpace(nodeID)
	if nodeID == "" {
		return false
	}
	data, err := os.ReadFile(filepath.Join(mount, markerName))
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(data)) == nodeID
}

// LoadConfig reads storage.json from dir. A missing file yields the zero
// Config without an error.
func LoadConfig(dir string) (Config, error) {
	var cfg Config
	data, err := os.ReadFile(filepath.Join(dir, configFile))
	if errors.Is(err, os.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return cfg, fmt.Errorf("storage: %w", err)
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("storage: %s: %w", configFile, err)
	}
	return cfg, nil
}

// SaveConfig writes storage.json into dir atomically.
func SaveConfig(dir string, cfg Config) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("storage: %w", err)
	}
	data = append(data, '\n')
	return atomicWrite(filepath.Join(dir, configFile), data)
}

// atomicWrite writes through a temp file and renames it into place, so a
// crash never leaves a half-written file behind.
func atomicWrite(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0750); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".storage-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if _, err = tmp.Write(data); err == nil {
		err = tmp.Sync()
	}
	closeErr := tmp.Close()
	if err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Chmod(name, 0640)
	}
	if err != nil {
		return err
	}
	return os.Rename(name, path)
}
