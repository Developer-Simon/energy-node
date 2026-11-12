package storage

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// pendingFile marks a job whose dashboard restart was requested. The follow-up
// step on the next boot reads it and removes it.
const pendingFile = "storage-pending.json"

// Kinds of a pending job.
const (
	pendingSetup  = "setup"
	pendingRemove = "remove"
)

type pendingJob struct {
	Kind string `json:"kind"`
}

// writePending records that a job of kind waits for the follow-up step.
func writePending(dataDir, kind string) error {
	data, err := json.Marshal(pendingJob{Kind: kind})
	if err != nil {
		return err
	}
	return atomicWrite(filepath.Join(dataDir, pendingFile), append(data, '\n'))
}

// readPending returns the kind of the pending job, if there is one.
func readPending(dataDir string) (string, bool, error) {
	raw, err := os.ReadFile(filepath.Join(dataDir, pendingFile))
	if errors.Is(err, fs.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	var p pendingJob
	if err := json.Unmarshal(raw, &p); err != nil {
		return "", false, fmt.Errorf("storage: %s: %w", pendingFile, err)
	}
	return p.Kind, true, nil
}

// clearPending removes the marker. A missing marker is not an error.
func clearPending(dataDir string) error {
	err := os.Remove(filepath.Join(dataDir, pendingFile))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}
