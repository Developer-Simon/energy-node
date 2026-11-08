package backup

import (
	"encoding/json"
	"reflect"
	"testing"
)

// envBase ist eine gültige Konfiguration nach dem Muster von energy-node.config.json.
const envBase = `{
  "schema_version": 2,
  "mqtt": {"host": "127.0.0.1", "port": 1883, "username": "energynode_client", "password_file": "/etc/energy-node/mqtt.pw"},
  "paths": {"devices_dir": "/home/energynode/devices", "data_dir": "/home/energynode/dashboard/data"},
  "logging": {"level": "INFO"},
  "services": {
    "apsystems": {"service_id": "apsystems", "poll_interval_s": 60, "diagnostic_poll_multiplier": 10},
    "shelly": {"service_id": "shelly", "poll_interval_s": 20, "diagnostic_poll_multiplier": 15, "http_timeout_s": 5},
    "trucki": {"service_id": "trucki", "poll_interval_s": 30, "diagnostic_poll_multiplier": 10, "http_timeout_s": 5},
    "tuya": {"service_id": "tuya", "poll_interval_s": 30, "diagnostic_poll_multiplier": 1},
    "battery_soc": {"service_id": "battery_soc", "poll_interval_s": 10, "diagnostic_poll_multiplier": 1},
    "automation": {"service_id": "automation"}
  },
  "dashboard": {
    "bind_address": "0.0.0.0", "port": 8080, "client_id": "energy-node-dashboard",
    "device_identifier": "energy-node-dashboard", "log_level": "info", "sweep_interval_seconds": 300,
    "admin_username": "admin", "admin_password_file": "/etc/energy-node-dashboard/auth.pw",
    "tls": {"cert_file": "", "key_file": ""},
    "system_action_helper": "/usr/local/sbin/energy-node-dashboard-system-action",
    "mosquitto_bridge_target": "/etc/mosquitto/conf.d/bridge.conf",
    "node_device_id": "energy_node", "node_device_name": "Energy Node",
    "node_poll_interval_s": 60, "node_diagnostic_poll_multiplier": 10
  },
  "tailscale": {"bin": "/usr/sbin/tailscale", "status_timeout_s": 10},
  "tinytuya": {"probe_python": "/usr/bin/python3", "probe_script": "/home/energynode/tuya_mqtt/tinytuya_probe.py", "probe_timeout_s": 30}
}`

// envConfig liefert envBase, nachdem edit die Felder geändert hat.
func envConfig(t *testing.T, edit func(doc map[string]any)) []byte {
	t.Helper()
	var doc map[string]any
	if err := json.Unmarshal([]byte(envBase), &doc); err != nil {
		t.Fatal(err)
	}
	if edit != nil {
		edit(doc)
	}
	out, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// section liefert den Unterblock name eines Testdokuments.
func section(doc map[string]any, name string) map[string]any {
	return doc[name].(map[string]any)
}

func rowKeys(rows []EnvRow) []string {
	keys := make([]string, 0, len(rows))
	for _, r := range rows {
		keys = append(keys, r.Key)
	}
	return keys
}

func mergedDoc(t *testing.T, archive, here []byte, use map[string]bool) map[string]any {
	t.Helper()
	out, err := MergeConfig(archive, here, use)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatal(err)
	}
	return doc
}

func TestNoRowsOnSameEnvironment(t *testing.T) {
	cfg := envConfig(t, nil)
	rows, err := EnvironmentRows(cfg, cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Fatalf("rows = %+v, want none", rows)
	}
}

func TestBrokerRowDefaultsToHere(t *testing.T) {
	archive := envConfig(t, func(doc map[string]any) {
		m := section(doc, "mqtt")
		m["host"] = "192.168.1.20"
	})
	here := envConfig(t, nil)

	rows, err := EnvironmentRows(archive, here, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := EnvRow{Key: "broker", Here: "127.0.0.1:1883", Archive: "192.168.1.20:1883"}
	if len(rows) != 1 || rows[0] != want {
		t.Fatalf("rows = %+v, want [%+v]", rows, want)
	}

	if host := section(mergedDoc(t, archive, here, nil), "mqtt")["host"]; host != "127.0.0.1" {
		t.Fatalf("ohne Wahl host = %v, want das Ziel 127.0.0.1", host)
	}
	if host := section(mergedDoc(t, archive, here, map[string]bool{"broker": true}), "mqtt")["host"]; host != "192.168.1.20" {
		t.Fatalf("mit Wahl Archiv host = %v, want 192.168.1.20", host)
	}
}

func TestPortRowDefaultsToHere(t *testing.T) {
	archive := envConfig(t, func(doc map[string]any) { section(doc, "dashboard")["port"] = 8443 })
	here := envConfig(t, nil)

	rows, err := EnvironmentRows(archive, here, nil)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"port"}; !reflect.DeepEqual(rowKeys(rows), want) || rows[0].Here != "8080" || rows[0].Archive != "8443" {
		t.Fatalf("rows = %+v, want Port 8080 gegen 8443", rows)
	}
	if port := section(mergedDoc(t, archive, here, nil), "dashboard")["port"]; port != float64(8080) {
		t.Fatalf("ohne Wahl port = %v, want 8080", port)
	}
}

