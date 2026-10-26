package updateinstall

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/updatecheck"
	"github.com/Developer-Simon/energy-node-webui/hostapi"
)

// fakeRunner veroeffentlicht run-started und run-finished selbst auf einem
// echten Bus.
type fakeRunner struct {
	bus         *hostapi.Bus
	hold        chan struct{}
	prepareCode string
	// redeployBusy liefert so viele redeploy-Starts RUN_IN_PROGRESS, bevor
	// einer durchgeht.
	redeployBusy int

	mu    sync.Mutex
	modes []hostapi.RunMode
	n     int
}

func newFakeRunner() *fakeRunner {
	return &fakeRunner{bus: hostapi.NewBus(64)}
}

func (f *fakeRunner) Bus() *hostapi.Bus { return f.bus }

func (f *fakeRunner) Modes() []hostapi.RunMode {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]hostapi.RunMode(nil), f.modes...)
}

func (f *fakeRunner) StartRun(ctx context.Context, req hostapi.RunRequest) (string, error) {
	f.mu.Lock()
	f.modes = append(f.modes, req.Mode)
	f.n++
	id := "run-" + string(req.Mode) + "-" + strconv.Itoa(f.n)
	if req.Mode == hostapi.ModeRedeploy && f.redeployBusy > 0 {
		f.redeployBusy--
		f.mu.Unlock()
		return "", &hostapi.Error{Code: "RUN_IN_PROGRESS", Status: http.StatusConflict}
	}
	f.mu.Unlock()

	f.bus.Publish("run-started", map[string]any{"run_id": id, "mode": string(req.Mode)})
	if req.Mode == hostapi.ModeRedeploy {
		f.bus.Publish("run-finished", map[string]any{"run_id": id, "ok": true})
		return id, nil
	}
	if f.hold != nil {
		<-f.hold
	}
	if f.prepareCode != "" {
		f.bus.Publish("run-finished", map[string]any{"run_id": id, "ok": false, "code": f.prepareCode})
	} else {
		f.bus.Publish("run-finished", map[string]any{"run_id": id, "ok": true})
	}
	return id, nil
}

func offer(latest string) func() (updatecheck.Result, bool) {
	return func() (updatecheck.Result, bool) {
		return updatecheck.Result{Current: "0.10.0", Latest: latest, Available: true}, true
	}
}

func candidate(v string) func(context.Context) (string, error) {
	return func(context.Context) (string, error) { return v, nil }
}

