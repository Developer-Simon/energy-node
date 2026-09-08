import asyncio
import json
import sys
from pathlib import Path

import pytest

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

import trucki_http_mqtt as trucki

TEMPLATES_DIR = Path(__file__).resolve().parents[3] / "templates"


def load_fixture(name: str) -> dict:
    path = TEMPLATES_DIR / name
    if not path.exists():
        pytest.skip(
            f"{name} ist eine lokale Mitschrift eines echten Trucki-Sticks und "
            "liegt nicht im oeffentlichen Repository (templates/ ist gitignored)."
        )
    return json.loads(path.read_text())


class FakeClient:
    def __init__(self):
        self.published = []

    def publish(self, topic, payload=None, qos=0, retain=True):
        self.published.append((topic, payload))


class FakeSlave:
    """Nur die Slave-Methoden, die der Service in der Discovery-Pipeline ruft."""

    def __init__(self):
        self.sim_discovery_blocks = []

    def publish_simulation_discovery(self, client, device_id, device_block, base_topic):
        self.sim_discovery_blocks.append(device_block)

    def simulation_active_for(self, device_id):
        return False


def device_config(**overrides):
    defaults = dict(id="t2sga55a69", name="Lumentree Trucki-Stick", host="192.0.2.10")
    defaults.update(overrides)
    return trucki.TruckiDeviceConfig(**defaults)


# ---------------------------------------------------------------------------
# Parsing /jsonlive
#
# templates/T2SGA55A69.jsonlive.json und templates/T2MG81A4E9.jsonlive.json
# sind echte Mitschnitte der /jsonlive-Antwort der beiden Sticks (siehe
# knowhow/trucki-stick-funktionsweise.md).
# ---------------------------------------------------------------------------

def test_parse_live_t2sg_curates_known_values():
    status = trucki.parse_live(load_fixture("T2SGA55A69.jsonlive.json"))

    assert status.fields["VGRID"] == 234.2
    assert status.fields["VBAT"] == 24.5
    assert status.fields["TEMP"] == 57
    assert status.fields["SETACPOWER"] == 868
    assert status.fields["ACPOWER"] == 880.1  # aus "880.10 W"
    assert status.fields["METERPOWER"] == 21.95  # aus "21.95 W"
    assert status.fields["DAYENERGY"] == 0.4
    assert status.fields["state"] == "ON"  # aus MQTT_STATE_VALUE
    assert status.fields["zepc"] == "(ENABLED) 1"  # aus MQTT_ZEPC_VALUE


def test_parse_live_t2sg_drops_mqtt_mirrors_and_empty_overrides():
    status = trucki.parse_live(load_fixture("T2SGA55A69.jsonlive.json"))

    mqtt_mirrors = [k for k in status.fields if k.startswith("MQTT_")]
    assert mqtt_mirrors == []
    assert "ACSETPOINTOVR" not in status.fields  # leeres Override-Feld


def test_parse_live_t2mg_has_dc_side_fields():
    status = trucki.parse_live(load_fixture("T2MG81A4E9.jsonlive.json"))

    assert status.fields["IOUT"] == 12.47
    assert status.fields["VOUTSET"] == 28.8
    assert status.fields["MWEFFICIENCY"] == 95
    assert status.fields["FAULT"] == 0  # aus " 0"
    assert status.fields["METERPOWER"] == -50.19  # aus "-50.19 W"


# ---------------------------------------------------------------------------
# Parsing /jsononce
# ---------------------------------------------------------------------------

def test_parse_once_t2sg_extracts_stammdaten_and_config():
    info = trucki.parse_once(load_fixture("T2SGA55A69.jsononce.json"))

    assert info.devicename == "T2SGA55A69"
    assert info.version == "1.16 21.05.2026 09:56"
    assert info.fields["MAXPOWER"] == 1000
    assert info.fields["MINPOWER"] == 0


def test_parse_once_never_leaks_password_fields():
    for fixture in ("T2SGA55A69.jsononce.json", "T2MG81A4E9.jsononce.json"):
        info = trucki.parse_once(load_fixture(fixture))
        for key in info.fields:
            assert "PASS" not in key
        assert "BEARER" not in info.fields


