"""Rechenlogik ohne Home Assistant: Halteverlauf, Differenz, Raster."""
import math

from custom_components.energy_node_companion.buckets import (
    bucketize,
    census,
    combine,
    in_ranges,
    raster_window,
)

# Sekunden, durch 60, 300 und 3600 teilbar.
T = 1_800_000_000


def _short(rows):
    return [(row["ts"] // 1000 - T, row["min"], row["max"], row["avg"]) for row in rows]


def test_held_value_fills_every_minute():
    rows = bucketize([(T - 30, 5.0)], T, T + 180, 60, "role:pv", "W")
    assert _short(rows) == [(0, 5.0, 5.0, 5.0), (60, 5.0, 5.0, 5.0), (120, 5.0, 5.0, 5.0)]
    assert rows[0] == {"series": "role:pv", "ts": T * 1000, "min": 5.0, "max": 5.0, "avg": 5.0, "n": 1, "u": "W"}


def test_change_inside_bucket_is_time_weighted():
    rows = bucketize([(T, 1000.0), (T + 90, 3000.0)], T, T + 180, 60, "role:pv", "W")
    assert _short(rows) == [(0, 1000.0, 1000.0, 1000.0), (60, 1000.0, 3000.0, 2000.0), (120, 3000.0, 3000.0, 3000.0)]


def test_unavailable_leaves_a_gap():
    rows = bucketize([(T, 5.0), (T + 60, None), (T + 120, 7.0)], T, T + 180, 60, "x", "W")
    assert [ts for ts, *_ in _short(rows)] == [0, 120]


def test_incomplete_last_bucket_is_not_emitted():
    # Der Browser ueberschreibt nie (writeMissing). Ein halber Bucket bliebe
    # fuer immer halb, deshalb endet die Ausgabe vor dem angebrochenen.
    rows = bucketize([(T, 5.0)], T, T + 150, 60, "x", "W")
    assert [ts for ts, *_ in _short(rows)] == [0, 60]


def test_unaligned_start_skips_partial_first_bucket():
    rows = bucketize([(T, 5.0)], T + 30, T + 180, 60, "x", "W")
    assert [ts for ts, *_ in _short(rows)] == [60, 120]


def test_no_reading_before_the_first_change():
    rows = bucketize([(T + 120, 5.0)], T, T + 180, 60, "x", "W")
    assert [ts for ts, *_ in _short(rows)] == [120]


def test_five_minute_buckets():
    rows = bucketize([(T, 2.0), (T + 150, 4.0)], T, T + 600, 300, "x", "W")
    assert _short(rows) == [(0, 2.0, 4.0, 3.0), (300, 4.0, 4.0, 4.0)]


def test_combine_subtracts_and_needs_both_sides():
    plus = [(T, 10.0), (T + 60, 20.0)]
    minus = [(T + 30, 4.0)]
    assert combine(plus, minus) == [(T, None), (T + 30, 6.0), (T + 60, 16.0)]


def test_combine_propagates_a_gap_on_either_side():
    assert combine([(T, 10.0)], [(T, 1.0), (T + 60, None)]) == [(T, 9.0), (T + 60, None)]


def test_census_matches_the_browser_raster():
    now = T * 1000 + 1_800_000  # halb nach einer vollen Stunde
    start, step, buckets = raster_window("1m", now)
    assert step == 3_600_000
    assert start == math.floor((now - 7 * 24 * 3_600_000) / step) * step
    assert buckets == 7 * 24 + 1

    result = census("1m", [start, start + 60_000, now - 1, now, start - 1], now)
    assert result["from"] == start and result["step"] == step
    assert len(result["n"]) == buckets
    assert result["n"][0] == 2
    assert result["n"][-1] == 1
    assert sum(result["n"]) == 3


def test_in_ranges_keeps_half_open_intervals():
    rows = [{"ts": 0}, {"ts": 60_000}, {"ts": 120_000}]
    assert in_ranges(rows, [(0, 60_000), (120_000, 180_000)]) == [{"ts": 0}, {"ts": 120_000}]
