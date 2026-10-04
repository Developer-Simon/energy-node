"""Reine Rechenlogik: Halteverlauf -> Bucket-Saetze, Differenzserien, Raster.

Ohne Home Assistant importierbar, damit alles hier ohne hass pruefbar ist.
Ein Satz hat dieselbe Form wie im Browser (history-store.js):
{series, ts, min, max, avg, n, u}, ts in Millisekunden am Bucket-Anfang.
"""
from __future__ import annotations

import math
from collections.abc import Iterable

from .const import RASTER_MS

# (Zeitpunkt in Sekunden, Wert oder None fuer "kein Messwert")
Change = tuple[float, float | None]


def combine(plus: list[Change], minus: list[Change]) -> list[Change]:
    """Differenz zweier Halteverlaeufe, z. B. Netzbezug minus Einspeisung.

    Definiert nur dort, wo beide Seiten einen Messwert haben. Vor der
    ersten Aenderung einer Seite gilt sie als "kein Messwert".
    """
    moments = sorted({moment for moment, _ in plus} | {moment for moment, _ in minus})
    result: list[Change] = []
    left: float | None = None
    right: float | None = None
    i = j = 0
    for moment in moments:
        while i < len(plus) and plus[i][0] <= moment:
            left = plus[i][1]
            i += 1
        while j < len(minus) and minus[j][0] <= moment:
            right = minus[j][1]
            j += 1
        result.append((moment, None if left is None or right is None else left - right))
    return result


def bucketize(
    changes: list[Change], start_s: float, end_s: float, bucket_s: int, series: str, unit: str
) -> list[dict]:
    """Zeitgewichtete Buckets ueber [start_s, end_s).

    Jeder Wert gilt bis zur naechsten Aenderung, der letzte bis end_s.
    Ausgegeben werden nur Buckets, die ganz in [start_s, end_s) liegen: der
    Browser ueberschreibt nie (writeMissing), ein angebrochener Bucket bliebe
    fuer immer unvollstaendig. n ist 1, weil ein Halteverlauf keine
    Stichprobenzahl kennt.
    """
    first = math.ceil(start_s / bucket_s) * bucket_s
    last = math.floor(end_s / bucket_s) * bucket_s  # exklusiv
    if last <= first or not changes:
        return []
    # acc[bucket] = [min, max, Summe Wert*Dauer, Dauer]
    acc: dict[int, list[float]] = {}
    for index, (moment, value) in enumerate(changes):
        until = changes[index + 1][0] if index + 1 < len(changes) else end_s
        begin = max(moment, first)
        finish = min(until, last)
        if value is None or finish <= begin:
            continue
        bucket = int(begin // bucket_s) * bucket_s
        while bucket < finish:
            overlap = min(finish, bucket + bucket_s) - max(begin, bucket)
            if overlap > 0:
                slot = acc.get(bucket)
                if slot is None:
                    acc[bucket] = [value, value, value * overlap, overlap]
                else:
                    slot[0] = min(slot[0], value)
                    slot[1] = max(slot[1], value)
                    slot[2] += value * overlap
                    slot[3] += overlap
            bucket += bucket_s
    return [
        {
            "series": series,
            "ts": int(bucket * 1000),
            "min": slot[0],
            "max": slot[1],
            "avg": round(slot[2] / slot[3], 6),
            "n": 1,
            "u": unit,
        }
        for bucket, slot in sorted(acc.items())
    ]


def raster_window(tier: str, now_ms: int) -> tuple[int, int, int]:
    """Wie rasterWindow() in history-coverage.js: (from, step, buckets)."""
    step, window = RASTER_MS[tier]
    start = math.floor((now_ms - window) / step) * step
    return start, step, math.ceil((now_ms - start) / step)


def census(tier: str, timestamps: Iterable[int], now_ms: int) -> dict:
    """Wie census() in history-coverage.js: Saetze je Rasterbucket."""
    start, step, buckets = raster_window(tier, now_ms)
    counts = [0] * buckets
    for ts in timestamps:
        if start <= ts < now_ms:
            counts[(ts - start) // step] += 1
    return {"from": start, "step": step, "n": counts}


def in_ranges(rows: list[dict], ranges: list[tuple[int, int]]) -> list[dict]:
    """Saetze, deren ts in einem der halboffenen Bereiche [a, b) liegt."""
    return [row for row in rows if any(begin <= row["ts"] < end for begin, end in ranges)]
