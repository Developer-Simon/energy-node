#!/usr/bin/env python3
"""
battery_soc_mqtt.py

Batterie-Ladezustands-Schätzung (SoC) fuer zwei LiFePO4-Baenke, die am selben
Gleichstrombus haengen (MeanWell-Ladegeraet laedt, Lumentree-Umrichter entlaedt).

KONZEPT
-------
1) Coulomb-Counting (Basis-SoC):
   Die kombinierte Netto-Ladeleistung (MeanWell-Shelly minus Lumentree-Shelly)
   wird ueber die Zeit aufintegriert (Ah). Das ist die Haupt-SoC-Quelle,
   driftet aber langsam (Messfehler, Wirkungsgrad, Selbstentladung).

2) Spannungsbasierte Rekalibrierung an den Enden (0 %/100 %):
   LiFePO4-Zellen haben eine sehr flache Spannungskurve im Mittelbereich
   (ca. 15-85 % SoC) - dort ist die Spannung zur SoC-Bestimmung praktisch
   nutzlos. Nur an den beiden Enden (nahe leer/nahe voll) aendert sich die
   Spannung pro Zelle deutlich messbar. Genau dort wird der Coulomb-Zaehler
   auf 0 % bzw. 100 % zurueckgesetzt, sobald die (lastkorrigierte) Spannung
   eine Schwelle ueberschreitet.
   Die Schwellen sind Ruhespannungen, die reale Anlage erreicht sie aber
   womoeglich nie (das Ladegeraet regelt tiefer ab, der Wechselrichter
   schaltet frueher aus). Deshalb weicht ein stromabhaengiges Toleranzfenster
   sie auf, sobald der Strom klein genug ist, dass die Messung als
   Ruhespannung durchgeht - siehe CALIBRATION_TAPER_C_RATE.

3) "Spannungskurvenschar" (lastabhaengig) - vereinfacht als IR-Kompensation:
   Statt vieler getrennter Kurven fuer jeden Laststrom wird die gemessene
   Spannung um einen stromabhaengigen Offset (mV/Zelle) korrigiert, bevor sie
   mit der Ruhespannungs-Kurve verglichen wird. Das ist mathematisch aequivalent
   zu einer Kurvenschar, aber mit deutlich weniger Parametern kalibrierbar.
   Die Offset-Tabelle unten sind grobe Startwerte - sollten anhand eigener
   Beobachtung (Spannung bei bekanntem Strom, Vergleich mit Ruhespannung)
   nachjustiert werden.

4) Topologie entscheidet ueber die Bankaufloesung:
   PARALLEL haengen beide Baenke an denselben Klemmen - Kirchhoff erzwingt
   dieselbe Spannung, und der Ausgleichsstrom zwischen ihnen ist von aussen
   nicht messbar. Zwei getrennte SoC-Werte waeren dort eine vorgetaeuschte
   Aufloesung, also rechnet der Dienst EINE Batterie mit der Summenkapazitaet
   und einer Busspannung.
   IN REIHE fuehren beide Baenke denselben Strom, koennen aber im Ladezustand
   auseinanderlaufen. Dort sind zwei Zaehler mit je eigener Spannungsmessung
   richtig, und die Spannungsdifferenz ist ein echtes Unsymmetrie-Signal.

5) Eigenstaendiges HA-Geraet, verknuepft ueber via_device:
   Batterie-SoC und Trucki-Stick bleiben zwei getrennte Home-Assistant-
   Geraete (kein Discovery-Merge). Die physische Zusammengehoerigkeit wird
   stattdessen ueber "via_device" abgebildet: der Trucki-Stick
   (trucki_http_mqtt.py) traegt in seiner eigenen Konfiguration
   `via_device = "<diese battery_soc id>"` ein und erscheint damit in Home
   Assistant als "Verbunden ueber" dieses Batterie-SoC-Geraet.

HINWEIS: Dies ist eine Schaetzung fuer Monitoring/Diagnose, keine sicherheits-
kritische BMS-Funktion. Nicht fuer automatische Abschaltungen ohne zusaetzliche
Absicherung (z. B. Zell-Ueberwachung im Ladegeraet selbst) verwenden.

Nutzt paho-mqtt v2 (CallbackAPIVersion.VERSION2), analog zu den anderen
Skripten auf diesem Knoten. Der Knoten "energy-node" wird per
via_device als Elterngeraet referenziert (gleiches Muster wie im
Shelly-mqtt-discovery-self-Skript).

Nutzt das gemeinsame energy_node_common-Paket (Slave-Seite des Master/
Slave-Protokolls) fuer MQTT-Aufbau, Availability, Discovery-Publishing und
den Poll-Scheduler.

Die eigentliche SoC-Fachlogik (Coulomb-Zaehlung, Kalibrierung, Spannungs-
korrektur, Entity-Spec) lebt seit der Core-Extraktion transport-frei in
battery_soc_core - siehe docs/knowledge/src/battery-soc-how-it-works.md.
Dieses Skript bleibt der duenne MQTT-Adapter: Eingangs-Subscriptions,
State-Persistenz, Discovery-Publishing und der Aufruf von engine.tick().
"""

