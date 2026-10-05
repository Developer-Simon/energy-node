package main

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/Developer-Simon/energy-node-webui/hostapi"
	"github.com/Developer-Simon/energy-node-webui/hostapi/hostapitest"
)

// fingerprint ist der Host-Schluessel der Vorlage.
const fingerprint = "SHA256:xK9v+Lm2Qd8pR4tN1wZa7YcB3fH6jE0sU5gV2nP8kQo"

type options struct {
	dashboard bool
	trusted   bool
	holdStep  string
	failStep  string
	failCode  string
	stepDelay time.Duration
}

// stagedBackend ist die Attrappe aus hostapitest mit zwei Zusaetzen, die nur
// ein Testwirt braucht: der TOFU-Dialog beim ersten Verbinden und ein
// Schritt, der bis zum Abbrechen haengt (die Ausfuehrungs-Vorlage zeigt
// Tailscale beim Warten auf die Anmeldung).
type stagedBackend struct {
	*hostapitest.FakeBackend
	opts    options
	mu      sync.Mutex
	trusted bool
}

func (b *stagedBackend) Connect(ctx context.Context, req hostapi.ConnectRequest) (hostapi.ConnectResult, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.trusted && req.AcceptFingerprint != fingerprint {
		return hostapi.ConnectResult{}, &hostapi.Error{Code: "HOSTKEY_UNKNOWN", Detail: fingerprint, Status: http.StatusConflict}
	}
	b.trusted = true
	return b.FakeBackend.Connect(ctx, req)
}

func (b *stagedBackend) Run(ctx context.Context, req hostapi.RunRequest, sink hostapi.Sink) error {
	// Delegate prepare runs to FakeBackend which handles them correctly
	if req.Mode == hostapi.ModePrepare {
		return b.FakeBackend.Run(ctx, req, sink)
	}

	for _, step := range b.Steps {
		if req.Only != "" && step.ID != req.Only {
			continue
		}
		if step.ID == webhookStep && b.webhookChosen() {
			step = hostapitest.FakeStep{ID: webhookStep, Log: []string{"Firewall: 8082/tcp fuer den Shelly-Wake-Webhook freigegeben"}}
		}
		if err := pause(ctx, b.opts.stepDelay); err != nil {
			return err
		}
		sink.Marker(step.ID, "begin", "")
		for _, line := range step.Log {
			if err := pause(ctx, b.opts.stepDelay/4); err != nil {
				return err
			}
			sink.Log(step.ID, line)
		}
		if step.ID == b.opts.holdStep {
			<-ctx.Done()
			return ctx.Err()
		}
		if step.ID == b.opts.failStep {
			sink.Marker(step.ID, "fail", b.opts.failCode)
			return &hostapi.Error{Code: b.opts.failCode, Detail: "step " + step.ID + " failed"}
		}
		state := step.State
		if state == "" {
			state = "ok"
		}
		sink.Marker(step.ID, state, step.Detail)
	}
	return nil
}

// systemUpgrade spielt im Update einen neuen Kernel ein, damit Ergebnis und
// Lauf den Neustart-Hinweis zeigen (scripts/bootstrap/15-system-upgrade.sh).
// Die Erstinstallation bleibt ohne, ihr Ergebnis liegt wie die Vorlage.
var systemUpgrade = hostapitest.FakeStep{ID: "15", Detail: "neustart noetig", Log: []string{
	"Aktualisiere 3 Pakete: libssl3 openssl raspberrypi-kernel",
	"Ein Neustart des Node ist noetig, damit alle Updates wirken.",
}}

// webhookStep ist die Opt-in-Freigabe des Shelly-Wake-Webhooks; sie laeuft
// nur mit dem Shelly-Dienst (83), wie auf einem echten Node.
const webhookStep = "35"

// webhookChosen folgt der zuletzt gespeicherten Auswahl, damit Ausfuehrung
// und Diagnose zeigen, was die Konfiguration eingestellt hat.
func (b *stagedBackend) webhookChosen() bool {
	if b.SelectionView == nil {
		return false
	}
	return b.SelectionView.Steps[webhookStep] && b.SelectionView.Steps["83"]
}

