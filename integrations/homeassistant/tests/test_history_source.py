"""Recorder -> Saetze: echte States, Statistik gepatcht."""
from datetime import datetime, timedelta, timezone
from unittest.mock import patch

import pytest
from pytest_homeassistant_custom_component.components.recorder.common import async_wait_recording_done

from custom_components.energy_node.series_map import SeriesSource
from custom_components.energy_node.source import async_rows, converter_for

T0 = datetime(2026, 9, 30, 12, 0, tzinfo=timezone.utc)
MS = int(T0.timestamp() * 1000)
SOURCE = "custom_components.energy_node.source"


@pytest.fixture(autouse=True)
def auto_enable_custom_integrations(recorder_mock, enable_custom_integrations):
    # Der Recorder muss vor hass stehen. Die conftest-Variante zieht hass zuerst.
    yield


async def _record(hass, freezer, entity_id, steps, unit):
    for offset, value in steps:
        freezer.move_to(T0 + timedelta(seconds=offset))
        hass.states.async_set(entity_id, value, {"unit_of_measurement": unit})
        await async_wait_recording_done(hass)


def _short(rows):
    return [(row["ts"] - MS, row["min"], row["max"], row["avg"]) for row in rows]


async def test_states_become_minute_rows_in_the_dashboard_unit(hass, freezer):
    await _record(hass, freezer, "sensor.pv", [(0, "1"), (90, "3")], "kW")
    freezer.move_to(T0 + timedelta(seconds=200))

    rows = await async_rows(hass, SeriesSource("role:pv", "W", "sensor.pv"), "1m", MS, MS + 180_000)

    assert _short(rows) == [
        (0, 1000.0, 1000.0, 1000.0),
        (60_000, 1000.0, 3000.0, 2000.0),
        (120_000, 3000.0, 3000.0, 3000.0),
    ]
    assert {row["series"] for row in rows} == {"role:pv"}
    assert {row["u"] for row in rows} == {"W"}


async def test_unavailable_and_nan_are_gaps(hass, freezer):
    await _record(hass, freezer, "sensor.pv", [(0, "5"), (60, "unavailable"), (120, "nan")], "W")
    freezer.move_to(T0 + timedelta(seconds=200))

    rows = await async_rows(hass, SeriesSource("role:pv", "W", "sensor.pv"), "1m", MS, MS + 180_000)

    assert [ts for ts, *_ in _short(rows)] == [0]


async def test_incompatible_unit_skips_the_series(hass, freezer):
    await _record(hass, freezer, "sensor.temp", [(0, "21")], "°C")
    freezer.move_to(T0 + timedelta(seconds=200))

    rows = await async_rows(hass, SeriesSource("role:pv", "W", "sensor.temp"), "1m", MS, MS + 180_000)

    assert rows == []


async def test_derived_role_subtracts_on_the_state_path(hass, freezer):
    await _record(hass, freezer, "sensor.imp", [(0, "500")], "W")
    await _record(hass, freezer, "sensor.exp", [(0, "200")], "W")
    freezer.move_to(T0 + timedelta(seconds=400))

    with patch(f"{SOURCE}.statistics_during_period") as stats:
        rows = await async_rows(
            hass, SeriesSource("role:grid", "W", "sensor.imp", "sensor.exp"), "5m", MS, MS + 300_000
        )

    stats.assert_not_called()
    assert _short(rows) == [(0, 300.0, 300.0, 300.0)]


async def test_five_minute_tier_prefers_statistics(hass, freezer):
    await _record(hass, freezer, "sensor.pv", [(0, "1")], "kW")
    freezer.move_to(T0 + timedelta(seconds=400))
    start = T0.timestamp()
    with (
        patch(f"{SOURCE}.get_metadata", return_value={"sensor.pv": (1, {"unit_of_measurement": "kW"})}),
        patch(
            f"{SOURCE}.statistics_during_period",
            return_value={"sensor.pv": [{"start": start, "end": start + 300, "mean": 2000.0, "min": 1000.0, "max": 3000.0}]},
        ) as stats,
    ):
        rows = await async_rows(hass, SeriesSource("role:pv", "W", "sensor.pv"), "5m", MS, MS + 300_000)

    assert _short(rows) == [(0, 1000.0, 3000.0, 2000.0)]
    assert stats.call_args.args[5] == {"power": "W"}


async def test_five_minute_tier_falls_back_to_states_without_statistics(hass, freezer):
    await _record(hass, freezer, "sensor.pv", [(0, "4")], "W")
    freezer.move_to(T0 + timedelta(seconds=400))

    with patch(f"{SOURCE}.get_metadata", return_value={}):
        rows = await async_rows(hass, SeriesSource("role:pv", "W", "sensor.pv"), "5m", MS, MS + 300_000)

    assert _short(rows) == [(0, 4.0, 4.0, 4.0)]


def test_converter_for():
    same, unit_class = converter_for("W", "W")
    assert same(5.0) == 5.0 and unit_class is None
    to_watt, unit_class = converter_for("kW", "W")
    assert to_watt(1.5) == 1500.0 and unit_class == "power"
    assert converter_for("°C", "W") is None
    assert converter_for("", "W") is None