def test_model_from_devicename():
    assert trucki.model_from_devicename("T2SGA55A69") == "T2SG"
    assert trucki.model_from_devicename("T2MG81A4E9") == "T2MG"
    assert trucki.model_from_devicename(None) is None
    assert trucki.model_from_devicename("UNKNOWN123") is None


# ---------------------------------------------------------------------------
# Werte-Coercion
# ---------------------------------------------------------------------------

def test_coerce_value_variants():
    assert trucki._coerce_value(221.0) == 221.0
    assert trucki._coerce_value("1000") == 1000
    assert isinstance(trucki._coerce_value("1000"), int)
    assert trucki._coerce_value("880.10 W") == 880.1
    assert trucki._coerce_value("-50.19 W") == -50.19
    assert trucki._coerce_value(" 0") == 0
    assert trucki._coerce_value("(ENABLED) 1") == "(ENABLED) 1"
    assert trucki._coerce_value("not set") == "not set"
    assert trucki._coerce_value("") == ""
    assert trucki._coerce_value("192.168.2.176") == "192.168.2.176"


# ---------------------------------------------------------------------------
# Feld-Metadaten
# ---------------------------------------------------------------------------

def test_field_hint_known_field():
    name, unit, device_class, state_class = trucki.field_hint("VGRID")
    assert unit == "V"
    assert device_class == "voltage"
    assert state_class == "measurement"


def test_field_hint_unknown_field_has_no_unit():
    name, unit, device_class, state_class = trucki.field_hint("SOME_NEW_T2HG_FIELD")
    assert name == "SOME_NEW_T2HG_FIELD"
    assert unit is None
    assert device_class is None


def test_is_known_field():
    assert trucki.is_known_field("VBAT")
    assert trucki.is_known_field("MAXPOWER")
    assert not trucki.is_known_field("SOME_NEW_T2HG_FIELD")


# ---------------------------------------------------------------------------
# Discovery
# ---------------------------------------------------------------------------

def test_publish_field_discovery_known_field_has_unit_and_is_enabled():
    cfg = device_config()
    client = FakeClient()
    dev_block = trucki.device_block(cfg, model="T2SG", serial="T2SGA55A69")

    trucki.publish_field_discovery(client, cfg, dev_block, "VGRID")

    topic, payload = client.published[0]
    assert topic == "homeassistant/sensor/t2sga55a69/vgrid/config"
    assert '"unit_of_measurement": "V"' in payload
    assert '"enabled_by_default": false' not in payload


def test_publish_field_discovery_unknown_field_is_diagnostic_and_disabled():
    cfg = device_config()
    client = FakeClient()
    dev_block = trucki.device_block(cfg)

    trucki.publish_field_discovery(client, cfg, dev_block, "SOME_NEW_T2HG_FIELD")

    topic, payload = client.published[0]
    assert topic == "homeassistant/sensor/t2sga55a69/some_new_t2hg_field/config"
    assert '"entity_category": "diagnostic"' in payload
    assert '"enabled_by_default": false' in payload


def test_publish_field_discovery_config_field_uses_cfg_prefix_and_is_diagnostic():
    cfg = device_config()
    client = FakeClient()
    dev_block = trucki.device_block(cfg)

    trucki.publish_field_discovery(client, cfg, dev_block, "MAXPOWER", config_field=True)

    topic, payload = client.published[0]
    assert topic == "homeassistant/sensor/t2sga55a69/cfg_maxpower/config"
    assert '"entity_category": "diagnostic"' in payload
    assert '"state_topic": "outstation/t2sga55a69/config/maxpower"' in payload


def test_publish_field_discovery_config_field_is_default_hidden():
    cfg = device_config()
    client = FakeClient()

    # bekanntes Konfig-Feld
    trucki.publish_field_discovery(client, cfg, trucki.device_block(cfg), "MAXPOWER", config_field=True)
    # unbekanntes Konfig-Feld
    trucki.publish_field_discovery(client, cfg, trucki.device_block(cfg), "SOME_NEW_CFG", config_field=True)

    for _, payload in client.published:
        assert '"enabled_by_default": false' in payload