// Diagnose ergaenzt die feste Pruefliste um den Shelly-Wake-Webhook, sobald
// er gewaehlt ist: Firewall-Regel gesetzt, der Webhook im Dashboard aber noch
// aus - der Zustand direkt nach der Installation, also ein Hinweis.
func (b *stagedBackend) Diagnose(ctx context.Context) (*hostapi.DiagnoseView, error) {
	view, err := b.FakeBackend.Diagnose(ctx)
	if err != nil || view == nil || !b.webhookChosen() {
		return view, err
	}
	out := *view
	out.Checks = append(append([]hostapi.Check{}, view.Checks...),
		hostapi.Check{Name: "shelly webhook firewall 8082", OK: true, Detail: "allowed", RetryStepID: webhookStep, Group: "system", Subject: "shelly-webhook-firewall:8082"},
		hostapi.Check{Name: "shelly webhook listener 8082", OK: false, Detail: "not listening", Group: "system", Subject: "shelly-webhook-listener:8082", Severity: "warn"},
	)
	return &out, nil
}

func pause(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(d):
		return nil
	}
}

func strptr(s string) *string { return &s }

func newScenario(name string, opts options) *stagedBackend {
	fake := hostapitest.NewFake()
	update := name == "vorlage-update"

	fake.Description = hostapi.Description{
		Host: hostapi.HostInstaller, EntryPoints: []string{"install", "redeploy", "diagnose"},
		NeedsConnection: true, BundleVersion: "v1.4.2", BundleArch: "armv6",
		Package: &hostapi.PackageInfo{
			Bundled: &hostapi.BundledInfo{Version: "v1.4.2", Arch: "armv6"},
			Repo:    hostapi.RepoInfo{Available: true, Path: "/home/dev/energy-node"},
		},
	}
	if opts.dashboard {
		fake.Description.Host = hostapi.HostDashboard
		fake.Description.NeedsConnection = false
		fake.Description.EntryPoints = []string{"redeploy", "diagnose"}
	}
	fake.ConnectResult = hostapi.ConnectResult{Connected: true, Host: "energy-node.local", User: "pi"}

	fake.PrecheckView = &hostapi.Precheck{
		OSID: "debian", OSVersionID: "12", OSPrettyName: "Raspberry Pi OS Lite 12 (bookworm)",
		Arch: "armv6l", PythonABI: "cp311", PythonVersion: "3.11.2",
		DiskFreeMB: 12698, DiskTotalMB: 30413, SudoNopasswd: true, Internet: true,
		Timezone: "Etc/UTC", ArchOK: true, PythonABIOK: true, DiskOK: true,
		Blocking: []string{}, Warnings: []string{"TIMEZONE_UTC"},
	}

	steps := []hostapi.StepView{
		{ID: "10"},
		{ID: "15", Optional: true, Default: true},
		{ID: "20"}, {ID: "30"},
		{ID: "35", Optional: true, Default: false, Requires: "83"},
		{ID: "40", Optional: true, Default: true},
		{ID: "50"}, {ID: "60"},
		{ID: "70", Optional: true, Default: true},
		{ID: "81", ServiceID: "apsystems", Unit: "apsystems-ez1.service", Optional: true, Default: true, Kind: "device"},
		{ID: "82", ServiceID: "battery_soc", Unit: "battery-soc.service", Optional: true, Default: false, Kind: "device"},
		{ID: "83", ServiceID: "shelly", Unit: "shelly-rpc.service", Optional: true, Default: true, Kind: "device"},
		{ID: "84", ServiceID: "trucki", Unit: "trucki-http.service", Optional: true, Default: true, Kind: "device"},
		{ID: "85", ServiceID: "tuya", Unit: "tuya.service", Optional: true, Default: true, Kind: "device"},
		{ID: "88", ServiceID: "automation", Unit: "automation.service", Optional: true, Default: true, Kind: "service"},
	}
	if update {
		steps = append(steps, hostapi.StepView{ID: "89", ServiceID: "modbus", Unit: "modbus.service", Optional: true, Default: false, Kind: "service"})
	}
	fake.ManifestView = &hostapi.ManifestView{
		BundleVersion: "v1.4.2", Arch: "armv6", PythonABI: "cp311",
		TargetUser: "energynode", TargetBase: "/home/energynode",
		Components: map[string]string{
			"bootstrap": "v1.4.2", "dashboard": "1.4.2", "services": "3.7.1",
			"energy_node_common": "1.4.2", "battery_soc_core": "0.9.3",
			"tinytuya": "1.16.0", "paho-mqtt": "2.1.0",
		},
		Steps: steps, HasCaddy: true,
		BundleBytes: 41 * 1024 * 1024, WheelCount: 12, UnitCount: 8, TemplateCount: 7,
	}

	selected := map[string]bool{"15": true, "35": false, "40": true, "70": true, "81": true, "82": false, "83": true, "84": true, "85": true, "88": true}
	fake.SelectionView = &hostapi.SelectionView{Steps: selected, Source: "manifest-default"}

	if update {
		fake.PrecheckView.Installed = true
		fake.PrecheckView.InstalledBundleVersion = "v1.4.1"
		fake.SelectionView.Source = "node"
		// Die Auswahl auf dem Node stammt von vor Schritt 15: der Schalter
		// steht trotzdem an (Manifest-Vorgabe eines Systemschritts).
		delete(selected, "15")
		fake.PlanResult = &hostapi.PlanView{
			BundleVersion: "v1.4.2",
			Steps: []hostapi.PlanStep{
				{ID: "10", State: "done"},
				{ID: "15", State: "pending", Optional: true, Selected: true},
				{ID: "20", State: "done"}, {ID: "30", State: "done"},
				{ID: "35", State: "deselected", Optional: true},
				{ID: "40", State: "done", Optional: true, Selected: true},
				{ID: "50", State: "pending"}, {ID: "60", State: "pending"},
				{ID: "70", State: "done", Optional: true, Selected: true},
				{ID: "81", State: "done", Optional: true, Selected: true, Unit: "apsystems-ez1.service", From: "v1.0.2", To: "v1.0.2"},
				{ID: "82", State: "deselected", Optional: true, Unit: "battery-soc.service"},
				{ID: "83", State: "done", Optional: true, Selected: true, Unit: "shelly-rpc.service", From: "v1.0.4", To: "v1.0.4"},
				{ID: "84", State: "done", Optional: true, Selected: true, Unit: "trucki-http.service", From: "v1.0.1", To: "v1.0.1"},
				{ID: "85", State: "pending", Optional: true, Selected: true, Unit: "tuya.service", From: "v1.0.0", To: "v1.0.1", Restart: "version"},
				{ID: "88", State: "pending", Optional: true, Selected: true, Unit: "automation.service", From: "v1.0.0", To: "v1.0.0", Restart: "library"},
				{ID: "89", State: "deselected", Optional: true, Unit: "modbus.service"},
			},
			Components: map[string]hostapi.ComponentDelta{
				"bootstrap":          {From: strptr("v1.4.1"), To: "v1.4.2"},
				"dashboard":          {From: strptr("1.4.1"), To: "1.4.2"},
				"services":           {From: strptr("3.6.0"), To: "3.7.1"},
				"energy_node_common": {From: strptr("1.4.0"), To: "1.4.2"},
				"battery_soc_core":   {From: strptr("0.9.3"), To: "0.9.3"},
				"tinytuya":           {From: strptr("1.15.1"), To: "1.16.0"},
				"paho-mqtt":          {From: strptr("2.1.0"), To: "2.1.0"},
			},
		}
	}

	fake.ChangelogView = fixtureChangelog(update)

	units := map[string]string{
		"apsystems-ez1.service": "active", "battery-soc.service": "active", "shelly-rpc.service": "failed",
		"trucki-http.service": "active", "tuya.service": "active", "automation.service": "active",
		"mosquitto.service": "active", "energy-node-dashboard.service": "active",
		"tailscaled.service": "active", "caddy.service": "active",
	}
	fake.DiagnoseView = &hostapi.DiagnoseView{
		BundleVersion: "v1.4.2", Units: units,
		Ports: map[string]bool{"1883": true, "8080": true, "443": true},
		Checks: []hostapi.Check{
			{Name: "unit apsystems-ez1.service", OK: true, Detail: "active", RetryStepID: "81", Group: "services", Subject: "apsystems-ez1.service"},
			{Name: "unit automation.service", OK: true, Detail: "active", RetryStepID: "88", Group: "services", Subject: "automation.service"},
			{Name: "unit battery-soc.service", OK: true, Detail: "active", RetryStepID: "82", Group: "services", Subject: "battery-soc.service"},
			{Name: "unit caddy.service", OK: true, Detail: "active", RetryStepID: "70", Group: "system", Subject: "caddy.service"},
			{Name: "unit energy-node-dashboard.service", OK: true, Detail: "active", RetryStepID: "60", Group: "system", Subject: "energy-node-dashboard.service"},
			{Name: "unit mosquitto.service", OK: true, Detail: "active", RetryStepID: "20", Group: "system", Subject: "mosquitto.service"},
			{Name: "unit shelly-rpc.service", OK: false, Detail: "failed", RetryStepID: "83", Group: "services", Subject: "shelly-rpc.service"},
			{Name: "unit tailscaled.service", OK: true, Detail: "active", RetryStepID: "40", Group: "system", Subject: "tailscaled.service"},
			{Name: "unit trucki-http.service", OK: true, Detail: "active", RetryStepID: "84", Group: "services", Subject: "trucki-http.service"},
			{Name: "unit tuya.service", OK: true, Detail: "active", RetryStepID: "85", Group: "services", Subject: "tuya.service"},
			{Name: "port 443", OK: true, Detail: "open", RetryStepID: "70", Group: "system", Subject: "443"},
			{Name: "port 1883", OK: true, Detail: "open", RetryStepID: "20", Group: "system", Subject: "1883"},
			{Name: "port 8080", OK: true, Detail: "open", RetryStepID: "60", Group: "system", Subject: "8080"},
			{Name: "config.json", OK: true, Detail: "present=true", RetryStepID: "60", Group: "config", Subject: "config.json"},
			{Name: "tailscale login", OK: true, Detail: "angemeldet=true", RetryStepID: "40", Group: "system", Subject: "tailscale"},
		},
		Versions: &hostapi.DiagnoseVersions{
			Components: map[string]string{
				"dashboard": "1.4.2", "bootstrap": "v1.0.5", "services": "3.7.1",
				"energy_node_common": "1.4.2", "battery_soc_core": "0.9.3",
				"tinytuya": "1.16.0", "paho-mqtt": "2.1.0",
			},
			Services: map[string]string{
				"apsystems-ez1.service": "v1.0.2", "automation.service": "v1.0.0", "battery-soc.service": "v1.1.0",
				"shelly-rpc.service": "v1.0.4", "trucki-http.service": "v1.0.1", "tuya.service": "v1.0.1",
			},
		},
		Devices: map[string][]hostapi.DeviceEntry{
			"apsystems-ez1.service": {{ID: "ez1_dach", Name: "EZ1 Dach"}},
			"battery-soc.service":   {{ID: "battery_soc", Name: "Batterie-Ladezustand"}},
			"shelly-rpc.service":    {{ID: "plug_kueche", Name: "Plug S+ Küche"}, {ID: "hauptzaehler", Name: "3EM Hauptzähler"}, {ID: "relais_1", Name: "Relais 1"}},
			"trucki-http.service":   {{ID: "trucki", Name: "Trucki Stick"}},
			"tuya.service":          {},
		},
	}

	fake.Steps = []hostapitest.FakeStep{
		{ID: "10", Log: []string{"apt-get install -y mosquitto mosquitto-clients ufw python3-venv", "12 Pakete installiert"}},
		{ID: "15", Log: []string{"Alle Systempakete sind aktuell."}},
		{ID: "20", Log: []string{"mosquitto_passwd -b energynode ***", "/etc/mosquitto/conf.d/default.conf geschrieben", "mosquitto neu gestartet, Testnachricht zugestellt"}},
		{ID: "30", Log: []string{"Regeln: 22/tcp, 1883/tcp, 8080/tcp, 443/tcp", "ufw aktiv"}},
		{ID: "35", State: "skip", Detail: "nicht ausgewaehlt"},
		{ID: "40", Log: []string{
			"tailscale_1.62.0_arm.tgz übertragen (24,1 MB)", "sha256 stimmt mit dem Manifest überein",
			"tailscale, tailscaled nach /usr/sbin kopiert", "tailscaled.service aktiviert und gestartet",
			"To authenticate, visit: https://login.tailscale.com/a/4f2c8ab19de3",
		}},
		{ID: "50", Log: []string{"pip install --no-index --find-links wheels/ (12 Wheels)"}},
		{ID: "60", Log: []string{"energy-node-dashboard 1.4.2 installiert", "auth.pw geschrieben (0640)"}},
		{ID: "70", Log: []string{"caddy validate: Valid configuration"}},
		{ID: "81"}, {ID: "82", State: "skip", Detail: "nicht ausgewaehlt"}, {ID: "83"}, {ID: "84"}, {ID: "85"}, {ID: "88"},
	}
	fake.PrepareNotes = []hostapitest.FakeNote{
		{Key: "package.log.github_search", Args: map[string]string{"arch": "armv6"}},
		{Key: "package.log.download", Args: map[string]string{"name": "energy-node-v1.4.2-armv6.tar.gz"}},
	}
	if update {
		fake.Steps = []hostapitest.FakeStep{
			{ID: "10", State: "skip", Detail: "bereits erledigt"}, systemUpgrade,
			{ID: "20", State: "skip", Detail: "bereits erledigt"},
			{ID: "30", State: "skip", Detail: "bereits erledigt"}, {ID: "35", State: "skip", Detail: "nicht ausgewaehlt"},
			{ID: "40", State: "skip", Detail: "bereits erledigt"},
			{ID: "50", Log: []string{"tinytuya 1.15.1 -> 1.16.0"}}, {ID: "60", Log: []string{"energy-node-dashboard 1.4.1 -> 1.4.2"}},
			{ID: "70", State: "skip", Detail: "bereits erledigt"}, {ID: "81", State: "skip", Detail: "bereits erledigt"},
			{ID: "82", State: "skip", Detail: "nicht ausgewaehlt"}, {ID: "83", State: "skip", Detail: "bereits erledigt"},
			{ID: "84", State: "skip", Detail: "bereits erledigt"}, {ID: "85"}, {ID: "88"},
		}
	}

	return &stagedBackend{FakeBackend: fake, opts: opts, trusted: opts.trusted}
}

