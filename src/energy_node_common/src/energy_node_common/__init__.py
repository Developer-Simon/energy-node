"""Gemeinsame Infrastruktur fuer die Energy-Node Python-MQTT-Bridges.

Dieses Paket kapselt das Master/Slave-Protokoll zur zentralen Steuerung der
Abfrageraten (siehe knowhow/plan.md): MQTT-/Home-Assistant-Discovery-Helfer,
den gemeinsamen Scheduler sowie die Slave- und Master-Seite des
Settings-Protokolls.

Enthaelt bewusst KEINE geraetespezifische Fachlogik (keine EZ1-/Tuya-API,
keine SoC-Berechnung, keine Pi-Systemdiagnose) - das bleibt Aufgabe der
einzelnen Services.
"""

from .master import Master, SlaveDescriptor
from .slave import Slave
from . import appconfig

__all__ = ["Master", "SlaveDescriptor", "Slave", "appconfig"]

__version__ = "0.1.0"
