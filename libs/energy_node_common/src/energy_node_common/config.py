"""Shared JSON configuration loading and atomic in-process reloading."""

from __future__ import annotations

import hashlib
import json
from pathlib import Path
from threading import RLock
from typing import Callable, Generic, Optional, Tuple, TypeVar

T = TypeVar("T")


def load_json(path: str | Path):
    """Load one complete JSON document from ``path``."""
    with Path(path).open("r", encoding="utf-8") as config_file:
        return json.load(config_file)


def file_revision(path: str | Path) -> str:
    """SHA-256 der Dateibytes, leer wenn unlesbar.

    Dieselbe Pruefsumme bildet das Dashboard beim Speichern (config.checksum).
    Stimmen beide ueberein, gehoert ein Status zu genau dieser Datei, ohne dass
    die Kennung ueber MQTT mitgeschickt werden muss."""
    try:
        return hashlib.sha256(Path(path).read_bytes()).hexdigest()
    except OSError:
        return ""


class ConfigRejected(ValueError):
    """Konfigurationsfehler mit stabilem Code, den das Dashboard uebersetzt."""

    def __init__(self, message: str, code: str):
        super().__init__(message)
        self.code = code


class ReloadableConfig(Generic[T]):
    """Load configuration values and replace them only after a valid load."""

    def __init__(self, path: str | Path, loader: Callable[[str], T]):
        self.path = str(path)
        self._loader = loader
        self._lock = RLock()
        self._value: T | None = None
        # Pruefsumme der Datei beim letzten Ladeversuch (gelungen oder nicht)
        # und der Datei, mit der der Dienst gerade arbeitet.
        self.attempted_revision = ""
        self.applied_revision = ""
        # Fehler eines load_or() beim Start; der Slave meldet ihn als rejected.
        self.load_error: Optional[BaseException] = None

    def load(self) -> T:
        return self._replace()

    def reload(self) -> T:
        return self._replace()

    def load_or(self, fallback: T) -> Tuple[T, Optional[Exception]]:
        """Beim Start laden, bei einem Fehler mit ``fallback`` weiterlaufen.

        Statt abzubrechen (und von systemd in einer Schleife neu gestartet zu
        werden) laeuft der Dienst mit dem Ersatzwert, der Slave meldet den
        Fehler als rejected, und ein spaeteres config/reload nimmt den
        Betrieb auf."""
        try:
            return self.load(), None
        except Exception as exc:  # jeder Ladefehler, auch OSError und JSON
            with self._lock:
                self._value = fallback
            self.load_error = exc
            return fallback, exc

    @property
    def value(self) -> T:
        with self._lock:
            if self._value is None:
                raise RuntimeError("configuration has not been loaded")
            return self._value

    def mark_attempt(self) -> None:
        """Die Pruefsumme der Datei festhalten, die gleich geladen wird."""
        self.attempted_revision = file_revision(self.path)

    def load_candidate(self) -> T:
        """Neu laden, ohne den aktiven Wert zu ersetzen.

        Gebraucht, wenn mehrere Konfigurationen gemeinsam uebernommen
        werden muessen: erst beide Kandidaten laden, dann beide committen.
        Ein Fehler in einer der beiden laesst so beide unveraendert - ein
        Dienst darf nie mit halb uebernommener Konfiguration weiterlaufen.
        """
        self.mark_attempt()
        return self._loader(self.path)

    def commit(self, value: T) -> T:
        """Einen zuvor geladenen Kandidaten aktiv setzen."""
        with self._lock:
            self._value = value
        self.applied_revision = self.attempted_revision
        self.load_error = None
        return value

    def _replace(self) -> T:
        return self.commit(self.load_candidate())