def test_publish_field_discovery_rarely_used_diagnostic_field_is_default_hidden():
    cfg = device_config()
    client = FakeClient()

    trucki.publish_field_discovery(client, cfg, trucki.device_block(cfg), "BSSID")

    _, payload = client.published[0]
    assert '"entity_category": "diagnostic"' in payload
    assert '"enabled_by_default": false' in payload


def test_publish_field_discovery_useful_diagnostic_field_stays_enabled():
    cfg = device_config()
    client = FakeClient()

    trucki.publish_field_discovery(client, cfg, trucki.device_block(cfg), "state")

    _, payload = client.published[0]
    assert '"entity_category": "diagnostic"' in payload
    assert '"enabled_by_default": false' not in payload


def test_publish_base_discovery_announces_availability_entity():
    cfg = device_config()
    client = FakeClient()

    trucki.publish_base_discovery(client, cfg)

    topics = [topic for topic, _ in client.published]
    assert "homeassistant/binary_sensor/t2sga55a69/online/config" in topics


# ---------------------------------------------------------------------------
# Simulation + Poll-Pipeline
# ---------------------------------------------------------------------------

def test_simulated_status_selects_variant_by_device_id():
    t2sg = trucki.simulated_status(device_config(id="t2sga55a69"))
    t2mg = trucki.simulated_status(device_config(id="t2mg81a4e9"))

    assert set(t2sg.fields) >= {"VGRID", "VBAT", "TEMP", "SETACPOWER"}
    assert "IOUT" not in t2sg.fields
    assert set(t2mg.fields) >= {"VGRID", "VBAT", "IOUT", "VOUTSET"}


def test_fetch_and_publish_status_simulation_publishes_fields_and_online():
    cfg = device_config()
    client = FakeClient()
    known_fields = set()

    asyncio.run(
        trucki.fetch_and_publish_status(client, cfg, known_fields, timeout_s=5.0, simulation_active=True)
    )

    topics = dict(client.published)
    assert topics[f"{cfg.base_topic}/field/vgrid"] is not None
    assert topics[f"{cfg.base_topic}/status/online"] == "1"
    assert "VGRID" in known_fields
    discovery_topics = [t for t in topics if t.startswith("homeassistant/sensor/")]
    assert discovery_topics, "expected dynamically discovered field sensors"


def test_fetch_and_publish_status_reports_offline_on_http_error(monkeypatch):
    cfg = device_config()
    client = FakeClient()
    known_fields = set()

    def raise_connection_error(_cfg, _path, _timeout_s):
        raise ConnectionError("no route to host")

    monkeypatch.setattr(trucki, "fetch_json", raise_connection_error)

    asyncio.run(
        trucki.fetch_and_publish_status(client, cfg, known_fields, timeout_s=5.0, simulation_active=False)
    )

    topics = dict(client.published)
    assert topics[f"{cfg.base_topic}/status/online"] == "0"


def test_fetch_and_publish_status_only_announces_new_fields_once():
    cfg = device_config()
    client = FakeClient()
    known_fields = set()

    asyncio.run(
        trucki.fetch_and_publish_status(client, cfg, known_fields, timeout_s=5.0, simulation_active=True)
    )
    first_discovery_count = len(
        [t for t, _ in client.published if t.startswith("homeassistant/sensor/")]
    )
    client.published.clear()

    asyncio.run(
        trucki.fetch_and_publish_status(client, cfg, known_fields, timeout_s=5.0, simulation_active=True)
    )
    second_discovery_count = len(
        [t for t, _ in client.published if t.startswith("homeassistant/sensor/")]
    )

    assert first_discovery_count > 0
    assert second_discovery_count == 0


