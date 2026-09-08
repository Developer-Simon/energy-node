"""Tests fuer battery_soc_mqtt.py.

Muster wie services/trucki/tests/: das Modul wird direkt importiert, MQTT wird
durch eine FakeClient-Klasse ersetzt. Ausfuehren mit
`.venv/bin/pytest services/battery_soc/tests` (Projekt-venv, nie globales python).

Die reine SoC-Fachlogik (Coulomb-Zaehlung, Kalibrierung, Spannungskorrektur,
Entity-Spec) ist nach battery_soc_core ausgelagert und dort getestet
(libs/battery_soc_core/tests/). Dieses Modul deckt nur noch die
Adapter-Verantwortung ab: MQTT-Eingaenge, Konfigurationsladung/-validierung,
Simulationsmodus-Verdrahtung und das manuelle SoC-Kommando.
"""

import dataclasses
import json
import sys
import time
from pathlib import Path

import pytest

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

import battery_soc_mqtt as battery_soc
import mqtt_discovery
import soc_config
import state_store
from battery_soc_core.entities import entity_specs

MODULE_DIR = Path(__file__).resolve().parents[1]


class FakeClient:
    def __init__(self):
        self.published = []
        self.published_kwargs = []

    def publish(self, topic, payload=None, qos=0, retain=True):
        self.published.append((topic, payload))
        self.published_kwargs.append((qos, retain))

    def state_payload(self):
        for topic, payload in reversed(self.published):
            if topic.endswith("/state"):
                return json.loads(payload)
        raise AssertionError("kein /state-Payload publiziert")

    def tuning_payload(self):
        for topic, payload in reversed(self.published):
            if topic.endswith("/tuning"):
                return json.loads(payload)
        raise AssertionError("kein /tuning-Payload publiziert")


class FakeMessage:
    """paho liefert Bytes; on_message dekodiert selbst."""

    def __init__(self, topic, payload):
        self.topic = topic
        self.payload = payload.encode()


class FakeSlave:
    """on_message fragt zuerst den Slave, ob die Nachricht ihm gehoert."""

    def handle_message(self, client, topic, payload_str):
        return False

    def simulation_active_for(self, device_id):
        return False


def device_config(**overrides):
    defaults = dict(
        id="battery_soc",
        name="Batterie-Ladezustand",
        charger_power_topic="shelly/netz_meanwell/power",
        inverter_power_topic="shelly/netz_lumentree/power",
        bank_a_voltage_topic="bms/bank_a/voltage",
        bank_b_voltage_topic="bms/bank_b/voltage",
        state_file=Path("/nonexistent/battery-soc-test/state.json"),
    )
    defaults.update(overrides)
    return battery_soc.BatteryConfig(**defaults)


def make_runtime(fresh_inputs=True, **overrides):
    """Runtime mit aktuellen Eingangsdaten, damit ein Test nur das aendern
    muss, worum es ihm geht."""
    runtime = battery_soc.BatteryRuntime(device_config(**overrides))
    if fresh_inputs:
        now = time.time()
        inputs = runtime.inputs
        inputs.bank_a_voltage_v = 26.8
        inputs.bank_b_voltage_v = 26.8
        inputs.bank_a_voltage_ts = now
        inputs.bank_b_voltage_ts = now
        inputs.charger_power_ts = now
        inputs.inverter_power_ts = now
    return runtime


def parallel_runtime(**overrides):
    defaults = dict(topology="parallel", bank_b_voltage_topic="")
    defaults.update(overrides)
    return make_runtime(**defaults)


def series_runtime(**overrides):
    defaults = dict(topology="series", bank_a_capacity_ah=100.0,
                    bank_b_capacity_ah=100.0)
    defaults.update(overrides)
    return make_runtime(**defaults)


@pytest.fixture(autouse=True)
def _no_state_persistence(monkeypatch):
    # save_state() schluckt Fehler ohnehin, aber ein Schreibversuch pro Tick
    # ist unnoetig - und load_state() darf keine echte Datei finden.
    monkeypatch.setattr(state_store, "save_state", lambda *a: None)


# ---------------------------------------------------------------------------
# Entity-Spec gegen State-Payload
# ---------------------------------------------------------------------------
def test_parallel_entities_do_not_reference_missing_payload_keys():
    runtime = parallel_runtime()
    client = FakeClient()
    battery_soc.compute_and_publish(client, runtime, dt_hours=0.0)
    payload = client.state_payload()

    referenced = {desc.value_key for desc in entity_specs(runtime.config.soc_params())}
    assert referenced <= set(payload), sorted(referenced - set(payload))


