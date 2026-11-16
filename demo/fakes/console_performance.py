"""Reiter Leistung der Fake-Konsole.

Schreibt das Szenario der Leistungsseite in die Zustandsdatei, die der Go-Teil
des Dashboards liest (siehe dashboard/internal/performance/fakefile.go, dort
gilt die Voreinstellung good, wenn die Datei fehlt). Die Werte der Szenarien
sind hier eine Kopie dieser Datei. Die Oberflaeche steht in
console_performance_ui.py.

Jede Aktion ist eine Zeile in ACTIONS. Ungueltige Eingaben und nicht
schreibbare Zustandsdateien werden als ValueError geworfen (400).
"""
import copy
import json
import math
import os
import tempfile
import threading
from pathlib import Path

from console_performance_ui import performance_html
from fake_console import Log, Tab

ACTIONS = ("preset", "global", "hour", "external", "unit")
PRESET_LABELS = {
    "measuring": "Messung läuft",
    "good": "Alles gut",
    "tight": "Knapp",
    "over": "Über Budget",
    "throttled": "Gedrosselt und heiß",
    "memory": "Speicher knapp",
    "lonely": "Allein ohne Dienste",
    "many-browsers": "Viele Browser",
}
CUSTOM = "custom"
CUSTOM_LABEL = "eigene Werte"
SERVICE_UNITS = ("shelly", "trucki", "battery_soc", "apsystems", "tuya")
FIXED_UNITS = ("automation.service", "mosquitto.service", "tailscaled.service", "caddy.service",
               "energy-node-dashboard.service")
UNIT_IDS = SERVICE_UNITS + FIXED_UNITS
FIXED_KEYS = ("present", "cpu_pct", "ram_mb")
EXTERNAL_DEFAULT = {"name": "Wärmepumpe", "every_s": 15}
HOUR_S = 3600

# Art, Untergrenze, Obergrenze, leer erlaubt. Art: number, integer oder bool.
GLOBAL_SPECS = {
    "measured_s": ("number", 0, None, False),
    "cores": ("integer", 1, 64, False),
    "cpu_pct": ("number", 0, 100, False),
    "temp_c": ("number", -20, 120, True),
    "throttled": ("bool", None, None, False),
    "ram_free_pct": ("number", 0, 100, False),
    "swap_pages_s": ("number", 0, None, False),
    "clients_max": ("integer", 1, None, False),
}
UNIT_SPECS = {
    "present": ("bool", None, None, False),
    "cpu_pct": ("number", 0, 100, False),
    "ram_mb": ("number", 0, None, False),
    "round_s": ("number", 0, None, False),
    "msg_per_min": ("number", 0, None, False),
}
SERVICE_ONLY = ("round_s", "msg_per_min")


def _service(cpu: float, ram_mb: float, round_s: float, msg_per_min: float) -> dict:
    return {"present": True, "cpu_pct": cpu, "ram_mb": ram_mb, "round_s": round_s,
            "msg_per_min": msg_per_min}


def _fixed(cpu: float, ram_mb: float) -> dict:
    return {"present": True, "cpu_pct": cpu, "ram_mb": ram_mb}


def _good() -> dict:
    return {
        "preset": "good",
        "measured_s": 25200,
        "cores": 4,
        "cpu_pct": 32.0,
        "temp_c": 54.0,
        "throttled": False,
        "ram_free_pct": 61.0,
        "swap_pages_s": 0.0,
        "clients_max": 3,
        "external": {"name": "Wärmepumpe", "every_s": 15},
        "dashboard_parts": {"push": 0.45, "http": 0.15, "mqtt": 0.25, "node": 0.05, "background": 0.10},
        "gc_share": 0.12,
        "units": {
            "shelly": _service(5.0, 22, 0.4, 126),
            "trucki": _service(3.0, 18, 1.2, 40),
            "battery_soc": _service(2.0, 16, 0.1, 60),
            "apsystems": _service(1.0, 20, 3.1, 8),
            "tuya": _service(1.5, 24, 2.4, 12),
            "automation.service": _fixed(4.0, 31),
            "mosquitto.service": _fixed(3.0, 12),
            "tailscaled.service": _fixed(0.5, 40),
            "caddy.service": _fixed(0.5, 36),
            "energy-node-dashboard.service": _fixed(8.0, 70),
        },
    }


