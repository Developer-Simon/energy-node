"""Gemeinsamer Scheduler fuer Kern- und Diagnoseabfragen.

Unterstuetzt sowohl synchrone Services (Tuya, Batterie-SoC, Node) ueber
einen Hintergrund-Thread als auch die asyncio-basierte EZ1-Bridge ueber
`AsyncScheduler`. Beide Varianten teilen sich die Zeitbasis in `PollClock`:
Aenderungen am Poll-Intervall oder Diagnose-Multiplikator wirken sofort,
weil die Faelligkeit anhand der verstrichenen Realzeit statt fester
Poll-Zaehler bestimmt wird.
"""

from __future__ import annotations

import asyncio
import threading
import time
from typing import Callable, Optional


class PollClock:
    """Bestimmt, wann die naechste Kern- bzw. Diagnoseabfrage faellig ist,
    basierend auf der aktuell konfigurierten Rate und dem Multiplikator."""

    def __init__(self, get_interval_s: Callable[[], float], get_diagnostic_multiplier: Callable[[], float]):
        self._get_interval_s = get_interval_s
        self._get_diagnostic_multiplier = get_diagnostic_multiplier
        self._last_diagnostic_ts = 0.0

    def diagnostic_due(self, now: float) -> bool:
        interval = max(self._get_interval_s(), 0.001)
        multiplier = max(self._get_diagnostic_multiplier(), 1)
        diagnostic_interval = interval * multiplier
        if now - self._last_diagnostic_ts >= diagnostic_interval:
            self._last_diagnostic_ts = now
            return True
        return False

    def core_sleep_s(self) -> float:
        return self._get_interval_s()


class Scheduler(threading.Thread):
    """Thread-basierter Scheduler fuer synchrone Poll-Callbacks."""

    def __init__(
        self,
        poll_core: Callable[[], None],
        get_interval_s: Callable[[], float],
        get_diagnostic_multiplier: Callable[[], float] = lambda: 1,
        poll_diagnostics: Optional[Callable[[], None]] = None,
        on_error: Optional[Callable[[Exception], None]] = None,
        name: str = "energy-node-slave-scheduler",
    ):
        super().__init__(daemon=True, name=name)
        self._poll_core = poll_core
        self._poll_diagnostics = poll_diagnostics
        self._clock = PollClock(get_interval_s, get_diagnostic_multiplier)
        self._on_error = on_error
        self._stop_event = threading.Event()

    def stop(self) -> None:
        self._stop_event.set()

    def run(self) -> None:
        while not self._stop_event.is_set():
            now = time.time()
            try:
                self._poll_core()
                if self._poll_diagnostics and self._clock.diagnostic_due(now):
                    self._poll_diagnostics()
            except Exception as exc:  # Poll-Fehler duerfen den Scheduler nicht beenden
                if self._on_error:
                    self._on_error(exc)
            self._stop_event.wait(self._clock.core_sleep_s())


class AsyncScheduler:
    """asyncio-basierter Scheduler fuer Coroutine-Poll-Callbacks (EZ1)."""

    def __init__(
        self,
        poll_core: Callable,
        get_interval_s: Callable[[], float],
        get_diagnostic_multiplier: Callable[[], float] = lambda: 1,
        poll_diagnostics: Optional[Callable] = None,
        on_error: Optional[Callable[[Exception], None]] = None,
    ):
        self._poll_core = poll_core
        self._poll_diagnostics = poll_diagnostics
        self._clock = PollClock(get_interval_s, get_diagnostic_multiplier)
        self._on_error = on_error
        self._task: Optional[asyncio.Task] = None

    def start(self, loop: Optional[asyncio.AbstractEventLoop] = None) -> asyncio.Task:
        loop = loop or asyncio.get_event_loop()
        self._task = loop.create_task(self._run())
        return self._task

    async def _run(self) -> None:
        while True:
            now = time.time()
            try:
                await self._poll_core()
                if self._poll_diagnostics and self._clock.diagnostic_due(now):
                    await self._poll_diagnostics()
            except asyncio.CancelledError:
                raise
            except Exception as exc:
                if self._on_error:
                    self._on_error(exc)
            await asyncio.sleep(self._clock.core_sleep_s())

    def stop(self) -> None:
        if self._task:
            self._task.cancel()