def test_series_entities_do_not_reference_missing_payload_keys():
    runtime = series_runtime()
    client = FakeClient()
    battery_soc.compute_and_publish(client, runtime, dt_hours=0.0)
    payload = client.state_payload()

    referenced = {desc.value_key for desc in entity_specs(runtime.config.soc_params())}
    assert referenced <= set(payload), sorted(referenced - set(payload))


# ---------------------------------------------------------------------------
# Konfiguration
# ---------------------------------------------------------------------------
def test_shipped_config_loads():
    configs = battery_soc.load_configs(MODULE_DIR / "battery_soc_devices.json")
    assert len(configs) == 1
    assert configs[0].id == "battery_soc"


def test_topology_defaults_to_parallel():
    assert device_config().topology == "parallel"


def test_load_configs_rejects_unknown_topology(tmp_path):
    path = tmp_path / "battery_soc_devices.json"
    path.write_text(json.dumps([{"id": "b", "name": "B", "topology": "sternschaltung"}]))
    with pytest.raises(ValueError, match="topology"):
        battery_soc.load_configs(path)


def test_load_configs_accepts_both_topologies(tmp_path):
    path = tmp_path / "battery_soc_devices.json"
    path.write_text(json.dumps([
        {"id": "p", "name": "P", "topology": "parallel",
         "bank_a_voltage_topic": "bus/v"},
        {"id": "s", "name": "S", "topology": "series",
         "bank_a_voltage_topic": "bank_a/v", "bank_b_voltage_topic": "bank_b/v"},
    ]))
    configs = battery_soc.load_configs(path)
    assert [c.topology for c in configs] == ["parallel", "series"]


def test_parallel_rejects_a_second_voltage_measurement(tmp_path):
    """In Parallelschaltung erzwingt Kirchhoff V_a == V_b - eine zweite
    Messung liefert nur Rauschen und taeuscht eine Bankaufloesung vor."""
    path = tmp_path / "battery_soc_devices.json"
    path.write_text(json.dumps([{
        "id": "b", "name": "B", "topology": "parallel",
        "bank_a_voltage_topic": "bus/v", "bank_b_voltage_topic": "bus2/v",
    }]))
    with pytest.raises(ValueError, match="bank_b_voltage_topic"):
        battery_soc.load_configs(path)


def test_series_requires_two_distinct_voltage_topics(tmp_path):
    path = tmp_path / "battery_soc_devices.json"
    path.write_text(json.dumps([{
        "id": "b", "name": "B", "topology": "series",
        "bank_a_voltage_topic": "bus/v", "bank_b_voltage_topic": "bus/v",
    }]))
    with pytest.raises(ValueError, match="verschiedene"):
        battery_soc.load_configs(path)


def test_series_requires_bank_b(tmp_path):
    path = tmp_path / "battery_soc_devices.json"
    path.write_text(json.dumps([{
        "id": "b", "name": "B", "topology": "series", "bank_b_enabled": False,
        "bank_a_voltage_topic": "a/v", "bank_b_voltage_topic": "b/v",
    }]))
    with pytest.raises(ValueError, match="bank_b_enabled"):
        battery_soc.load_configs(path)


def test_series_requires_equal_capacities(tmp_path):
    """In Reihe fliesst derselbe Strom durch beide Baenke - ungleiche
    Kapazitaeten sind ein Konfigurationsfehler, kein zu modellierender Fall."""
    path = tmp_path / "battery_soc_devices.json"
    path.write_text(json.dumps([{
        "id": "b", "name": "B", "topology": "series",
        "bank_a_capacity_ah": 100, "bank_b_capacity_ah": 120,
        "bank_a_voltage_topic": "a/v", "bank_b_voltage_topic": "b/v",
    }]))
    with pytest.raises(ValueError, match="Kapazitaeten"):
        battery_soc.load_configs(path)


