from tests.conftest import make_params
from battery_soc_core.entities import entity_specs, EntityDesc, ALL_OBJECT_IDS
from battery_soc_core.state import SocState
from battery_soc_core.inputs import SocInputs
from battery_soc_core.engine import tick
import time


def _keys_for(params):
    s = SocState(params, last_tick=time.time())
    i = SocInputs()
    i.charger_power_configured = i.bank_a_voltage_configured = i.inverter_power_configured = True
    i.charger_power_ts = i.inverter_power_ts = i.bank_a_voltage_ts = time.time()
    i.bank_a_voltage_v = 26.8
    if params.topology == "series":
        i.bank_b_voltage_configured = True
        i.bank_b_voltage_ts = time.time()
        i.bank_b_voltage_v = 26.8
    return set(tick(params, s, i, time.time()).outputs)


def test_every_sensor_value_key_exists_in_tick_output_parallel():
    p = make_params(topology="parallel")
    out_keys = _keys_for(p)
    missing = [d.value_key for d in entity_specs(p)
              if d.component in ("sensor", "binary_sensor") and d.value_key not in out_keys]
    assert missing == []


def test_every_sensor_value_key_exists_in_tick_output_series():
    p = make_params(topology="series", bank_a_capacity_ah=100, bank_b_capacity_ah=100)
    out_keys = _keys_for(p)
    missing = [d.value_key for d in entity_specs(p)
              if d.component in ("sensor", "binary_sensor") and d.value_key not in out_keys]
    assert missing == []


def test_parallel_has_no_bank_labelled_entities():
    p = make_params(topology="parallel")
    ids = {d.object_id for d in entity_specs(p)}
    assert "soc_a" not in ids and "voltage_delta" not in ids
    assert "manual_soc" in ids


def test_series_adds_imbalance_and_two_manual_soc_numbers():
    p = make_params(topology="series", bank_a_capacity_ah=100, bank_b_capacity_ah=100)
    ids = {d.object_id for d in entity_specs(p)}
    assert {"soc_a", "soc_b", "voltage_delta", "imbalance_warning",
            "manual_soc_bank_a", "manual_soc_bank_b"} <= ids


def test_all_object_ids_superset_covers_every_generated_id():
    # NOTE: battery_soc_mqtt.py's ALL_OBJECT_IDS omits the calibration threshold
    # sensors; reproduced verbatim here. Pre-existing gap, out of scope for the
    # core extraction.
    calibration_ids = {
        "calibration_empty_v_a", "calibration_empty_v_b",
        "calibration_empty_v_pack", "calibration_empty_v_bank_a", "calibration_empty_v_bank_b",
        "calibration_full_v_a", "calibration_full_v_b",
        "calibration_full_v_pack", "calibration_full_v_bank_a", "calibration_full_v_bank_b",
    }
    for topo, kw in [("parallel", {}), ("series", {"bank_a_capacity_ah": 100, "bank_b_capacity_ah": 100})]:
        p = make_params(topology=topo, **kw)
        for d in entity_specs(p):
            if d.object_id not in calibration_ids:
                assert d.object_id in ALL_OBJECT_IDS[d.component], (topo, d.component, d.object_id)
