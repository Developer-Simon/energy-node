"""Gemeinsame Topic-Konventionen und Statusstruktur fuer die Slave-Seite.

Nach dem Master-Rueckbau (siehe installer-vorarbeit-design.md V1) gibt es
keine zentrale Live-Umschaltung der Abfrageraten mehr: Poll-Intervall und
Diagnose-Multiplikator kommen ausschliesslich aus der config.json. Dieses
Modul haelt nur noch die Topics fuer Simulation, Status und config/reload
sowie die retained Statusstruktur.
"""

from __future__ import annotations

from dataclasses import dataclass
from typing import Optional

SIMULATION_SETTING = "simulation_active"
CONFIG_RELOAD_TOPIC_SUFFIX = "config/reload"


def base_topic(service_id: str) -> str:
    return f"outstation/{service_id}"


def settings_set_topic(service_id: str, setting: str) -> str:
    """Retained Command-Topic, auf das der Slave hoert (aktuell nur
    simulation_active, direkt aus Home Assistant oder vom Node-Broadcast)."""
    return f"{base_topic(service_id)}/settings/{setting}/set"


def settings_state_topic(service_id: str, setting: str) -> str:
    """Retained Ack-Topic mit dem vom Slave tatsaechlich uebernommenen Wert."""
    return f"{base_topic(service_id)}/settings/{setting}"


def settings_status_topic(service_id: str) -> str:
    """Gemeinsame Statusstruktur (JSON) mit Rate, Diagnose-Multiplikator,
    letzter erfolgreicher Aktualisierung, Laufzeitstatus und optionalem
    Fehlergrund."""
    return f"{base_topic(service_id)}/settings/status"


def master_settings_set_topic(node_device_id: str, setting: str) -> str:
    """Globaler Command-Topic eines Nodes, den alle Slaves abonnieren
    (nur noch fuer den simulation_active-Broadcast)."""
    return f"{base_topic(node_device_id)}/settings/{setting}/set"


def config_reload_topic(service_id: str) -> str:
    """Command topic for asking a service to reload its JSON configuration."""
    return f"{base_topic(service_id)}/{CONFIG_RELOAD_TOPIC_SUFFIX}"


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
    """Gemeinsame Statusstruktur, die jeder Slave retained veroeffentlicht.
    Ein ablehnender Slave wird ueber `runtime_status`/`error` sichtbar."""

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
