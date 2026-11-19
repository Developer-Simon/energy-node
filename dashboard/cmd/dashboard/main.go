// Command dashboard is the entry point of the Energy Node
// Dashboard. It wires process lifecycle (start, signal handling, graceful
// shutdown), the MQTT discovery client, the in-memory device registry, and
// the HTTP/web UI layer.
package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/appconfig"
	"github.com/Developer-Simon/energy-node-dashboard/internal/auth"
	"github.com/Developer-Simon/energy-node-dashboard/internal/autolearn"
	"github.com/Developer-Simon/energy-node-dashboard/internal/backup"
	"github.com/Developer-Simon/energy-node-dashboard/internal/basepath"
	"github.com/Developer-Simon/energy-node-dashboard/internal/bundlefetch"
	"github.com/Developer-Simon/energy-node-dashboard/internal/chainsuggest"
	"github.com/Developer-Simon/energy-node-dashboard/internal/chainwatch"
	"github.com/Developer-Simon/energy-node-dashboard/internal/companion"
	"github.com/Developer-Simon/energy-node-dashboard/internal/companiontls"
	"github.com/Developer-Simon/energy-node-dashboard/internal/config"
	"github.com/Developer-Simon/energy-node-dashboard/internal/devicefilter"
	"github.com/Developer-Simon/energy-node-dashboard/internal/diagnostics"
	"github.com/Developer-Simon/energy-node-dashboard/internal/energy"
	"github.com/Developer-Simon/energy-node-dashboard/internal/energydiscovery"
	"github.com/Developer-Simon/energy-node-dashboard/internal/energystats"
	"github.com/Developer-Simon/energy-node-dashboard/internal/example"
	"github.com/Developer-Simon/energy-node-dashboard/internal/historyexchange"
	"github.com/Developer-Simon/energy-node-dashboard/internal/httpapi"
	"github.com/Developer-Simon/energy-node-dashboard/internal/mqttclient"
	"github.com/Developer-Simon/energy-node-dashboard/internal/netmgr"
	"github.com/Developer-Simon/energy-node-dashboard/internal/nodeagent"
	"github.com/Developer-Simon/energy-node-dashboard/internal/onboarding"
	"github.com/Developer-Simon/energy-node-dashboard/internal/performance"
	"github.com/Developer-Simon/energy-node-dashboard/internal/registry"
	"github.com/Developer-Simon/energy-node-dashboard/internal/runtimecache"
	"github.com/Developer-Simon/energy-node-dashboard/internal/servicelogs"
	"github.com/Developer-Simon/energy-node-dashboard/internal/settings"
	"github.com/Developer-Simon/energy-node-dashboard/internal/shellypresets"
	"github.com/Developer-Simon/energy-node-dashboard/internal/storage"
	"github.com/Developer-Simon/energy-node-dashboard/internal/storagehealth"
	"github.com/Developer-Simon/energy-node-dashboard/internal/storageusage"
	"github.com/Developer-Simon/energy-node-dashboard/internal/systemactions"
	"github.com/Developer-Simon/energy-node-dashboard/internal/tailscale"
	"github.com/Developer-Simon/energy-node-dashboard/internal/tinytuya"
	"github.com/Developer-Simon/energy-node-dashboard/internal/updatecheck"
	"github.com/Developer-Simon/energy-node-dashboard/internal/updateprogress"
	"github.com/Developer-Simon/energy-node-dashboard/internal/updaterjob"
	dashwebui "github.com/Developer-Simon/energy-node-dashboard/internal/webui"
)

// updatesRepo is the GitHub repository internal/updatecheck asks about. It
// is the same repository docs/developing/releasing.md publishes tagged
// releases to.
const updatesRepo = "Developer-Simon/energy-node"

// buildVersion is overridden at build time via
// -ldflags "-X main.buildVersion=...", computed from dashboard/VERSION plus
// a branch suffix by scripts/deploy/deploy_dashboard_to_remote.sh. Left at "dev" for
// `go run`/`go test` and any build that skips ldflags.
var buildVersion = "dev"

// runBackupApply spielt einen vorgemerkten Import ein und startet danach die
// Dienste über startServices neu. Fehler werden nur protokolliert, der Start
// läuft weiter.
func runBackupApply(src backup.Sources, startServices func() error) {
	res, err := backup.ApplyPending(src)
	if err != nil {
		log.Printf("energy-node-dashboard: backup apply: %v", err)
	}
	if res == nil {
		// Ein Fehler ohne Ergebnis tritt nur mit einem vorgemerkten Einspielen oder Journal
		// auf. Dessen Lauf kann die Dienste angehalten haben, sie müssen wieder laufen.
		if err != nil {
			if serr := startServices(); serr != nil {
				log.Printf("energy-node-dashboard: backup apply: Dienste starten: %v", serr)
			}
		}
		return
	}
	log.Printf("energy-node-dashboard: backup apply: ok=%t code=%q", res.OK, res.Code)
	if err := startServices(); err != nil {
		log.Printf("energy-node-dashboard: backup apply: Dienste starten: %v", err)
	}
}

// batteryStateFiles liest id -> state_file aus battery_soc_devices.json. Fehlt
// die Datei oder ist sie nicht lesbar, ist das Ergebnis leer.
func batteryStateFiles(devicesDir string) map[string]string {
	out := map[string]string{}
	data, err := os.ReadFile(filepath.Join(devicesDir, "battery_soc_devices.json"))
	if err != nil {
		return out
	}
	var devices []struct {
		ID        string `json:"id"`
		StateFile string `json:"state_file"`
	}
	if json.Unmarshal(data, &devices) != nil {
		return out
	}
	for _, d := range devices {
		if d.ID != "" && d.StateFile != "" {
			out[d.ID] = d.StateFile
		}
	}
	return out
}

// Fester Einhaengepunkt des Datenlaufwerks, wie es der Speicherhelfer anlegt,
// und der RAM-Ausweich aus der Unit (RuntimeDirectory=energy-node).
var (
	storageMountPoint  = storagePath(os.Getenv(storageDemoEnv), "mount", "/mnt/energy-node-data")
	storageRAMFallback = storagePath(os.Getenv(storageDemoEnv), "ram", "/run/energy-node/runtime")
)