import dataclasses
import json
import logging
import sys
import time

import paho.mqtt.client as mqtt

from energy_node_common import Slave
from energy_node_common import appconfig
from energy_node_common import config as common_config
from energy_node_common import mqtt as common_mqtt

import mqtt_discovery
import state_store
from mqtt_inputs import apply_message, mark_configured
from soc_config import BatteryConfig, SOC_PARAM_FIELDS, input_topics, load_configs

from battery_soc_core.engine import effective_power, tick
from battery_soc_core.inputs import SocInputs, availability, sample_is_fresh, stale_groups
from battery_soc_core.simulation import simulated_bank_voltage_v
from battery_soc_core.state import SocState, set_state_of_charge

# ---------------------------------------------------------------------------
# Laufzeit-Zustand
# ---------------------------------------------------------------------------
class BatteryRuntime:
    def __init__(self, config):
        self.config = config
        self.inputs = SocInputs()
        mark_configured(config, self.inputs)
        self.state = SocState(config.soc_params())
        self.logged_calibration = {}


configs = []
runtimes = []
slave: Slave = None


# ---------------------------------------------------------------------------
# Verfuegbarkeit / Online-Status
# ---------------------------------------------------------------------------
def _input_group_topics(config):
    """Gruppenname -> die tatsaechlich konfigurierten Topics dieser Gruppe,
    DC zuerst - dieselbe Gruppierung wie battery_soc_core.inputs.input_groups,
    nur mit Topic-Strings statt configured/ts-Paaren, weil availability() die
    Topics selbst nicht kennt (Core-Layer, Transport-frei)."""
    groups = [
        ("Ladeleistung", [config.charger_dc_power_topic, config.charger_power_topic]),
        ("Umrichterleistung", [config.inverter_dc_power_topic, config.inverter_power_topic]),
        ("Busspannung" if config.topology == "parallel" else "Spannung Bank A",
         [config.bank_a_voltage_topic]),
    ]
    if config.topology == "series" and config.bank_b_enabled:
        groups.append(("Spannung Bank B", [config.bank_b_voltage_topic]))
    return {name: [topic for topic in topics if topic] for name, topics in groups}


def build_online_reason(config, inputs, avail):
    """Baut den Klartext-Grund fuer den Online-Status aus dem Core-Ergebnis
    availability(). Die Topic-Details (welches Topic genau fehlt) kennt nur
    der Adapter - der Core kennt nur Gruppen-Namen, keine Topics."""
    del inputs  # avail ist bereits aus inputs abgeleitet; nur fuer Signatur-Paritaet
    if not avail.any_configured:
        return "Keine Eingangs-Topics konfiguriert"
    if not avail.missing:
        return ""
    group_topics = _input_group_topics(config)
    missing = []
    for name in avail.missing:
        topics = group_topics.get(name, [])
        if topics:
            missing.append(f"{name} ({', '.join(topics)})")
        else:
            missing.append(f"{name} (nicht konfiguriert)")
    return "Fehlende/veraltete Topics: " + ", ".join(missing)


def publish_online_status(client, runtime, now=None, simulation_active=False):
    """Publiziert online, sobald mindestens ein Eingang aktuelle Daten liefert."""
    now = now or time.time()
    config = runtime.config
    if simulation_active:
        common_mqtt.publish_online_status(
            client, config.base_topic, True, "Simulation aktiv",
            online_payload="1", offline_payload="0",
        )
        return

    avail = availability(config.soc_params(), runtime.inputs, now)
    reason = build_online_reason(config, runtime.inputs, avail)
    common_mqtt.publish_online_status(
        client, config.base_topic, avail.available, reason,
        online_payload="1", offline_payload="0",
    )


