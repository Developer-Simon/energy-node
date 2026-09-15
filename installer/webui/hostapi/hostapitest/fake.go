// Package hostapitest liefert ein Backend, das den Vertrag der Schicht 2
// erfuellt, ohne irgendetwas zu tun. Es traegt die Handler-Tests dieses
// Moduls, den Nachweis der Wirt-Neutralitaet (Abnahmekriterium 10) und die
// Browser-Tests aus Plan C-II - und ist zugleich die Vorlage fuer den
// Dashboard-Wirt aus Plan D.
package hostapitest

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/Developer-Simon/energy-node-webui/hostapi"
)

// FakeStep ist ein Schritt, den ein gespielter Lauf abarbeitet.
type FakeStep struct {
	ID     string
	State  string // "ok", "skip" oder "fail"
	Detail string // Grund bzw. Fehlercode
	Log    []string
}

// FakeBackend ist ein steuerbares Backend. Jedes Feld darf vor dem Start
// gesetzt werden; die Zeiger-Felder mit Fehlern erzwingen den Fehlerpfad.
type FakeBackend struct {
	mu sync.Mutex

	Description   hostapi.Description
	ConnectResult hostapi.ConnectResult
	ConnectErr    error
	Keypair       hostapi.KeypairResult
	KeypairErr    error
	PrecheckView  *hostapi.Precheck
	PrecheckErr   error
	ManifestView  *hostapi.ManifestView
	ManifestErr   error
	SelectionView *hostapi.SelectionView
	SelectionErr  error
	SaveErr       error
	PlanResult    *hostapi.PlanView
	PlanErr       error
	DiagnoseView  *hostapi.DiagnoseView
	DiagnoseErr   error

	// Steps ist das Drehbuch eines Laufs.
	Steps []FakeStep
	// StepDelay laesst einen Lauf langsam genug sein, um ein Abbrechen zu
	// testen. Vorgabe: keine Pause.
	StepDelay time.Duration
	// RunErr wird nach dem Drehbuch zurueckgegeben.
	RunErr error

	// Aufzeichnung fuer Tests.
	LastConnect   hostapi.ConnectRequest
	LastRun       hostapi.RunRequest
	SavedSelected map[string]bool
	Runs          int
}

// NewFake liefert eine Attrappe mit brauchbaren Vorgaben: ein Installer-Wirt
// mit drei Einstiegspunkten, zwei Schritten im Drehbuch.
func NewFake() *FakeBackend {
	version := "v1.4.2"
	return &FakeBackend{
		Description: hostapi.Description{
			Host:            hostapi.HostInstaller,
			EntryPoints:     []string{"install", "redeploy", "diagnose"},
			NeedsConnection: true,
			BundleVersion:   version,
			BundleArch:      "armv6",
		},
		ConnectResult: hostapi.ConnectResult{Connected: true, Host: "node.local", User: "orgelbau"},
		Keypair:       hostapi.KeypairResult{PublicKey: "ssh-ed25519 AAAA… installer", PrivatePath: "/home/dev/.energy-node/id_ed25519", Installed: true},
		PrecheckView: &hostapi.Precheck{
			OSID: "debian", OSVersionID: "12", Arch: "armv6l", PythonABI: "cp311",
			PythonVersion: "3.11.2", DiskFreeMB: 4096, SudoNopasswd: true, Internet: true,
			OSPrettyName: "Debian GNU/Linux 12 (bookworm)", DiskTotalMB: 29700, Timezone: "Europe/Berlin",
			ArchOK: true, PythonABIOK: true, DiskOK: true,
		},
		ManifestView: &hostapi.ManifestView{
			BundleVersion: version, Arch: "armv6", PythonABI: "cp311",
			TargetUser: "orgelbau", TargetBase: "/opt/energy-node",
			Components: map[string]string{"dashboard": "v2.1.0", "services": "v1.9.0"},
			Steps: []hostapi.StepView{
				{ID: "10"},
				{ID: "40", Optional: true, Default: true},
				{ID: "85", ServiceID: "tuya", Dir: "tuya_bridge", Unit: "energy-node-tuya.service", Optional: true, Default: true, Kind: "device"},
			},
			HasCaddy:    true,
			BundleBytes: 43_000_000, WheelCount: 12, UnitCount: 8, TemplateCount: 7,
		},
		SelectionView: &hostapi.SelectionView{Steps: map[string]bool{"40": true, "85": true}, Source: "manifest-default"},
		PlanResult: &hostapi.PlanView{
			BundleVersion: version,
			Steps:         []hostapi.PlanStep{{ID: "10", State: "done"}, {ID: "40", State: "pending", Optional: true, Selected: true}},
			Components:    map[string]hostapi.ComponentDelta{"dashboard": {From: strptr("v2.0.0"), To: "v2.1.0"}},
		},
		DiagnoseView: &hostapi.DiagnoseView{
			BundleVersion: version,
			Units:         map[string]string{"energy-node-dashboard.service": "active"},
			Ports:         map[string]bool{"1883": true, "8080": true},
			Checks:        []hostapi.Check{{Name: "unit energy-node-dashboard.service", OK: true, Detail: "active", Group: "system", Subject: "energy-node-dashboard.service"}},
		},
		Steps: []FakeStep{
			{ID: "10", State: "ok", Log: []string{"apt: nothing to do"}},
			{ID: "40", State: "skip", Detail: "nicht ausgewaehlt"},
		},
	}
}

