// Package updateprogress turns the updater's job directory into the short
// summary the dashboard's update bar shows on every page: is a job waiting,
// running or finished, how many of its steps are through, which one is
// running and how it ended. It only reads the files updaterjob documents
// (pending.json, current.json, log, status.json) and never writes.
package updateprogress

import (
	"errors"
	"io/fs"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/updaterjob"
	"github.com/Developer-Simon/energy-node-webui/hostapi"
)

// Step names a bootstrap step. Title is filled in by the HTTP handler in the
// request's language; this package leaves it empty.
type Step struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

// Progress is the answer of GET /api/v1/updates/progress.
type Progress struct {
	// State is idle, pending, running, ok, fail or rejected.
	State   string `json:"state"`
	RunID   string `json:"run_id,omitempty"`
	Version string `json:"version,omitempty"`
	Total   int    `json:"total"`
	Done    int    `json:"done"`
	// Step is the running step (running), the failed step (fail), or nil.
	Step       *Step      `json:"step,omitempty"`
	Code       string     `json:"code,omitempty"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
}

// Read summarises dir. A directory without any job is idle, not an error.
func Read(dir string) (*Progress, error) {
	if updaterjob.Pending(dir) {
		job, err := updaterjob.ReadPending(dir)
		if err != nil {
			return nil, err
		}
		return &Progress{State: "pending", RunID: job.RunID, Version: job.BundleVersion, Total: len(job.Steps)}, nil
	}

	job, err := updaterjob.ReadCurrent(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return &Progress{State: "idle"}, nil
	}
	if err != nil {
		return nil, err
	}
	p := &Progress{RunID: job.RunID, Version: job.BundleVersion, Total: len(job.Steps)}

	lines, err := updaterjob.ReadLog(dir)
	if err != nil {
		return nil, err
	}
	finished := map[string]bool{}
	running, failed := "", ""
	for _, event := range hostapi.ReplayEvents(lines) {
		if event.Type != "step" {
			continue
		}
		data, ok := event.Data.(map[string]string)
		if !ok {
			continue
		}
		id := data["id"]
		switch data["state"] {
		case "begin":
			running = id
		case "ok", "skip":
			finished[id] = true
			if running == id {
				running = ""
			}
		case "fail":
			failed = id
		}
	}
	for _, id := range job.Steps {
		if finished[id] {
			p.Done++
		}
	}

	status, done, err := updaterjob.ReadStatus(dir)
	if err != nil {
		return nil, err
	}
	if !done {
		p.State = "running"
		if running == "" {
			running = nextStep(job.Steps, finished)
		}
		if running != "" {
			p.Step = &Step{ID: running}
		}
		return p, nil
	}

	if at, ok := updaterjob.StatusTime(dir); ok {
		p.FinishedAt = &at
	}
	p.Code = status.Code
	switch status.Result {
	case "ok":
		p.State = "ok"
		p.Done = p.Total
	case "rejected":
		p.State = "rejected"
	default:
		p.State = "fail"
		// status.json names the step for a step failure. An interrupted run
		// (UPDATER_INTERRUPTED) leaves it empty; the step that was running
		// when the updater died is the honest answer then.
		stepID := status.Step
		if stepID == "" {
			stepID = failed
		}
		if stepID == "" {
			stepID = running
		}
		if stepID != "" {
			p.Step = &Step{ID: stepID}
		}
	}
	return p, nil
}

func nextStep(steps []string, finished map[string]bool) string {
	for _, id := range steps {
		if !finished[id] {
			return id
		}
	}
	return ""
}
