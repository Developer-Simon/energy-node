// dashboard/internal/updaterhost/host.go

// Package updaterhost is the dashboard's own Schicht-2 handler: it
// implements hostapi.Backend without SSH, by staging a job into
// updaterjob's on-node directory and tailing its log file instead of
// reading an SSH stdout stream (Plan D, Komponente D of the installer
// spec). It never runs a bootstrap step itself and never calls sudo -- a
// job is only ever a file the root energy-node-updater unit picks up.
package updaterhost

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/updaterjob"
	"github.com/Developer-Simon/energy-node-webui/hostapi"
)

// pollInterval is how often Run and TailInFlight check job/log and
// status.json for changes. There is no inotify dependency here on
// purpose: the job directory already lives on a local filesystem, and a
// short poll is simpler to reason about across a dashboard process
// restart than a watch handle that dies with the process anyway.
const pollInterval = 250 * time.Millisecond

// maxTailDuration bounds how long a single tail waits for status.json.
// Long enough for a real bootstrap run on a Pi (apt, wheels, restarts),
// short enough that a job whose updater died without writing a status
// eventually surfaces as an error instead of an endless spinner.
const maxTailDuration = 30 * time.Minute

// Config points Host at the files it reads and writes. CandidateBundleDir
// is wherever a new bundle to redeploy already sits unpacked -- how it got
// there is out of scope for this plan (the Dashboard-OTA-Spec's job); this
// package only stages it into JobDir when asked to run.
type Config struct {
	CandidateBundleDir    string
	InstalledManifestPath string
	SelectionPath         string
	JobDir                string
	// Prepare fetches the newest package into CandidateBundleDir (see
	// internal/bundlefetch). It is called for hostapi.ModePrepare and gets a
	// callback for one human-readable log line at a time. Nil means this host
	// cannot fetch: Describe does not advertise AutoPrepare and ModePrepare
	// answers NOT_SUPPORTED. Errors of type *hostapi.Error keep their code.
	Prepare func(ctx context.Context, log func(line string)) error
}

// candidateManifest is the handful of manifest.json fields this package
// needs. It deliberately does not import installer/internal/bundle.Manifest
// -- that would pull the installer module's SSH dependency chain into the
// dashboard binary (Komponente C keeps it out of the ARMv6 build).
type candidateManifest struct {
	Version    string            `json:"version"`
	Arch       string            `json:"arch"`
	Components map[string]string `json:"components"`
	Steps      []struct {
		ID        string `json:"id"`
		Optional  bool   `json:"optional"`
		Default   bool   `json:"default"`
		ServiceID string `json:"service_id"`
		Kind      string `json:"kind"`
		Dir       string `json:"dir"`
		Unit      string `json:"unit"`
		Version   string `json:"version"`
		Requires  string `json:"requires,omitempty"`
	} `json:"steps"`
}

type installedManifest struct {
	Version    string            `json:"version"`
	Components map[string]string `json:"components"`
	Steps      []struct {
		ID      string `json:"id"`
		Version string `json:"version"`
	} `json:"steps"`
}

type selectionFile struct {
	Steps map[string]bool `json:"steps"`
}

// Host implements hostapi.Backend over a local job directory.
type Host struct {
	cfg Config
}

func New(cfg Config) (*Host, error) {
	return &Host{cfg: cfg}, nil
}