def _presets() -> dict:
    presets = {}

    measuring = _good()
    measuring["preset"] = "measuring"
    measuring["measured_s"] = 1800
    presets["measuring"] = measuring

    presets["good"] = _good()

    tight = _good()
    tight["preset"] = "tight"
    tight["cpu_pct"] = 64
    presets["tight"] = tight

    over = _good()
    over["preset"] = "over"
    over["cpu_pct"] = 78
    over["units"]["shelly"] = _service(14.0, 22, 0.4, 126)
    over["units"]["trucki"] = _service(9.0, 18, 1.2, 40)
    presets["over"] = over

    throttled = _good()
    throttled["preset"] = "throttled"
    throttled["throttled"] = True
    throttled["temp_c"] = 82.0
    throttled["cpu_pct"] = 40
    presets["throttled"] = throttled

    memory = _good()
    memory["preset"] = "memory"
    memory["ram_free_pct"] = 9
    memory["swap_pages_s"] = 40
    presets["memory"] = memory

    lonely = _good()
    lonely["preset"] = "lonely"
    lonely["external"] = None
    lonely["clients_max"] = 1
    lonely["units"] = {
        "energy-node-dashboard.service": _fixed(8.0, 70),
        "mosquitto.service": _fixed(3.0, 12),
        "automation.service": _fixed(4.0, 31),
    }
    presets["lonely"] = lonely

    browsers = _good()
    browsers["preset"] = "many-browsers"
    browsers["clients_max"] = 7
    presets["many-browsers"] = browsers

    return presets


PRESETS = _presets()


def _checked(name: str, value, kind: str, low, high, nullable: bool):
    """Prueft einen Wert und gibt ihn im richtigen Typ zurueck."""
    if value is None and nullable:
        return None
    if kind == "bool":
        if not isinstance(value, bool):
            raise ValueError(f"{name} ist ungültig")
        return value
    if isinstance(value, bool) or not isinstance(value, (int, float)) or not math.isfinite(value):
        raise ValueError(f"{name} ist ungültig")
    if kind == "integer":
        if not float(value).is_integer():
            raise ValueError(f"{name} ist ungültig")
        value = int(value)
    else:
        value = float(value)
    if (low is not None and value < low) or (high is not None and value > high):
        raise ValueError(f"{name} liegt außerhalb des Bereichs")
    return value


def _unit(state: dict, unit_id: str) -> dict:
    """Die Einheit aus der Datei, fehlt sie, deren Vorgabe mit present false."""
    units = state.get("units") or {}
    if isinstance(units.get(unit_id), dict):
        return dict(units[unit_id])
    absent = dict(copy.deepcopy(PRESETS["good"]["units"][unit_id]))
    absent["present"] = False
    return absent


