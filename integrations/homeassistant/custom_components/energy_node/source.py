"""Saetze aus dem Recorder: States fuer 1m, Kurzzeitstatistik fuer 5m.

Alle Datenbankzugriffe laufen im Executor des Recorders, nie in der
Ereignisschleife. Die Einheit wird auf die des Dashboards umgerechnet;
laesst sie sich nicht umrechnen, faellt die Serie weg, statt falsche
Zahlen zu liefern.
"""
from __future__ import annotations

import math
from collections.abc import Callable
from datetime import datetime, timedelta

from homeassistant.components.recorder import get_instance
from homeassistant.components.recorder.history import state_changes_during_period
from homeassistant.components.recorder.statistics import get_metadata, statistics_during_period
from homeassistant.const import ATTR_UNIT_OF_MEASUREMENT
from homeassistant.core import HomeAssistant
from homeassistant.util import dt as dt_util
from homeassistant.util.unit_conversion import (
    ElectricCurrentConverter,
    ElectricPotentialConverter,
    EnergyConverter,
    PowerConverter,
    TemperatureConverter,
)

from .buckets import Change, bucketize, combine
from .const import BUCKET_S
from .series_map import SeriesSource

CONVERTERS = (
    PowerConverter,
    EnergyConverter,
    TemperatureConverter,
    ElectricCurrentConverter,
    ElectricPotentialConverter,
)

Convert = Callable[[float], float]


def converter_for(from_unit: str, to_unit: str) -> tuple[Convert, str | None] | None:
    """(Umrechnung, unit_class) oder None, wenn die Einheiten unvertraeglich sind."""
    if (from_unit or "") == (to_unit or ""):
        return (lambda value: value), None
    for converter in CONVERTERS:
        if from_unit in converter.VALID_UNITS and to_unit in converter.VALID_UNITS:
            return converter.converter_factory(from_unit, to_unit), converter.UNIT_CLASS
    return None


def _conversion(hass: HomeAssistant, entity_id: str, unit: str) -> tuple[Convert, str | None] | None:
    # Die aktuelle Einheit gilt fuer den ganzen Verlauf. Wer die Anzeige-
    # einheit einer Entitaet umstellt, bekommt aeltere States falsch skaliert;
    # das nimmt die Integration in Kauf, statt jeden State samt Attributen zu
    # lesen.
    state = hass.states.get(entity_id)
    current = state.attributes.get(ATTR_UNIT_OF_MEASUREMENT) if state else None
    return converter_for(current or "", unit)


def _at(ms: int) -> datetime:
    return dt_util.utc_from_timestamp(ms / 1000)


def _changes(hass: HomeAssistant, entity_id: str, start: datetime, end: datetime, convert: Convert) -> list[Change]:
    # Der Recorder liefert einen State genau zum Startzeitpunkt weder als
    # Startzustand (< start) noch als Aenderung (> start). Eine Sekunde frueher
    # beginnen, der Startzustand haelt den Wert von davor.
    start_before = start - timedelta(seconds=1)
    found = state_changes_during_period(
        hass, start_before, end, entity_id, no_attributes=True, include_start_time_state=True
    )
    changes: list[Change] = []
    for state in found.get(entity_id, []):
        try:
            value: float | None = convert(float(state.state))
        except ValueError:
            # unavailable, unknown und jeder andere Text sind kein Messwert.
            value = None
        if value is not None and not math.isfinite(value):
            value = None
        changes.append((state.last_changed.timestamp(), value))
    return changes


def _state_rows(
    hass: HomeAssistant,
    source: SeriesSource,
    tier: str,
    plus: Convert,
    minus: Convert | None,
    start_ms: int,
    end_ms: int,
) -> list[dict]:
    start, end = _at(start_ms), _at(end_ms)
    changes = _changes(hass, source.plus, start, end, plus)
    if source.minus and minus is not None:
        changes = combine(changes, _changes(hass, source.minus, start, end, minus))
    return bucketize(changes, start_ms / 1000, end_ms / 1000, BUCKET_S[tier], source.series, source.unit)


def _statistics_rows(hass: HomeAssistant, source: SeriesSource, start_ms: int, end_ms: int) -> list[dict] | None:
    """5-Minuten-Saetze aus der Kurzzeitstatistik, None ohne Statistik."""
    metadata = get_metadata(hass, statistic_ids={source.plus})
    if source.plus not in metadata:
        return None
    stored = metadata[source.plus][1].get("unit_of_measurement") or ""
    units = None
    if stored != (source.unit or ""):
        conversion = converter_for(stored, source.unit)
        if conversion is None or conversion[1] is None:
            return None
        units = {conversion[1]: source.unit}
    found = statistics_during_period(
        hass, _at(start_ms), _at(end_ms), {source.plus}, "5minute", units, {"mean", "min", "max"}
    )
    rows: list[dict] = []
    for item in found.get(source.plus, []):
        mean = item.get("mean")
        if mean is None:
            continue
        begin = int(item["start"] * 1000)
        if begin < start_ms or int(item["end"] * 1000) > end_ms:
            continue
        low = item.get("min")
        high = item.get("max")
        rows.append({
            "series": source.series,
            "ts": begin,
            "min": mean if low is None else low,
            "max": mean if high is None else high,
            "avg": mean,
            "n": 1,
            "u": source.unit,
        })
    return rows


async def async_rows(hass: HomeAssistant, source: SeriesSource, tier: str, start_ms: int, end_ms: int) -> list[dict]:
    """Saetze einer Serie und Stufe ueber [start_ms, end_ms)."""
    plus = _conversion(hass, source.plus, source.unit)
    if plus is None:
        return []
    minus = None
    if source.minus:
        minus = _conversion(hass, source.minus, source.unit)
        if minus is None:
            return []
    instance = get_instance(hass)
    # Differenzserien lassen sich aus Statistik nicht bilden: min und max der
    # Differenz folgen nicht aus min und max der beiden Seiten.
    if tier == "5m" and source.minus is None:
        rows = await instance.async_add_executor_job(_statistics_rows, hass, source, start_ms, end_ms)
        if rows is not None:
            return rows
    return await instance.async_add_executor_job(
        _state_rows, hass, source, tier, plus[0], minus[0] if minus else None, start_ms, end_ms
    )
