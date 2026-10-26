// dashboard/cmd/dashboard/redeploy.go

// The dashboard-hosted Re-Deploy screen (Plan D of the installer spec:
// Komponente D). This file only wires internal/updaterhost's Backend into
// a mounted hostapi.Server; the actual staging/tailing logic lives in
// internal/updaterhost and internal/updaterjob, where it has its own tests.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/bundlefetch"
	"github.com/Developer-Simon/energy-node-dashboard/internal/localize"
	"github.com/Developer-Simon/energy-node-dashboard/internal/updatecheck"
	"github.com/Developer-Simon/energy-node-dashboard/internal/updateinstall"
	"github.com/Developer-Simon/energy-node-dashboard/internal/updaterhost"
	"github.com/Developer-Simon/energy-node-dashboard/internal/updaterjob"
	webui "github.com/Developer-Simon/energy-node-webui"
	"github.com/Developer-Simon/energy-node-webui/hostapi"
	"github.com/Developer-Simon/energy-node-webui/i18n"
)

type redeployConfig struct {
	candidateBundleDir    string
	installedManifestPath string
	selectionPath         string
	stepsDir              string
	jobDir                string
	// prepare fetches the newest package into candidateBundleDir; nil
	// disables the download and the redeploy screen starts at the preview.
	prepare func(ctx context.Context, note func(key string, args map[string]string)) error
	// pageToken renders the screen's page with the session's CSRF token, the
	// value the /redeploy/ gate expects on every non-GET request (see
	// httpapi.SessionCSRFToken). Nil leaves the token empty.
	pageToken func(*http.Request) string
	// languages are the dashboard's UI languages. The screen resolves its
	// language per request against them, exactly like every dashboard page
	// (localize.Resolve). Nil leaves the screen in the catalogs' fallback.
	languages []string
}

var timeNowUnixNano = func() int64 { return time.Now().UnixNano() }

// prepareFunc adapts a bundlefetch.Fetcher to updaterhost's Prepare seam:
// its typed errors become hostapi errors the UI translates (error.<code>).
func prepareFunc(f *bundlefetch.Fetcher) func(context.Context, func(string, map[string]string)) error {
	return func(ctx context.Context, note func(string, map[string]string)) error {
		err := f.Fetch(ctx, note)
		var fetchErr *bundlefetch.Error
		if errors.As(err, &fetchErr) {
			return &hostapi.Error{Code: fetchErr.Code, Detail: fetchErr.Detail}
		}
		return err
	}
}

// redeploySetup is the wired redeploy screen: the mounted handler, the
// server that runs its jobs (the installer's Runner) and the host that reads
// the candidate manifest.
type redeploySetup struct {
	handler http.Handler
	server  *hostapi.Server
	host    *updaterhost.Host
}

// buildRedeployHandler builds the mounted /redeploy/ handler. It is the thin
// wrapper over buildRedeploy for callers that need only the handler.
func buildRedeployHandler(cfg redeployConfig) (http.Handler, error) {
	setup, err := buildRedeploy(cfg)
	if err != nil {
		return nil, err
	}
	return setup.handler, nil
}

// buildRedeploy builds the mounted /redeploy/ handler. Token is
// empty: the dashboard's own session auth already gates every request that
// reaches here (E8's "dieselbe Authentifizierung wie das Dashboard"; that
// gate wants the session's CSRF token on writes, which the page gets
// through pageToken). The screen follows the dashboard's language per
// request (cookie, then Accept-Language) and shows no switch of its own,
// hence LanguageFixed: the switch in the dashboard header decides.
func buildRedeploy(cfg redeployConfig) (*redeploySetup, error) {
	host, err := updaterhost.New(updaterhost.Config{
		CandidateBundleDir: cfg.candidateBundleDir, InstalledManifestPath: cfg.installedManifestPath,
		SelectionPath: cfg.selectionPath, StepsDir: cfg.stepsDir, JobDir: cfg.jobDir, Prepare: cfg.prepare,
	})
	if err != nil {
		return nil, err
	}
	backend := host
	catalogs, err := i18n.Load(webui.Catalogs())
	if err != nil {
		return nil, err
	}

	opts := hostapi.Options{
		Backend: backend, Catalogs: catalogs, LanguageFixed: true,
		BasePath: "/redeploy", PageToken: cfg.pageToken,
	}
	if len(cfg.languages) > 0 {
		languages := cfg.languages
		opts.LanguageFor = func(r *http.Request) string { return localize.Resolve(r, languages) }
	}

	// Resume across a self-update restart (Plan D, E10): if the updater
	// unit claimed a job before this process existed, seed the bus from
	// its log instead of starting empty, then keep tailing in the
	// background exactly like a live Run would. The run keeps the id it was
	// staged under, so a page still open from before the restart accepts
	// its run-finished; the restored bus opens with that run's run-started,
	// so a page loaded after the restart can follow it from seq 0. A job
	// staged by a dashboard that did not record its id gets a fresh one.
	resuming := updaterjob.InFlight(cfg.jobDir)
	resumeID := fmt.Sprintf("resumed-%d", timeNowUnixNano())
	if resuming {
		mode, only := "", ""
		if job, err := updaterjob.ReadCurrent(cfg.jobDir); err != nil {
			log.Printf("redeploy: reading in-flight job: %v", err)
		} else {
			mode, only = job.Mode, job.Only
			if job.RunID != "" {
				resumeID = job.RunID
			}
		}
		lines, err := updaterjob.ReadLog(cfg.jobDir)
		if err != nil {
			log.Printf("redeploy: reading in-flight job log: %v", err)
		}
		opts.InitialBus = hostapi.RestoreBus(hostapi.ReplayRun(resumeID, mode, only, lines), 5000)
	}

	server, err := hostapi.New(opts)
	if err != nil {
		return nil, err
	}

	if resuming {
		finish := server.ResumeRun(resumeID)
		sink := busSink{bus: server.Bus()}
		go func() {
			finish(backend.TailInFlight(context.Background(), sink))
		}()
	}

	return &redeploySetup{handler: server.Handler(), server: server, host: host}, nil
}

