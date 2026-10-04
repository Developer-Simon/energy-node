"""Home Assistant als Lieferant im Verlauf-Austausch.

Der Peer bietet an und beantwortet Nachfragen. Er fragt selbst nie nach,
fuehrt keinen eigenen Bestand und fuettert den Ringpuffer des Servers nicht:
der Recorder ist sein Bestand.
"""
from __future__ import annotations

import asyncio
import json
import logging
import time
from collections.abc import Awaitable, Callable, Coroutine
from typing import Any

import aiohttp

from .buckets import census, in_ranges, raster_window
from .client import AuthRequired, DashboardClient, ExchangeError
from .const import PEER_LABEL, PROTOCOL, RECONNECT_MAX_S, RECONNECT_MIN_S, REFRESH_SECONDS, TIERS
from .series_map import SeriesSource

_LOGGER = logging.getLogger(__name__)

Resolve = Callable[[list[dict]], list[SeriesSource]]
Rows = Callable[[SeriesSource, str, int, int], Awaitable[list[dict]]]


class HistoryPeer:
    """Ein dauerhafter Peer an genau einem Dashboard."""

    def __init__(
        self,
        client: DashboardClient,
        resolve: Resolve,
        rows: Rows,
        now_ms: Callable[[], int] | None = None,
        label: str = PEER_LABEL,
    ) -> None:
        self._client = client
        self._resolve = resolve
        self._rows = rows
        self._now_ms = now_ms or (lambda: int(time.time() * 1000))
        self._label = label
        self._peer_id = ""
        self._announcement: dict = {}
        self._sources: dict[str, SeriesSource] = {}
        self._last_offer = ""
        # (Rasteranfang der 1m-Stufe, Deckung). Der 5m-Rasteranfang wechselt
        # nur an vollen Stunden, ein Wechsel der 1m-Stufe deckt ihn also mit ab.
        self._cached: tuple[int, dict] | None = None
        self._tasks: set[asyncio.Task] = set()

    async def run(self) -> None:
        """Verbinden, bedienen, bei Abbruch mit wachsendem Abstand neu verbinden."""
        delay = RECONNECT_MIN_S
        while True:
            try:
                await self._session()
                delay = RECONNECT_MIN_S
            except asyncio.CancelledError:
                raise
            except (AuthRequired, ExchangeError, aiohttp.ClientError, TimeoutError) as err:
                _LOGGER.debug("history exchange disconnected: %s", err)
            except Exception:  # noqa: BLE001 - die Schleife darf nie enden
                _LOGGER.exception("history exchange failed")
            await asyncio.sleep(delay)
            delay = min(delay * 2, RECONNECT_MAX_S)

    async def _session(self) -> None:
        if not self._client.has_session:
            await self._client.login()
        await self.refresh_announcement()
        refresher = asyncio.create_task(self._refresh_loop())
        try:
            async for event, payload in self._client.stream():
                self.handle(event, payload)
        finally:
            refresher.cancel()
            for task in list(self._tasks):
                task.cancel()
            self._peer_id = ""

    async def refresh_announcement(self) -> None:
        announcement = await self._client.announcement()
        if announcement.get("protocol") != PROTOCOL:
            raise ExchangeError(f"protocol {announcement.get('protocol')} is not supported")
        if "series" not in announcement:
            raise ExchangeError("the dashboard does not list its series, it is too old")
        self._announcement = announcement
        self._sources = {source.series: source for source in self._resolve(announcement["series"])}

    def handle(self, event: str, payload: dict) -> asyncio.Task | None:
        """Verteilt ein Ereignis. Laengere Arbeit laeuft als eigene Aufgabe,
        damit der Strom weitergelesen wird: staut sich die Warteschlange des
        Servers, trennt er den Peer."""
        if event == "hello":
            if payload.get("protocol") != PROTOCOL:
                raise ExchangeError(f"protocol {payload.get('protocol')} is not supported")
            self._peer_id = str(payload.get("peer_id") or "")
            self._last_offer = ""
            return self._spawn(self.offer(force=True, fresh=True))
        if event == "peer-joined":
            # Der Neue kennt unser Angebot noch nicht.
            return self._spawn(self.offer(force=True, fresh=False))
        if event == "request":
            return self._spawn(self.answer(payload))
        return None

    def _spawn(self, work: Coroutine[Any, Any, None]) -> asyncio.Task:
        task = asyncio.create_task(self._guard(work))
        self._tasks.add(task)
        task.add_done_callback(self._tasks.discard)
        return task

    async def _guard(self, work: Coroutine[Any, Any, None]) -> None:
        try:
            await work
        except asyncio.CancelledError:
            raise
        except AuthRequired:
            # Der Strom bricht kurz darauf ebenfalls ab, run() meldet neu an.
            _LOGGER.debug("history exchange session expired")
        except Exception:  # noqa: BLE001
            _LOGGER.exception("history exchange handler failed")

    async def _coverage(self, fresh: bool) -> dict:
        now = self._now_ms()
        key = raster_window("1m", now)[0]
        if not fresh and self._cached is not None and self._cached[0] == key:
            return self._cached[1]
        coverage: dict = {}
        for tier in TIERS:
            start = raster_window(tier, now)[0]
            per_series = {}
            for source in self._sources.values():
                rows = await self._rows(source, tier, start, now)
                if rows:
                    per_series[source.series] = census(tier, (row["ts"] for row in rows), now)
            if per_series:
                coverage[tier] = per_series
        self._cached = (key, coverage)
        return coverage

    async def offer(self, force: bool, fresh: bool) -> None:
        if not self._peer_id:
            return
        coverage = await self._coverage(fresh)
        if not coverage:
            return
        fingerprint = json.dumps(coverage, sort_keys=True)
        if not force and fingerprint == self._last_offer:
            return
        self._last_offer = fingerprint
        await self._client.post("/offer", {"peer": self._peer_id, "label": self._label, "coverage": coverage})

    async def answer(self, payload: dict) -> None:
        to = payload.get("peer")
        req_id = payload.get("req_id")
        tier = payload.get("tier")
        if not to or not req_id or tier not in TIERS:
            return
        source = self._sources.get(str(payload.get("series") or ""))
        limit = int(self._announcement.get("max_rows_per_request", 20000))
        chunk = int(self._announcement.get("max_rows_per_deliver", 500))
        now = self._now_ms()
        rows: list[dict] = []
        if source is not None:
            for span in payload.get("ranges") or []:
                if not isinstance(span, (list, tuple)) or len(span) != 2:
                    continue
                begin, end = int(span[0]), min(int(span[1]), now)
                if end <= begin:
                    continue
                rows.extend(in_ranges(await self._rows(source, tier, begin, end), [(begin, end)]))
                if len(rows) >= limit:
                    rows = rows[:limit]
                    break
        base = {"peer": self._peer_id, "to": to, "req_id": req_id, "tier": tier}
        if not rows:
            # Eine leere Schlusslieferung beendet die Nachfrage beim Browser.
            await self._client.post("/deliver", {**base, "seq": 0, "final": True, "rows": []})
            return
        for seq, start in enumerate(range(0, len(rows), chunk)):
            await self._client.post("/deliver", {
                **base, "seq": seq, "final": start + chunk >= len(rows), "rows": rows[start:start + chunk],
            })

    async def refresh(self) -> None:
        """Ankuendigung neu lesen und das Angebot nur bei Aenderung erneuern."""
        await self.refresh_announcement()
        await self.offer(force=False, fresh=True)

    async def _refresh_loop(self) -> None:
        while True:
            await asyncio.sleep(REFRESH_SECONDS)
            await self._guard(self.refresh())