// storageDriveWatcher baut den Laufwerkswaechter, wenn storage.json ein
// Datenlaufwerk waehlt. Fehlt die Auswahl, bleibt alles beim bisherigen Ort,
// und die Funktion liefert nil.
func storageDriveWatcher(cfg appconfig.Config, dataDir string, reload func() error) *storage.Watcher {
	choice, err := storage.LoadConfig(dataDir)
	if err != nil {
		log.Printf("energy-node-dashboard: storage config ignored: %v", err)
		return nil
	}
	if !choice.UseData {
		return nil
	}
	if cfg.Paths.RuntimeDir == "" {
		log.Printf("energy-node-dashboard: data drive selected, but runtime_dir is not set, runtime data stays where it is")
		return nil
	}
	nodeID, err := storage.MachineID()
	if err != nil {
		log.Printf("energy-node-dashboard: data drive watcher off: %v", err)
		return nil
	}
	return &storage.Watcher{
		Config: choice,
		Paths: storage.Paths{
			Runtime:        cfg.Paths.RuntimeDir,
			Mount:          storageMountPoint,
			SystemFallback: filepath.Join(dataDir, "runtime-fallback"),
			RAMFallback:    storageRAMFallback,
		},
		NodeID: nodeID,
		Reload: reload,
	}
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == "backup" {
		os.Exit(runBackupCLI(os.Args[2:], os.Stdout, os.Stderr))
	}
	configPath := flag.String("config", appconfig.DefaultPath, "Pfad zur zentralen Konfigurationsdatei")
	flag.Parse()
	rawConfig, _ := os.ReadFile(*configPath)
	cfg, err := appconfig.Load(*configPath)
	if err != nil {
		log.Fatalf("energy-node-dashboard: %v", err)
	}
	if err := validateNodeDeviceID(cfg.Dashboard.NodeDeviceID, *configPath); err != nil {
		log.Fatalf("energy-node-dashboard: %v", err)
	}

	startedAt := time.Now().UTC()
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	port := strconv.Itoa(cfg.Dashboard.Port)
	bindAddress := cfg.Dashboard.BindAddress

	reg := registry.New()
	energyResolver := energy.NewResolver(nil)
	devicesDir := cfg.Paths.DevicesDir
	configManager := config.NewManager(devicesDir)
	dataDir := cfg.Paths.DataDir
	systemExecutor := systemactions.NewExecutor(nil, cfg.Dashboard.SystemActionHelper)
	backupSources := buildSources(cfg, *configPath)
	runBackupApply(backupSources, systemExecutor.StartServices)
	shellyPresetsPath := cfg.ShellyPresets()
	shellyPresetsStore := shellypresets.NewStore(shellyPresetsPath)
	configManager.ExcludeName(strings.TrimSuffix(filepath.Base(shellyPresetsPath), ".json"))
	settingsStore := settings.NewStore(dataDir)
	onboardingStore := onboarding.NewStore(dataDir)
	settingsStore.SetSweepIntervalDefault(cfg.Dashboard.SweepIntervalSeconds)
	deviceFilterStore := devicefilter.NewStore(dataDir)
	if err := deviceFilterStore.Load(); err != nil {
		log.Printf("energy-node-dashboard: ignored device store ignored: %v", err)
	}
	// Ein unlesbares Admin-Passwort darf den Dienst nicht am Start hindern:
	// ohne laufendes Dashboard gibt es sonst keinen Weg mehr, den Fehler zu
	// beheben, ausser direktem Dateisystemzugriff. Fail-closed gilt fuer
	// config.json selbst (s.o.), nicht fuer diese eine Passwortdatei -
	// stattdessen startet der Dienst mit leerem Bootstrap-Passwort (keine
	// Wirkung auf einen bereits bestehenden Admin-Nutzer in users.json) und
	// zeigt den Fehler auf der Anmeldeseite an.
	adminPassword, err := cfg.AdminPassword()
	var adminAuthWarningKey string
	if err != nil {
		log.Printf("energy-node-dashboard: Admin-Passwort nicht lesbar, Anmeldung eingeschraenkt: %v", err)
		adminAuthWarningKey = "login.admin_auth_unavailable"
	}
	authManager, err := auth.NewManager(filepath.Join(dataDir, "users.json"), cfg.Dashboard.AdminUsername, adminPassword)
	if err != nil {
		log.Fatalf("energy-node-dashboard: auth store failed: %v", err)
	}
	// Die Vorgaben des Installers liegen neben auth.pw und gelten einmal beim
	// Start. Ein Fehler darin bricht den Start nicht ab.
	if cfg.Dashboard.AdminPasswordFile != "" {
		seedPath := filepath.Join(filepath.Dir(cfg.Dashboard.AdminPasswordFile), onboarding.SeedFileName)
		if err := onboarding.ApplySeed(seedPath, authManager, settingsStore, onboardingStore, slog.Default()); err != nil {
			log.Printf("energy-node-dashboard: Vorgaben aus %s nicht vollständig übernommen: %v", onboarding.SeedFileName, err)
		}
	}
	// Der Beispiel-Seed aus dem Bootstrap gilt ebenfalls einmal beim Start.
	if err := example.ApplySeed(dataDir, settingsStore); err != nil {
		log.Printf("energy-node-dashboard: Beispiel aus %s nicht vollständig übernommen: %v", example.SeedFileName, err)
	}
	// Nur ein echter Reader im Interface, sonst waere ServiceLogs nie nil.
	var serviceLogs httpapi.ServiceLogReader
	if cfg.Dashboard.SystemActionHelper != "" {
		serviceLogs = servicelogs.NewReader(nil, cfg.Dashboard.SystemActionHelper)
	}

	candidateDir := filepath.Join(dataDir, "redeploy-candidate")
	// Fetching the newest bundle needs to know which architecture to ask
	// for; an unknown GOARCH leaves prepare nil and the redeploy screen as
	// before (the candidate directory is then filled by hand).
	var prepare func(context.Context, func(string, map[string]string)) error
	if arch, ok := bundlefetch.ArchForGo(runtime.GOARCH); ok {
		client := &bundlefetch.Client{
			Repo: updatesRepo,
			// No overall timeout: a bundle download on a Pi takes minutes
			// and the run's context cancels it. A stalled connection is
			// caught by the header timeout.
			HTTP: &http.Client{Transport: &http.Transport{
				Proxy:                 http.ProxyFromEnvironment,
				TLSHandshakeTimeout:   15 * time.Second,
				ResponseHeaderTimeout: 30 * time.Second,
			}},
		}
		// Same override the update check honors (see below): lets the smoke
		// test point the download at a fake GitHub.
		if base := os.Getenv("ENERGY_NODE_UPDATES_API_BASE"); base != "" {
			client.APIBase = base
		}
		prepare = prepareFunc(&bundlefetch.Fetcher{Client: client, Arch: arch, DestDir: candidateDir})
	}

	// The installer's state directory. Fixed on a node; the override lets the
	// smoke test (test/smoke) run the redeploy screens without root.
	installerStateDir := "/var/lib/energy-node-installer"
	jobDir := updaterjob.DefaultDir
	if dir := os.Getenv("ENERGY_NODE_INSTALLER_STATE_DIR"); dir != "" {
		installerStateDir = dir
		jobDir = filepath.Join(dir, "job")
	}
	redeploy, err := buildRedeploy(redeployConfig{
		candidateBundleDir:    candidateDir,
		installedManifestPath: filepath.Join(installerStateDir, "installed-manifest.json"),
		selectionPath:         filepath.Join(installerStateDir, "selection.json"),
		stepsDir:              filepath.Join(installerStateDir, "steps"),
		jobDir:                jobDir,
		prepare:               prepare,
		pageToken:             httpapi.SessionCSRFToken(authManager),
		languages:             dashwebui.Languages(),
	})
	if err != nil {
		log.Printf("redeploy: could not start the local update handler: %v", err)
	}
	var redeployHandler http.Handler
	if redeploy != nil {
		redeployHandler = redeploy.handler
	}

	stepTitle, err := newStepTitler(dashwebui.Languages(), filepath.Join(installerStateDir, "installed-manifest.json"))
	if err != nil {
		log.Fatalf("energy-node-dashboard: %v", err)
	}

	updatesChecker := &updatecheck.Checker{Repo: updatesRepo}
	// ENERGY_NODE_UPDATES_API_BASE points the GitHub lookup at a different
	// host instead of the real API. Unset in every real deployment; it
	// exists so demo/demo.sh (fake GitHub via fake_github_releases.py) can make
	// an update look available without a real newer tag on GitHub.
	if base := os.Getenv("ENERGY_NODE_UPDATES_API_BASE"); base != "" {
		updatesChecker.BaseURL = base
	}
	updatesCache := &updatecheck.Cache{}
	// recheck is the update check the installer runs before it trusts the
	// offer again; it refreshes the same cache the background check writes.
	recheck := func(ctx context.Context) {
		result, err := updatesChecker.Check(ctx, buildVersion)
		if err != nil {
			log.Printf("energy-node-dashboard: update check failed: %v", err)
			return
		}
		updatesCache.Set(result)
	}
	// Passed on only when non-nil: a nil *Installer in the interface would
	// register the install endpoint.
	var updateInstaller httpapi.UpdateInstaller
	if installer := newUpdateInstaller(redeploy, jobDir, updatesCache, recheck); installer != nil {
		updateInstaller = installer
	}

	// appconfig.Load hat eine schema_version-1-Datei nur in-memory nach v2
	// gehoben. Die Datei auf der Platte ist noch v1 - das Dashboard laeuft
	// zwar, aber die Python-Bridges lehnen sie ab. Einmalig ueber den
	// privilegierten Helper persistieren; schlaegt das fehl (Helper nicht
	// eingerichtet, sudoers-Zeile fehlt), laeuft das Dashboard mit der
	// in-memory-Config weiter und versucht es beim naechsten Start erneut.
	if appconfig.IsSchemaVersionOne(rawConfig) {
		if migrated, warnings, mErr := appconfig.MigrateV1toV2(rawConfig); mErr == nil {
			for _, warning := range warnings {
				log.Printf("energy-node-dashboard: %s: %s", *configPath, warning)
			}
			if perr := persistV1Migration(ctx, systemExecutor, cfg.Paths.DataDir, migrated); perr != nil {
				log.Printf("energy-node-dashboard: %s: schema_version 1 nur in-memory auf 2 gehoben, Persistieren ueber apply-app-config fehlgeschlagen (%v) - die Datei bleibt v1, die Python-Bridges lehnen sie ab", *configPath, perr)
			} else {
				log.Printf("energy-node-dashboard: %s: schema_version 1 -> 2 migriert und ueber apply-app-config persistiert (Sicherung: /etc/energy-node/.config.json.bak)", *configPath)
			}
		}
	}
	// The Debian package puts the CLI in /usr/bin, the config still names
	// the /usr/sbin path of the tarball install.
	tailscaleBin := tailscale.ResolveBinary(cfg.Tailscale.Bin)
	tailscaleClient := tailscale.NewClient(tailscaleBin, systemactions.ExecOutputRunner{}, time.Duration(cfg.Tailscale.StatusTimeoutS)*time.Second)
	// WLAN and LAN page: nur mit System-Action-Helper, sonst fehlen die Routen.
	var networkClient *netmgr.Client
	if cfg.Dashboard.SystemActionHelper != "" {
		networkClient = netmgr.NewClient(nil, cfg.Dashboard.SystemActionHelper, dataDir)
	}
	runtimeStore := runtimecache.NewStore(cfg.RuntimePath(dataDir, ""))
	energyStatsStore := energystats.NewStore(cfg.RuntimePath(dataDir, "energystats"))
	historySampler := historyexchange.NewSampler()
	energyRecorder := energystats.NewRecorder(energyStatsStore, energystats.SystemClockReady("/run/systemd/timesync", 15*time.Minute))
	// Messketten-Stunden in eigenem Verzeichnis. Ein Schreiben hebt die
	// Revision, damit energy_stats Statistik und Reiter nachladen laesst.
	energyChainStore := energystats.NewChainStore(filepath.Join(dataDir, "energystats", "chains"), energyStatsStore.Bump)
	energyRecorder.SetChainStore(energyChainStore)
	// Gelernte Verlustwerte der Messketten, gespeichert neben den Stunden.
	// Die Diagnose laeuft in der Energieschleife unten.
	chainLearnedPath := filepath.Join(dataDir, "energystats", "chains", "learned.json")
	chainMonitor := chainwatch.NewMonitor(chainwatch.Load(chainLearnedPath, log.Printf), holdsFromEnv(os.Getenv), time.Now)
	chainMonitor.SetPersistence(chainLearnedPath, log.Printf)
	energyResolver.SetChainInsight(chainMonitor)
	// Verlustenergie je Kette, integriert aus dem Verlust des Kettentopics.
	lossTotals := energystats.NewLossTotals(filepath.Join(dataDir, "energystats", "chains", "loss_totals.json"), log.Printf)
	// Messketten-Vorschlaege aus Minutenmitteln. Die Summen und verworfenen
	// Paare liegen getrennt von energy.json, Verwerfen aendert keine Konfiguration.
	chainSuggestPath := filepath.Join(dataDir, "energystats", "chains", "suggest.json")
	chainSuggest := chainsuggest.Load(chainSuggestPath, chainsuggest.ThresholdsFromEnv(os.Getenv), time.Local, log.Printf)
	chainSuggest.SetPersistence(chainSuggestPath, log.Printf)
	// Regelpruefung der Automationen. Die Beobachtungen laufen in der
	// 10-s-Schleife unten, ignorierte Hinweise liegen in autolearn/ignored.json.
	autolearnDir := filepath.Join(dataDir, "autolearn")
	automationObserver := autolearn.LoadObserver(filepath.Join(autolearnDir, "checks.json"), time.Local, log.Printf)
	automationObserver.SetPersistence(filepath.Join(autolearnDir, "checks.json"), log.Printf)
	automationChecker := autolearn.NewChecker(
		automationObserver,
		autolearn.EnergyStatsSeason(energyStatsStore),
		func() ([]byte, error) { return configManager.Read("automation_rules") },
		func(entityID string) bool {
			for _, device := range reg.Snapshot() {
				for _, entity := range device.Entities {
					if entity.UniqueID == entityID {
						return true
					}
				}
			}
			return false
		},
		filepath.Join(autolearnDir, "ignored.json"),
		log.Printf,
	)
	automationChecker.SetThresholds(autolearn.ThresholdsFromEnv(os.Getenv))
	automationEntityValue := func(entityID string) (float64, bool) {
		for _, value := range reg.Values([]string{entityID}) {
			if !value.HasValue {
				return 0, false
			}
			parsed, err := strconv.ParseFloat(value.Value, 64)
			return parsed, err == nil
		}
		return 0, false
	}
	// Der State-Payload des Automations-Dienstes, leer ohne Dienst.
	automationState := func() string {
		device, ok := reg.Get("automation")
		if !ok {
			return ""
		}
		for _, entity := range device.Entities {
			if entity.ObjectID == "state" {
				payload, _ := reg.StatePayload(entity.StateTopic)
				return payload
			}
		}
		return ""
	}
	// Nur nach einem erfolgreichen Laden von energy.json: sonst wuerden die
	// gelernten Paare einer unlesbaren Konfiguration verworfen.
	if value, err := settingsStore.LoadEnergy(); err == nil {
		chainMonitor.Prune(value.Chains)
	}
	energyCategories := func() map[string]energy.Category { return energyResolver.Model().Categories }
	energySync := energystats.NewSync(energyStatsStore, time.Now)
	energySync.SetCategories(energyCategories)
	// Modus aus settings.json, Lebenszeichen aus Sync. LoadSettings ist
	// gecacht, der Aufruf alle 10 s kostet nichts.
	energyRecorder.SetPolicy(func() energystats.Policy {
		mode := energystats.RecordingAlways
		if value, err := settingsStore.LoadSettings(); err == nil {
			mode = value.EnergyStatsRecording
		}
		return energystats.Policy{Mode: mode, CompanionCurrent: energySync.Status().Current}
	})
	energyStats := &energystats.Service{
		Store: energyStatsStore, Recorder: energyRecorder, Sync: energySync,
		Categories: energyCategories,
		ChainStore: energyChainStore,
		Insight:    chainMonitor,
		Chains: func() []energystats.ChainInfo {
			value, err := settingsStore.LoadEnergy()
			if err != nil {
				return nil
			}
			return chainInfos(value)
		},
	}
	energyRecorderDone := make(chan struct{})
	// Laufzeit-Summen je Teil der Dashboard-Arbeit fuer die Leistungsmessung.
	perfTiming := performance.NewTiming()
	// Der Laufwerkswaechter laeuft vor den Recordern und vor dem Laden des
	// Laufzeitcaches, damit beide den richtigen Ort sehen.
	storageWatcher := storageDriveWatcher(cfg, dataDir, energyStatsStore.Reload)
	storageLister := storageDriveLister()
	storageService := newStorageService(cfg, dataDir, *configPath, systemExecutor, storageLister)
	if storageWatcher != nil {
		// A drive that was unplugged and plugged back in is not mounted by
		// anything else, the mount unit starts only at boot.
		storageWatcher.Mount = func(ctx context.Context, uuid string) error {
			out, err := systemExecutor.Storage(ctx, "mount", uuid)
			if err != nil && out != "" {
				return fmt.Errorf("%s: %w", strings.Join(strings.Fields(out), " "), err)
			}
			return err
		}
		storageWatcher.Busy = storageService.Busy
	}
	storageStatus := func() storage.Status {
		if storageWatcher == nil {
			return storage.Status{Mode: storage.ModeNone}
		}
		return storageWatcher.Status()
	}
	storageRecheck := func(ctx context.Context) storage.Status {
		if storageWatcher == nil {
			return storage.Status{Mode: storage.ModeNone}
		}
		return storageWatcher.Tick(ctx)
	}
	go runStorageResume(ctx, storageService)
	// Die Belegung der Systemplatte: in der Demo feste Werte, sonst eine Messung
	// im Hintergrund, die alle zehn Minuten erneuert wird.
	var storageUsage *storageusage.Meter
	if os.Getenv(storageDemoEnv) != "" {
		storageUsage = demoStorageUsage(storageStatus, dataDir)
	} else {
		exe, _ := os.Executable()
		storageUsage = storageusage.NewMeter(func() storageusage.Parts {
			return storageusage.Measure(storageUsageSources(storageService, *configPath, installerStateDir, exe))
		}, 10*time.Minute)
	}
	storageFallback := func() string {
		if storageWatcher == nil {
			return ""
		}
		switch mode := storageWatcher.Status().Mode; mode {
		case storage.ModeFallbackSystem, storage.ModeFallbackRAM:
			return string(mode)
		}
		return ""
	}
	if storageWatcher != nil {
		lastReason := ""
		tickStorage := func() {
			st := storageWatcher.Tick(ctx)
			if st.Reason != lastReason {
				lastReason = st.Reason
				if st.Reason != "" {
					log.Printf("energy-node-dashboard: data drive: %s", st.Reason)
				}
			}
		}
		tickStorage()
		go func() {
			ticker := time.NewTicker(30 * time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					start := time.Now()
					tickStorage()
					perfTiming.Since(performance.PartBackground, start)
				}
			}
		}()
	}
	credentialStore := tinytuya.NewCredentialStore(dataDir)
	if err := runtimeStore.Load(); err != nil {
		log.Printf("energy-node-dashboard: runtime cache ignored: %v", err)
	}

	mqttCredentialStore := mqttclient.NewCredentialStore(dataDir)
	bridgeCredentialStore := mqttclient.NewBridgeCredentialStore(dataDir)
	mqttPassword, err := cfg.MQTTPassword()
	if err != nil {
		log.Fatalf("energy-node-dashboard: %v", err)
	}
	mqttBase := mqttclient.Config{
		Host:              cfg.MQTT.Host,
		Port:              strconv.Itoa(cfg.MQTT.Port),
		Username:          cfg.MQTT.Username,
		Password:          mqttPassword,
		ClientID:          cfg.Dashboard.ClientID,
		CleanSession:      true,
		KeepaliveSeconds:  30,
		ConnectTimeoutSec: 10,
		DiscoveryPrefix:   "homeassistant",
		AvailabilityTopic: energydiscovery.AvailabilityTopic,
		Source:            "config",
	}
	mqttCfg, mqttSource, err := httpapi.ResolveMQTTConfig(settingsStore, mqttCredentialStore, mqttBase)
	if err != nil {
		log.Fatalf("energy-node-dashboard: mqtt config invalid: %v", err)
	}
	log.Printf("energy-node-dashboard: mqtt configuration source: %s", mqttSource)
	mqttLogger := log.New(os.Stdout, "mqttclient: ", log.LstdFlags)

	client := mqttclient.NewWithContextAndFilter(ctx, mqttCfg, reg, mqttLogger, deviceFilterStore)
	client.SetVerboseLogging(strings.EqualFold(cfg.Dashboard.LogLevel, "debug"))
	client.SetStateObserver(func(changes []registry.StateChange) {
		runtimeStore.ObserveChanges(changes)
	})
	client.SetDiscoveryObserver(func(discovery registry.Discovery) {
		if runtimeStore.Restore(reg, discovery.Device.ID, discovery.Entity.UniqueID) {
			log.Printf("energy-node-dashboard: restored %s from runtime cache", discovery.Entity.UniqueID)
		}
	})
	if !client.StartConnecting() {
		log.Printf("energy-node-dashboard: mqtt not connected, retrying in the background: see the MQTT tab")
	}
	defer client.Close()

	// nodeagent publiziert den Pi-Knoten selbst als HA-Geraet "energy_node"
	// (Systemdiagnose ueber MQTT). Das Dashboard ist ab hier alleiniger
	// Publisher von outstation/energy_node/* - der fruehere Python-Node-Dienst
	// ist geloescht.
	nodeAgent := nodeagent.New(nodeagent.Options{
		NodeID:                   cfg.Dashboard.NodeDeviceID,
		NodeName:                 cfg.Dashboard.NodeDeviceName,
		DiscoveryPrefix:          client.DiscoveryPrefix(),
		PollIntervalS:            cfg.Dashboard.NodePollIntervalS,
		DiagnosticPollMultiplier: cfg.Dashboard.NodeDiagnosticPollMultiplier,
		TailscaleBin:             tailscaleBin,
	})

	// Bridge-Liveness: aus den deployten Dienst-Manifesten den Dienstkatalog
	// laden und outstation/<id>/status/online + .../settings/status
	// beobachten, damit /api/v1/health je Dienst active/configured meldet.
	manifestsDir := filepath.Join(filepath.Dir(*configPath), "manifests")
	serviceIDs, err := nodeagent.LoadServiceIDs(manifestsDir)
	if err != nil {
		log.Printf("energy-node-dashboard: manifests unreadable: %v", err)
	}
	nodeAgent.SetServiceCatalog(serviceIDs, cfg.Services)
	client.WatchTopics(nodeAgent.WatchTopicsFor(), nodeAgent.ObserveLiveness)

	// Leistungsmessung: Messung alle 15 s, Fenster 6 h, Anlaufphase 5 min.
	// ENERGY_NODE_PERFORMANCE_FAKE=1 liefert erfundene Werte (Demo, Smoke).
	perfManifests, err := nodeagent.LoadServiceManifests(manifestsDir)
	if err != nil {
		log.Printf("energy-node-dashboard: performance manifests unreadable: %v", err)
	}
	perfUnits := map[string]string{}
	perfFloors := map[string]float64{}
	for _, m := range perfManifests {
		if m.Unit != "" {
			perfUnits[m.ServiceID] = m.Unit
		}
		perfFloors[m.ServiceID] = m.PollFloorS
	}
	var perfSource performance.Source
	if os.Getenv("ENERGY_NODE_PERFORMANCE_FAKE") == "1" {
		perfSource = performance.NewFakeSource(time.Now().UnixNano())
	} else {
		perfSource = performance.NewProcSource("/proc", "/sys/fs/cgroup/system.slice")
	}
	perfCounter := performance.NewCounter(func(topic string) (string, string) {
		return performanceOwner(topic, reg, perfUnits, cfg.Dashboard.NodeDeviceID)
	})
	perfCounter.SetDeviceName(func(deviceID string) string {
		if dev, ok := reg.Get(deviceID); ok {
			return dev.Name
		}
		return ""
	})
	client.SetMessageObserver(perfCounter.Observe)
	client.SetIngestObserver(func(d time.Duration) { perfTiming.Add(performance.PartMQTT, d) })
	eventStreams := &httpapi.EventStreams{Push: func(d time.Duration) { perfTiming.Add(performance.PartPush, d) }}
	perfMonitor := performance.NewMonitor(perfSource, perfCounter, performance.Options{
		Interval: 15 * time.Second,
		Window:   6 * time.Hour,
		Warmup:   5 * time.Minute,
		Units:    perfUnits,
		Busy:     performanceBusy(jobDir),
		Clients:  eventStreams.Subscribers,
		Device: func() performance.DeviceSignals {
			telemetry, ok := nodeAgent.Telemetry()
			if !ok {
				return performance.DeviceSignals{}
			}
			return performance.DeviceSignals{TempC: telemetry.CPUTempC, Throttled: telemetry.UndervoltageNow}
		},
		RoundS:     nodeAgent.ServiceRoundS,
		Timing:     perfTiming,
		RuntimeCPU: performance.ReadRuntimeCPU,
	})
	// Der Befund performance_over_budget vergleicht die Last mit dem Budget der
	// Seite Leistung, Ziel minus Abzug.
	perfMonitor.SetBudget(func(snap performance.Snapshot) performance.Budget {
		return performance.Budget{
			Target:    float64(httpapi.PerformanceTarget(settingsStore, snap)),
			Deduction: performance.DeductionFor(snap),
		}
	})
	go func() {
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-ticker.C:
				perfMonitor.Tick(now)
			}
		}
	}()
	// Mit ENERGY_NODE_PERFORMANCE_FAKE_FILE liest die Seite den Szenario-Stand
	// der Demo-Konsole statt des Monitors; der Monitor laeuft unbenutzt weiter.
	// Die letzte Übernahme liegt in performance/last-change.json und wird hier geladen.
	if err := perfMonitor.AttachChange(dataDir, time.Now()); err != nil {
		log.Printf("energy-node-dashboard: Leistungsänderung laden: %v", err)
	}
	var perfProvider httpapi.PerformanceProvider = perfMonitor
	if fakeFile := os.Getenv("ENERGY_NODE_PERFORMANCE_FAKE_FILE"); fakeFile != "" && os.Getenv("ENERGY_NODE_PERFORMANCE_FAKE") == "1" {
		perfProvider = performance.NewFileProvider(fakeFile)
	}
	// Die Automationen für die Aufschlüsselung der Leistungsseite. Ohne lesbare
	// Datei gibt es keine Einträge.
	performanceAutomation := func() []performance.AutomationEntry {
		raw, err := configManager.Read("automation_rules")
		if err != nil {
			return nil
		}
		entries, err := performance.ParseAutomation(raw)
		if err != nil {
			return nil
		}
		return entries
	}
	performanceRates := func() performance.RateConfig {
		// Ohne lesbaren Wert gelten die Standardwerte: Live-Aktualisierung 3 s
		// (Einstellungen), Automation tick_interval_s 10 (Schema), Regler 10,
		// Dienste 10 s ohne eigenen poll_interval_s.
		// NodeS folgt dem Agenten, damit eine Übernahme sofort sichtbar wird.
		rates := performance.RateConfig{
			LiveUpdateS: 3,
			TickS:       10,
			RegulatorS:  10,
			NodeS:       nodeAgent.PollInterval().Seconds(),
			Services:    map[string]float64{},
		}
		if value, err := settingsStore.LoadSettings(); err == nil && value.LiveUpdateIntervalSeconds > 0 {
			rates.LiveUpdateS = float64(value.LiveUpdateIntervalSeconds)
		}
		// Takt und Regler stehen in automation_rules.settings. Der Regler
		// fällt auf den Takt zurück, wenn er fehlt.
		if raw, err := configManager.Read("automation_rules"); err == nil {
			var doc struct {
				Settings struct {
					Tick *float64 `json:"tick_interval_s"`
					Reg  *float64 `json:"regulator_interval_s"`
				} `json:"settings"`
			}
			if json.Unmarshal(raw, &doc) == nil {
				if doc.Settings.Tick != nil && *doc.Settings.Tick > 0 {
					rates.TickS = *doc.Settings.Tick
				}
				rates.RegulatorS = rates.TickS
				if doc.Settings.Reg != nil && *doc.Settings.Reg > 0 {
					rates.RegulatorS = *doc.Settings.Reg
				}
			}
		}
		for id := range perfUnits {
			rates.Services[id] = 10
			if svc, ok := cfg.Services[id]; ok && svc.PollIntervalS > 0 {
				rates.Services[id] = svc.PollIntervalS
			}
		}
		return rates
	}

	// metricEnabled liest bei jedem Aufruf den je-Metrik-Schalter aus
	// mqtt.json frisch, damit ein "Speichern" im MQTT-Tab sofort greift; ein
	// fehlender Schluessel (auch: unlesbare Datei) bedeutet "an".
	metricEnabled := func(metric string) bool {
		if stored, err := settingsStore.LoadMQTT(); err == nil {
			return stored.MetricEnabled(metric)
		}
		return true
	}

	// Der MQTT-Tab schaltet den globalen simulation_active-Broadcast; der
	// Sollzustand liegt in mqtt.json und wird retained auf ein festes Topic
	// gelegt - hier beim Umschalten (ueber nodeSimPublisher), im
	// Connect-Publisher unten fuer jeden (Re-)Connect.
	nodeSimTopic := "outstation/" + cfg.Dashboard.NodeDeviceID + "/settings/simulation_active/set"
	nodeSimPublisher := nodeSimAdapter{client: client, topic: nodeSimTopic}

	// Geraete-Link fuer Home Assistant, gefuellt von der Tailscale-Abfrage
	// weiter unten. Leer, bis Tailscale einen Namen liefert.
	discoveryAddress := &energydiscovery.Address{}

	// energyDiscovery baut die sieben Energie-Discovery-Configs. Der Schalter
	// wird bei jedem Aufruf frisch gelesen, damit ein "Speichern und neu
	// verbinden" ihn sofort anwendet; bei false trägt energydiscovery.Configs
	// leere Payloads (Removal) auf dieselben Topics.
	energyDiscovery := func() []mqttclient.OutboundMessage {
		publish := true
		if stored, err := settingsStore.LoadMQTT(); err == nil {
			publish = stored.PublishEnergyDevice
		}
		configs := energydiscovery.Configs(client.DiscoveryPrefix(), buildVersion, discoveryAddress.URL(), publish)
		msgs := make([]mqttclient.OutboundMessage, 0, len(configs))
		for _, cfg := range configs {
			msgs = append(msgs, mqttclient.OutboundMessage{
				Topic:   cfg.Topic,
				Payload: string(cfg.Payload),
				Retain:  true,
			})
		}
		return msgs
	}

	// Optionale Energie-Sensoren (Wallbox, Waermepumpe, Kategorien): was schon
	// angekuendigt ist, merkt sich announced. optionalWanted liefert die
	// gewuenschten Sensoren, leer wenn das Energie-Geraet aus ist. Kann
	// mqtt.json nicht gelesen werden, liefert es ok=false, und der Abgleich
	// faellt aus, damit keine Sensoren zurueckgezogen werden.
	announcedDir := filepath.Join(dataDir, "energydiscovery")
	if err := os.MkdirAll(announcedDir, 0o755); err != nil {
		log.Printf("energy-node-dashboard: energy discovery state dir: %v", err)
	}
	announced := energydiscovery.LoadAnnounced(filepath.Join(announcedDir, "announced.json"))
	currentSnapshot := func() energy.Snapshot {
		return energy.Aggregate(reg.Snapshot(), energyResolver, time.Now().UTC())
	}
	optionalWanted := func(snapshot energy.Snapshot) ([]energydiscovery.OptionalSensor, bool) {
		stored, err := settingsStore.LoadMQTT()
		if err != nil {
			return nil, false
		}
		if !stored.PublishEnergyDevice {
			return nil, true
		}
		return energydiscovery.Filter(desiredOptionalSensors(snapshot, stored.PublishChainSensors), stored.EnergySensors), true
	}

	// Bei jedem (Re-)Connect die eigene HA-Discovery retained neu absetzen.
	client.SetConnectPublisher(func() []mqttclient.OutboundMessage {
		msgs := energyDiscovery()

		// Der Connect-Publisher kann nicht wissen, ob das Senden klappt, darum
		// committet er sofort (wie die fixen Configs, die ebenfalls erneut
		// gesendet werden).
		if wanted, ok := optionalWanted(currentSnapshot()); ok {
			optionalMsgs, commitOptional := announced.Plan(client.DiscoveryPrefix(), buildVersion, discoveryAddress.URL(), wanted, true)
			for _, m := range optionalMsgs {
				msgs = append(msgs, mqttclient.OutboundMessage{Topic: m.Topic, Payload: string(m.Payload), Retain: true})
			}
			commitOptional()
		}

		// Node-Systemdiagnose (energy_node) + einmalige Abraeumung des alten,
		// mit Bindestrich benannten energy-node-Geraets des geloeschten
		// Python-Dienstes.
		msgs = append(msgs, nodeAgent.LegacyCleanupMessages()...)
		msgs = append(msgs, nodeAgent.DiscoveryMessages(metricEnabled)...)

		// Einmalige Abraeumung des frueheren dashboard-eigenen Topic-Baums
		// samt der dashboard_energy-Discovery-Configs; nach dem Umzug ist
		// outstation/energy_node/... der einzige Node-Topic-Baum.
		for _, m := range energydiscovery.LegacyCleanupMessages(client.DiscoveryPrefix()) {
			msgs = append(msgs, mqttclient.OutboundMessage{Topic: m.Topic, Payload: string(m.Payload), Retain: true})
		}

		// Globaler simulation_active-Sollzustand, retained, bei jedem Connect.
		simPayload := "0"
		if stored, err := settingsStore.LoadMQTT(); err == nil && stored.SimulationActive {
			simPayload = "1"
		}
		msgs = append(msgs, mqttclient.OutboundMessage{
			Topic:   nodeSimTopic,
			Payload: simPayload,
			Retain:  true,
		})
		return msgs
	})

	// Der MagicDNS-Name ist beim Start oft noch unbekannt (Tailscale verbindet
	// spaeter) und kann sich aendern. Deshalb sofort und dann alle 5 Minuten
	// nachsehen und nur bei einem neuen Namen neu publizieren. Ein leerer Name
	// (CLI-Zeitueberschreitung, abgemeldet) behaelt den letzten Link.
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for {
			status, err := tailscaleClient.Status(ctx)
			if url := energydiscovery.ConfigurationURL(status.DNSName); err == nil && url != "" && discoveryAddress.Set(url) {
				for _, msg := range energyDiscovery() {
					if err := client.PublishRetained(msg.Topic, msg.Payload); err != nil {
						// Getrennt: der naechste Connect publiziert ohnehin neu.
						break
					}
				}
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()

	if bridgeCfg, err := settingsStore.LoadBridge(); err != nil {
		log.Printf("energy-node-dashboard: bridge config ignored: %v", err)
	} else if len(bridgeCfg.Connections) > 0 {
		client.SetBridgeWatch(bridgeCfg.Connections[0].RemoteClientID)
	}
	configManager.SetReloadFunc(func(name string, _ []byte) error {
		if serviceID, ok := config.ServiceIDForConfig(name); ok {
			return client.ReloadService(serviceID)
		}
		return client.Reload()
	})
	probeTimeout := time.Duration(cfg.TinyTuya.ProbeTimeoutS) * time.Second
	storageProvider := storagehealth.New(dataDir)

	go func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		lastSweep := time.Time{}
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				start := time.Now()
				value, err := settingsStore.LoadSettings()
				if err != nil {
					perfTiming.Since(performance.PartBackground, start)
					continue
				}
				if lastSweep.IsZero() || time.Since(lastSweep) >= time.Duration(value.SweepIntervalSeconds)*time.Second {
					if err := runtimeStore.Sweep(reg.Snapshot(), time.Now().UTC()); err != nil {
					}
					lastSweep = time.Now()
				}
				perfTiming.Since(performance.PartBackground, start)
			}
		}
	}()

	// Die Einstellungen werden bei jedem Aufruf neu gelesen, damit ein Speichern
	// in der Oberfläche sofort auch für den Zeitplan gilt.
	backupSettingsFn := func() backup.Settings {
		s, err := backup.LoadSettings(dataDir)
		if err != nil {
			log.Printf("backup settings: %v, using defaults", err)
			return backup.DefaultSettings()
		}
		return s
	}
	backupLocation := backup.DriveLocation{
		Drive:  storageBackupDrive(dataDir),
		System: backup.SystemLocation{DataDir: dataDir, Settings: backupSettingsFn},
	}
	sched := &backup.Scheduler{
		Settings: backupSettingsFn,
		Location: backupLocation,
		Version:  buildVersion,
		Create:   newArchiveCreator(backupSources, buildVersion, "auto"),
	}
	backupDeps := &httpapi.BackupDeps{
		DataDir:   dataDir,
		Sources:   backupSources,
		Version:   buildVersion,
		Settings:  backupSettingsFn,
		Location:  backupLocation,
		Jobs:      &backup.Jobs{},
		Scheduler: sched,
		Exec:      systemExecutor,
	}
	go func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				start := time.Now()
				sched.Tick(ctx)
				perfTiming.Since(performance.PartBackground, start)
			}
		}
	}()

	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				reg.ExpirePendingCommands(time.Now().UTC())
			}
		}
	}()

	go func() {
		// Once a day, not at a fixed clock time: an hourly tick just asks
		// "has it been >=24h since the last successful check (or has there
		// never been one)", which survives restarts without drifting or
		// needing a missed-the-slot special case (see the "recommended"
		// option discussed for this feature).
		runIfDue := func() {
			value, err := settingsStore.LoadSettings()
			if err != nil || value.UpdateCheckDisabled {
				return
			}
			if last, has := updatesCache.Get(); has && time.Since(last.CheckedAt) < 24*time.Hour {
				return
			}
			result, err := updatesChecker.Check(ctx, buildVersion)
			if err != nil {
				log.Printf("energy-node-dashboard: update check failed: %v", err)
				return
			}
			updatesCache.Set(result)
		}
		runIfDue()
		ticker := time.NewTicker(time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				runIfDue()
			}
		}
	}()

	// Second listener for the Home Assistant companion, TLS on its own port.
	// "0" disables it. A certificate failure only disables this listener.
	companionPort := os.Getenv("ENERGY_NODE_COMPANION_PORT")
	if companionPort == "" {
		companionPort = "8081"
	}
	var companionHub *companion.Hub
	var companionCert tls.Certificate
	var companionFingerprint string
	if companionPort != "0" {
		cert, fingerprint, err := companiontls.LoadOrCreate(dataDir)
		if err != nil {
			log.Printf("energy-node-dashboard: companion port disabled, certificate: %v", err)
		} else {
			companionHub = companion.NewHub()
			authManager.SetHAVerifier(companionHub)
			companionCert = cert
			companionFingerprint = fingerprint
		}
	}

	// basepath.Middleware sits outermost so the router only ever sees
	// root-relative paths, even when a reverse proxy serves the dashboard
	// under a subpath (see knowhow/dashboard/dashboard-reverse-proxy-unterpfad.md).
	// Both listeners serve this same handler.
	handler := perfTiming.Middleware(basepath.Middleware(httpapi.NewAuthenticatedRouter(reg, configManager, settingsStore, tinytuya.NewClient(
		cfg.TinyTuya.ProbePython,
		cfg.TinyTuya.ProbeScript,
		probeTimeout,
	), credentialStore, client, storageProvider, runtimeStore, httpapi.RouterDependencies{
		PerformanceStatus: diagnostics.PerformanceStatusFunc(func(now time.Time) (diagnostics.OverBudget, bool) {
			info, ok := perfMonitor.OverBudget(now)
			return diagnostics.OverBudget{
				UsedPct:      info.UsedPct,
				AvailablePct: info.AvailablePct,
				TargetPct:    info.TargetPct,
				DeductionPct: info.DeductionPct,
				Minutes:      info.Minutes,
				Reason:       info.Reason,
			}, ok
		}),
		NodeDeviceID:         cfg.Dashboard.NodeDeviceID,
		MQTT:                 client,
		MQTTReconfigure:      client,
		MQTTCredentials:      mqttCredentialStore,
		Reloader:             client,
		Auth:                 authManager,
		AdminAuthWarningKey:  adminAuthWarningKey,
		SystemActions:        systemExecutor,
		ServiceLogs:          serviceLogs,
		StartedAt:            startedAt,
		DeviceFilter:         deviceFilterStore,
		DeviceActions:        client,
		ShellyPresets:        shellyPresetsStore,
		Version:              buildVersion,
		BridgeCredentials:    bridgeCredentialStore,
		MQTTBridgeWatcher:    client,
		DataDir:              dataDir,
		AutomationHistoryDir: cfg.RuntimePath(devicesDir, ""),
		Onboarding:           onboardingStore,
		TourTargets:          dashwebui.TourTargets,
		Backup:               backupDeps,
		Storage: &httpapi.StorageDeps{
			DataDir:   dataDir,
			Service:   storageService,
			Lister:    storageLister,
			Status:    storageStatus,
			Recheck:   storageRecheck,
			DemoSpace: os.Getenv(storageDemoEnv) != "",
			Usage:     storageUsage,
		},
		InstallerStateDir:  installerStateDir,
		AppConfigPath:      *configPath,
		BridgeTargetPath:   cfg.Dashboard.MosquittoBridgeTarget,
		Tailscale:          tailscaleClient,
		Network:            networkClient,
		Resolver:           energyResolver,
		EnergyStats:        energyStats,
		ChainMonitor:       chainMonitor,
		ChainSuggest:       chainSuggest,
		AutomationFindings: automationChecker,
		StorageFallback:    storageFallback,
		HistorySampler:     historySampler,
		InstalledServices: map[string]bool{
			"automation": cfg.ServiceInstalled("automation"),
			"tailscale":  cfg.ServiceInstalled("tailscale"),
			"tuya":       cfg.ServiceInstalled("tuya"),
		},
		MQTTBase:       mqttBase,
		NodeAgent:      nodeAgent,
		NodeSimulation: nodeSimPublisher,
		// Die API zeigt alle passenden Sensoren, ungefiltert, damit der
		// MQTT-Tab sie abwaehlen kann.
		EnergySensorOptions: func() []energydiscovery.OptionalSensor {
			chainSensors := false
			if stored, err := settingsStore.LoadMQTT(); err == nil {
				chainSensors = stored.PublishChainSensors
			}
			return desiredOptionalSensors(currentSnapshot(), chainSensors)
		},
		Redeploy:             redeployHandler,
		UpdatesChecker:       updatesChecker,
		UpdatesCache:         updatesCache,
		UpdaterJobDir:        jobDir,
		StepTitle:            stepTitle,
		UpdateInstaller:      updateInstaller,
		Companion:            companionHub,
		CompanionFingerprint: companionFingerprint,
		EventStreams:         eventStreams,
		Performance: &httpapi.PerformanceDeps{
			Provider:   perfProvider,
			Rates:      performanceRates,
			Automation: performanceAutomation,
			Floors:     perfFloors,
			Changes:    perfMonitor,
			Auth:       authManager,
			Settings:   settingsStore,
			Configs:    configManager,
			SystemPath: *configPath,
			DataDir:    dataDir,
			Executor:   systemExecutor,
			NodeRates:  nodeAgent,
		},
	})))

	srv := &http.Server{
		Addr:              net.JoinHostPort(bindAddress, port),
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		// Eigene Monotonuhr: time.Since(monoStart) laeuft auch dann
		// gleichmaessig weiter, wenn NTP die Wanduhr stellt. Daran erkennt
		// der Recorder den Uhrsprung.
		defer close(energyRecorderDone)
		monoStart := time.Now()
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				energyRecorder.Close()
				chainMonitor.Close()
				lossTotals.Close()
				chainSuggest.Close()
				automationObserver.Close()
				return
			case <-ticker.C:
				start := time.Now()
				now := time.Now()
				snapshot := energy.Aggregate(reg.Snapshot(), energyResolver, now.UTC()).WithInterpretation(energyResolver.Interpretation())
				energyRecorder.Observe(now, time.Since(monoStart), snapshot)
				// Ohne gestellte Uhr waere FirstSeen falsch. Der Modus off
				// haelt die Diagnose nicht an.
				if energyRecorder.Status().ClockReady {
					// Die Schalter aus den Einstellungen gelten ohne Neustart. Ohne
					// lesbare Einstellungen bleibt alles an.
					learningOn, suggestionsOn := true, true
					if value, err := settingsStore.LoadSettings(); err == nil {
						learningOn = !value.ChainLearningDisabled
						suggestionsOn = !value.ChainSuggestionsDisabled
					}
					chainMonitor.SetLearning(learningOn)
					chainSuggest.SetEnabled(suggestionsOn)
					chainMonitor.Observe(now, snapshot)
					// Wie das Pruning oben: ohne lesbare energy.json fehlen
					// Ketten im Snapshot, und Observe wuerde ihre Summen loeschen.
					if _, err := settingsStore.LoadEnergy(); err == nil {
						lossTotals.Observe(now, snapshot.Chains)
					}
					if suggestionsOn {
						chainSuggest.Observe(now, reg.Snapshot())
					}
				}
				historySampler.Observe(now, httpapi.HistorySamplerValues(snapshot, reg, configManager))
				energySync.Tick()
				perfTiming.Since(performance.PartBackground, start)
			}
		}
	}()

	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		var chainGate chainSignalsGate
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				start := time.Now()
				payload, snapshot, err := buildBalancePayload(reg, energyResolver, time.Now())
				if err != nil {
					log.Printf("energy-node-dashboard: energy balance marshal failed: %v", err)
					perfTiming.Since(performance.PartBackground, start)
					continue
				}
				automationChecker.ObserveTick(time.Now(), automationState(), balanceRuleValues(snapshot.Balance), automationEntityValue)
				if err := client.PublishRetained(energydiscovery.StateTopic, string(payload)); err != nil {
					log.Printf("energy-node-dashboard: energy balance publish skipped: %v", err)
				}

				// Messketten-Signale fuer die Automationen. Ohne Ketten
				// wird einmal ein leeres Signal veroeffentlicht, damit das
				// alte retained Signal verschwindet.
				if chainGate.Want(len(snapshot.Chains)) {
					chainSignals := energy.BuildChainSignals(snapshot.Chains, energyResolver.ChainInsight(), time.Now())
					chainSignals.SetLossTotals(lossTotals.Totals())
					signals, err := json.Marshal(chainSignals)
					if err == nil {
						err = client.PublishRetained(energydiscovery.ChainsTopic, string(signals))
					}
					if err != nil {
						log.Printf("energy-node-dashboard: chain signals publish skipped: %v", err)
					} else {
						chainGate.published = len(snapshot.Chains) > 0
					}
				}

				// Optionale Sensoren nachziehen, die seit dem letzten Connect
				// dazugekommen oder weggefallen sind. Der Stand wird nur
				// uebernommen, wenn alle Nachrichten angekommen sind.
				if wanted, ok := optionalWanted(snapshot); ok {
					optionalMsgs, commitOptional := announced.Plan(client.DiscoveryPrefix(), buildVersion, discoveryAddress.URL(), wanted, false)
					published := true
					for _, m := range optionalMsgs {
						if err := client.PublishRetained(m.Topic, string(m.Payload)); err != nil {
							log.Printf("energy-node-dashboard: optional energy sensor %s publish skipped: %v", m.Topic, err)
							published = false
							break
						}
					}
					if published {
						commitOptional()
					}
				}
				perfTiming.Since(performance.PartBackground, start)
			}
		}
	}()

	// Node-Telemetrie: der schnelle Ticker publiziert nur
	// outstation/energy_node/state, der langsame zusaetzlich
	// .../diagnostics. Beim Start einmal beides.
	go func() {
		fast := time.NewTicker(nodeAgent.PollInterval())
		slow := time.NewTicker(nodeAgent.DiagnosticInterval())
		defer fast.Stop()
		defer slow.Stop()
		publish := func(msgs ...mqttclient.OutboundMessage) {
			for _, m := range msgs {
				if err := client.PublishRetained(m.Topic, m.Payload); err != nil {
					log.Printf("energy-node-dashboard: node telemetry publish skipped: %v", err)
				}
			}
		}
		start := time.Now()
		publish(nodeAgent.StateMessages(ctx, metricEnabled)...) // beim Start einmal voll
		perfTiming.Since(performance.PartNode, start)
		for {
			select {
			case <-ctx.Done():
				return
			case <-fast.C:
				start := time.Now()
				publish(nodeAgent.StateMessage(ctx, metricEnabled))
				perfTiming.Since(performance.PartNode, start)
			case <-slow.C:
				start := time.Now()
				publish(nodeAgent.DiagnosticsMessage(ctx, metricEnabled))
				perfTiming.Since(performance.PartNode, start)
			case <-nodeAgent.RatesChanged():
				// Neues Poll-Intervall aus den Einstellungen: beide Ticker neu takten.
				fast.Reset(nodeAgent.PollInterval())
				slow.Reset(nodeAgent.DiagnosticInterval())
			}
		}
	}()

	go func() {
		log.Printf("energy-node-dashboard: listening on :%s", port)
		certFile := cfg.Dashboard.TLS.CertFile
		keyFile := cfg.Dashboard.TLS.KeyFile
		var err error
		if certFile != "" || keyFile != "" {
			if certFile == "" || keyFile == "" {
				log.Fatal("energy-node-dashboard: dashboard.tls.cert_file und dashboard.tls.key_file muessen gemeinsam gesetzt sein")
			}
			log.Printf("energy-node-dashboard: serving HTTPS on :%s", port)
			err = srv.ListenAndServeTLS(certFile, keyFile)
		} else {
			log.Printf("energy-node-dashboard: serving HTTP; use TLS or a trusted HTTPS reverse proxy for password login")
			err = srv.ListenAndServe()
		}
		if err != nil && err != http.ErrServerClosed {
			log.Fatalf("energy-node-dashboard: http server error: %v", err)
		}
	}()

	var companionSrv *http.Server
	if companionHub != nil {
		companionSrv = &http.Server{
			Addr:              net.JoinHostPort(bindAddress, companionPort),
			Handler:           httpapi.CompanionGate(authManager, handler),
			TLSConfig:         &tls.Config{Certificates: []tls.Certificate{companionCert}, MinVersion: tls.VersionTLS12},
			ReadHeaderTimeout: 5 * time.Second,
		}
		go func() {
			log.Printf("energy-node-dashboard: companion listening on :%s (HTTPS)", companionPort)
			if err := companionSrv.ListenAndServeTLS("", ""); err != nil && err != http.ErrServerClosed {
				log.Printf("energy-node-dashboard: companion server error: %v", err)
			}
		}()
	}

	<-ctx.Done()

	log.Println("energy-node-dashboard: shutdown signal received, shutting down...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("energy-node-dashboard: graceful shutdown failed: %v", err)
	}
	if companionSrv != nil {
		if err := companionSrv.Shutdown(shutdownCtx); err != nil {
			log.Printf("energy-node-dashboard: companion graceful shutdown failed: %v", err)
		}
	}
	// Beim sauberen Beenden den aktuellen Stand sichern. Der periodische
	// Sweep laeuft nur alle sweep_interval_seconds (Vorgabe 1800).
	if err := runtimeStore.Sweep(reg.Snapshot(), time.Now().UTC()); err != nil {
		log.Printf("energy-node-dashboard: runtime cache flush failed: %v", err)
	}
	if flusher, ok := storageProvider.(storagehealth.Flusher); ok {
		if err := flusher.Flush(); err != nil {
			log.Printf("energy-node-dashboard: storage health flush failed: %v", err)
		}
	}
	select {
	case <-energyRecorderDone:
	case <-time.After(5 * time.Second):
		log.Printf("energy-node-dashboard: energy statistics did not flush in time")
	}
}

