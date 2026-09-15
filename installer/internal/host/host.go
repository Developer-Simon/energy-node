// Package host ist der Installer-Wirt: es bindet die Oberflaeche (Modul webui,
// Schicht 2) an Schicht 1 (SSH, Bundle, Schritte, Diagnose). Es enthaelt
// selbst keine Installationslogik - jede Methode reicht an ein Paket aus
// internal/ weiter und formt dessen Ergebnis in einen Anzeigetyp um.
package host

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"

	"github.com/Developer-Simon/energy-node-installer/internal/bundle"
	"github.com/Developer-Simon/energy-node-installer/internal/diag"
	"github.com/Developer-Simon/energy-node-installer/internal/selection"
	"github.com/Developer-Simon/energy-node-installer/internal/steps"
	"github.com/Developer-Simon/energy-node-installer/internal/transport"
	"github.com/Developer-Simon/energy-node-webui/hostapi"
)

// MinDiskFreeMB ist die Schwelle, unterhalb derer die Vorpruefung warnt.
// Bundle, entpackte Wheels und das Dashboard-Binary zusammen liegen deutlich
// darunter; die Reserve deckt apt-Caches ab.
const MinDiskFreeMB = 500

// Config sind die Pfade, mit denen der Wirt arbeitet.
type Config struct {
	// BundleDir ist das entpackte Bundle auf dem Rechner des Betreibers.
	BundleDir string
	// RemoteBundleDir und RemoteStateDir sind die Pfade auf dem Node.
	RemoteBundleDir string
	RemoteStateDir  string
	// KnownHostsPath ist der TOFU-Speicher.
	KnownHostsPath string
	// IdentityDir ist das Verzeichnis fuer erzeugte Schluesselpaare.
	IdentityDir string
}

// Host erfuellt hostapi.Backend ueber SSH.
type Host struct {
	cfg      Config
	manifest *bundle.Manifest

	mu      sync.Mutex
	client  *transport.Client
	target  hostapi.ConnectRequest
	pending *selection.Selection
}

// New liest das Manifest des Bundles und baut den Wirt.
func New(cfg Config) (*Host, error) {
	if cfg.RemoteBundleDir == "" {
		cfg.RemoteBundleDir = "/var/lib/energy-node-installer/bundle"
	}
	if cfg.RemoteStateDir == "" {
		cfg.RemoteStateDir = "/var/lib/energy-node-installer"
	}
	manifest, err := bundle.LoadManifest(cfg.BundleDir)
	if err != nil {
		return nil, err
	}
	return &Host{cfg: cfg, manifest: manifest}, nil
}

func (h *Host) Describe() hostapi.Description {
	return hostapi.Description{
		Host:            hostapi.HostInstaller,
		EntryPoints:     []string{"install", "redeploy", "diagnose"},
		NeedsConnection: true,
		BundleVersion:   h.manifest.Version,
		BundleArch:      h.manifest.Arch,
	}
}

func (h *Host) Connect(ctx context.Context, req hostapi.ConnectRequest) (hostapi.ConnectResult, error) {
	store := transport.NewHostKeyStore(h.cfg.KnownHostsPath)

	var offered string
	callback, err := store.Callback(func(hostname, fingerprint string) (bool, error) {
		offered = fingerprint
		// Bestaetigt der Betreiber genau diesen Fingerabdruck, wird gepinnt;
		// sonst wird abgelehnt und die Oberflaeche zeigt ihn zur Bestaetigung.
		return req.AcceptFingerprint != "" && req.AcceptFingerprint == fingerprint, nil
	})
	if err != nil {
		return hostapi.ConnectResult{}, &hostapi.Error{Code: "BACKEND_ERROR", Detail: err.Error()}
	}

	cfg := transport.Config{Host: req.Host, User: req.User, HostKeyCallback: callback}
	switch req.Kind {
	case hostapi.AuthKey:
		key, err := readKeyFile(req.KeyPath)
		if err != nil {
			return hostapi.ConnectResult{}, &hostapi.Error{Code: "KEYFILE_UNREADABLE", Detail: err.Error(), Status: http.StatusBadRequest}
		}
		cfg.PrivateKeyPEM = key
	default:
		cfg.Password = req.Secret
	}

	client, err := transport.Dial(ctx, cfg)
	if err != nil {
		if offered != "" && req.AcceptFingerprint == "" {
			return hostapi.ConnectResult{}, &hostapi.Error{Code: "HOSTKEY_UNKNOWN", Detail: offered, Status: http.StatusConflict}
		}
		if strings.Contains(err.Error(), "key mismatch") || strings.Contains(err.Error(), "knownhosts: key mismatch") {
			return hostapi.ConnectResult{}, &hostapi.Error{Code: "HOSTKEY_CHANGED", Detail: err.Error(), Status: http.StatusConflict}
		}
		return hostapi.ConnectResult{}, &hostapi.Error{Code: "AUTH_FAILED", Detail: err.Error(), Status: http.StatusUnauthorized}
	}

	h.mu.Lock()
	if h.client != nil {
		_ = h.client.Close()
	}
	h.client = client
	h.target = req
	h.target.Secret = "" // nie festhalten, was nicht festgehalten werden muss
	h.mu.Unlock()

	return hostapi.ConnectResult{Connected: true, Host: req.Host, User: req.User}, nil
}

