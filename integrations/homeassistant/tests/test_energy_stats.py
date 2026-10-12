"""Energiestatistik: Rolle -> Quelle, Stundensaetze, Tagesraster."""
from datetime import datetime, timezone
from unittest.mock import patch

from homeassistant.helpers import entity_registry as er

from custom_components.energy_node_companion.const import DAY_MS, HOUR_MS, STATS_WINDOW_DAYS
from custom_components.energy_node_companion.stats import StatsOffer, StatsSource, _hour_rows, resolve_stats

STATS = "custom_components.energy_node_companion.stats"
T0 = datetime(2026, 9, 30, 0, 0, tzinfo=timezone.utc)
MS = int(T0.timestamp() * 1000)
TODAY = 20834 * DAY_MS
NOW = TODAY + 5 * HOUR_MS + HOUR_MS // 2
PV = StatsSource("pv", "sensor.en_pv", False)


async def test_resolve_prefers_counters_and_skips_roles_without_sensor(hass):
    registry = er.async_get(hass)
    registry.async_get_or_create("sensor", "mqtt", "energy_node_pv_power", suggested_object_id="en_pv")
    registry.async_get_or_create("sensor", "mqtt", "energy_node_house_load", suggested_object_id="en_load")
    sources = resolve_stats(hass, {"counter_load_total": "sensor.house_meter", "counter_wallbox": "sensor.wb_meter"})
    assert sorted((s.role, s.entity_id, s.counter) for s in sources) == [
        ("load_total", "sensor.house_meter", True),
        ("pv", "sensor.en_pv", False),
        ("wallbox", "sensor.wb_meter", True),
    ]
    assert {s.series for s in sources} == {"role:load_total", "role:pv", "role:wallbox"}


async def test_power_rows_are_complete_hours_in_watts(hass):
    start = T0.timestamp()
    items = [
        {"start": start, "end": start + 3600, "mean": 1200.0, "min": 0.0, "max": 3000.0},
        {"start": start + 3600, "end": start + 7200, "mean": 800.0, "min": None, "max": None},
    ]
    with patch(f"{STATS}.statistics_during_period", return_value={"sensor.en_pv": items}) as stats:
        rows = _hour_rows(hass, PV, MS, MS + 2 * HOUR_MS - 1)
    assert rows == [{"series": "role:pv", "ts": MS, "min": 0.0, "max": 3000.0, "avg": 1200.0, "n": 1, "u": "W"}]
    assert stats.call_args.args[4] == "hour"
    assert stats.call_args.args[5] == {"power": "W"}
    assert stats.call_args.args[6] == {"mean", "min", "max"}


async def test_missing_min_and_max_fall_back_to_the_mean(hass):
    start = T0.timestamp()
    items = [{"start": start, "end": start + 3600, "mean": 800.0, "min": None, "max": None}]
    with patch(f"{STATS}.statistics_during_period", return_value={"sensor.en_pv": items}):
        rows = _hour_rows(hass, PV, MS, MS + HOUR_MS)
    assert (rows[0]["min"], rows[0]["max"]) == (800.0, 800.0)


async def test_counter_rows_use_the_hourly_change_in_wh_and_skip_resets(hass):
    meter = StatsSource("load_total", "sensor.meter", True)
    start = T0.timestamp()
    items = [
        {"start": start, "end": start + 3600, "change": 1500.0},
        {"start": start + 3600, "end": start + 7200, "change": -20.0},
        {"start": start + 7200, "end": start + 10800, "change": None},
    ]
    with patch(f"{STATS}.statistics_during_period", return_value={"sensor.meter": items}) as stats:
        rows = _hour_rows(hass, meter, MS, MS + 3 * HOUR_MS)
    assert rows == [{"series": "role:load_total", "ts": MS, "min": 1500.0, "max": 1500.0, "avg": 1500.0, "n": 1, "u": "W"}]
    assert stats.call_args.args[5] == {"energy": "Wh"}
    assert stats.call_args.args[6] == {"change"}


class FakeHourRows:
    def __init__(self, hours):
        self.hours = hours
        self.calls = []

    async def __call__(self, source, start, end):
        self.calls.append((source.role, start, end))
        return [
            {"series": source.series, "ts": ts, "min": 1.0, "max": 1.0, "avg": 1.0, "n": 1, "u": "W"}
            for ts in self.hours.get(source.role, []) if start <= ts and ts + HOUR_MS <= end
        ]


async def test_first_coverage_scans_the_window_then_only_from_yesterday():
    rows = FakeHourRows({"pv": [TODAY - 3 * DAY_MS + h * HOUR_MS for h in range(24)] + [TODAY + h * HOUR_MS for h in range(5)]})
    offer = StatsOffer(lambda: [PV], rows)

    coverage = await offer.coverage(NOW)

    assert coverage == {"role:pv": {"from": TODAY - 3 * DAY_MS, "step": DAY_MS, "n": [24, 0, 0, 5]}}
    assert rows.calls[0][1] == TODAY - STATS_WINDOW_DAYS * DAY_MS
    rows.calls.clear()
    rows.hours["pv"].append(TODAY + 5 * HOUR_MS)

    coverage = await offer.coverage(NOW + HOUR_MS)

    assert [call[1] for call in rows.calls] == [TODAY - DAY_MS]
    assert coverage["role:pv"]["n"] == [24, 0, 0, 6]


async def test_changed_source_is_scanned_again():
    sources = [PV]
    rows = FakeHourRows({"pv": [TODAY - 2 * DAY_MS]})
    offer = StatsOffer(lambda: list(sources), rows)
    await offer.coverage(NOW)
    sources[0] = StatsSource("pv", "sensor.meter", True)
    rows.calls.clear()
    await offer.coverage(NOW)
    assert rows.calls[0][1] == TODAY - STATS_WINDOW_DAYS * DAY_MS


async def test_vanished_source_is_no_longer_offered():
    sources = [PV]
    offer = StatsOffer(lambda: list(sources), FakeHourRows({"pv": [TODAY]}))
    assert "role:pv" in await offer.coverage(NOW)
    sources.clear()
    assert await offer.coverage(NOW) == {}
    assert offer.series == set()


async def test_rows_only_for_known_series():
    offer = StatsOffer(lambda: [PV], FakeHourRows({"pv": [TODAY]}))
    await offer.coverage(NOW)
    assert len(await offer.rows("role:pv", TODAY, TODAY + HOUR_MS)) == 1
    assert await offer.rows("role:wallbox", TODAY, TODAY + HOUR_MS) == []