// validateNodeDeviceID guards against a node-ID split-brain. The MQTT LWT is
// pinned to energydiscovery.AvailabilityTopic (outstation/energy_node/status/
// online), while nodeagent derives its availability/state topics from
// dashboard.node_device_id. They only agree when node_device_id is
// "energy_node"; any other value (e.g. an upgraded node that kept the old
// hyphenated "energy-node") leaves every node diagnostic entity permanently
// unavailable and triggers a self-erasing legacy cleanup. An empty value is
// rejected too - appconfig does not default it here.
func validateNodeDeviceID(id, configPath string) error {
	if id == energydiscovery.DeviceID {
		return nil
	}
	return fmt.Errorf(
		"dashboard.node_device_id is %q but must be %q: the MQTT availability topic is fixed to outstation/%s/status/online, so any other id makes the node's diagnostic entities permanently unavailable. Set dashboard.node_device_id to %q in %s (see INSTALLATION.md, section \"Upgrading from an earlier release (<= 0.4)\")",
		id, energydiscovery.DeviceID, energydiscovery.DeviceID, energydiscovery.DeviceID, configPath)
}

// buildBalancePayload liefert den State-Payload und den Snapshot, aus dem er
// gebaut ist, damit der Abgleich der optionalen Sensoren denselben Stand sieht.
// chainSignalsGate decides whether the chains topic gets a payload. It is
// wanted while chains exist, and once more after the last chain went away,
// so the retained payload is cleared.
type chainSignalsGate struct{ published bool }