# ---------------------------------------------------------------------------
# Haupt-Regelschleife
# ---------------------------------------------------------------------------
def _simulation_inputs(config, params, inputs, now):
    """Ersatz-SocInputs fuer den Simulationsmodus. Echte Messwerte haben auch
    hier Vorrang: nur der jeweilige AC-Kanal (Lade-/Umrichterleistung) wird
    durch den Simulations-Konstantwert ersetzt, und zwar nur, wenn er selbst
    gerade veraltet ist - so bewegt sich der Ladezustand mit der echten
    Anlage, wo sie liefert. Der ersetzte Kanal wird dabei zugleich als frisch
    markiert (configured/ts=jetzt), damit ihn die anschliessende
    Gruppen-Alterungspruefung in effective_power()/tick() nicht sofort wieder
    verwirft - in der Simulation gibt es keine echten Eingaenge, die veralten
    koennten. Aus demselben Grund gilt das auch fuer die Spannungs-Zeitstempel;
    die tatsaechliche Spannung liefert ohnehin voltages_override, nicht diese
    Werte hier. DC-Felder bleiben unangetastet: ihre reale Frische entscheidet
    weiterhin, ob effective_power() den DC- oder den AC-Pfad waehlt."""
    sim_inputs = dataclasses.replace(inputs)

    if not sample_is_fresh(inputs.charger_power_configured, inputs.charger_power_ts,
                           now, params.stale_input_s):
        sim_inputs.charger_power_w = config.simulation_charger_power_w
    sim_inputs.charger_power_configured = True
    sim_inputs.charger_power_ts = now

    if not sample_is_fresh(inputs.inverter_power_configured, inputs.inverter_power_ts,
                           now, params.stale_input_s):
        sim_inputs.inverter_power_w = config.simulation_inverter_power_w
    sim_inputs.inverter_power_configured = True
    sim_inputs.inverter_power_ts = now

    sim_inputs.bank_a_voltage_configured = True
    sim_inputs.bank_a_voltage_ts = now
    sim_inputs.bank_b_voltage_configured = True
    sim_inputs.bank_b_voltage_ts = now
    return sim_inputs


def compute_and_publish(client, runtime, dt_hours, simulation_active=False):
    now = time.time()
    config = runtime.config
    params = config.soc_params()

    if simulation_active:
        sim_inputs = _simulation_inputs(config, params, runtime.inputs, now)
        groups_stale = stale_groups(params, sim_inputs, now)
        ep = effective_power(params, sim_inputs, now, groups_stale)
        units = runtime.state.units
        if params.topology == "series":
            bank_power_w = ep.net_power_w / len(units)
            voltages_override = [
                simulated_bank_voltage_v(units[0], params, bank_power_w),
                simulated_bank_voltage_v(units[1], params, bank_power_w),
            ]
        else:
            voltages_override = [simulated_bank_voltage_v(units[0], params, ep.net_power_w)]
        result = tick(params, runtime.state, sim_inputs, now,
                      dt_hours=dt_hours, voltages_override=voltages_override)
    else:
        result = tick(params, runtime.state, runtime.inputs, now, dt_hours=dt_hours)

    for unit in runtime.state.units:
        if unit.events and unit.events[-1].iso != runtime.logged_calibration.get(unit.name):
            event = unit.events[-1]
            runtime.logged_calibration[unit.name] = event.iso
            # Eine Zeile je Kalibrierung ins Journal - ein Sprung im
            # SoC-Verlauf soll ohne MQTT-Mitschnitt nachvollziehbar sein.
            logging.warning(
                "Kalibrierung %s/%s: %.1f -> %.1f Ah (Residuum %+.1f Ah) bei "
                "%.3f V/Zelle, %.1f A, Schwelle %.3f, Haltezeit %.0f s, "
                "Taper %s, Bilanz +%.1f/-%.1f Ah",
                unit.name, event.side, event.coulomb_before_ah,
                event.coulomb_after_ah, event.residual_ah,
                event.corrected_v_per_cell, event.current_a,
                event.threshold_v_per_cell, event.hold_s, event.taper_met,
                event.charged_ah, event.discharged_ah)

    client.publish(f"{config.base_topic}/state", json.dumps(result.outputs),
                   retain=True, qos=0)
    publish_online_status(client, runtime, now, simulation_active)
    state_store.save_state(config, runtime.state)


# ---------------------------------------------------------------------------
# MQTT Callbacks
# ---------------------------------------------------------------------------
def _manual_soc_topics(config):
    """(Topic, unit_name) je manuellem SoC-Kommando dieser Anlage. Parallel
    gibt es nur den einen Zaehler (unit_name=None -> alle Einheiten),
    in Reihe je einen pro Bank."""
    base = f"{config.base_topic}/{mqtt_discovery.MANUAL_SOC_COMMAND_SUFFIX}"
    if config.topology == "series":
        return [(f"{base}/bank_a", "bank_a"), (f"{base}/bank_b", "bank_b")]
    return [(base, None)]


def _subscribe_runtime_topics(client, config):
    for topic in input_topics(config):
        if topic:
            client.subscribe(topic)
    for topic, _unit_name in _manual_soc_topics(config):
        client.subscribe(topic)