// fixtureChangelog is what "Was ist neu" shows. Two components with several
// releases, one breaking change and a scope per entry -- enough to exercise the
// summary, the breaking block, every filter and the "already installed" cut. On a
// first install (update == false) nothing is installed, so only the newest
// release of each component is offered.
func fixtureChangelog(update bool) *hostapi.ChangelogView {
	installed := map[string]string{}
	if update {
		installed = map[string]string{"dashboard": "v1.4.1", "service:shelly": "v0.3.0"}
	}
	return &hostapi.ChangelogView{
		BundleVersion: "v1.4.2",
		Installed:     installed,
		Document: json.RawMessage(`{
  "schema_version": 1,
  "bundle_version": "v1.4.2",
  "components": [
    {"id": "dashboard", "label": "Dashboard", "kind": "app", "version": "v1.4.2", "releases": [
      {"version": "v1.4.2", "date": "2026-09-21", "groups": [
        {"type": "feat", "label": "Features", "entries": [
          {"text": "add a versions page", "breaking": false, "scope": "dashboard", "pr": 51, "highlight": true},
          {"text": "fold the config.json node block into dashboard.node_*", "breaking": true, "scope": "dashboard", "pr": 14, "highlight": true}]},
        {"type": "fix", "label": "Fixes", "entries": [
          {"text": "stop duplicate energy card mounts from fighting over springs", "breaking": false, "scope": "dashboard", "pr": 41, "highlight": true},
          {"text": "show the download icon in the masthead update badge", "breaking": false, "scope": "dashboard", "pr": 40, "highlight": true}]},
        {"type": "test", "label": "Tests", "entries": [
          {"text": "cover the versions page in the browser", "breaking": false, "scope": "dashboard", "highlight": false}]}]},
      {"version": "v1.4.1", "date": "2026-09-16", "groups": [
        {"type": "fix", "label": "Fixes", "entries": [
          {"text": "an older fix the node already has", "breaking": false, "scope": "dashboard", "pr": 30, "highlight": true}]}]}]},
    {"id": "service:shelly", "label": "Shelly", "kind": "service", "version": "v0.4.0", "releases": [
      {"version": "v0.4.0", "date": "2026-09-15", "groups": [
        {"type": "feat", "label": "Features", "entries": [
          {"text": "make device services self-describing with per-service manifests", "breaking": false, "pr": 12, "highlight": true}]}]}]}
  ]
}`),
	}
}