// Want reports whether a payload is due for chainCount chains.
func (g *chainSignalsGate) Want(chainCount int) bool {
	return chainCount > 0 || g.published
}

// balanceRuleValues names the balance values by the field a balance condition
// reads. Custom categories are keyed as custom:<id>.
func balanceRuleValues(balance energy.Balance) map[string]float64 {
	values := map[string]float64{
		"pv":                balance.PV,
		"grid_import":       balance.GridImport,
		"grid_export":       balance.GridExport,
		"battery_charge":    balance.BatteryCharge,
		"battery_discharge": balance.BatteryDischarge,
		"wallbox":           balance.Wallbox,
		"heat_pump":         balance.HeatPump,
		"load_total":        balance.LoadTotal,
	}
	for id, value := range balance.Categories {
		values["custom:"+id] = value
	}
	return values
}

func buildBalancePayload(reg *registry.Registry, resolver *energy.Resolver, now time.Time) ([]byte, energy.Snapshot, error) {
	snapshot := energy.Aggregate(reg.Snapshot(), resolver, now.UTC())
	cfg := resolver.Interpretation()
	full := snapshot.WithInterpretation(cfg)
	payload, err := json.Marshal(struct {
		At             int64                 `json:"at"`
		Balance        energy.Balance        `json:"balance"`
		Categories     map[string]float64    `json:"categories"`
		Interpretation energy.Interpretation `json:"interpretation"`
	}{At: now.Unix(), Balance: full.Balance, Categories: full.Balance.Categories, Interpretation: cfg})
	return payload, full, err
}