class PerformanceTab(Tab):
    name = "performance"
    title = "Leistung"
    sources = ("performance",)
    html = performance_html(PRESET_LABELS, UNIT_IDS, SERVICE_UNITS)

    def __init__(self, log: Log, state_path: Path) -> None:
        self._log = log
        self._path = Path(state_path)
        self._lock = threading.Lock()

    def _load(self) -> dict:
        """Liest die Datei. Fehlt sie oder ist sie kaputt, gilt das Szenario good."""
        good = copy.deepcopy(PRESETS["good"])
        try:
            data = json.loads(self._path.read_text(encoding="utf-8"))
        except (OSError, ValueError):
            return good
        if not isinstance(data, dict):
            return good
        return {**good, **data}

    def _write(self, state: dict) -> None:
        """Schreibt atomar: temporaere Datei im selben Ordner, dann os.replace."""
        tmp = None
        try:
            handle, tmp = tempfile.mkstemp(dir=self._path.parent, prefix=".performance-state-", suffix=".tmp")
            with os.fdopen(handle, "w", encoding="utf-8") as out:
                json.dump(state, out, ensure_ascii=False)
            os.replace(tmp, self._path)
        except OSError:
            if tmp is not None:
                try:
                    os.unlink(tmp)
                except OSError:
                    pass
            raise ValueError("Zustandsdatei nicht schreibbar") from None

    def act(self, action: str, body: dict) -> dict:
        if action not in ACTIONS:
            raise ValueError(f"unbekannte Aktion {action}")
        if not isinstance(body, dict):
            raise ValueError("ungültiger Body")
        with self._lock:
            state = self._load()
            if action == "preset":
                name = body.get("name")
                if name not in PRESET_LABELS:
                    raise ValueError("Szenario ist ungültig")
                state = copy.deepcopy(PRESETS[name])
                text = f"Szenario {PRESET_LABELS[name]}"
            elif action == "global":
                if not body:
                    raise ValueError("Wert fehlt")
                for name in body:
                    if name not in GLOBAL_SPECS:
                        raise ValueError(f"{name} ist unbekannt")
                for name, value in body.items():
                    state[name] = _checked(name, value, *GLOBAL_SPECS[name])
                state["preset"] = CUSTOM
                text = "Leistungswerte geändert"
            elif action == "hour":
                state["measured_s"] = float(state["measured_s"]) + HOUR_S
                state["preset"] = CUSTOM
                text = "Messdauer um 1 Stunde erhöht"
            elif action == "external":
                if not body:
                    raise ValueError("Wert fehlt")
                for name in body:
                    if name not in ("none", "name", "every_s"):
                        raise ValueError(f"{name} ist unbekannt")
                if "none" in body and not isinstance(body["none"], bool):
                    raise ValueError("none ist ungültig")
                if body.get("none"):
                    state["external"] = None
                    text = "Fremdgerät entfernt"
                else:
                    external = dict(state.get("external") or EXTERNAL_DEFAULT)
                    if "name" in body:
                        if not isinstance(body["name"], str) or not body["name"].strip():
                            raise ValueError("name ist ungültig")
                        external["name"] = body["name"].strip()
                    if "every_s" in body:
                        external["every_s"] = _checked("every_s", body["every_s"], "number", 0, None, False)
                    state["external"] = external
                    text = "Fremdgerät geändert"
                state["preset"] = CUSTOM
            else:
                unit_id = body.get("id")
                if unit_id not in UNIT_IDS:
                    raise ValueError("Einheit ist ungültig")
                fields = {name: value for name, value in body.items() if name != "id"}
                if not fields:
                    raise ValueError("Wert fehlt")
                service = unit_id in SERVICE_UNITS
                unit = _unit(state, unit_id)
                for name, value in fields.items():
                    if name not in UNIT_SPECS:
                        raise ValueError(f"{name} ist unbekannt")
                    if name in SERVICE_ONLY and not service:
                        raise ValueError(f"{name} gilt nur für Dienste")
                    unit[name] = _checked(name, value, *UNIT_SPECS[name])
                if not service:
                    unit = {key: unit[key] for key in FIXED_KEYS}
                state["units"] = {**(state.get("units") or {}), unit_id: unit}
                state["preset"] = CUSTOM
                text = f"Einheit {unit_id} geändert"
            self._write(state)
            self._log.add("performance", text)
        return self.state()

    def state(self) -> dict:
        with self._lock:
            state = self._load()
        units = []
        for unit_id in UNIT_IDS:
            unit = _unit(state, unit_id)
            service = unit_id in SERVICE_UNITS
            units.append({
                "id": unit_id,
                "service": service,
                "present": bool(unit.get("present")),
                "cpu_pct": unit.get("cpu_pct", 0),
                "ram_mb": unit.get("ram_mb", 0),
                "round_s": unit.get("round_s") if service else None,
                "msg_per_min": unit.get("msg_per_min") if service else None,
            })
        preset = state.get("preset", CUSTOM)
        values = {name: state.get(name) for name in GLOBAL_SPECS}
        values["external"] = state.get("external")
        return {
            "preset": preset,
            "preset_label": PRESET_LABELS.get(preset, CUSTOM_LABEL),
            "values": values,
            "units": units,
        }

    def summary(self) -> dict:
        with self._lock:
            preset = self._load().get("preset", CUSTOM)
        return {"level": "ok" if preset == "good" else "changed",
                "text": PRESET_LABELS.get(preset, CUSTOM_LABEL)}

    def reset(self) -> None:
        with self._lock:
            self._write(copy.deepcopy(PRESETS["good"]))