func (h *Host) GenerateKeypair(ctx context.Context) (hostapi.KeypairResult, error) {
	client, err := h.connected()
	if err != nil {
		return hostapi.KeypairResult{}, err
	}
	privatePath, publicLine, err := GenerateEd25519(h.cfg.IdentityDir)
	if err != nil {
		return hostapi.KeypairResult{}, &hostapi.Error{Code: "BACKEND_ERROR", Detail: err.Error()}
	}
	// Anhaengen statt ersetzen, und ohne Duplikat: grep -qxF prueft die ganze
	// Zeile. Ein zweiter Lauf legt keine zweite Zeile an.
	command := transport.BuildCommand(nil, fmt.Sprintf(
		"mkdir -p ~/.ssh && chmod 700 ~/.ssh && touch ~/.ssh/authorized_keys && chmod 600 ~/.ssh/authorized_keys && "+
			"grep -qxF %s ~/.ssh/authorized_keys || printf '%%s\\n' %s >> ~/.ssh/authorized_keys",
		transport.ShellQuote(publicLine), transport.ShellQuote(publicLine)))
	var out strings.Builder
	if err := client.Run(ctx, command, &out, &out); err != nil {
		return hostapi.KeypairResult{}, &hostapi.Error{Code: "AUTHORIZED_KEYS_FAILED", Detail: out.String()}
	}
	return hostapi.KeypairResult{PublicKey: publicLine, PrivatePath: privatePath, Installed: true}, nil
}

// PreflightFacts ist die Ausgabe von preflight.sh (Vertrag 4).
type PreflightFacts struct {
	OSID                   string `json:"os_id"`
	OSVersionID            string `json:"os_version_id"`
	Arch                   string `json:"arch"`
	PythonABI              string `json:"python_abi"`
	PythonVersion          string `json:"python_version"`
	DiskFreeMB             int64  `json:"disk_free_mb"`
	SudoNopasswd           bool   `json:"sudo_nopasswd"`
	Internet               bool   `json:"internet"`
	Installed              bool   `json:"installed"`
	InstalledBundleVersion string `json:"installed_bundle_version"`
	OSPrettyName           string `json:"os_pretty_name"`
	DiskTotalMB            int64  `json:"disk_total_mb"`
	Timezone               string `json:"timezone"`
}