// desiredOptionalSensors nennt die optionalen Sensoren, die zur Anlage
// passen: Hauszaehler (beide Hauslasten), Wallbox und Waermepumpe, sobald
// eine Quelle zugeordnet ist, und jede Kategorie mit zugeordneter Quelle.
func desiredOptionalSensors(snapshot energy.Snapshot, chainSensors bool) []energydiscovery.OptionalSensor {
	var categories []energydiscovery.CategoryInput
	for id, category := range snapshot.Categories {
		if len(snapshot.Sources[energy.Role("custom:"+id)]) == 0 {
			continue
		}
		categories = append(categories, energydiscovery.CategoryInput{
			ID:      id,
			Label:   category.Label,
			Storage: category.Base == energy.CategoryStorage,
		})
	}
	sensors := energydiscovery.OptionalSensors(
		len(snapshot.Sources[energy.RoleLoad]) > 0,
		len(snapshot.Sources[energy.RoleWallbox]) > 0,
		len(snapshot.Sources[energy.RoleHeatPump]) > 0,
		categories,
	)
	if chainSensors {
		var chains []energydiscovery.ChainInput
		for _, chain := range snapshot.Chains {
			chains = append(chains, energydiscovery.ChainInput{ID: chain.ID, Label: chain.Label})
		}
		sensors = append(sensors, energydiscovery.ChainSensors(chains)...)
	}
	return sensors
}

