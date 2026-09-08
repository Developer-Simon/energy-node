"""Gemeinsame Infrastruktur fuer die Energy-Node Python-MQTT-Bridges.

Kapselt MQTT-/Home-Assistant-Discovery-Helfer, den gemeinsamen Scheduler
und die Slave-Seite des Settings-Protokolls: geraeteweise und globale
Simulation, `config/reload`, die retained `settings/status`-Struktur und
`last_update`. Eine zentrale Master-Seite (Live-Umschaltung der Abfrageraten
ueber HA-Number-Entities) gibt es nicht mehr; Poll-Raten kommen aus der
config.json.

Enthaelt bewusst KEINE geraetespezifische Fachlogik.
"""

from .slave import Slave
from . import appconfig

__all__ = ["Slave", "appconfig"]

__version__ = "0.1.0"