// waitState wartet bis der Zustand erreicht ist oder 2 s vergangen sind.
func waitState(t *testing.T, i *Installer, state string) Status {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		st := i.Status()
		if st.State == state {
			return st
		}
		if time.Now().After(deadline) {
			t.Fatalf("Zustand %q nicht erreicht, letzter Zustand %+v", state, st)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestInstallRunsPrepareThenRedeploy(t *testing.T) {
	runner := newFakeRunner()
	recheck := false
	i := &Installer{
		Runner:    runner,
		Offer:     offer("0.11.0"),
		Recheck:   func(context.Context) { recheck = true },
		Candidate: candidate("v0.11.0"),
		JobBusy:   func() bool { return false },
	}

	version, err := i.Install(context.Background())
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if version != "0.11.0" {
		t.Fatalf("version = %q, want 0.11.0", version)
	}
	waitState(t, i, "idle")

	modes := runner.Modes()
	if len(modes) != 2 || modes[0] != hostapi.ModePrepare || modes[1] != hostapi.ModeRedeploy {
		t.Fatalf("modes = %v, want [prepare redeploy]", modes)
	}
	if recheck {
		t.Fatal("Recheck darf bei passendem Kandidaten nicht laufen")
	}
}

func TestInstallWithoutOfferFails(t *testing.T) {
	runner := newFakeRunner()
	empty := &Installer{
		Runner: runner,
		Offer:  func() (updatecheck.Result, bool) { return updatecheck.Result{}, false },
	}
	if _, err := empty.Install(context.Background()); !errors.Is(err, ErrNoUpdate) {
		t.Fatalf("ohne Cache-Eintrag: err = %v, want ErrNoUpdate", err)
	}

	notAvailable := &Installer{
		Runner: runner,
		Offer: func() (updatecheck.Result, bool) {
			return updatecheck.Result{Current: "0.11.0", Latest: "0.11.0", Available: false}, true
		},
	}
	if _, err := notAvailable.Install(context.Background()); !errors.Is(err, ErrNoUpdate) {
		t.Fatalf("ohne Angebot: err = %v, want ErrNoUpdate", err)
	}
	if n := len(runner.Modes()); n != 0 {
		t.Fatalf("StartRun aufgerufen %d Mal, want 0", n)
	}
}

func TestSecondInstallWhilePreparingIsBusy(t *testing.T) {
	runner := newFakeRunner()
	runner.hold = make(chan struct{})
	i := &Installer{
		Runner:    runner,
		Offer:     offer("0.11.0"),
		Candidate: candidate("v0.11.0"),
		JobBusy:   func() bool { return false },
	}

	if _, err := i.Install(context.Background()); err != nil {
		t.Fatalf("erster Install: %v", err)
	}
	if _, err := i.Install(context.Background()); !errors.Is(err, ErrBusy) {
		t.Fatalf("zweiter Install: err = %v, want ErrBusy", err)
	}
	close(runner.hold)
	waitState(t, i, "idle")

	if n := len(runner.Modes()); n != 2 {
		t.Fatalf("StartRun aufgerufen %d Mal, want 2 (prepare, redeploy)", n)
	}
}

func TestInstallRefusesWhileAJobIsQueued(t *testing.T) {
	runner := newFakeRunner()
	i := &Installer{
		Runner:  runner,
		Offer:   offer("0.11.0"),
		JobBusy: func() bool { return true },
	}
	if _, err := i.Install(context.Background()); !errors.Is(err, ErrBusy) {
		t.Fatalf("err = %v, want ErrBusy", err)
	}
	if n := len(runner.Modes()); n != 0 {
		t.Fatalf("StartRun aufgerufen %d Mal, want 0", n)
	}
}

func TestCandidateMismatchStopsBeforeRedeploy(t *testing.T) {
	runner := newFakeRunner()
	recheck := make(chan struct{}, 1)
	i := &Installer{
		Runner:    runner,
		Offer:     offer("0.11.0"),
		Recheck:   func(context.Context) { recheck <- struct{}{} },
		Candidate: candidate("v0.12.0"),
		JobBusy:   func() bool { return false },
	}
	if _, err := i.Install(context.Background()); err != nil {
		t.Fatalf("Install: %v", err)
	}
	st := waitState(t, i, "fail")
	if st.Code != "OFFER_CHANGED" {
		t.Fatalf("Code = %q, want OFFER_CHANGED", st.Code)
	}
	select {
	case <-recheck:
	case <-time.After(2 * time.Second):
		t.Fatal("Recheck wurde nicht aufgerufen")
	}
	for _, mode := range runner.Modes() {
		if mode == hostapi.ModeRedeploy {
			t.Fatal("redeploy darf bei abweichendem Kandidaten nicht starten")
		}
	}
}

func TestFailedPrepareIsReportedAndRetryable(t *testing.T) {
	runner := newFakeRunner()
	runner.prepareCode = "PACKAGE_SIGNATURE_INVALID"
	i := &Installer{
		Runner:    runner,
		Offer:     offer("0.11.0"),
		Candidate: candidate("v0.11.0"),
		JobBusy:   func() bool { return false },
	}
	if _, err := i.Install(context.Background()); err != nil {
		t.Fatalf("Install: %v", err)
	}
	st := waitState(t, i, "fail")
	if st.Code != "PACKAGE_SIGNATURE_INVALID" {
		t.Fatalf("Code = %q, want PACKAGE_SIGNATURE_INVALID", st.Code)
	}
	if st.FinishedAt == nil {
		t.Fatal("FinishedAt fehlt im Fehlerzustand")
	}

	runner.prepareCode = ""
	if _, err := i.Install(context.Background()); err != nil {
		t.Fatalf("erneuter Install aus fail: %v", err)
	}
	waitState(t, i, "idle")
}

func TestRedeployWaitsForThePrepareRunToWindDown(t *testing.T) {
	runner := newFakeRunner()
	runner.redeployBusy = 2
	i := &Installer{
		Runner:    runner,
		Offer:     offer("0.11.0"),
		Candidate: candidate("v0.11.0"),
		JobBusy:   func() bool { return false },
	}
	if _, err := i.Install(context.Background()); err != nil {
		t.Fatalf("Install: %v", err)
	}
	st := waitState(t, i, "idle")
	if st.State == "fail" {
		t.Fatalf("Status = %+v, want kein fail", st)
	}
	redeploys := 0
	for _, mode := range runner.Modes() {
		if mode == hostapi.ModeRedeploy {
			redeploys++
		}
	}
	if redeploys != 3 {
		t.Fatalf("redeploy-Starts = %d, want 3 (zwei RUN_IN_PROGRESS, dann Erfolg)", redeploys)
	}
}