// nodeSimAdapter turns the MQTT tab's simulation_active switch into a
// retained publish on outstation/<node>/settings/simulation_active/set.
// *mqttclient.Client itself grows no method for this - the topic is fixed
// at startup from cfg.Dashboard.NodeDeviceID, so a tiny adapter keeps that
// knowledge in main.go.
type nodeSimAdapter struct {
	client *mqttclient.Client
	topic  string
}

func (a nodeSimAdapter) PublishNodeSimulation(active bool) {
	payload := "0"
	if active {
		payload = "1"
	}
	if err := a.client.PublishRetained(a.topic, payload); err != nil {
		log.Printf("energy-node-dashboard: node simulation publish skipped: %v", err)
	}
}

// chainInfos lists the measurement chains sorted by ID. A chain is a storage
// chain when its counting point has a storage role. Counting empty means the
// first point counts, as the chain form saves it.
func chainInfos(cfg settings.EnergyConfig) []energystats.ChainInfo {
	model := cfg.Model()
	infos := make([]energystats.ChainInfo, 0, len(cfg.Chains))
	for id, chain := range cfg.Chains {
		counting := chain.Counting
		if counting == "" && len(chain.Points) > 0 {
			counting = chain.Points[0]
		}
		role := cfg.Assignments[counting].Role
		infos = append(infos, energystats.ChainInfo{ID: id, Chain: chain, Storage: model.IsStorageRole(role)})
	}
	slices.SortFunc(infos, func(a, b energystats.ChainInfo) int { return strings.Compare(a.ID, b.ID) })
	return infos
}

