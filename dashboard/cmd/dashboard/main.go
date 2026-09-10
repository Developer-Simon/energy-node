// Command dashboard is the entry point of the Energy Node
// Dashboard. It wires process lifecycle (start, signal handling, graceful
// shutdown), the MQTT discovery client, the in-memory device registry, and
// the HTTP/web UI layer.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/appconfig"
	"github.com/Developer-Simon/energy-node-dashboard/internal/auth"
	"github.com/Developer-Simon/energy-node-dashboard/internal/basepath"
	"github.com/Developer-Simon/energy-node-dashboard/internal/config"
	"github.com/Developer-Simon/energy-node-dashboard/internal/devicefilter"
	"github.com/Developer-Simon/energy-node-dashboard/internal/energy"
	"github.com/Developer-Simon/energy-node-dashboard/internal/energydiscovery"
	"github.com/Developer-Simon/energy-node-dashboard/internal/httpapi"
	"github.com/Developer-Simon/energy-node-dashboard/internal/mqttclient"
	"github.com/Developer-Simon/energy-node-dashboard/internal/nodeagent"
	"github.com/Developer-Simon/energy-node-dashboard/internal/registry"
	"github.com/Developer-Simon/energy-node-dashboard/internal/runtimecache"
	"github.com/Developer-Simon/energy-node-dashboard/internal/settings"
	"github.com/Developer-Simon/energy-node-dashboard/internal/shellypresets"
	"github.com/Developer-Simon/energy-node-dashboard/internal/storagehealth"
	"github.com/Developer-Simon/energy-node-dashboard/internal/systemactions"
	"github.com/Developer-Simon/energy-node-dashboard/internal/tailscale"
	"github.com/Developer-Simon/energy-node-dashboard/internal/tinytuya"
)

// buildVersion is overridden at build time via
// -ldflags "-X main.buildVersion=...", computed from dashboard/VERSION plus
// a branch suffix by scripts/deploy/deploy_dashboard_to_remote.sh. Left at "dev" for
// `go run`/`go test` and any build that skips ldflags.
var buildVersion = "dev"

