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