func (h *Host) loadCandidateManifest() (*candidateManifest, error) {
	raw, err := os.ReadFile(h.cfg.CandidateBundleDir + "/manifest.json")
	if err != nil {
		return nil, err
	}
	var m candidateManifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

// Describe advertises only "redeploy". Diagnose below is still
// NOT_SUPPORTED, and Schicht 3 shows a Diagnose tab as soon as a host
// advertises more than one entry point -- which would dead-end on a 501.
func (h *Host) Describe() hostapi.Description {
	desc := hostapi.Description{Host: hostapi.HostDashboard, EntryPoints: []string{"redeploy"}, NeedsConnection: false, AutoPrepare: h.cfg.Prepare != nil}
	if m, err := h.loadCandidateManifest(); err == nil {
		desc.BundleVersion = m.Version
		desc.BundleArch = m.Arch
	}
	return desc
}

// Connect, GenerateKeypair and Precheck exist only to satisfy
// hostapi.Backend. NeedsConnection is false (Describe, above), so Schicht
// 3 never shows the connection screen and never calls these for this host
// -- they return NOT_SUPPORTED rather than pretending to do something.
func (h *Host) Connect(context.Context, hostapi.ConnectRequest) (hostapi.ConnectResult, error) {
	return hostapi.ConnectResult{}, &hostapi.Error{Code: "NOT_SUPPORTED", Status: http.StatusNotImplemented}
}

func (h *Host) GenerateKeypair(context.Context) (hostapi.KeypairResult, error) {
	return hostapi.KeypairResult{}, &hostapi.Error{Code: "NOT_SUPPORTED", Status: http.StatusNotImplemented}
}

func (h *Host) Precheck(context.Context) (*hostapi.Precheck, error) {
	return nil, &hostapi.Error{Code: "NOT_SUPPORTED", Status: http.StatusNotImplemented}
}

func (h *Host) Manifest(context.Context) (*hostapi.ManifestView, error) {
	m, err := h.loadCandidateManifest()
	if err != nil {
		return nil, &hostapi.Error{Code: "MANIFEST_UNREADABLE", Detail: err.Error()}
	}
	view := &hostapi.ManifestView{BundleVersion: m.Version, Arch: m.Arch, Components: m.Components}
	for _, s := range m.Steps {
		view.Steps = append(view.Steps, hostapi.StepView{
			ID: s.ID, ServiceID: s.ServiceID, Dir: s.Dir, Unit: s.Unit,
			Optional: s.Optional, Default: s.Default, Kind: s.Kind,
			Requires: s.Requires,
		})
	}
	return view, nil
}

func (h *Host) Selection(context.Context) (*hostapi.SelectionView, error) {
	raw, err := os.ReadFile(h.cfg.SelectionPath)
	if err != nil {
		return nil, &hostapi.Error{Code: "SELECTION_UNREADABLE", Detail: err.Error()}
	}
	var sel selectionFile
	if err := json.Unmarshal(raw, &sel); err != nil {
		return nil, &hostapi.Error{Code: "SELECTION_UNREADABLE", Detail: err.Error()}
	}
	return &hostapi.SelectionView{Steps: sel.Steps, Source: "node"}, nil
}

// SaveSelection is a no-op error for this host: "Dienste ändern" during a
// dashboard-hosted redeploy still only ever reads the node's own
// selection.json (spec, Ablauf/Aktualisieren) -- there is deliberately no
// path from this screen back into changing which optional services run.
func (h *Host) SaveSelection(context.Context, map[string]bool) error {
	return &hostapi.Error{Code: "NOT_SUPPORTED", Status: http.StatusNotImplemented}
}

func (h *Host) Plan(context.Context) (*hostapi.PlanView, error) {
	candidate, err := h.loadCandidateManifest()
	if err != nil {
		return nil, &hostapi.Error{Code: "MANIFEST_UNREADABLE", Detail: err.Error()}
	}
	sel, err := h.Selection(context.Background())
	if err != nil {
		return nil, err
	}
	var installed *installedManifest
	if raw, err := os.ReadFile(h.cfg.InstalledManifestPath); err == nil {
		var doc installedManifest
		if json.Unmarshal(raw, &doc) == nil {
			installed = &doc
		}
	}

	view := &hostapi.PlanView{BundleVersion: candidate.Version, Components: map[string]hostapi.ComponentDelta{}}
	// From mirrors plan.sh: the installed manifest's version of the
	// component, nil when there is no installed manifest or it lacks one.
	for name, to := range candidate.Components {
		delta := hostapi.ComponentDelta{To: to}
		if installed != nil {
			if from, ok := installed.Components[name]; ok {
				delta.From = &from
			}
		}
		view.Components[name] = delta
	}
	for _, s := range candidate.Steps {
		selected := !s.Optional || sel.Steps[s.ID]
		state := "pending"
		if s.Optional && !sel.Steps[s.ID] {
			state = "deselected"
		}
		ps := hostapi.PlanStep{ID: s.ID, Optional: s.Optional, Selected: selected, State: state, Unit: s.Unit, To: s.Version}
		if s.Dir != "" {
			if installed != nil {
				for _, is := range installed.Steps {
					if is.ID == s.ID {
						ps.From = is.Version
					}
				}
			}
			if state == "pending" {
				ps.Restart = restartReason(installed, candidate, s.ID, false)
			}
		}
		view.Steps = append(view.Steps, ps)
	}
	return view, nil
}

// Run stages a job for exactly the selected steps and follows it to
// completion by tailing job/log, translating lines into sink calls the
// same way installer/internal/host.Host.Run does for its SSH stdout
// stream. If the dashboard process itself gets restarted partway through
// (step 60 replacing the binary) this call simply never returns -- the
// process serving it is gone. Host.TailInFlight is what a freshly started
// process uses instead, to pick the same job back up (see
// cmd/dashboard/main.go, Task 8).
func (h *Host) Run(ctx context.Context, req hostapi.RunRequest, sink hostapi.Sink) error {
	if req.Mode == hostapi.ModePrepare {
		return h.runPrepare(ctx, sink)
	}

	candidate, err := h.loadCandidateManifest()
	if err != nil {
		return &hostapi.Error{Code: "MANIFEST_UNREADABLE", Detail: err.Error()}
	}
	sel, err := h.Selection(ctx)
	if err != nil {
		return err
	}

	var stepIDs []string
	if req.Only != "" {
		stepIDs = []string{req.Only}
	} else {
		for _, s := range candidate.Steps {
			if !s.Optional || sel.Steps[s.ID] {
				stepIDs = append(stepIDs, s.ID)
			}
		}
	}

	// req.TargetUser/TargetBase are deliberately not forwarded: they would
	// travel through the unsigned, dashboard-writable job.json into a
	// root-run install/chown. The updater takes both from the verified
	// manifest instead.
	job := updaterjob.Job{
		BundleVersion: candidate.Version, Mode: string(req.Mode), Only: req.Only,
		Steps: stepIDs, RestartAll: req.RestartAll, RunID: req.RunID,
	}
	if err := updaterjob.Stage(h.cfg.JobDir, job, h.cfg.CandidateBundleDir); err != nil {
		return &hostapi.Error{Code: "JOB_STAGING_FAILED", Detail: err.Error()}
	}

	return tailJobLog(ctx, h.cfg.JobDir+"/log", h.cfg.JobDir+"/status.json", 0, sink, pollInterval, maxTailDuration)
}

// TailInFlight resumes watching a job the updater already claimed before
// this process started -- used only right after a self-update restart
// (Task 8). It reads the whole log from the start: a fresh process has no
// memory of how much of it a previous incarnation already saw, and
// replaying it through sink here is exactly what cmd/dashboard/main.go's
// hostapi.ReplayEvents/RestoreBus path (Task 1) already reconstructed once
// for the SSE history -- this is the live continuation of that same job.
func (h *Host) TailInFlight(ctx context.Context, sink hostapi.Sink) error {
	if !updaterjob.InFlight(h.cfg.JobDir) {
		return fmt.Errorf("updaterhost: no job in flight in %s", h.cfg.JobDir)
	}
	return tailJobLog(ctx, h.cfg.JobDir+"/log", h.cfg.JobDir+"/status.json", 0, sink, pollInterval, maxTailDuration)
}

// runPrepare fetches the newest package into the candidate directory and
// reports it as the pseudo step "package", the same way the installer host
// reports its own prepare run, so one prepare screen serves both hosts.
func (h *Host) runPrepare(ctx context.Context, sink hostapi.Sink) error {
	if h.cfg.Prepare == nil {
		return &hostapi.Error{Code: "NOT_SUPPORTED", Status: http.StatusNotImplemented}
	}
	// The updater moves the staged bundle out of the job directory when it
	// claims the job, and Stage copies the candidate; replacing the candidate
	// under a job that is queued or running would make "what will run" a lie.
	if updaterjob.Pending(h.cfg.JobDir) || updaterjob.InFlight(h.cfg.JobDir) {
		return &hostapi.Error{Code: "JOB_IN_PROGRESS", Status: http.StatusConflict}
	}

	sink.Marker("package", "begin", "")
	err := h.cfg.Prepare(ctx, func(line string) { sink.Log("package", line) })
	if err == nil {
		sink.Marker("package", "ok", "")
		return nil
	}
	if errors.Is(err, context.Canceled) {
		sink.Marker("package", "fail", "RUN_CANCELLED")
		return err
	}
	var typed *hostapi.Error
	if errors.As(err, &typed) {
		sink.Marker("package", "fail", typed.Code)
		return err
	}
	sink.Marker("package", "fail", "PREPARE_FAILED")
	return &hostapi.Error{Code: "PREPARE_FAILED", Detail: err.Error()}
}

func (h *Host) Diagnose(context.Context) (*hostapi.DiagnoseView, error) {
	// TODO(Plan D follow-up): implement Diagnose locally
	return nil, &hostapi.Error{Code: "NOT_SUPPORTED", Status: http.StatusNotImplemented}
}