def test_duplicate_topic_inside_one_config_names_that_config_once(tmp_path):
    """Regression: die Kollisionspruefung zaehlte Vorkommen statt Anlagen und
    meldete dieselbe id doppelt als 'mehrere Batterieanlagen'."""
    path = tmp_path / "battery_soc_devices.json"
    path.write_text(json.dumps([{
        "id": "b", "name": "B", "topology": "parallel",
        "charger_power_topic": "shelly/x", "inverter_power_topic": "shelly/x",
    }]))
    with pytest.raises(ValueError) as excinfo:
        battery_soc.load_configs(path)
    assert "mehreren Batterieanlagen" not in str(excinfo.value)
    assert "shelly/x" in str(excinfo.value)


def test_schema_properties_match_dataclass_fields():
    """Faengt 'Schema-Key ergaenzt, Dataclass-Feld vergessen' in beide
    Richtungen - durch additionalProperties: false plus BatteryConfig(**values)
    waere das sonst ein harter Reload-Fehler erst auf dem Pi."""
    schema = json.loads((MODULE_DIR / "battery_soc_devices.schema.json").read_text())
    assert set(schema["items"]["properties"]) == set(battery_soc.BatteryConfig.__dataclass_fields__)


def test_json_key_defaults_mean_bare_number():
    """Ein NICHT gesetzter json_key muss 'das Payload ist eine nackte Zahl'
    bedeuten - nie einen geratenen Feldnamen eines bestimmten Herstellers.

    Das Dashboard speichert ein leeres optionales Feld als weggelassenen Key
    (config.page.js readNode), es kann 'ausdruecklich leer' also gar nicht
    ausdruecken. Steht dann ein Herstellername wie 'apower' als Default, ist
    'kein JSON-Key' ueber die Oberflaeche unerreichbar - und ein Topic mit
    nacktem Zahlenpayload (die Trucki-Sticks publizieren so) wird still
    verworfen, bis der Eingang als veraltet gilt. Vorschlaege gehoeren in die
    <datalist> aus dem echten Payload, nicht in den Default."""
    schema = json.loads((MODULE_DIR / "battery_soc_devices.schema.json").read_text())
    properties = schema["items"]["properties"]
    fields = battery_soc.BatteryConfig.__dataclass_fields__

    offenders = {}
    for key in fields:
        if not key.endswith("_json_key"):
            continue
        if fields[key].default != "":
            offenders[f"{key} (dataclass)"] = fields[key].default
        if properties[key].get("default", "") != "":
            offenders[f"{key} (schema)"] = properties[key]["default"]
    assert offenders == {}


def test_schema_defaults_match_dataclass_defaults():
    """Beide Defaults muessen denselben Wert nennen, weil das Dashboard ein
    Feld, das auf seinem Schema-Default steht, als WEGGELASSEN speichert
    (config.page.js readNode: schemaDefault -> undefined). Den weggelassenen
    Key loest dann die Dataclass auf. Gehen die beiden auseinander, zeigt das
    Formular einen anderen Wert an, als der Dienst tatsaechlich benutzt - und
    es gibt keinen Weg, ueber das Dashboard den Dataclass-Wert zu treffen.

    Genau so war charger_power_json_key/inverter_power_json_key auf 'apower'
    festgenagelt, obwohl die Trucki-Topics nackte Zahlen liefern: der Eingang
    wurde still verworfen und galt nach stale_input_s als veraltet."""
    schema = json.loads((MODULE_DIR / "battery_soc_devices.schema.json").read_text())
    fields = battery_soc.BatteryConfig.__dataclass_fields__

    mismatches = {}
    for key, prop in schema["items"]["properties"].items():
        if "default" not in prop:
            continue
        expected = fields[key].default
        if expected is dataclasses.MISSING:
            continue  # Pflichtfeld (id/name): der Schema-Default ist nur ein Startwert
        if isinstance(expected, Path):
            expected = str(expected)
        if prop["default"] != expected:
            mismatches[key] = {"schema": prop["default"], "dataclass": expected}
    assert mismatches == {}


def test_load_configs_rejects_impossible_efficiency(tmp_path):
    path = tmp_path / "battery_soc_devices.json"
    path.write_text(json.dumps([
        {"id": "b", "name": "B", "charger_ac_dc_efficiency": 0.0},
    ]))
    with pytest.raises(ValueError, match="charger_ac_dc_efficiency"):
        battery_soc.load_configs(path)


def test_load_configs_accepts_a_single_bank_installation(tmp_path):
    """Bank B abgeschaltet: die Bank-B-Pruefungen duerfen nicht mehr greifen."""
    path = tmp_path / "battery_soc_devices.json"
    path.write_text(json.dumps([
        {"id": "b", "name": "B", "bank_b_enabled": False, "bank_b_capacity_ah": 0},
    ]))
    assert battery_soc.load_configs(path)[0].bank_b_enabled is False


