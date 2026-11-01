package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
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
	// english: die Log-Zeilen der Attrappe auf Englisch (--lang en). Ein
	// echter Node meldet sie aus den Skripten, deutsch; die Attrappe zeigt sie
	// fuer die Dokumentation in der Sprache des Fensters.
	english bool
	// systemUpdates: Vorschau und Diagnose nennen ausstehende Systempakete.
	systemUpdates bool
	// abiMismatch: das Gerät hat eine andere Python-Version als das Paket.
	// Das Vorbereiten scheitert mit PYTHON_ABI_MISMATCH, bis der Betreiber die
	// Online-Installation freigibt.
	abiMismatch bool
	// deltaVerifyFails: die Teilübertragung besteht die Prüfung auf dem Gerät
	// nicht. Erst die ganze Übertragung (force_full_transfer) gelingt.
	deltaVerifyFails bool
	// journalStep: das Manifest enthält den optionalen Schritt 17 (Journal nur
	// im Arbeitsspeicher). Die Vorlage kennt ihn nicht, darum nur auf Wunsch.
	journalStep bool
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
	// sudoLocked: nach einer Anmeldung mit Schluesseldatei verlangt der
	// Knoten ein sudo-Passwort, bis EnableSudo gelingt. Eine Anmeldung mit
	// Passwort nutzt dieses auch fuer sudo.
	sudoLocked bool
	// onlineDeps: die Online-Installation der Abhaengigkeiten ist freigegeben.
	onlineDeps bool
}

// wrongSudoPassword lehnt die Attrappe ab; jedes andere nichtleere Passwort
// schaltet sudo frei. So laesst sich die Fehlermeldung im Dialog sehen.
const wrongSudoPassword = "wrong"

func (b *stagedBackend) Precheck(ctx context.Context) (*hostapi.Precheck, error) {
	b.mu.Lock()
	locked := b.sudoLocked
	b.mu.Unlock()
	if locked {
		return nil, &hostapi.Error{Code: "SUDO_REQUIRED", Status: http.StatusConflict}
	}
	view, err := b.FakeBackend.Precheck(ctx)
	if err != nil || view == nil {
		return view, err
	}
	b.mu.Lock()
	online := b.onlineDeps
	b.mu.Unlock()
	if online {
		// A copy: the shared fixture stays as it is.
		changed := *view
		changed.PythonABIOK = false
		changed.Warnings = append(append([]string(nil), view.Warnings...), "PYTHON_ABI_ONLINE")
		return &changed, nil
	}
	return view, nil
}

func (b *stagedBackend) EnableSudo(ctx context.Context, password string) error {
	if password == "" {
		return &hostapi.Error{Code: "SUDO_REQUIRED", Status: http.StatusConflict}
	}
	if password == wrongSudoPassword {
		return &hostapi.Error{Code: "SUDO_PASSWORD_WRONG", Status: http.StatusUnauthorized}
	}
	b.mu.Lock()
	b.sudoLocked = false
	b.mu.Unlock()
	return b.FakeBackend.EnableSudo(ctx, password)
}

func (b *stagedBackend) Connect(ctx context.Context, req hostapi.ConnectRequest) (hostapi.ConnectResult, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.trusted && req.AcceptFingerprint != fingerprint {
		return hostapi.ConnectResult{}, &hostapi.Error{Code: "HOSTKEY_UNKNOWN", Detail: fingerprint, Status: http.StatusConflict}
	}
	b.trusted = true
	b.sudoLocked = req.Kind == hostapi.AuthKey
	return b.FakeBackend.Connect(ctx, req)
}

