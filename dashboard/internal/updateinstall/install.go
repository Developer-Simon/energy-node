// Package updateinstall installiert genau das Update, das der Dashboard-
// Oberflaeche angeboten wurde - nicht das, was gerade neu geprueft wird.
package updateinstall

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/updatecheck"
	"github.com/Developer-Simon/energy-node-webui/hostapi"
)

// Runner startet Laeufe des Installers und liefert den Ereignisbus, auf dem
// ihr Ende erscheint. *hostapi.Server erfuellt die Schnittstelle.
type Runner interface {
	StartRun(ctx context.Context, req hostapi.RunRequest) (string, error)
	Bus() *hostapi.Bus
}

// Installer fuehrt die Vorbereitung und die Aufspielung eines angebotenen
// Updates aus. Die Funktionsfelder sind Naehte fuer Tests und Verdrahtung.
type Installer struct {
	Runner    Runner
	Offer     func() (updatecheck.Result, bool)         // updatecheck.Cache.Get
	Recheck   func(ctx context.Context)                 // neue Pruefung, schreibt den Cache
	Candidate func(ctx context.Context) (string, error) // Version im Kandidatenverzeichnis
	JobBusy   func() bool                               // updaterjob.Pending || updaterjob.InFlight
	Now       func() time.Time

	mu     sync.Mutex
	status Status
}

// Status ist der Zustand des letzten Installationsversuchs. State ist
// idle, preparing oder fail.
type Status struct {
	State      string     `json:"state"` // idle, preparing, fail
	Version    string     `json:"version,omitempty"`
	Code       string     `json:"code,omitempty"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
}

var (
	ErrNoUpdate = errors.New("updateinstall: no update offered")
	ErrBusy     = errors.New("updateinstall: update in progress")
)

// Install startet die Vorbereitung des angebotenen Updates und liefert dessen
// Version. Der Rest des Ablaufs laeuft im Hintergrund weiter.
func (i *Installer) Install(ctx context.Context) (version string, err error) {
	i.mu.Lock()
	if i.status.State == "preparing" || (i.JobBusy != nil && i.JobBusy()) {
		i.mu.Unlock()
		return "", ErrBusy
	}
	if i.Offer == nil {
		i.mu.Unlock()
		return "", ErrNoUpdate
	}
	offer, ok := i.Offer()
	if !ok || !offer.Available {
		i.mu.Unlock()
		return "", ErrNoUpdate
	}
	i.status = Status{State: "preparing", Version: offer.Latest}
	i.mu.Unlock()

	go i.run(context.WithoutCancel(ctx), offer.Latest)
	return offer.Latest, nil
}

// Status liefert eine Kopie des aktuellen Zustands.
func (i *Installer) Status() Status {
	i.mu.Lock()
	defer i.mu.Unlock()
	return i.status
}

// run fuehrt Vorbereitung und Aufspielung nacheinander aus.
func (i *Installer) run(ctx context.Context, version string) {
	bus := i.Runner.Bus()
	// Vor dem Start abonnieren, damit ein schnelles run-finished nicht verloren geht.
	events, cancel := bus.Subscribe(bus.Seq())
	defer cancel()

	runID, err := i.Runner.StartRun(ctx, hostapi.RunRequest{Mode: hostapi.ModePrepare})
	if err != nil {
		i.fail(version, codeOf(err, "PREPARE_FAILED"))
		return
	}
	code, ok := awaitFinish(events, runID)
	if !ok {
		i.fail(version, code)
		return
	}

	candidate, err := i.candidate(ctx)
	if err != nil || normalize(candidate) != normalize(version) {
		if i.Recheck != nil {
			i.Recheck(ctx)
		}
		i.fail(version, "OFFER_CHANGED")
		return
	}

	if _, err := i.Runner.StartRun(ctx, hostapi.RunRequest{Mode: hostapi.ModeRedeploy}); err != nil {
		i.fail(version, codeOf(err, "REDEPLOY_FAILED"))
		return
	}
	i.mu.Lock()
	i.status = Status{State: "idle"}
	i.mu.Unlock()
}

// candidate liest die Kandidatenversion, ohne Funktion ist sie leer.
func (i *Installer) candidate(ctx context.Context) (string, error) {
	if i.Candidate == nil {
		return "", errors.New("updateinstall: no candidate reader")
	}
	return i.Candidate(ctx)
}

// awaitFinish wartet auf das run-finished des eigenen Laufs. Ein Ereignis
// fremder Laeufe wird uebergangen. ok ist true, wenn der Lauf erfolgreich war.
func awaitFinish(events <-chan hostapi.Event, runID string) (code string, ok bool) {
	for event := range events {
		if event.Type != "run-finished" {
			continue
		}
		data, isMap := event.Data.(map[string]any)
		if !isMap || data["run_id"] != runID {
			continue
		}
		if success, _ := data["ok"].(bool); success {
			return "", true
		}
		code, _ = data["code"].(string)
		if code == "" {
			code = "PREPARE_FAILED"
		}
		return code, false
	}
	// Der Abonnent wurde abgeworfen, bevor das Ende kam.
	return "PREPARE_FAILED", false
}

// fail haelt den Fehlerzustand fest. Ein neuer Install ist danach erlaubt.
func (i *Installer) fail(version, code string) {
	i.mu.Lock()
	defer i.mu.Unlock()
	now := i.now()
	i.status = Status{State: "fail", Version: version, Code: code, FinishedAt: &now}
}

func (i *Installer) now() time.Time {
	if i.Now != nil {
		return i.Now()
	}
	return time.Now()
}

// codeOf liefert den Code eines typisierten Fehlers, sonst den Fallback.
func codeOf(err error, fallback string) string {
	var typed *hostapi.Error
	if errors.As(err, &typed) && typed.Code != "" {
		return typed.Code
	}
	return fallback
}

// normalize entfernt ein fuehrendes v, damit v0.11.0 und 0.11.0 gleich sind.
func normalize(version string) string {
	return strings.TrimPrefix(strings.TrimSpace(version), "v")
}