def test_dc_power_topics_are_part_of_the_input_topics():
    config = device_config(
        charger_dc_power_topic="outstation/t2mg/field/dcpower",
        inverter_dc_power_topic="outstation/t2sg/field/dcpower",
    )
    topics = soc_config.input_topics(config)
    assert "outstation/t2mg/field/dcpower" in topics
    assert "outstation/t2sg/field/dcpower" in topics


def test_dc_power_messages_land_in_the_inputs(monkeypatch):
    runtime = make_runtime(
        charger_dc_power_topic="outstation/t2mg/field/dcpower",
        inverter_dc_power_topic="outstation/t2sg/field/dcpower",
        inverter_dc_power_json_key="DCPOWER",
    )
    monkeypatch.setattr(battery_soc, "runtimes", [runtime])
    monkeypatch.setattr(battery_soc, "slave", FakeSlave())
    client = FakeClient()

    battery_soc.on_message(client, None, FakeMessage("outstation/t2mg/field/dcpower", "310.5"))
    battery_soc.on_message(client, None, FakeMessage("outstation/t2sg/field/dcpower", '{"DCPOWER": 42.0}'))

    assert runtime.inputs.charger_dc_power_w == 310.5
    assert runtime.inputs.inverter_dc_power_w == 42.0
    assert runtime.inputs.charger_dc_power_ts > 0
    assert runtime.inputs.inverter_dc_power_ts > 0


def test_series_reads_both_voltage_topics(monkeypatch):
    """In Reihenschaltung muessen BEIDE Spannungsmessungen ankommen - sie sind
    dort die einzige bankeigene Information. Ohne konfigurierte Leistungs-
    Topics bleiben die Energiefluss-Eingaenge veraltet."""
    runtime = battery_soc.BatteryRuntime(device_config(
        topology="series",
        bank_a_voltage_topic="bms/bank_a/voltage",
        bank_b_voltage_topic="bms/bank_b/voltage"))
    monkeypatch.setattr(battery_soc, "runtimes", [runtime])
    monkeypatch.setattr(battery_soc, "slave", FakeSlave())
    client = FakeClient()

    battery_soc.on_message(client, None, FakeMessage("bms/bank_a/voltage", "25.4"))
    battery_soc.on_message(client, None, FakeMessage("bms/bank_b/voltage", "27.1"))

    assert runtime.inputs.bank_a_voltage_v == pytest.approx(25.4)
    assert runtime.inputs.bank_b_voltage_v == pytest.approx(27.1)

    battery_soc.compute_and_publish(client, runtime, dt_hours=0.0)
    payload = client.state_payload()
    assert payload["inputs_stale"] is True
    assert "Ladeleistung" in payload["stale_inputs"]
    assert "Umrichterleistung" in payload["stale_inputs"]


# ---------------------------------------------------------------------------
# Simulation (Ende-zu-Ende ueber den Adapter)
# ---------------------------------------------------------------------------
def test_simulation_uses_real_power_values_when_the_topics_deliver():
    runtime = parallel_runtime(simulation_charger_power_w=999.0, simulation_inverter_power_w=999.0)
    runtime.inputs.charger_power_w = 100.0
    runtime.inputs.inverter_power_w = 0.0

    client = FakeClient()
    battery_soc.compute_and_publish(client, runtime, dt_hours=0.0, simulation_active=True)

    assert client.state_payload()["net_power_w"] == pytest.approx(round(100.0 * 0.9, 1))


def test_simulation_falls_back_to_the_configured_power_when_nothing_arrives():
    runtime = parallel_runtime(fresh_inputs=False, simulation_charger_power_w=200.0,
                           simulation_inverter_power_w=50.0)

    client = FakeClient()
    battery_soc.compute_and_publish(client, runtime, dt_hours=0.0, simulation_active=True)

    expected = 200.0 * 0.9 - 50.0 / 0.9
    assert client.state_payload()["net_power_w"] == pytest.approx(round(expected, 1))