// EvaluatePreflight bewertet die Tatsachen des Node gegen das Manifest. Das
// ist die einzige eigene Entscheidung dieses Pakets - und sie ist bewusst eine
// reine Funktion, damit sie ohne SSH pruefbar ist.
func (h *Host) EvaluatePreflight(facts PreflightFacts) *hostapi.Precheck {
	view := &hostapi.Precheck{
		OSID: facts.OSID, OSVersionID: facts.OSVersionID, Arch: facts.Arch,
		PythonABI: facts.PythonABI, PythonVersion: facts.PythonVersion,
		DiskFreeMB: facts.DiskFreeMB, SudoNopasswd: facts.SudoNopasswd,
		Internet: facts.Internet, Installed: facts.Installed,
		InstalledBundleVersion: facts.InstalledBundleVersion,
		OSPrettyName:           facts.OSPrettyName, DiskTotalMB: facts.DiskTotalMB, Timezone: facts.Timezone,
	}
	for _, machine := range h.manifest.UnameMachine {
		if machine == facts.Arch {
			view.ArchOK = true
			break
		}
	}
	view.PythonABIOK = h.manifest.PythonABI == "" || h.manifest.PythonABI == facts.PythonABI
	view.DiskOK = facts.DiskFreeMB >= MinDiskFreeMB

	if !view.ArchOK {
		view.Blocking = append(view.Blocking, "ARCH_MISMATCH")
	}
	if !view.PythonABIOK {
		view.Blocking = append(view.Blocking, "PYTHON_ABI_MISMATCH")
	}
	if !view.DiskOK {
		view.Warnings = append(view.Warnings, "DISK_LOW")
	}
	if !facts.SudoNopasswd {
		view.Warnings = append(view.Warnings, "SUDO_PASSWORD_REQUIRED")
	}
	if !facts.Internet {
		view.Warnings = append(view.Warnings, "NO_INTERNET")
	}
	if facts.Timezone == "Etc/UTC" || facts.Timezone == "UTC" {
		view.Warnings = append(view.Warnings, "TIMEZONE_UTC")
	}
	return view
}

func (h *Host) Precheck(ctx context.Context) (*hostapi.Precheck, error) {
	client, err := h.connected()
	if err != nil {
		return nil, err
	}
	// /var/lib is root-owned 0755, so on a node that has never had this
	// installer run before, plain SFTP's mkdir of RemoteStateDir (for
	// preflight.sh below, and later for the selection and secrets steps
	// upload) fails with EACCES. Grant the connecting user ownership first,
	// exactly like the developer CLI does before its own first upload
	// (internal/devcli.defaultProvisionRemoteStateDir) - passwordless sudo is
	// already a hard precondition of this whole installer.
	if err := provisionRemoteStateDir(ctx, client, h.cfg.RemoteStateDir); err != nil {
		return nil, &hostapi.Error{Code: "PREFLIGHT_UPLOAD_FAILED", Detail: err.Error()}
	}
	// preflight.sh liegt im Bundle unter bootstrap/ - make_bundle.sh kopiert
	// scripts/bootstrap/ vollstaendig dorthin, eine Aenderung an Komponente B
	// braucht es dafuer nicht.
	local := path.Join(h.cfg.BundleDir, "bootstrap", "preflight.sh")
	remote := path.Join(h.cfg.RemoteStateDir, "preflight.sh")
	if err := client.UploadFile(local, remote, 0o755); err != nil {
		return nil, &hostapi.Error{Code: "PREFLIGHT_UPLOAD_FAILED", Detail: err.Error()}
	}
	var stdout, stderr strings.Builder
	command := transport.BuildCommand(map[string]string{"EN_STATE_DIR": h.cfg.RemoteStateDir}, "bash "+remote)
	if err := client.Run(ctx, command, &stdout, &stderr); err != nil {
		return nil, &hostapi.Error{Code: "PREFLIGHT_FAILED", Detail: stderr.String()}
	}
	var facts PreflightFacts
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout.String())), &facts); err != nil {
		return nil, &hostapi.Error{Code: "PREFLIGHT_UNREADABLE", Detail: err.Error()}
	}
	return h.EvaluatePreflight(facts), nil
}

func (h *Host) Manifest(ctx context.Context) (*hostapi.ManifestView, error) {
	view := &hostapi.ManifestView{
		BundleVersion: h.manifest.Version,
		PythonABI:     h.manifest.PythonABI,
		TargetUser:    h.manifest.TargetUser,
		TargetBase:    h.manifest.TargetBase,
		Components:    h.manifest.Components,
		HasCaddy:      h.manifest.Caddy != nil,
	}
	if len(h.manifest.UnameMachine) > 0 {
		view.Arch = h.manifest.UnameMachine[0]
	}
	for _, step := range h.manifest.Steps {
		view.Steps = append(view.Steps, hostapi.StepView{
			ID: step.ID, ServiceID: step.ServiceID, Dir: step.Dir, Unit: step.Unit,
			Optional: step.Optional, Default: step.Default, Kind: step.Kind,
		})
	}
	view.BundleBytes, view.WheelCount, view.UnitCount, view.TemplateCount = bundleStats(h.cfg.BundleDir, h.manifest.Files)
	return view, nil
}

