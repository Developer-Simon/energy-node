import pytest
from battery_soc_core.params import SocParams


def test_defaults_are_parallel_lifepo4():
    p = SocParams()
    assert p.topology == "parallel"
    assert p.soc_curve == "generic_lifepo4"
    assert p.internal_resistance_mohm_per_cell is None


def test_from_dict_ignores_unknown_keys():
    p = SocParams.from_dict({"topology": "series", "charger_power_topic": "x/y", "bank_a_capacity_ah": 200})
    assert p.topology == "series"
    assert p.bank_a_capacity_ah == 200


@pytest.mark.parametrize("kw", [
    {"topology": "reihe"},
    {"charge_efficiency": 0.0},
    {"charge_efficiency": 1.5},
    {"bank_a_cell_count": 0},
    {"bank_a_capacity_ah": -1},
])
def test_validate_rejects_impossible_physics(kw):
    with pytest.raises(ValueError):
        SocParams(**kw).validate()


def test_validate_series_requires_bank_b_and_equal_capacities():
    with pytest.raises(ValueError):
        SocParams(topology="series", bank_b_enabled=False).validate()
    with pytest.raises(ValueError):
        SocParams(topology="series", bank_a_capacity_ah=100, bank_b_capacity_ah=120).validate()
    SocParams(topology="series", bank_a_capacity_ah=100, bank_b_capacity_ah=100).validate()  # ok


def test_validate_parallel_requires_equal_cell_counts_when_bank_b_on():
    with pytest.raises(ValueError):
        SocParams(topology="parallel", bank_a_cell_count=8, bank_b_cell_count=16).validate()
    SocParams(topology="parallel", bank_b_enabled=False, bank_a_cell_count=8, bank_b_cell_count=16).validate()
