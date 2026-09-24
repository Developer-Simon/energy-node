"""validate_sources: one rule set for both adapters (spec section 5)."""
import pytest

from battery_soc_core.sources import SourceConfig, validate_sources

AC_BOTH = frozenset({"charger_power", "inverter_power", "bank_a_voltage"})
DC_BOTH = frozenset({"charger_dc_power", "inverter_dc_power", "bank_a_voltage"})


def test_ac_coupled_default_is_valid():
    assert validate_sources(SourceConfig(configured=AC_BOTH)) == []


def test_dc_only_with_dc_slots_is_valid():
    cfg = SourceConfig(configured=DC_BOTH, system_type="dc_only",
                       units={"charger_dc_power": "A", "inverter_dc_power": "A"})
    assert validate_sources(cfg) == []


def test_mixed_sides_are_valid():
    """AC on one side, DC on the other: each side has a source."""
    cfg = SourceConfig(configured=frozenset({"charger_dc_power", "inverter_power",
                                             "bank_a_voltage"}))
    assert validate_sources(cfg) == []


@pytest.mark.parametrize("configured,code", [
    (AC_BOTH - {"bank_a_voltage"}, "bank_a_voltage_required"),
    (AC_BOTH - {"charger_power"}, "charge_source_required"),
    (AC_BOTH - {"inverter_power"}, "discharge_source_required"),
])
def test_missing_source_is_reported(configured, code):
    assert validate_sources(SourceConfig(configured=frozenset(configured))) == [code]


def test_series_needs_bank_b_voltage():
    cfg = SourceConfig(configured=AC_BOTH, topology="series")
    assert validate_sources(cfg) == ["bank_b_voltage_required"]
    ok = SourceConfig(configured=AC_BOTH | {"bank_b_voltage"}, topology="series")
    assert validate_sources(ok) == []


def test_parallel_does_not_need_bank_b_voltage():
    cfg = SourceConfig(configured=AC_BOTH, topology="parallel", bank_b_enabled=True)
    assert validate_sources(cfg) == []


def test_current_on_an_ac_slot_is_rejected():
    cfg = SourceConfig(configured=AC_BOTH, units={"charger_power": "A"})
    assert validate_sources(cfg) == ["current_only_on_dc"]


def test_current_on_a_dc_slot_is_fine():
    cfg = SourceConfig(configured=AC_BOTH | {"charger_dc_power"},
                       units={"charger_dc_power": "A"})
    assert validate_sources(cfg) == []


def test_ac_slot_in_dc_only_system_is_rejected():
    cfg = SourceConfig(configured=DC_BOTH | {"charger_power"}, system_type="dc_only")
    assert validate_sources(cfg) == ["ac_source_in_dc_system"]


def test_codes_come_in_table_order():
    cfg = SourceConfig(configured=frozenset({"charger_power"}), topology="series",
                       system_type="dc_only", units={"charger_power": "A"})
    assert validate_sources(cfg) == [
        "bank_a_voltage_required", "bank_b_voltage_required",
        "discharge_source_required", "current_only_on_dc", "ac_source_in_dc_system",
    ]


@pytest.mark.parametrize("configured,expected", [
    (AC_BOTH, False),
    (DC_BOTH, False),
    (AC_BOTH | {"charger_dc_power"}, True),
    (AC_BOTH | {"inverter_dc_power"}, True),
    (frozenset({"charger_power", "inverter_dc_power"}), False),
])
def test_fallback_possible_needs_ac_and_dc_on_one_side(configured, expected):
    assert SourceConfig(configured=frozenset(configured)).fallback_possible() is expected
