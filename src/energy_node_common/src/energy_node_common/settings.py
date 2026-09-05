"""Gemeinsame Topic-Konventionen, Grenzwerte und Statusstruktur fuer das
Master/Slave-Polling-Protokoll.

Master und Slave importieren ausschliesslich aus diesem Modul, damit ein
zentral gesetzter Wert niemals von einem Slave abweichend validiert wird.
"""

from __future__ import annotations

from dataclasses import dataclass
from typing import Optional

# Sinnvolle, service-uebergreifende Grenzen fuer die Poll-Rate und den
# Diagnose-Multiplikator.
MIN_POLL_INTERVAL_S = 5
MAX_POLL_INTERVAL_S = 3600

MIN_DIAGNOSTIC_MULTIPLIER = 1
MAX_DIAGNOSTIC_MULTIPLIER = 200

POLL_INTERVAL_SETTING = "poll_interval_s"
DIAGNOSTIC_MULTIPLIER_SETTING = "diagnostic_poll_multiplier"
SIMULATION_SETTING = "simulation_active"
CONFIG_RELOAD_TOPIC_SUFFIX = "config/reload"

SETTINGS = (POLL_INTERVAL_SETTING, DIAGNOSTIC_MULTIPLIER_SETTING, SIMULATION_SETTING)


def base_topic(device_id: str) -> str:
    return f"outstation/{device_id}"


def settings_set_topic(device_id: str, setting: str) -> str:
    """Retained Command-Topic, auf das der Slave hoert (vom Master oder
    direkt aus Home Assistant beschrieben)."""
    return f"{base_topic(device_id)}/settings/{setting}/set"


def settings_state_topic(device_id: str, setting: str) -> str:
    """Retained Ack-Topic mit dem vom Slave tatsaechlich uebernommenen Wert."""
    return f"{base_topic(device_id)}/settings/{setting}"


def settings_status_topic(device_id: str) -> str:
    """Gemeinsame Statusstruktur (JSON) mit Soll-/Ist-Rate, Diagnose-
    Multiplikator, letzter erfolgreicher Aktualisierung, Laufzeitstatus und
    optionalem Fehlergrund."""
    return f"{base_topic(device_id)}/settings/status"


def master_settings_set_topic(master_device_id: str, setting: str) -> str:
    """Globaler Command-Topic eines Nodes, den alle Slaves abonnieren."""
    return f"{base_topic(master_device_id)}/settings/{setting}/set"


def master_settings_state_topic(master_device_id: str, setting: str) -> str:
    """Retained globaler Zustand, den der Node fuer seine Anzeige spiegelt."""
    return f"{base_topic(master_device_id)}/settings/{setting}"


def config_reload_topic(device_id: str) -> str:
    """Command topic for asking a service to reload its JSON configuration."""
    return f"{base_topic(device_id)}/{CONFIG_RELOAD_TOPIC_SUFFIX}"


def validate_poll_interval_s(value: float) -> Optional[str]:
    """Gibt None zurueck, wenn `value` gueltig ist, sonst einen kurzen
    Ablehnungsgrund fuer Diagnose/Logging."""
    if value < MIN_POLL_INTERVAL_S or value > MAX_POLL_INTERVAL_S:
        return f"out_of_range_{MIN_POLL_INTERVAL_S}_{MAX_POLL_INTERVAL_S}"
    return None


def validate_diagnostic_multiplier(value: float) -> Optional[str]:
    if value < MIN_DIAGNOSTIC_MULTIPLIER or value > MAX_DIAGNOSTIC_MULTIPLIER:
        return f"out_of_range_{MIN_DIAGNOSTIC_MULTIPLIER}_{MAX_DIAGNOSTIC_MULTIPLIER}"
    return None


def parse_bool(value) -> tuple[Optional[bool], Optional[str]]:
    normalized = str(value).strip().lower()
    if normalized in {"1", "true", "on", "yes", "enabled", "enable"}:
        return True, None
    if normalized in {"0", "false", "off", "no", "disabled", "disable"}:
        return False, None
    return None, "invalid_boolean_value"


def validate_simulation_active(value) -> Optional[str]:
    _, error = parse_bool(value)
    return error


@dataclass
class SlaveStatus:
    """Gemeinsame Statusstruktur, die jeder Slave retained veroeffentlicht
    und die der Master unveraendert fuer die zentrale Anzeige uebernimmt.
    Ein nicht erreichbarer oder ablehnender Slave wird ueber
    `runtime_status`/`error` sichtbar, nicht stillschweigend uebernommen."""

    poll_interval_s: float
    diagnostic_poll_multiplier: float
    actual_poll_interval_s: float
    simulation_active: bool = False
    last_update: Optional[int] = None
    runtime_status: str = "ok"  # "ok" | "rejected" | "pending"
    error: str = ""

    def to_dict(self) -> dict:
        return {
            "poll_interval_s": self.poll_interval_s,
            "diagnostic_poll_multiplier": self.diagnostic_poll_multiplier,
            "actual_poll_interval_s": self.actual_poll_interval_s,
            "simulation_active": int(self.simulation_active),
            "last_update": self.last_update,
            "runtime_status": self.runtime_status,
            "error": self.error,
        }

    @classmethod
    def from_dict(cls, data: dict) -> "SlaveStatus":
        simulation_active_value, _ = parse_bool(data.get("simulation_active", False))
        return cls(
            poll_interval_s=data.get("poll_interval_s"),
            diagnostic_poll_multiplier=data.get("diagnostic_poll_multiplier"),
            actual_poll_interval_s=data.get("actual_poll_interval_s"),
            simulation_active=simulation_active_value or False,
            last_update=data.get("last_update"),
            runtime_status=data.get("runtime_status", "ok"),
            error=data.get("error", ""),
        )
