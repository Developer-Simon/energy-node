"""Protokoll der Fake-Konsole: Ringpuffer mit Stufen und fortlaufender Nummer."""
import threading
import time
from collections import deque

LOG_SIZE = 500
LEVELS = ("info", "warn", "error")


class Log:
    """Ringpuffer fuer Meldungen mit fortlaufender Nummer."""

    def __init__(self, size: int = LOG_SIZE) -> None:
        self._entries = deque(maxlen=size)
        self._counter = 0
        self._lock = threading.Lock()

    def add(self, source: str, text: str, level: str = "info") -> None:
        if level not in LEVELS:
            level = "info"
        with self._lock:
            self._counter += 1
            self._entries.append({
                "n": self._counter,
                "t": time.strftime("%H:%M:%S"),
                "source": source,
                "text": text,
                "level": level,
            })

    def since(self, n: int) -> tuple[int, list[dict]]:
        with self._lock:
            entries = [dict(e) for e in self._entries if e["n"] > n]
            return self._counter, entries