func (h *Host) Selection(ctx context.Context) (*hostapi.SelectionView, error) {
	if current := h.currentSelection(); current != nil {
		return current, nil
	}
	defaults := selection.DefaultFor(h.manifest)
	return &hostapi.SelectionView{Steps: defaults.Steps, Source: "manifest-default"}, nil
}

func (h *Host) SaveSelection(ctx context.Context, stepsMap map[string]bool) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.pending = &selection.Selection{Steps: stepsMap}
	return nil
}

func (h *Host) Plan(ctx context.Context) (*hostapi.PlanView, error) {
	client, err := h.connected()
	if err != nil {
		return nil, err
	}
	preview, err := steps.Preview(ctx, client, h.cfg.RemoteBundleDir, h.cfg.RemoteStateDir, h.manifest.Version)
	if err != nil {
		return nil, &hostapi.Error{Code: "PLAN_FAILED", Detail: err.Error()}
	}
	view := &hostapi.PlanView{BundleVersion: preview.BundleVersion, Components: map[string]hostapi.ComponentDelta{}}
	for _, step := range preview.Steps {
		view.Steps = append(view.Steps, hostapi.PlanStep{
			ID: step.ID, State: step.State, Optional: step.Optional,
			Selected: step.Selected, Unit: step.Unit,
		})
	}
	for name, versions := range preview.Components {
		view.Components[name] = hostapi.ComponentDelta{From: versions.From, To: versions.To}
	}
	return view, nil
}

func (h *Host) Run(ctx context.Context, req hostapi.RunRequest, sink hostapi.Sink) error {
	client, err := h.connected()
	if err != nil {
		return err
	}
	list := h.manifest.Steps
	if req.Only != "" {
		entry, ok := h.manifest.StepByID(req.Only)
		if !ok {
			return &hostapi.Error{Code: "UNKNOWN_STEP", Detail: req.Only, Status: http.StatusBadRequest}
		}
		list = []bundle.StepEntry{entry}
	}

	opts := steps.RunOptions{
		Client:          client,
		RemoteBundleDir: h.cfg.RemoteBundleDir,
		RemoteStateDir:  h.cfg.RemoteStateDir,
		BundleVersion:   h.manifest.Version,
		TargetUser:      firstNonEmpty(req.TargetUser, h.manifest.TargetUser),
		TargetBase:      firstNonEmpty(req.TargetBase, h.manifest.TargetBase),
		MQTTUser:        req.MQTTUser,
		Steps:           list,
		Selection:       h.selectionForRun(),
		Secrets:         &steps.Secrets{MQTTPassword: req.MQTTPassword, AdminPassword: req.AdminPassword},
		OnMarker: func(marker steps.Marker) {
			sink.Marker(marker.StepID, markerState(marker.Kind), marker.Detail)
		},
		OnLog: func(stepID, line string) { sink.Log(stepID, line) },
	}
	if err := steps.Run(ctx, opts); err != nil {
		var failure *steps.StepFailure
		if errors.As(err, &failure) {
			return &hostapi.Error{Code: failure.Code, Detail: "Schritt " + failure.StepID}
		}
		return err
	}
	return nil
}

func (h *Host) Diagnose(ctx context.Context) (*hostapi.DiagnoseView, error) {
	client, err := h.connected()
	if err != nil {
		return nil, err
	}
	report, err := diag.Run(ctx, client, h.cfg.RemoteBundleDir, h.cfg.RemoteStateDir, h.manifest.Version)
	if err != nil {
		return nil, &hostapi.Error{Code: "DIAGNOSE_FAILED", Detail: err.Error()}
	}
	view := &hostapi.DiagnoseView{
		BundleVersion: report.BundleVersion,
		Units:         report.Units,
		Ports:         report.Ports,
	}
	for _, check := range report.Checklist(h.manifest.Steps) {
		view.Checks = append(view.Checks, hostapi.Check{
			Name: check.Name, OK: check.OK, Detail: check.Detail, RetryStepID: check.RetryStepID,
			Group: check.Group, Subject: check.Subject,
		})
	}
	return view, nil
}