func TestPathsAndTLSAlwaysFromTarget(t *testing.T) {
	archive := envConfig(t, func(doc map[string]any) {
		doc["paths"] = map[string]any{"devices_dir": "/archiv/devices", "data_dir": "/archiv/data"}
		d := section(doc, "dashboard")
		d["bind_address"] = "127.0.0.9"
		d["tls"] = map[string]any{"cert_file": "/etc/energy-node/archiv.pem", "key_file": "/etc/energy-node/archiv.key"}
		d["admin_password_file"] = "/etc/energy-node/archiv-admin.pw"
		d["mosquitto_bridge_target"] = "/etc/mosquitto/archiv.conf"
		doc["tailscale"] = map[string]any{"bin": "/usr/bin/tailscale", "status_timeout_s": 5}
		m := section(doc, "mqtt")
		m["username"] = "archiv"
		m["password_file"] = "/etc/energy-node/archiv.pw"
		m["host"] = "192.168.1.20"
	})
	here := envConfig(t, nil)

	rows, err := EnvironmentRows(archive, here, nil)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"broker", "tailscale"}; !reflect.DeepEqual(rowKeys(rows), want) {
		t.Fatalf("Zeilen = %v, want %v", rowKeys(rows), want)
	}
	if rows[1].Off != "tailscale" {
		t.Fatalf("Tailscale-Zeile = %+v, want Off tailscale", rows[1])
	}

	doc := mergedDoc(t, archive, here, map[string]bool{"broker": true})
	var want map[string]any
	if err := json.Unmarshal([]byte(envBase), &want); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"paths", "tailscale"} {
		if !reflect.DeepEqual(doc[name], want[name]) {
			t.Fatalf("%s = %v, want das Ziel %v", name, doc[name], want[name])
		}
	}
	for _, key := range []string{"bind_address", "tls", "admin_password_file", "mosquitto_bridge_target"} {
		if !reflect.DeepEqual(section(doc, "dashboard")[key], section(want, "dashboard")[key]) {
			t.Fatalf("dashboard.%s = %v, want das Ziel %v", key, section(doc, "dashboard")[key], section(want, "dashboard")[key])
		}
	}
	if u, p := section(doc, "mqtt")["username"], section(doc, "mqtt")["password_file"]; u != "energynode_client" || p != "/etc/energy-node/mqtt.pw" {
		t.Fatalf("mqtt Benutzer und Passwortdatei = %v, %v, want die des Ziels", u, p)
	}
	if host := section(doc, "mqtt")["host"]; host != "192.168.1.20" {
		t.Fatalf("mqtt.host = %v, want die Wahl Archiv 192.168.1.20", host)
	}
}

func TestBridgeOffWithoutManagedBroker(t *testing.T) {
	cfg := envConfig(t, nil)
	unmanaged := envConfig(t, func(doc map[string]any) { section(doc, "dashboard")["mosquitto_bridge_target"] = "" })
	activeBridge := []byte(`{"connections":[{"enabled":true,"name":"cloud"}]}`)
	idleBridge := []byte(`{"connections":[{"enabled":false,"name":"cloud"}]}`)

	rows, err := EnvironmentRows(cfg, unmanaged, activeBridge)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Key != "bridge" || rows[0].Off != "bridge" {
		t.Fatalf("rows = %+v, want eine Zeile bridge mit Off", rows)
	}

	rows, err = EnvironmentRows(cfg, cfg, activeBridge)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Fatalf("mit verwaltetem Broker rows = %+v, want keine", rows)
	}

	rows, err = EnvironmentRows(cfg, unmanaged, idleBridge)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Fatalf("ohne aktive Bridge rows = %+v, want keine", rows)
	}
}

func TestMergeMigratesOldArchive(t *testing.T) {
	archive := envConfig(t, func(doc map[string]any) {
		doc["schema_version"] = 1
		doc["node"] = map[string]any{"device_id": "energy_node", "device_name": "Energy Node", "poll_interval_s": 60, "diagnostic_poll_multiplier": 10}
		d := section(doc, "dashboard")
		for _, k := range []string{"node_device_id", "node_device_name", "node_poll_interval_s", "node_diagnostic_poll_multiplier"} {
			delete(d, k)
		}
	})
	here := envConfig(t, nil)

	doc := mergedDoc(t, archive, here, nil)
	if v := doc["schema_version"]; v != float64(2) {
		t.Fatalf("schema_version = %v, want 2", v)
	}
	if _, ok := doc["node"]; ok {
		t.Fatal("der Block node bleibt nach der Migration")
	}
}