func strptr(s string) *string { return &s }

// Script ersetzt das Drehbuch eines Laufs.
func (f *FakeBackend) Script(steps ...FakeStep) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Steps = steps
}

func (f *FakeBackend) Describe() hostapi.Description { return f.Description }

func (f *FakeBackend) Connect(ctx context.Context, req hostapi.ConnectRequest) (hostapi.ConnectResult, error) {
	f.mu.Lock()
	f.LastConnect = req
	f.mu.Unlock()
	if f.ConnectErr != nil {
		return hostapi.ConnectResult{}, f.ConnectErr
	}
	return f.ConnectResult, nil
}

func (f *FakeBackend) GenerateKeypair(ctx context.Context) (hostapi.KeypairResult, error) {
	if f.KeypairErr != nil {
		return hostapi.KeypairResult{}, f.KeypairErr
	}
	return f.Keypair, nil
}

func (f *FakeBackend) Precheck(ctx context.Context) (*hostapi.Precheck, error) {
	return f.PrecheckView, f.PrecheckErr
}

func (f *FakeBackend) Manifest(ctx context.Context) (*hostapi.ManifestView, error) {
	return f.ManifestView, f.ManifestErr
}

func (f *FakeBackend) Selection(ctx context.Context) (*hostapi.SelectionView, error) {
	return f.SelectionView, f.SelectionErr
}

func (f *FakeBackend) SaveSelection(ctx context.Context, steps map[string]bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.SaveErr != nil {
		return f.SaveErr
	}
	f.SavedSelected = steps
	if f.SelectionView != nil {
		f.SelectionView.Steps = steps
	}
	return nil
}

func (f *FakeBackend) Plan(ctx context.Context) (*hostapi.PlanView, error) {
	return f.PlanResult, f.PlanErr
}

// Run spielt das Drehbuch ab und meldet jeden Schritt ueber sink. Ein
// abgebrochener Kontext beendet den Lauf mit context.Canceled - genau wie ein
// echter Wirt, dessen SSH-Sitzung abgebrochen wird.
func (f *FakeBackend) Run(ctx context.Context, req hostapi.RunRequest, sink hostapi.Sink) error {
	f.mu.Lock()
	f.LastRun = req
	f.Runs++
	steps := append([]FakeStep(nil), f.Steps...)
	delay := f.StepDelay
	runErr := f.RunErr
	f.mu.Unlock()

	for _, step := range steps {
		if err := sleepCtx(ctx, delay); err != nil {
			return err
		}
		sink.Marker(step.ID, "begin", "")
		for _, line := range step.Log {
			sink.Log(step.ID, line)
		}
		state := step.State
		if state == "" {
			state = "ok"
		}
		sink.Marker(step.ID, state, step.Detail)
		if state == "fail" {
			return &hostapi.Error{Code: step.Detail, Detail: "step " + step.ID + " failed"}
		}
	}
	return runErr
}

func (f *FakeBackend) Diagnose(ctx context.Context) (*hostapi.DiagnoseView, error) {
	return f.DiagnoseView, f.DiagnoseErr
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			return nil
		}
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// ErrNotConnected ist der Fehler, den ein Wirt liefert, wenn eine Verbindung
// fehlt. Tests setzen ihn in ConnectErr oder PrecheckErr.
var ErrNotConnected = errors.New("not connected")