func main() {
	configPath := flag.String("config", appconfig.DefaultPath, "Pfad zur zentralen Konfigurationsdatei")
	flag.Parse()
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
	shellyPresetsPath := cfg.ShellyPresets()
	shellyPresetsStore := shellypresets.NewStore(shellyPresetsPath)
	configManager.ExcludeName(strings.TrimSuffix(filepath.Base(shellyPresetsPath), ".json"))
	settingsStore := settings.NewStore(dataDir)
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
	servicesVersion := ""
	if cfg.Paths.ServicesVersionFile != "" {
		if data, err := os.ReadFile(cfg.Paths.ServicesVersionFile); err != nil {
			log.Printf("energy-node-dashboard: services version file unreadable: %v", err)
		} else {
			servicesVersion = strings.TrimSpace(string(data))
		}
	}
	adminPassword, err := cfg.AdminPassword()
	var adminAuthWarning string
	if err != nil {
		log.Printf("energy-node-dashboard: Admin-Passwort nicht lesbar, Anmeldung eingeschraenkt: %v", err)
		adminAuthWarning = "Admin-Anmeldedaten konnten nicht geladen werden (siehe Server-Log). Bitte die Konfiguration pruefen; bis dahin ist nur der Gastzugang verfuegbar."
	}
	authManager, err := auth.NewManager(filepath.Join(dataDir, "users.json"), cfg.Dashboard.AdminUsername, adminPassword)
	if err != nil {
		log.Fatalf("energy-node-dashboard: auth store failed: %v", err)
	}
	systemExecutor := systemactions.NewExecutor(nil, cfg.Dashboard.SystemActionHelper)
	tailscaleClient := tailscale.NewClient(cfg.Tailscale.Bin, systemactions.ExecOutputRunner{}, time.Duration(cfg.Tailscale.StatusTimeoutS)*time.Second)
	runtimeStore := runtimecache.NewStore(dataDir)
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
	if err := client.Connect(); err != nil {
		log.Fatalf("energy-node-dashboard: mqtt connect failed: %v", err)
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
		TailscaleBin:             cfg.Tailscale.Bin,
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

	// Bei jedem (Re-)Connect die eigene HA-Discovery retained neu absetzen.
	// Der Schalter wird bei jedem Aufruf frisch gelesen, damit ein
	// "Speichern und neu verbinden" ihn sofort anwendet; bei false trägt
	// energydiscovery.Configs leere Payloads (Removal) auf dieselben Topics.
	client.SetConnectPublisher(func() []mqttclient.OutboundMessage {
		publish := true
		if stored, err := settingsStore.LoadMQTT(); err == nil {
			publish = stored.PublishEnergyDevice
		}
		configs := energydiscovery.Configs(client.DiscoveryPrefix(), buildVersion, publish)
		msgs := make([]mqttclient.OutboundMessage, 0, len(configs))
		for _, cfg := range configs {
			msgs = append(msgs, mqttclient.OutboundMessage{
				Topic:   cfg.Topic,
				Payload: string(cfg.Payload),
				Retain:  true,
			})
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

	if bridgeCfg, err := settingsStore.LoadBridge(); err != nil {
		log.Printf("energy-node-dashboard: bridge config ignored: %v", err)
	} else if len(bridgeCfg.Connections) > 0 {
		client.SetBridgeWatch(bridgeCfg.Connections[0].RemoteClientID)
	}
	configManager.SetReloadFunc(func(name string, _ []byte) error {
		if serviceID, ok := serviceIDForConfig(name); ok {
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
				value, err := settingsStore.LoadSettings()
				if err != nil {
					continue
				}
				if lastSweep.IsZero() || time.Since(lastSweep) >= time.Duration(value.SweepIntervalSeconds)*time.Second {
					if err := runtimeStore.Sweep(reg.Snapshot(), time.Now().UTC()); err != nil {
					}
					lastSweep = time.Now()
				}
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

	srv := &http.Server{
		Addr: net.JoinHostPort(bindAddress, port),
		// basepath.Middleware sits outermost so the router only ever sees
		// root-relative paths, even when a reverse proxy serves the dashboard
		// under a subpath (see knowhow/dashboard/dashboard-reverse-proxy-unterpfad.md).
		Handler: basepath.Middleware(httpapi.NewAuthenticatedRouter(reg, configManager, settingsStore, tinytuya.NewClient(
			cfg.TinyTuya.ProbePython,
			cfg.TinyTuya.ProbeScript,
			probeTimeout,
		), credentialStore, client, storageProvider, runtimeStore, httpapi.RouterDependencies{
			MQTT:              client,
			MQTTReconfigure:   client,
			MQTTCredentials:   mqttCredentialStore,
			Reloader:          client,
			Auth:              authManager,
			AdminAuthWarning:  adminAuthWarning,
			SystemActions:     systemExecutor,
			StartedAt:         startedAt,
			DeviceFilter:      deviceFilterStore,
			DeviceActions:     client,
			ShellyPresets:     shellyPresetsStore,
			Version:           buildVersion,
			ServicesVersion:   servicesVersion,
			BridgeCredentials: bridgeCredentialStore,
			MQTTBridgeWatcher: client,
			DataDir:           dataDir,
			AppConfigPath:     *configPath,
			BridgeTargetPath:  cfg.Dashboard.MosquittoBridgeTarget,
			Tailscale:         tailscaleClient,
			Resolver:          energyResolver,
			MQTTBase:          mqttBase,
			NodeAgent:         nodeAgent,
			NodeSimulation:    nodeSimPublisher,
		})),
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				payload, err := buildBalancePayload(reg, energyResolver, time.Now())
				if err != nil {
					log.Printf("energy-node-dashboard: energy balance marshal failed: %v", err)
					continue
				}
				if err := client.PublishRetained(energydiscovery.StateTopic, string(payload)); err != nil {
					log.Printf("energy-node-dashboard: energy balance publish skipped: %v", err)
				}
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
		publish(nodeAgent.StateMessages(ctx, metricEnabled)...) // beim Start einmal voll
		for {
			select {
			case <-ctx.Done():
				return
			case <-fast.C:
				publish(nodeAgent.StateMessage(ctx, metricEnabled))
			case <-slow.C:
				publish(nodeAgent.DiagnosticsMessage(ctx, metricEnabled))
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

	<-ctx.Done()

	log.Println("energy-node-dashboard: shutdown signal received, shutting down...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("energy-node-dashboard: graceful shutdown failed: %v", err)
	}
	if flusher, ok := storageProvider.(storagehealth.Flusher); ok {
		if err := flusher.Flush(); err != nil {
			log.Printf("energy-node-dashboard: storage health flush failed: %v", err)
		}
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

func buildBalancePayload(reg *registry.Registry, resolver *energy.Resolver, now time.Time) ([]byte, error) {
	snapshot := energy.Aggregate(reg.Snapshot(), resolver, now.UTC())
	cfg := resolver.Interpretation()
	full := snapshot.WithInterpretation(cfg)
	return json.Marshal(struct {
		At             int64                 `json:"at"`
		Balance        energy.Balance        `json:"balance"`
		Interpretation energy.Interpretation `json:"interpretation"`
	}{At: now.Unix(), Balance: full.Balance, Interpretation: cfg})
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

func serviceIDForConfig(name string) (string, bool) {
	aliases := map[string]string{
		"automation_rules": "automation",
	}
	if id, ok := aliases[name]; ok {
		return id, true
	}
	const suffix = "_devices"
	if !strings.HasSuffix(name, suffix) {
		return "", false
	}
	return strings.TrimSuffix(name, suffix), true
}
