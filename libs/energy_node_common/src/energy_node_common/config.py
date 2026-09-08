"""Shared JSON configuration loading and atomic in-process reloading."""

from __future__ import annotations

import json
from pathlib import Path
from threading import RLock
from typing import Callable, Generic, TypeVar

T = TypeVar("T")


def load_json(path: str | Path):
    """Load one complete JSON document from ``path``."""
    with Path(path).open("r", encoding="utf-8") as config_file:
        return json.load(config_file)


class ReloadableConfig(Generic[T]):
    """Load configuration values and replace them only after a valid load."""

    def __init__(self, path: str | Path, loader: Callable[[str], T]):
        self.path = str(path)
        self._loader = loader
        self._lock = RLock()
        self._value: T | None = None

    def load(self) -> T:
        return self._replace()

    def reload(self) -> T:
        return self._replace()

    @property
    def value(self) -> T:
        with self._lock:
            if self._value is None:
                raise RuntimeError("configuration has not been loaded")
            return self._value

    def load_candidate(self) -> T:
        """Neu laden, ohne den aktiven Wert zu ersetzen.

        Gebraucht, wenn mehrere Konfigurationen gemeinsam uebernommen
        werden muessen: erst beide Kandidaten laden, dann beide committen.
        Ein Fehler in einer der beiden laesst so beide unveraendert - ein
        Dienst darf nie mit halb uebernommener Konfiguration weiterlaufen.
        """
        return self._loader(self.path)

    def commit(self, value: T) -> T:
        """Einen zuvor geladenen Kandidaten aktiv setzen."""
        with self._lock:
            self._value = value
        return value

    def _replace(self) -> T:
        return self.commit(self.load_candidate())