// newUpdateInstaller wires the installer for the offered update. It returns
// nil when setup is nil or when the host cannot fetch (no prepare seam, so a
// direct update has no download). Callers must pass the result on only when
// it is non-nil: a nil *Installer inside an interface is not a nil interface.
func newUpdateInstaller(setup *redeploySetup, jobDir string, cache *updatecheck.Cache, recheck func(context.Context)) *updateinstall.Installer {
	if setup == nil || !setup.host.Describe().AutoPrepare {
		return nil
	}
	return &updateinstall.Installer{
		Runner:  setup.server,
		Offer:   cache.Get,
		Recheck: recheck,
		Candidate: func(ctx context.Context) (string, error) {
			manifest, err := setup.host.Manifest(ctx)
			if err != nil {
				return "", err
			}
			return manifest.BundleVersion, nil
		},
		JobBusy: func() bool {
			return updaterjob.Pending(jobDir) || updaterjob.InFlight(jobDir)
		},
	}
}

// busSink adapts a *hostapi.Bus to hostapi.Sink for the resume path, which
// has no Backend.Run call (and thus no sink) of its own to reuse -- see
// hostapi.Server.Bus's doc comment ("ein Wirt, der einen Lauf ausserhalb
// von POST /api/run anstoesst, speist ihn direkt").
type busSink struct{ bus *hostapi.Bus }

func (s busSink) Marker(stepID, state, detail string) {
	s.bus.Publish("step", map[string]string{"id": stepID, "state": state, "detail": detail})
}
func (s busSink) Log(stepID, line string) {
	s.bus.Publish("log", map[string]string{"step_id": stepID, "line": line})
}
func (s busSink) Message(stepID, key string, args map[string]string) {
	s.bus.Publish("log", map[string]any{"step_id": stepID, "key": key, "args": args})
}

// newStepTitler names a bootstrap step the way the redeploy screen does
// (services.js stepLabel): a service step by the shared installer catalogs'
// service.<service_id>, every other step by step.<id>, in the request's
// dashboard language. The service id comes from the installed manifest. It is
// read per call because the update bar polls rarely and the file is small. An
// unknown step has no title; the bar then shows its number only.
func newStepTitler(languages []string, manifestPath string) (func(*http.Request, string) string, error) {
	catalogs, err := i18n.Load(webui.Catalogs())
	if err != nil {
		return nil, err
	}
	return func(r *http.Request, stepID string) string {
		lang := localize.Resolve(r, languages)
		if serviceID := serviceIDForStep(manifestPath, stepID); serviceID != "" {
			if text, ok := catalogs.Lookup(lang, "service."+serviceID); ok {
				return text
			}
		}
		text, _ := catalogs.Lookup(lang, "step."+stepID)
		return text
	}, nil
}

// serviceIDForStep reads one step's service id from an installer manifest
// ({"steps":[{"id":"81","service_id":"apsystems"}]}). A missing or unreadable
// file reads as "no service step".
func serviceIDForStep(manifestPath, stepID string) string {
	raw, err := os.ReadFile(manifestPath)
	if err != nil {
		return ""
	}
	var manifest struct {
		Steps []struct {
			ID        string `json:"id"`
			ServiceID string `json:"service_id"`
		} `json:"steps"`
	}
	if json.Unmarshal(raw, &manifest) != nil {
		return ""
	}
	for _, step := range manifest.Steps {
		if step.ID == stepID {
			return step.ServiceID
		}
	}
	return ""
}