func (b *stagedBackend) Run(ctx context.Context, req hostapi.RunRequest, sink hostapi.Sink) error {
	// Delegate prepare runs to FakeBackend which handles them correctly
	if req.Mode == hostapi.ModePrepare {
		if b.opts.abiMismatch {
			b.mu.Lock()
			b.onlineDeps = b.onlineDeps || req.AllowOnlineDeps
			allowed := b.onlineDeps
			b.mu.Unlock()
			if !allowed {
				sink.Marker("package", "begin", "")
				sink.Log("package", b.say("Prüfe das Paket auf dem Gerät", "Checking the package on the device"))
				return &hostapi.Error{Code: "PYTHON_ABI_MISMATCH", Detail: "abi=cp313 expected=cp311", Status: http.StatusConflict}
			}
		}
		if b.opts.deltaVerifyFails && !req.ForceFullTransfer {
			sink.Marker("package", "begin", "")
			sink.Log("package", b.say("Prüfe das Paket auf dem Gerät", "Checking the package on the device"))
			return &hostapi.Error{Code: "PACKAGE_VERIFY_FAILED_DELTA", Detail: "missing=wheels/aiohttp-3.14.5-cp313-linux_armv6l.whl\ntampered=bootstrap/10-apt.sh", Status: http.StatusConflict}
		}
		return b.FakeBackend.Run(ctx, req, sink)
	}

	for _, step := range b.Steps {
		if req.Only != "" && step.ID != req.Only {
			continue
		}
		if step.ID == webhookStep && b.webhookChosen() {
			step = hostapitest.FakeStep{ID: webhookStep, Log: []string{b.say("Firewall: 8082/tcp fuer den Shelly-Wake-Webhook freigegeben", "Firewall: opened 8082/tcp for the Shelly wake webhook")}}
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
func systemUpgrade(english bool) hostapitest.FakeStep {
	return hostapitest.FakeStep{ID: "15", Detail: "neustart noetig", Log: []string{
		say(english, "Aktualisiere 3 Pakete: libssl3 openssl raspberrypi-kernel", "Upgrading 3 packages: libssl3 openssl raspberrypi-kernel"),
		say(english, "Ein Neustart des Node ist noetig, damit alle Updates wirken.", "The node needs a reboot for all updates to take effect."),
	}}
}

// say waehlt die deutsche oder englische Fassung einer Log-Zeile.
func say(english bool, de, en string) string {
	if english {
		return en
	}
	return de
}

func (b *stagedBackend) say(de, en string) string { return say(b.opts.english, de, en) }

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
	// updater ist vorlage-update so, wie der Updater des Dashboards
	// (dashboard/internal/updaterhost) den Lauf schreibt: nur gewaehlte
	// Schritte stehen im Auftrag. Abgewaehlte melden gar nichts, auch der
	// an die Firewall-Station gehaengte Schritt 35 nicht.
	updater := name == "updater"
	update := name == "vorlage-update" || updater

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
		// Wie dashboard/internal/updaterhost: nur Aktualisieren, keine Diagnose.
		fake.Description.EntryPoints = []string{"redeploy"}
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
	if opts.journalStep {
		steps = append(steps, hostapi.StepView{ID: "17", Optional: true, Default: false})
		sort.SliceStable(steps, func(i, j int) bool { return steps[i].ID < steps[j].ID })
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
	liveLogs(fake)

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
		{ID: "10", Log: []string{"apt-get install -y mosquitto mosquitto-clients ufw python3-pip", say(opts.english, "12 Pakete installiert", "12 packages installed")}},
		{ID: "15", Log: []string{say(opts.english, "Alle Systempakete sind aktuell.", "All system packages are up to date.")}},
		{ID: "20", Log: []string{"mosquitto_passwd -b energynode ***", say(opts.english, "/etc/mosquitto/conf.d/default.conf geschrieben", "wrote /etc/mosquitto/conf.d/default.conf"), say(opts.english, "mosquitto neu gestartet, Testnachricht zugestellt", "restarted mosquitto, test message delivered")}},
		{ID: "30", Log: []string{say(opts.english, "Regeln: 22/tcp, 1883/tcp, 8080/tcp, 443/tcp", "Rules: 22/tcp, 1883/tcp, 8080/tcp, 443/tcp"), say(opts.english, "ufw aktiv", "ufw active")}},
		{ID: "35", State: "skip", Detail: "nicht ausgewaehlt"},
		{ID: "40", Log: []string{
			say(opts.english, "tailscale_1.62.0_arm.tgz übertragen (24,1 MB)", "transferred tailscale_1.62.0_arm.tgz (24.1 MB)"),
			say(opts.english, "sha256 stimmt mit dem Manifest überein", "sha256 matches the manifest"),
			say(opts.english, "tailscale, tailscaled nach /usr/sbin kopiert", "copied tailscale, tailscaled to /usr/sbin"),
			say(opts.english, "tailscaled.service aktiviert und gestartet", "enabled and started tailscaled.service"),
			"To authenticate, visit: https://login.tailscale.com/a/4f2c8ab19de3",
		}},
		{ID: "50", Log: []string{"pip install --no-index --find-links wheels/ (12 Wheels)"}},
		{ID: "60", Log: []string{say(opts.english, "energy-node-dashboard 1.4.2 installiert", "installed energy-node-dashboard 1.4.2"), say(opts.english, "auth.pw geschrieben (0640)", "wrote auth.pw (0640)")}},
		{ID: "70", Log: []string{"caddy validate: Valid configuration"}},
		{ID: "81"}, {ID: "82", State: "skip", Detail: "nicht ausgewaehlt"}, {ID: "83"}, {ID: "84"}, {ID: "85"}, {ID: "88"},
	}
	fake.PrepareNotes = []hostapitest.FakeNote{
		{Key: "package.log.github_search", Args: map[string]string{"arch": "armv6"}},
		{Key: "package.log.download", Args: map[string]string{"name": "energy-node-v1.4.2-armv6.tar.gz"}},
	}
	if update {
		fake.Steps = []hostapitest.FakeStep{
			{ID: "10", State: "skip", Detail: "bereits erledigt"}, systemUpgrade(opts.english),
			{ID: "20", State: "skip", Detail: "bereits erledigt"},
			{ID: "30", State: "skip", Detail: "bereits erledigt"}, {ID: "35", State: "skip", Detail: "nicht ausgewaehlt"},
			{ID: "40", State: "skip", Detail: "bereits erledigt"},
			{ID: "50", Log: []string{"tinytuya 1.15.1 -> 1.16.0"}}, {ID: "60", Log: []string{"energy-node-dashboard 1.4.1 -> 1.4.2"}},
			{ID: "70", State: "skip", Detail: "bereits erledigt"}, {ID: "81", State: "skip", Detail: "bereits erledigt"},
			{ID: "82", State: "skip", Detail: "nicht ausgewaehlt"}, {ID: "83", State: "skip", Detail: "bereits erledigt"},
			{ID: "84", State: "skip", Detail: "bereits erledigt"}, {ID: "85"}, {ID: "88"},
		}
	}

	if updater {
		fake.Steps = jobSteps(fake.Steps)
	}

	if opts.systemUpdates {
		updates := &hostapi.SystemUpdates{Count: 3, CheckedAt: "2026-10-04T06:12:00+00:00", Packages: []hostapi.SystemPackage{
			{Name: "libssl3", From: "3.0.11-1~deb12u2", To: "3.0.13-1~deb12u1"},
			{Name: "openssl", From: "3.0.11-1~deb12u2", To: "3.0.13-1~deb12u1"},
			{Name: "raspberrypi-kernel", From: "1:1.20240529-1", To: "1:1.20240924-1"},
		}}
		fake.SystemUpdatesView = updates
		fake.DiagnoseView.SystemUpdates = updates
	}

	return &stagedBackend{FakeBackend: fake, opts: opts, trusted: opts.trusted}
}

// logEntry ist eine Zeile der Neustart-Folge aus dem Mockup (livelog-layout.html).
type logEntry struct {
	prio       int
	ident, pid string
	msg        string
}

// dashboardCycle ist eine Neustart-Folge des Dashboards. Nur die letzte Zeile
// traegt die Nummer des Neustarts (%n).
var dashboardCycle = []logEntry{
	{6, "systemd", "1", "Started energy-node-dashboard.service - Energy Node Dashboard."},
	{6, "energy-node-dashboard", "4411", "loading config /opt/energy-node/dashboard/data/config.json"},
	{6, "energy-node-dashboard", "4411", "mqtt: connecting to 127.0.0.1:1883 as energy-node"},
	{3, "energy-node-dashboard", "4411", "mqtt: connection refused: not authorised"},
	{3, "energy-node-dashboard", "4411", "fatal: bridge start failed"},
	{4, "systemd", "1", "energy-node-dashboard.service: Main process exited, code=exited, status=1/FAILURE"},
	{4, "systemd", "1", "energy-node-dashboard.service: Failed with result 'exit-code'."},
	{6, "systemd", "1", "energy-node-dashboard.service: Scheduled restart job, restart counter is at %n."},
}

// dashboardLine liefert Zeile k der Folge. Jeder Neustart sind acht Zeilen,
// der erste Neustart traegt die Nummer 8.
func dashboardLine(k int, at time.Time) hostapi.UnitLogLine {
	cycle, idx := k/len(dashboardCycle), k%len(dashboardCycle)
	entry := dashboardCycle[idx]
	msg := strings.ReplaceAll(entry.msg, "%n", strconv.Itoa(8+cycle))
	return hostapi.UnitLogLine{
		Unit: "energy-node-dashboard.service", At: at.UnixMilli(), Priority: entry.prio,
		Ident: entry.ident, PID: entry.pid, Message: msg, Cursor: fmt.Sprintf("dash-%d", k),
	}
}

// liveLogs gibt der Attrappe den Live-Log der Diagnose: zwölf Zeilen Vorlauf
// (eine Neustart-Folge und ein Stück der nächsten), danach kommt alle 700 ms
// eine weitere Zeile, bis der Kontext endet. Der Updater läuft ruhig.
func liveLogs(fake *hostapitest.FakeBackend) {
	now := time.Now()
	since := now.Add(-3 * time.Hour).UnixMilli()
	fake.UnitLogHostname = "energy-pi"
	fake.UnitStatuses = []hostapi.UnitStatus{
		{Unit: "energy-node-dashboard.service", Load: "loaded", Active: "activating", Sub: "auto-restart", Result: "exit-code", ExitStatus: 1, Restarts: 8},
		{Unit: "energy-node-updater.service", Load: "loaded", Active: "active", Sub: "running", Result: "success", Restarts: 0, PID: 812, Since: since},
	}
	for k := 0; k < 12; k++ {
		at := now.Add(time.Duration(k-12) * time.Second)
		fake.UnitLogLines = append(fake.UnitLogLines, dashboardLine(k, at))
	}
	updater := []logEntry{
		{6, "energy-node-updater", "812", "poll: no new release (0.10.9 is current)"},
		{6, "energy-node-updater", "812", "sessions: 1 active"},
		{6, "energy-node-updater", "812", "GET /api/status 200 3ms"},
	}
	for i, entry := range updater {
		fake.UnitLogLines = append(fake.UnitLogLines, hostapi.UnitLogLine{
			Unit: "energy-node-updater.service", At: now.Add(time.Duration(i-3) * time.Second).UnixMilli(),
			Priority: entry.prio, Ident: entry.ident, PID: entry.pid, Message: entry.msg, Cursor: fmt.Sprintf("upd-%d", i),
		})
	}
	fake.UnitLogInterval = 700 * time.Millisecond
	fake.UnitLogTick = func(i int) (hostapi.UnitLogLine, bool) {
		return dashboardLine(12+i, time.Now()), true
	}
}

// jobSteps laesst die abgewaehlten Schritte weg, wie sie im Auftrag des
// Dashboard-Updaters fehlen.
func jobSteps(steps []hostapitest.FakeStep) []hostapitest.FakeStep {
	var out []hostapitest.FakeStep
	for _, step := range steps {
		if step.State == "skip" && step.Detail == "nicht ausgewaehlt" {
			continue
		}
		out = append(out, step)
	}
	return out
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