def test_simulation_charges_the_bank_until_it_calibrates_to_full():
    """Der geschlossene Kreis: Leistung fuellt den Zaehler, die Spannung folgt
    der Kurve, am oberen Ende greift die Rekalibrierung."""
    runtime = parallel_runtime(fresh_inputs=False, bank_b_enabled=False,
                           calibration_hold_s=0.0,
                           simulation_charger_power_w=500.0,
                           simulation_inverter_power_w=0.0)
    runtime.state.units[0].coulomb_ah = 0.0

    client = FakeClient()
    for _ in range(60):
        battery_soc.compute_and_publish(client, runtime, dt_hours=0.5, simulation_active=True)

    payload = client.state_payload()
    assert payload["soc_combined_pct"] == 100.0
    assert runtime.state.units[0].last_calibration_iso is not None
    assert payload["pack_voltage_v"] >= 8 * runtime.config.full_v_per_cell


def test_simulation_voltage_starts_low_on_an_empty_bank():
    runtime = parallel_runtime(fresh_inputs=False, bank_b_enabled=False,
                           simulation_charger_power_w=0.0,
                           simulation_inverter_power_w=0.0)
    runtime.state.units[0].coulomb_ah = 0.0

    client = FakeClient()
    battery_soc.compute_and_publish(client, runtime, dt_hours=0.0, simulation_active=True)

    assert client.state_payload()["pack_voltage_v"] == pytest.approx(8 * runtime.config.empty_v_per_cell)


# ---------------------------------------------------------------------------
# Manuelles SoC-Kommando
# ---------------------------------------------------------------------------
def test_manual_soc_command_sets_the_counter_and_republishes(monkeypatch):
    runtime = make_runtime()
    monkeypatch.setattr(battery_soc, "runtimes", [runtime])
    monkeypatch.setattr(battery_soc, "slave", FakeSlave())
    client = FakeClient()
    topic = f"{runtime.config.base_topic}/{mqtt_discovery.MANUAL_SOC_COMMAND_SUFFIX}"

    battery_soc.on_message(client, None, FakeMessage(topic, "80"))

    assert runtime.state.units[0].soc_pct == 80.0
    assert client.state_payload()["soc_combined_pct"] == 80.0


def test_manual_soc_command_addresses_a_single_bank_in_series(monkeypatch):
    runtime = series_runtime()
    monkeypatch.setattr(battery_soc, "runtimes", [runtime])
    monkeypatch.setattr(battery_soc, "slave", FakeSlave())
    client = FakeClient()
    topic = f"{runtime.config.base_topic}/{mqtt_discovery.MANUAL_SOC_COMMAND_SUFFIX}/bank_b"

    battery_soc.on_message(client, None, FakeMessage(topic, "40"))

    assert runtime.state.units[1].soc_pct == 40.0
    assert runtime.state.units[0].soc_pct != 40.0


# ---------------------------------------------------------------------------
# Zentralisierte Konfiguration
# ---------------------------------------------------------------------------
def test_module_has_no_environment_constants():
    for name in ("MQTT_HOST", "MQTT_PORT", "SERVICE_DEVICE_ID", "BATTERY_CONFIG_PATH",
                 "LOOP_INTERVAL_S", "DIAGNOSTIC_POLL_MULTIPLIER"):
        assert not hasattr(battery_soc, name), f"{name} steht noch auf Modulebene"


def test_battery_config_path_follows_convention(app_config):
    config = app_config()
    assert config.devices_config("battery_soc").name == "battery_soc_devices.json"


# ---------------------------------------------------------------------------
# Kalibrier-Vorschlaege / Tuning-Payload
# ---------------------------------------------------------------------------
def test_compute_and_publish_emits_a_tuning_payload_per_unit():
    """Kein Anspruch an den Inhalt hier - der ist in test_tuning.py
    (Task 4b/5) geprueft. Hier zaehlt nur, dass der Adapter analyse()
    ueberhaupt aufruft und retained veroeffentlicht."""
    client = FakeClient()
    runtime = battery_soc.BatteryRuntime(device_config())
    battery_soc.compute_and_publish(client, runtime, dt_hours=0.0)
    payload = client.tuning_payload()
    assert "generated_iso" in payload
    assert set(payload["units"]) == {u.name for u in runtime.state.units}
    for unit in payload["units"].values():
        assert "suggestions" in unit and "findings" in unit
    tuning_idx = next(i for i, (t, _p) in enumerate(client.published) if t.endswith("/tuning"))
    assert client.published_kwargs[tuning_idx][1] is True  # retain=True
