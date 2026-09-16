// Package updaterjob is the on-node staging area the dashboard's local
// Schicht-2 handler (internal/updaterhost) and the root
// energy-node-updater unit both read and write. Neither side runs the
// other's code; job.json, pending.json, log and status.json are the only
// contract between them (Plan D, Komponente D).
package updaterjob

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

// DefaultDir is where a real node keeps the job directory. Tests always
// pass an explicit temp dir instead.
const DefaultDir = "/var/lib/energy-node-installer/job"

// Job is what the dashboard stages for the updater to run. It carries no
// secrets (see the plan's Global Constraints) -- a redeploy never needs
// mqtt.pw/auth.pw, which already exist on a node the dashboard runs on.
type Job struct {
	BundleVersion string   `json:"bundle_version"`
	Mode          string   `json:"mode"`
	Only          string   `json:"only,omitempty"`
	TargetUser    string   `json:"target_user"`
	TargetBase    string   `json:"target_base"`
	Steps         []string `json:"steps"`
}

// Status is the updater's final report, written once to status.json. A
// missing file means the job has not finished (or was never started).
type Status struct {
	Result string `json:"result"` // "ok" | "fail" | "rejected"
	Step   string `json:"step,omitempty"`
	Code   string `json:"code,omitempty"`
}

// Stage copies bundleSrc into dir/bundle, writes dir/job.json, resets
// dir/log and dir/status.json, and finally renames a temp file into
// dir/pending.json -- the one file whose *existence* the energy-node-updater.path
// unit watches. Everything else is written first so the rename is the
// single atomic instant at which the job becomes visible to the updater.
func Stage(dir string, job Job, bundleSrc string) error {
	if _, err := os.Stat(filepath.Join(dir, "pending.json")); err == nil {
		return errors.New("updaterjob: a job is already pending")
	}
	if _, err := os.Stat(filepath.Join(dir, "current.json")); err == nil {
		return errors.New("updaterjob: a job is already running")
	}

	bundleDst := filepath.Join(dir, "bundle")
	if err := os.RemoveAll(bundleDst); err != nil {
		return fmt.Errorf("updaterjob: clearing old bundle: %w", err)
	}
	if err := copyDir(bundleSrc, bundleDst); err != nil {
		return fmt.Errorf("updaterjob: staging bundle: %w", err)
	}

	raw, err := json.Marshal(job)
	if err != nil {
		return fmt.Errorf("updaterjob: encoding job: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "log"), nil, 0o644); err != nil {
		return fmt.Errorf("updaterjob: resetting log: %w", err)
	}
	_ = os.Remove(filepath.Join(dir, "status.json"))

	tmp := filepath.Join(dir, ".pending.json.tmp")
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return fmt.Errorf("updaterjob: writing pending.json: %w", err)
	}
	if err := os.Rename(tmp, filepath.Join(dir, "pending.json")); err != nil {
		return fmt.Errorf("updaterjob: activating pending.json: %w", err)
	}
	return nil
}

// ReadStatus reads dir/status.json. done is false and status is nil when
// the file does not exist yet.
func ReadStatus(dir string) (status *Status, done bool, err error) {
	raw, err := os.ReadFile(filepath.Join(dir, "status.json"))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	var s Status
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, false, fmt.Errorf("updaterjob: parsing status.json: %w", err)
	}
	return &s, true, nil
}

// ReadLog returns dir/log split into lines, or nil if the file does not
// exist yet (the job has not been claimed by the updater at all).
func ReadLog(dir string) ([]string, error) {
	raw, err := os.ReadFile(filepath.Join(dir, "log"))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return splitLines(string(raw)), nil
}

// InFlight is true exactly when the updater has claimed a job
// (current.json exists, written by the updater's own first action) but
// has not yet finished it (status.json does not exist). The dashboard
// checks this once, at process startup, to decide whether to resume
// watching a job the previous process instance started (Plan D: a
// self-update restarts the dashboard partway through the very job it
// requested).
func InFlight(dir string) bool {
	if _, err := os.Stat(filepath.Join(dir, "current.json")); err != nil {
		return false
	}
	_, err := os.Stat(filepath.Join(dir, "status.json"))
	return errors.Is(err, fs.ErrNotExist)
}

func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}

func copyDir(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		return copyFile(path, target, info.Mode())
	})
}

func copyFile(src, dst string, mode fs.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}
