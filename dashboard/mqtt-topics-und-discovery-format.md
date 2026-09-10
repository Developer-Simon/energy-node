# MQTT-Topics und Discovery-Format der vorhandenen Bridges

Dieses Dokument hält fest, welchen Ausschnitt des Home-Assistant-MQTT-
Discovery-Standards die vorhandenen Python-Bridges tatsächlich verwenden.

**Wichtig (siehe Kernprinzip im Implementierungsplan):** Diese Dokumentation
ist NICHT die Spezifikationsquelle für den Discovery-Parser des Dashboards.
Der Parser liest den HA-Discovery-Standard selbst, nicht bridge-spezifischen
Code. Dieses Dokument dient nur als Grundlage für die bewusste
Scope-Entscheidung, welcher Teil des Standards (3- vs. 4-Ebenen-Topics,
Retain-Verhalten) in Phase 1-3 tatsächlich implementiert werden muss.

**Verifizierungsstand:** Die Angaben wurden gegen die vorhandenen Bridge- und
Common-Quellen (`apsystems_ez1_mqtt.py`, `battery_soc_mqtt.py`,
`shelly_rpc_mqtt.py`, `tuya_mqtt.py`, `energy_node_mqtt.py`,
`energy_node_common/discovery.py`, `energy_node_common/mqtt.py`)
gegengeprüft. Die Doku beschreibt damit die aktuell implementierte
Bridge-Konvention; sie ersetzt weiterhin nicht die HA-Discovery-Spezifikation.

## Topic-Konventionen

- **Zustands-Topics:** `outstation/{device_id}/...` - das ist ein Präfix,
  unter dem jede Bridge die Zustände ihrer Geräte als einzelne Topics
  veröffentlicht. Beispiele sind `outstation/tuya_valve/switch`,
  `outstation/{shelly_id}/relay/0`,
  `outstation/{apsystems_id}/pv1/power_w` und
  `outstation/battery_soc/state`.
- **Discovery-Konfiguration:** `homeassistant/{component}/{device_id}/{object_id}/config`
  - Das ist eine **3-Ebenen-Discovery-Struktur** (`component` /
    `device_id` / `object_id`), **ohne** die optionale 4. Ebene
    (`node_id`) des HA-Standards.
  - Die gemeinsame Funktion `publish_discovery()` veröffentlicht jedes
    Discovery-JSON mit `qos=0` und `retain=True`. Eine leere retained
    Discovery-Payload wird außerdem genutzt, um die frühere
    APsystems-Sensor-Discovery `sensor/{device_id}/power_status` zu löschen.
  - Der Dashboard-Discovery-Parser deckt in Phase 1-3 bewusst nur diese
    3-Ebenen-Form ab. Ein 4-Ebenen-Format (`node_id`) ist erst in Phase 5
    vorgesehen, falls tatsächlich eine Bridge oder ein Fremdgerät es
    benötigt (siehe Kernprinzip, Scope-Reduktion).
- **Zentraler Knoten:** `internal/nodeagent` im Dashboard veröffentlicht den
  Knoten selbst als eigenes HA-Gerät (`energy_node`), damit andere
  Geräte sich per `via_device` daran anhängen können.
- **Geräteverknüpfung:** APsystems, Shelly, Tuya und der Batterie-SoC-Block
  verwenden `via_device: energy_node`; der Trucki-Stick verwendet
  `via_device` konfigurierbar je Gerät (aktuell auf den zugehörigen
  Batterie-SoC-Eintrag gesetzt, da beide physisch dasselbe Gerät sind). Kein
  Discovery-Merge zwischen den Bridges — jedes Gerät bleibt eigenständig.

## Verifizierte Objekt-IDs und Payload-Muster

Die Objekt-IDs werden direkt Bestandteil von `unique_id` und des
Discovery-Topics (`{device_id}_{object_id}`). Repräsentative, nicht
vollständige Beispiele:

| Bridge | Komponente / Objekt-ID | Zustands-Payload |
|---|---|---|
| `apsystems_ez1_mqtt.py` | `sensor/pv1_power`, `number/max_power_limit`, `switch/power_status` | Einzelwerte auf `/pv1/power_w`, `/max_power_limit_w`, `/power_status` |
| `battery_soc_mqtt.py` | `sensor/soc_a`, `sensor/soc_b`, `sensor/corrected_v_a`, `binary_sensor/inputs_stale`, `binary_sensor/imbalance_warning` | JSON auf `/state`, Auswahl per `value_template` |
| `shelly_rpc_mqtt.py` | `switch/relay` oder `switch/relay_{n}`, `sensor/power_{n}`, `sensor/adc_{n}` | Einzelwerte auf `/relay/{n}`, `/power_{n}`, `/adc/{n}` |
| `trucki_http_mqtt.py` | `sensor/{key}` (dynamisch pro gefundenem Feld, z. B. `sensor/vgrid`), `binary_sensor/online` | Einzelwerte auf `/field/{key}` |
| `tuya_mqtt.py` | `switch/switch`, `binary_sensor/online` | `ON`/`OFF` auf `/switch` und `1`/`0` auf `/status/online` je konfiguriertem Gerät |
| `energy_node_mqtt.py` | `sensor/cpu_temp`, `binary_sensor/undervoltage_now`, `sensor/ip_address` | JSON auf `/state` beziehungsweise `/diagnostics` |

Die üblichen Discovery-Payloads enthalten `name`, `unique_id`, `device`,
`state_topic` sowie `availability_topic` mit `payload_available: "1"` und
`payload_not_available: "0"`. Schalter ergänzen `command_topic` sowie
`payload_on`/`payload_off`; JSON-Zustände ergänzen ein
`value_template`.

## Retain-Verhalten

