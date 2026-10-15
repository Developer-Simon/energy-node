"""Energiestatistik: Stundenwerte je Rolle aus der Langzeitstatistik.

Ein Satz der Stufe 1h hat dieselbe Form wie im Verlauf-Austausch. avg ist
das Stundenmittel in W, zahlenmaessig also die Energie der Stunde in Wh.
Bei einem Zaehler ist es die Differenz der Stunde in Wh.
"""
from __future__ import annotations

from collections.abc import Awaitable, Callable, Mapping
from dataclasses import dataclass
from datetime import datetime, timezone

from homeassistant.components.recorder import get_instance
from homeassistant.components.recorder.statistics import statistics_during_period
from homeassistant.core import HomeAssistant
from homeassistant.helpers import entity_registry as er

from .buckets import count_hours, day_census
from .const import CUSTOM_UNIQUE_PREFIX, DAY_MS, HOUR_MS, STATS_DISCOVERY, STATS_ROLES, STATS_WINDOW_DAYS, counter_key

# Der erste Lauf liest das Fenster in Jahresstuecken, damit eine Abfrage
# nicht 10 Jahre auf einmal in den Speicher holt.
SCAN_CHUNK_MS = 365 * DAY_MS


@dataclass(frozen=True)
class StatsSource:
    """Eine Rolle und woraus Home Assistant sie liefert."""

    role: str
    entity_id: str
    counter: bool

    @property
    def series(self) -> str:
        return f"role:{self.role}"


def resolve_stats(hass: HomeAssistant, options: Mapping) -> list[StatsSource]:
    """Zaehler aus den Optionen, sonst der Discovery-Sensor des Dashboards."""
    registry = er.async_get(hass)
    sources: list[StatsSource] = []
    for role in STATS_ROLES:
        counter = options.get(counter_key(role))
        if counter:
            sources.append(StatsSource(role, str(counter), True))
            continue
        unique_id = STATS_DISCOVERY.get(role)
        if unique_id is None:
            continue
        entity_id = registry.async_get_entity_id("sensor", "mqtt", unique_id)
        if entity_id:
            sources.append(StatsSource(role, entity_id, False))
    # Verbrauchskategorien: jede Entitaet mit dem Praefix, sortiert nach Rolle.
    custom = [
        (entry.unique_id[len(CUSTOM_UNIQUE_PREFIX):], entry.entity_id)
        for entry in registry.entities.values()
        if entry.domain == "sensor" and entry.platform == "mqtt" and entry.unique_id.startswith(CUSTOM_UNIQUE_PREFIX)
    ]
    for category, entity_id in sorted(custom):
        sources.append(StatsSource(f"custom:{category}", entity_id, False))
    return sources


def _at(ms: int) -> datetime:
    return datetime.fromtimestamp(ms / 1000, tz=timezone.utc)


def _hour_rows(hass: HomeAssistant, source: StatsSource, start_ms: int, end_ms: int) -> list[dict]:
    """Abgeschlossene Stunden in [start_ms, end_ms)."""
    if source.counter:
        units, types = {"energy": "Wh"}, {"change"}
    else:
        units, types = {"power": "W"}, {"mean", "min", "max"}
    found = statistics_during_period(hass, _at(start_ms), _at(end_ms), {source.entity_id}, "hour", units, types)
    rows: list[dict] = []
    for item in found.get(source.entity_id, []):
        begin = int(item["start"] * 1000)
        if begin < start_ms or begin + HOUR_MS > end_ms:
            continue
        if source.counter:
            value = item.get("change")
            # Negative Differenz: Zaehler zurueckgesetzt. Die Stunde fehlt
            # lieber, als dass sie falsch zaehlt.
            if value is None or value < 0:
                continue
            low = high = value
        else:
            value = item.get("mean")
            if value is None:
                continue
            low = value if item.get("min") is None else item["min"]
            high = value if item.get("max") is None else item["max"]
        rows.append({"series": source.series, "ts": begin, "min": low, "max": high, "avg": value, "n": 1, "u": "W"})
    return rows


async def async_hour_rows(hass: HomeAssistant, source: StatsSource, start_ms: int, end_ms: int) -> list[dict]:
    return await get_instance(hass).async_add_executor_job(_hour_rows, hass, source, start_ms, end_ms)


HourRows = Callable[[StatsSource, int, int], Awaitable[list[dict]]]


class StatsOffer:
    """Stundenzahl je UTC-Tag und Serie fuer das Angebot der Stufe 1h.

    Eine neue oder geaenderte Quelle wird einmal ueber das ganze Fenster
    gezaehlt, danach nur noch ab gestern. Quellen werden bei jedem Aufruf
    neu aufgeloest, weil die Discovery-Sensoren nach einem Neustart von
    Home Assistant erst spaeter erscheinen.
    """

    def __init__(self, resolve: Callable[[], list[StatsSource]], rows: HourRows) -> None:
        self._resolve = resolve
        self._rows = rows
        self._sources: dict[str, StatsSource] = {}
        self._counts: dict[str, dict[int, int]] = {}

    @property
    def series(self) -> set[str]:
        return set(self._sources)

    async def coverage(self, now_ms: int) -> dict:
        today = now_ms // DAY_MS * DAY_MS
        end = now_ms // HOUR_MS * HOUR_MS
        current = {source.series: source for source in self._resolve()}
        for series in list(self._counts):
            if current.get(series) != self._sources.get(series):
                del self._counts[series]
        self._sources = current
        for series, source in current.items():
            counts = self._counts.get(series)
            if counts is None:
                counts = self._counts[series] = {}
                begin = today - STATS_WINDOW_DAYS * DAY_MS
            else:
                begin = today - DAY_MS
                for day in [day for day in counts if day >= begin]:
                    del counts[day]
            for chunk in range(begin, end, SCAN_CHUNK_MS):
                found = await self._rows(source, chunk, min(chunk + SCAN_CHUNK_MS, end))
                for day, hours in count_hours((row["ts"] for row in found), now_ms).items():
                    counts[day] = counts.get(day, 0) + hours
        coverage = {}
        for series, counts in self._counts.items():
            census = day_census(counts, now_ms)
            if census is not None:
                coverage[series] = census
        return coverage

    async def rows(self, series: str, begin: int, end: int) -> list[dict]:
        source = self._sources.get(series)
        return [] if source is None else await self._rows(source, begin, end)