// holdsFromEnv returns the hold times of the chain diagnoses. The variable
// ENERGY_NODE_CHAIN_HOLD_SECONDS sets all five to that many seconds, for a
// look at the demo. Unset or not a positive whole number: DefaultHolds.
func holdsFromEnv(getenv func(string) string) chainwatch.Holds {
	seconds, err := strconv.Atoi(strings.TrimSpace(getenv("ENERGY_NODE_CHAIN_HOLD_SECONDS")))
	if err != nil || seconds <= 0 {
		return chainwatch.DefaultHolds
	}
	d := time.Duration(seconds) * time.Second
	return chainwatch.Holds{Drift: d, Frozen: d, Contradiction: d, Unit: d, Clear: d}
}

// performanceOwner names the service or the foreign device behind an MQTT
// topic for the message counter. Service topics are outstation/<service>/...;
// a device whose via_device names a service counts for that service. Other
// outstation devices, except the node itself, are foreign devices. Topics
// outside outstation/ are not attributed.
func performanceOwner(topic string, reg *registry.Registry, units map[string]string, nodeID string) (string, string) {
	parts := strings.SplitN(topic, "/", 3)
	if len(parts) < 2 || parts[0] != "outstation" {
		return "", ""
	}
	id := parts[1]
	if _, ok := units[id]; ok {
		return id, ""
	}
	if id == nodeID || id == "" {
		return "", ""
	}
	if dev, ok := reg.Get(id); ok {
		if _, ok := units[dev.ViaDevice]; ok {
			return dev.ViaDevice, ""
		}
	}
	return "", id
}

// performanceBusy reports whether an update or install job is pending or running.
func performanceBusy(jobDir string) func() bool {
	return func() bool {
		progress, err := updateprogress.Read(jobDir)
		if err != nil || progress == nil {
			return false
		}
		return progress.State == "pending" || progress.State == "running"
	}
}