def test_fetch_and_publish_info_simulation_publishes_config_fields_and_updates_device_block():
    cfg = device_config()
    client = FakeClient()
    known_config_fields = set()

    info = asyncio.run(
        trucki.fetch_and_publish_info(client, cfg, known_config_fields, timeout_s=5.0, simulation_active=True)
    )

    assert info is not None
    assert info.devicename == "SIM-T2SG-t2sga55a69"
    topics = dict(client.published)
    assert topics[f"{cfg.base_topic}/config/maxpower"] is not None
    config_discovery_topics = [
        t for t in topics if t.startswith("homeassistant/sensor/") and "/cfg_" in t
    ]
    assert config_discovery_topics, "expected dynamically discovered config sensors"


def test_fetch_and_publish_info_does_not_reannounce_availability_entity():
    cfg = device_config()
    client = FakeClient()

    asyncio.run(
        trucki.fetch_and_publish_info(client, cfg, set(), timeout_s=5.0, simulation_active=True)
    )

    topics = [topic for topic, _ in client.published]
    assert "homeassistant/binary_sensor/t2sga55a69/online/config" not in topics


def test_module_has_no_environment_constants():
    for name in ("MQTT_HOST", "MQTT_PORT", "DEVICE_ID", "NODE_DEVICE_ID", "BASE_TOPIC",
                 "DEVICES_CONFIG_PATH", "DEFAULT_POLL_INTERVAL_S",
                 "DEFAULT_DIAGNOSTIC_MULTIPLIER", "HTTP_TIMEOUT_S"):
        assert not hasattr(trucki, name), f"{name} steht noch auf Modulebene"


def test_service_derives_base_topic_from_service_config(app_config):
    config = app_config(services={"trucki": {"device_id": "trucki-x", "poll_interval_s": 30,
                                             "diagnostic_poll_multiplier": 10, "http_timeout_s": 5}})
    service = trucki.TruckiService([], None, config, "trucki")

    assert service.base_topic == "outstation/trucki-x"


# ---------------------------------------------------------------------------
# Geraeteblock-Synchronisierung
#
# Modell/Seriennummer/Firmware stehen erst nach dem ersten /jsononce-Poll
# fest. Frueher (mit Platzhalter-Block) angelegte /jsonlive-Entities muessen
# den echten Block dann nachgereicht bekommen, sonst meldet das Dashboard
# "Discovery-Daten stimmen nicht mit dem Entitaetsmodell ueberein".
# ---------------------------------------------------------------------------

def _service_with_fakes(app_config):
    service = trucki.TruckiService([device_config()], None, app_config(), "trucki")
    service.client = FakeClient()
    service.slave = FakeSlave()
    return service, service.devices[0]


def test_sync_device_block_republishes_known_entities_on_model_change(app_config):
    service, cfg = _service_with_fakes(app_config)

    # Erstanmeldung mit Platzhalter-Block (noch kein /jsononce)
    service.known_fields[cfg.id].update({"VGRID", "ACPOWER"})
    service.known_config_fields[cfg.id].add("MAXPOWER")
    service._published_block_hash[cfg.id] = service._block_hash(
        service._current_device_block(cfg)
    )
    service.client.published.clear()

    # /jsononce liefert das echte Modell
    service.device_info[cfg.id] = trucki.TruckiInfo(
        devicename="T2SGA55A69", version="1.16", fields={}
    )
    returned = service._sync_device_block(cfg)

    published = dict(service.client.published)
    vgrid = published["homeassistant/sensor/t2sga55a69/vgrid/config"]
    assert '"model": "T2SG"' in vgrid
    assert "T2SG/T2MG/T2HG" not in vgrid
    assert "homeassistant/sensor/t2sga55a69/cfg_maxpower/config" in published
    assert "homeassistant/binary_sensor/t2sga55a69/online/config" in published
    assert service.slave.sim_discovery_blocks[-1]["model"] == "T2SG"
    assert returned["model"] == "T2SG"


def test_sync_device_block_is_quiet_when_block_unchanged(app_config):
    service, cfg = _service_with_fakes(app_config)
    service.known_fields[cfg.id].add("VGRID")

    service._sync_device_block(cfg)  # erster Aufruf: Hash lernen
    service.client.published.clear()
    service._sync_device_block(cfg)  # zweiter Aufruf: nichts geaendert

    assert service.client.published == []
