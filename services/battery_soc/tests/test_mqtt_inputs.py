"""Tests fuer mqtt_inputs.py.

Muster wie services/trucki/tests/: das Modul wird direkt importiert.
"""

import sys
import time
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

import soc_config
from mqtt_inputs import apply_message, mark_configured, extract_value, extraction_warnings
from battery_soc_core.inputs import SocInputs


def test_extract_value_reads_json_key_and_bare_numbers():
    assert extract_value('{"apower": 12.5}', "apower") == 12.5
    assert extract_value("26.8", "") == 26.8
    # Shelly liefert manche Werte als String.
    assert extract_value('{"apower": "12.5"}', "apower") == 12.5


def test_extract_value_warns_once_on_key_mismatch(capsys):
    extraction_warnings.clear()
    context = "shelly/x (charger_power)"

    assert extract_value("26.8", "apower", context) is None
    first = capsys.readouterr().out
    assert first.count("WARNUNG") == 1
    assert "apower" in first and context in first

    # Gleicher Grund -> Schweigen, sonst waere das Log bei jedem Poll voll.
    assert extract_value("26.8", "apower", context) is None
    assert capsys.readouterr().out == ""

    # Anderer Grund -> wieder melden.
    assert extract_value('{"id": 0}', "apower", context) is None
    second = capsys.readouterr().out
    assert second.count("WARNUNG") == 1
    assert "id" in second


def test_extract_value_does_not_fall_back_to_the_regex_path():
    """Wer einen Key konfiguriert hat, will diesen Key - die erste Zahl im
    Rohtext waere hier die 0 und damit ein stiller Falschwert."""
    extraction_warnings.clear()
    assert extract_value('{"id":0,"apower":12.5}', "voltage") is None


def test_apply_message_scales_voltage_and_timestamps_it():
    cfg = soc_config.BatteryConfig(id="b", name="B", bank_a_voltage_topic="bms/v",
                                   bank_a_voltage_scale=0.1)
    i = SocInputs()
    now = time.time()
    assert apply_message(cfg, i, "bms/v", "268", now) is True
    assert i.bank_a_voltage_v == 26.8
    assert i.bank_a_voltage_ts == now


def test_mark_configured_sets_flags_only_for_present_topics():
    cfg = soc_config.BatteryConfig(id="b", name="B", charger_power_topic="p/c")
    i = SocInputs()
    mark_configured(cfg, i)
    assert i.charger_power_configured is True
    assert i.inverter_power_configured is False


def _cfg(**overrides):
    base = dict(id="b", name="B", bank_a_voltage_topic="v")
    base.update(overrides)
    return soc_config.BatteryConfig(**base)


def test_one_signed_topic_feeds_both_slots_with_its_own_invert():
    cfg = _cfg(charger_dc_power_topic="ina/i", inverter_dc_power_topic="ina/i",
               inverter_dc_power_invert=True)
    inputs = SocInputs()
    assert apply_message(cfg, inputs, "ina/i", "-0.3", now=10.0) is True
    assert inputs.charger_dc_power_w == -0.3
    assert inputs.inverter_dc_power_w == 0.3
    assert inputs.charger_dc_power_ts == inputs.inverter_dc_power_ts == 10.0


def test_one_topic_with_two_json_keys_feeds_both_slots():
    cfg = _cfg(charger_dc_power_topic="bms/state", charger_dc_power_json_key="charge_a",
               inverter_dc_power_topic="bms/state", inverter_dc_power_json_key="discharge_a")
    inputs = SocInputs()
    apply_message(cfg, inputs, "bms/state", '{"charge_a": 1.5, "discharge_a": 0.2}', now=5.0)
    assert (inputs.charger_dc_power_w, inputs.inverter_dc_power_w) == (1.5, 0.2)


def test_invert_applies_to_ac_slots_too():
    cfg = _cfg(charger_power_topic="grid", charger_power_invert=True)
    inputs = SocInputs()
    apply_message(cfg, inputs, "grid", "200", now=1.0)
    assert inputs.charger_power_w == -200.0


def test_a_slot_without_a_value_does_not_block_the_other():
    cfg = _cfg(charger_dc_power_topic="bms/state", charger_dc_power_json_key="missing",
               inverter_dc_power_topic="bms/state", inverter_dc_power_json_key="discharge_a")
    inputs = SocInputs()
    assert apply_message(cfg, inputs, "bms/state", '{"discharge_a": 0.4}', now=2.0) is True
    assert inputs.inverter_dc_power_w == 0.4
    assert inputs.charger_dc_power_ts == 0.0


def test_mark_configured_copies_the_dc_units():
    cfg = _cfg(charger_dc_power_topic="ina/i", charger_dc_power_unit="A")
    inputs = SocInputs()
    mark_configured(cfg, inputs)
    assert inputs.charger_dc_power_unit == "A"
    assert inputs.inverter_dc_power_unit == "W"
