"""Pure unit normalisation (no hass needed)."""
import pytest

from custom_components.battery_soc.units import UnitError, base_unit, normalize


@pytest.mark.parametrize("value,uom,expected", [
    (300.0, "W", (300.0, "W")),
    (0.5, "kW", (500.0, "W")),
    (1500.0, "mW", (1.5, "W")),
])
def test_power_units_become_watts(value, uom, expected):
    got = normalize(value, uom, allow_current=False)
    assert got[0] == pytest.approx(expected[0]) and got[1] == expected[1]


@pytest.mark.parametrize("value,uom,expected", [
    (-0.3, "A", -0.3),
    (500.0, "mA", 0.5),
])
def test_current_units_become_amps_on_dc_slots(value, uom, expected):
    got = normalize(value, uom, allow_current=True)
    assert got[0] == pytest.approx(expected) and got[1] == "A"


def test_current_is_rejected_on_ac_slots():
    with pytest.raises(UnitError, match="DC"):
        normalize(1.0, "A", allow_current=False)


@pytest.mark.parametrize("uom,reason", [(None, "no unit_of_measurement"),
                                        ("Wh", "unsupported unit 'Wh'")])
def test_missing_or_unknown_unit_is_an_error(uom, reason):
    with pytest.raises(UnitError, match=reason):
        normalize(1.0, uom, allow_current=True)


@pytest.mark.parametrize("uom,expected", [("kW", "W"), ("W", "W"), ("mA", "A"),
                                          ("A", "A"), ("Wh", None), (None, None)])
def test_base_unit(uom, expected):
    assert base_unit(uom) == expected
