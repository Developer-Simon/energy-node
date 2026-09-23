// dashboard/cmd/dashboard/redeploy.go

// The dashboard-hosted Re-Deploy screen (Plan D of the installer spec:
// Komponente D). This file only wires internal/updaterhost's Backend into
// a mounted hostapi.Server; the actual staging/tailing logic lives in
// internal/updaterhost and internal/updaterjob, where it has its own tests.
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/bundlefetch"
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
	jobDir                string
	// prepare fetches the newest package into candidateBundleDir; nil
	// disables the download and the redeploy screen starts at the preview.
	prepare func(ctx context.Context, log func(line string)) error
	// pageToken renders the screen's page with the session's CSRF token, the
	// value the /redeploy/ gate expects on every non-GET request (see
	// httpapi.SessionCSRFToken). Nil leaves the token empty.
	pageToken func(*http.Request) string
}

var timeNowUnixNano = func() int64 { return time.Now().UnixNano() }

// prepareFunc adapts a bundlefetch.Fetcher to updaterhost's Prepare seam:
// its typed errors become hostapi errors the UI translates (error.<code>).
func prepareFunc(f *bundlefetch.Fetcher) func(context.Context, func(string)) error {
	return func(ctx context.Context, log func(string)) error {
		err := f.Fetch(ctx, log)
		var fetchErr *bundlefetch.Error
		if errors.As(err, &fetchErr) {
			return &hostapi.Error{Code: fetchErr.Code, Detail: fetchErr.Detail}
		}
		return err
	}
}

// buildRedeployHandler builds the mounted /redeploy/ handler. Token is
// empty and LanguageFixed is true throughout: the dashboard's own session
// auth already gates every request that reaches here (E8's "dieselbe
// Authentifizierung wie das Dashboard"; that gate wants the session's CSRF
// token on writes, which the page gets through pageToken), and the language
// switch stays off
// until P2.9 localizes the rest of the dashboard.
func buildRedeployHandler(cfg redeployConfig) (http.Handler, error) {
	backend, err := updaterhost.New(updaterhost.Config{
		CandidateBundleDir: cfg.candidateBundleDir, InstalledManifestPath: cfg.installedManifestPath,
		SelectionPath: cfg.selectionPath, JobDir: cfg.jobDir, Prepare: cfg.prepare,
	})
	if err != nil {
		return nil, err
	}
	catalogs, err := i18n.Load(webui.Catalogs())
	if err != nil {
		return nil, err
	}

	opts := hostapi.Options{
		Backend: backend, Catalogs: catalogs, Language: "de", LanguageFixed: true,
		BasePath: "/redeploy", PageToken: cfg.pageToken,
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

	return server.Handler(), nil
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