`energy_node_common/mqtt.py` setzt State- und JSON-Publishes standardmäßig
mit `retain=True` ab. Auch Availability (`status/online`, Attribute und das
Legacy-Topic `status/last_update`) wird über diesen Helper retained. Die
Discovery-Konfigurationen sind unabhängig davon immer retained.

Für alle Bridges gelten nun HA-konforme Retain-Defaults:

| Bridge | Betroffenes Topic | Retain |
|---|---|---|
| `battery_soc_mqtt.py` | `/state` | `True` |
| `energy_node_mqtt.py` | `/state` und `/diagnostics` | `True` |
| alle übrigen Bridges (Standard aus `energy_node_common/mqtt.py`) | Zustands-Topics | `True` |

Damit liefert der Broker nach einem Reconnect wieder den letzten veröffentlichten
Zustand zurück. Der Dashboard-Parser kann sich weiterhin auf eine Runtime-Cache-
Absicherung verlassen, muss diese beiden Fälle aber nicht mehr als
Retain-Sonderfall behandeln.

## Angebundene Bridges (Übersicht)

| Datei | Gerätefamilie |
|---|---|
| `apsystems_ez1_mqtt.py` | AP Systems EZ1 Microinverter |
| `battery_soc_mqtt.py` | Batterie-Ladezustand (SoC) |
| `shelly_rpc_mqtt.py` | Shelly-Geräte (RPC/Status-Abfrage) |
| `trucki_http_mqtt.py` | Trucki-Sticks (T2SG/T2MG/T2HG, HTTP-Poll statt geräteeigenem MQTT) |
| `tuya_mqtt.py` | Tuya-Geräte, zunächst WLAN-Wasserventile |
| `energy_node_mqtt.py` | zentraler Knoten / Master |

## Dashboard als Discovery-Publisher

Zusätzlich zum Parsen fremder Discovery veröffentlicht das Dashboard **sich
selbst** als ein HA-Gerät für seine server-seitig berechneten Energiewerte
(`internal/energydiscovery`).

- **Gerät:** `identifiers: ["energy_node"]`, Name
  „Energy Node", `manufacturer: "Energy Node"`,
  `model: "Dashboard Energy"`, `sw_version` = Dashboard-Build-Version. Kein
  `via_device`. Die sieben Energie-Sensoren sind damit Teil desselben
  `energy_node`-HA-Geräts wie die Systemdiagnose aus `internal/nodeagent`
  (gleiches Pfadsegment, kollisionsfreie `object_id`s).
- **Discovery-Topics:** `<discovery_prefix>/sensor/energy_node/<object_id>/config`,
  retained, `qos=0`, `unique_id: energy_node_<object_id>`.
- **Sieben Sensoren** (`state_class: measurement`), alle mit
  `state_topic: outstation/energy_node/energy/balance` und
  `value_template: {{ value_json.balance['<feld>'] }}`. Die Subscript-Form
  (nicht `value_json.balance.<feld>`) ist Absicht: Home Assistant versteht
  beide, aber der value_template-Parser des Dashboards selbst
  (`internal/registry`) deckt nur eine Punktebene plus optionalem `['key']`
  ab — mit der zweistufigen Punkt-Form bliebe die eigene Energie-Kachel
  unaufgelöst:

  | object_id | balance-Feld | device_class | Einheit |
  |---|---|---|---|
  | `pv_power` | `pv` | power | W |
  | `grid_import` | `grid_import` | power | W |
  | `grid_export` | `grid_export` | power | W |
  | `battery_charge` | `battery_charge` | power | W |
  | `battery_discharge` | `battery_discharge` | power | W |
  | `house_load` | `load_total` | power | W |
  | `battery_soc` | `battery_soc` | battery | % |

- **State-Topic:** `outstation/energy_node/energy/balance` wird alle 10 s
  **retained** veröffentlicht (`{"at":…,"balance":…,"interpretation":…}`).
- **Erreichbarkeit:** retained MQTT-LWT `outstation/energy_node/status/online`
  = `0`, retained Birth `1` bei Connect; jede Config verweist mit
  `availability_topic` darauf (`payload_available: "1"`,
  `payload_not_available: "0"`).
- **Schalter:** `MQTTConfig.publish_energy_device` (Default an). Bei `aus`
  setzt das Dashboard beim nächsten Connect leere retained Payloads auf die
  sieben Discovery-Topics (HA-Removal). Der Schalter wirkt beim nächsten
  (Re-)Connect, nicht sofort.
- **Nicht angeboten:** kWh-Zähler / HA-Energie-Dashboard (nur
  Momentanleistung vorhanden — in HA ggf. per Riemann-Sum-Helper bilden),
  Wallbox/Wärmepumpe/Autarkie/Eigenverbrauch, Command-Topics.

## Kommandos an Geräte

Der Dashboard-Service verändert keine Gerätewerte ohne ausdrückliche
Benutzeraktion. Falls später implementiert, erfolgen Kommandos ausschließlich
als MQTT-Publish auf die jeweiligen `.../set`-Topics - kein direkter
HTTP-Sonderweg zum Gerät.

## Offene Punkte vor Phase 1

- [ ] Für reale Parser-Tests je Komponententyp (`sensor`, `binary_sensor`,
  `switch`, `number`) eine tatsächlich gesendete Discovery-Payload aus
  dem Broker mitschneiden; die Quellcode-Muster sind jetzt verifiziert.
- [ ] Die dynamischen Geräte-IDs und Shelly-Kanalzahlen aus den jeweiligen
  Environment-/Gerätekonfigurationen für die Test-Fixtures festlegen.
- [ ] Beim Tuya-Ventil den konfigurierten Data-Point (`datapoints.switch`) am
  realen Gerät bestätigen; das beeinflusst den Zustandswert, nicht das
  Discovery-Topic.
