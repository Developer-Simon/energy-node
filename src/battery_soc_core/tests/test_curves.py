"""Tests for curves.py."""
import pytest

from battery_soc_core.curves import (
    DYNESS_AR25_CURVE, GENERIC_LIFEPO4_CURVE, load_offset_mv, soc_curve_for,
)


@pytest.mark.parametrize("current_a,offset_mv", [
    (2.0, 5),      # 0.02C
    (15.0, 25),    # 0.15C
    (30.0, 60),    # 0.30C
    (80.0, 120),   # 0.80C
    (150.0, 200),  # > 1C
])
def test_load_offset_table_is_the_default_path(current_a, offset_mv):
    """Ohne Override muss exakt die bisherige Bin-Tabelle herauskommen -
    in Lade- und in Entladerichtung."""
    c_rate = abs(current_a) / 100.0  # capacity is 100 Ah
    assert load_offset_mv(c_rate) == offset_mv


def test_unknown_soc_curve_falls_back_to_the_generic_one(capsys):
    assert soc_curve_for("does-not-exist") == GENERIC_LIFEPO4_CURVE
    assert "generic_lifepo4" in capsys.readouterr().out


def test_known_curve_keys_resolve():
    assert soc_curve_for("dyness_ar2.5") == DYNESS_AR25_CURVE