func (h *Host) connected() (*transport.Client, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.client == nil {
		return nil, &hostapi.Error{Code: "NOT_CONNECTED", Status: http.StatusConflict}
	}
	return h.client, nil
}

// currentSelection liest selection.json vom Node, wenn eine Verbindung
// besteht. Fehlt die Datei auf dem Node oder besteht keine Verbindung, ist
// das kein Fehler - eine frische Installation hat noch keine eigene Auswahl.
func (h *Host) currentSelection() *hostapi.SelectionView {
	h.mu.Lock()
	client := h.client
	h.mu.Unlock()
	if client == nil {
		return nil
	}
	tmp, err := os.CreateTemp("", "energy-node-selection-*.json")
	if err != nil {
		return nil
	}
	tmp.Close()
	defer os.Remove(tmp.Name())

	remote := path.Join(h.cfg.RemoteStateDir, "selection.json")
	if err := client.DownloadFile(remote, tmp.Name()); err != nil {
		return nil
	}
	sel, err := selection.Load(tmp.Name())
	if err != nil {
		return nil
	}
	return &hostapi.SelectionView{Steps: sel.Steps, Source: "node"}
}

// selectionForRun liefert die Auswahl, mit der der naechste Lauf startet:
// die in dieser Sitzung gemerkte (PUT /api/selection), sonst die vom Node,
// sonst die Vorgaben des Manifests.
func (h *Host) selectionForRun() *selection.Selection {
	h.mu.Lock()
	pending := h.pending
	h.mu.Unlock()
	if pending != nil {
		return pending
	}
	if current := h.currentSelection(); current != nil {
		return &selection.Selection{Steps: current.Steps}
	}
	return selection.DefaultFor(h.manifest)
}

// readKeyFile liest eine private Schluesseldatei vom Rechner des
// Betreibers (Kind == AuthKey).
func readKeyFile(keyPath string) ([]byte, error) {
	data, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, fmt.Errorf("host: Schluesseldatei %s: %w", keyPath, err)
	}
	return data, nil
}

func markerState(kind steps.Kind) string {
	switch kind {
	case steps.Begin:
		return "begin"
	case steps.OK:
		return "ok"
	case steps.Skip:
		return "skip"
	default:
		return "fail"
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

// provisionRemoteStateDir is a seam for tests; production always runs
// defaultProvisionRemoteStateDir.
var provisionRemoteStateDir = defaultProvisionRemoteStateDir

// defaultProvisionRemoteStateDir grants the connecting user ownership of dir
// before anything is written under it. install -d is idempotent, so calling
// this on every Precheck is safe even once the directory exists and is
// already owned correctly.
func defaultProvisionRemoteStateDir(ctx context.Context, client *transport.Client, dir string) error {
	cmd := fmt.Sprintf(`sudo install -d -o "$(id -un)" -g "$(id -un)" -m 0755 %s`, transport.ShellQuote(dir))
	var discard, stderr strings.Builder
	if err := client.Run(ctx, cmd, &discard, &stderr); err != nil {
		return fmt.Errorf("%w (stderr: %s)", err, stderr.String())
	}
	return nil
}

// bundleStats zaehlt, was die Karte "Was uebertragen wird" zeigt. Eine
// fehlende Datei zaehlt mit, traegt aber keine Bytes bei - die Pruefsummen
// verifiziert ohnehin bundle.Verify, nicht diese Anzeige.
func bundleStats(dir string, files map[string]string) (total int64, wheels, units, templates int) {
	for rel := range files {
		switch {
		case strings.HasPrefix(rel, "wheels/"):
			wheels++
		case strings.HasPrefix(rel, "config/"):
			templates++
		}
		if strings.HasSuffix(rel, ".service") {
			units++
		}
		if info, err := os.Stat(filepath.Join(dir, filepath.FromSlash(rel))); err == nil {
			total += info.Size()
		}
	}
	return total, wheels, units, templates
}