def on_connect(client, userdata, flags, reason_code, properties=None):
    for runtime in runtimes:
        config = runtime.config
        publish_online_status(
            client, runtime,
            simulation_active=slave.simulation_active_for(config.id),
        )
        mqtt_discovery.publish_discovery(client, config, runtime.state)
        mqtt_discovery.publish_simulation_discovery(client, config, slave)
        _subscribe_runtime_topics(client, config)
    slave.start(client)


def on_message(client, userdata, msg):
    now = time.time()
    payload_str = msg.payload.decode(errors="ignore")

    if slave.handle_message(client, msg.topic, payload_str):
        return

    for runtime in runtimes:
        config = runtime.config

        for topic, unit_name in _manual_soc_topics(config):
            if msg.topic == topic:
                try:
                    pct = float(payload_str)
                except ValueError:
                    return
                set_state_of_charge(runtime.state, config.soc_params(), pct,
                                    unit_name=unit_name)
                state_store.save_state(config, runtime.state)
                compute_and_publish(client, runtime, 0.0,
                                    slave.simulation_active_for(config.id))
                return

        if msg.topic in input_topics(config):
            if apply_message(config, runtime.inputs, msg.topic, payload_str, now):
                publish_online_status(client, runtime, now)
            return


def main() -> None:
    global configs, runtimes, slave, app_config, service_name
    try:
        app_config = appconfig.load(appconfig.config_path_from_argv())
    except appconfig.ConfigError as exc:
        print(f"Konfigurationsfehler: {exc}", file=sys.stderr)
        raise SystemExit(1)

    logging.basicConfig(level=app_config.log_level, format="%(asctime)s %(levelname)s %(name)s: %(message)s")
    service_name = "battery_soc"
    service_config = app_config.service(service_name)
    devices_path = app_config.devices_config("battery_soc")
    config_store = common_config.ReloadableConfig(devices_path, load_configs)
    configs = config_store.load()
    runtimes = [BatteryRuntime(config) for config in configs]
    for runtime in runtimes:
        state_store.load_state(runtime.config, runtime.state)
    client = mqtt.Client(
        callback_api_version=mqtt.CallbackAPIVersion.VERSION2,
        client_id="battery-soc-bridge",
    )
    if app_config.mqtt.username:
        client.username_pw_set(app_config.mqtt.username, app_config.mqtt.password())
    if len(configs) == 1:
        client.will_set(
            f"{configs[0].base_topic}/status/online", "0", retain=True, qos=1
        )

    def poll_core():
        now = time.time()
        for runtime in runtimes:
            dt_hours = (now - runtime.state.last_tick) / 3600.0
            compute_and_publish(
                client,
                runtime,
                dt_hours,
                slave.simulation_active_for(runtime.config.id),
            )
        slave.note_update(client)

    def reload_config():
        global configs, runtimes, app_config, service_config
        new_app_config = appconfig.load(app_config.path)
        new_service_config = new_app_config.service(service_name)
        new_configs = config_store.load_candidate()

        app_config = new_app_config
        service_config = new_service_config
        config_store.commit(new_configs)
        logging.getLogger().setLevel(new_app_config.log_level)
        slave.apply_config_defaults(
            poll_interval_s=new_service_config.poll_interval_s,
            diagnostic_multiplier=new_service_config.diagnostic_poll_multiplier,
        )
        new_runtimes = [BatteryRuntime(config) for config in new_configs]
        slave.register_devices([config.id for config in new_configs], client)
        for runtime in new_runtimes:
            state_store.load_state(runtime.config, runtime.state)
            publish_online_status(
                client,
                runtime,
                simulation_active=slave.simulation_active_for(runtime.config.id),
            )
            mqtt_discovery.publish_discovery(client, runtime.config, runtime.state)
            mqtt_discovery.publish_simulation_discovery(client, runtime.config, slave)
            _subscribe_runtime_topics(client, runtime.config)
        configs = new_configs
        runtimes = new_runtimes

    slave = Slave(
        device_id=service_config.device_id,
        poll_core=poll_core,
        default_poll_interval_s=service_config.poll_interval_s,
        default_diagnostic_multiplier=service_config.diagnostic_poll_multiplier,
        master_device_id=app_config.node.device_id,
        on_config_reload=reload_config,
        on_poll_error=lambda exc: print(f"Scheduler-Fehler: {exc}"),
    )
    slave.register_devices([config.id for config in configs])

    client.on_connect = on_connect
    client.on_message = on_message

    client.connect(app_config.mqtt.host, app_config.mqtt.port, keepalive=60)
    client.loop_start()

    try:
        while True:
            time.sleep(3600)
    except KeyboardInterrupt:
        pass
    finally:
        slave.stop()
        for config in configs:
            client.publish(f"{config.base_topic}/status/online", "0", retain=True, qos=1)
        client.loop_stop()
        client.disconnect()


if __name__ == "__main__":
    main()
