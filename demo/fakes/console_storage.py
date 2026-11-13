"""Reiter Speicher der Fake-Konsole.

Steckt Demo-Sticks ein und ab, stellt ein, ob Formatieren oder Einhängen
fehlschlaegt. Der Zustand liegt im Speicherordner der Demo, das Modell in
fake_storage.py. Der Helfer ist ein Prozess, aber er liest denselben Zustand,
deshalb spricht dieser Reiter ihn nicht ueber den Steuerport an. Die
Oberflaeche steht in console_storage_ui.py.

Jede Aktion ist eine Zeile in ACTIONS. Ungueltige Eingaben und Fehler im
Ablauf werden als ValueError geworfen (400).
"""
import threading
from pathlib import Path

import fake_storage
from console_storage_ui import storage_html
from fake_console import Log, Tab
from fake_journal_helper import machine_id

ACTIONS = ("plug", "fault")
FAULT_LABELS = {"format": "Formatieren", "mount": "Einhängen"}
MARKER_TEXT = {"ok": "Kennung passt", "foreign": "fremde Kennung", "none": "keine Kennung"}


def _bool(body: dict, name: str) -> bool:
    value = body.get(name)
    if not isinstance(value, bool):
        raise ValueError(f"{name} ist ungültig")
    return value


class StorageTab(Tab):
    name = "storage"
    title = "Speicher"
    sources = ("storage",)
    html = storage_html(fake_storage.load_catalog())

    def __init__(self, log: Log, storage_dir: Path, catalog_path: Path = fake_storage.CATALOG_PATH,
                 machine_id_value: str | None = None) -> None:
        self._log = log
        self._storage = Path(storage_dir)
        self._mount = self._storage / "mount"
        self._catalog = fake_storage.load_catalog(catalog_path)
        self._machine = machine_id_value if machine_id_value is not None else machine_id()
        self._lock = threading.Lock()

    def _stick(self, body: dict) -> dict:
        kname = body.get("kname")
        stick = fake_storage.find_stick(self._catalog, kname) if isinstance(kname, str) else None
        if stick is None:
            raise ValueError("Stick ist ungültig")
        return stick

    def _marker_status(self) -> str:
        try:
            marker = (self._mount / fake_storage.MARKER).read_text(encoding="utf-8").strip()
        except OSError:
            return "none"
        return "ok" if marker == self._machine else "foreign"

    def _mounted_text(self, state: dict) -> str:
        if not (state["mounted"] and state["active"]):
            return "Nichts eingehängt"
        stick = fake_storage.find_stick(self._catalog, state["active"])
        name = f"{state['active']} ({stick['name']})"
        return f"Eingehängt: {name}, {MARKER_TEXT[self._marker_status()]}"

    def _plugged_text(self, state: dict) -> str:
        if not state["plugged"]:
            return "Kein Stick eingesteckt"
        return "Eingesteckt: " + ", ".join(state["plugged"])

    def state(self) -> dict:
        with self._lock:
            state = fake_storage.load_state(self._storage)
            sticks = [{
                "kname": stick["kname"],
                "name": stick["name"],
                "plugged": stick["kname"] in state["plugged"],
                "active": state["active"] == stick["kname"],
                "foreign": "marker" in stick,
                "formatted": fake_storage.is_formatted(stick, state),
            } for stick in self._catalog if stick.get("disk", stick["kname"]) == stick["kname"]]
            return {
                "sticks": sticks,
                "format_fails": state["format_fails"],
                "mount_fails": state["mount_fails"],
                "plugged_text": self._plugged_text(state),
                "mounted_text": self._mounted_text(state),
            }

    def act(self, action: str, body: dict) -> dict:
        if action not in ACTIONS:
            raise ValueError(f"unbekannte Aktion {action}")
        if not isinstance(body, dict):
            raise ValueError("ungültiger Body")
        with self._lock:
            if action == "plug":
                stick = self._stick(body)
                plugged = _bool(body, "plugged")
                try:
                    if plugged:
                        fake_storage.plug(self._storage, self._mount, self._catalog,
                                          stick["kname"], self._machine)
                    else:
                        fake_storage.pull(self._storage, self._mount, self._catalog, stick["kname"])
                except (fake_storage.StorageError, fake_storage.StorageRefused) as exc:
                    raise ValueError(str(exc)) from None
                if not plugged:
                    self._log.add("storage", f"{stick['kname']} gezogen")
                elif "marker" in stick:
                    self._log.add("storage", f"{stick['kname']} eingesteckt, fremde Kennung eingehängt", "warn")
                else:
                    self._log.add("storage", f"{stick['kname']} eingesteckt")
            else:
                name = body.get("name")
                if not isinstance(name, str) or name not in fake_storage.FAULTS:
                    raise ValueError("name ist ungültig")
                on = _bool(body, "on")
                fake_storage.set_fault(self._storage, name, on)
                self._log.add("storage", f"{FAULT_LABELS[name]} schlägt fehl: {'an' if on else 'aus'}")
        return self.state()

    def summary(self) -> dict:
        with self._lock:
            state = fake_storage.load_state(self._storage)
        parts = []
        if not state["plugged"]:
            parts.append("Kein Stick eingesteckt")
        if state["format_fails"]:
            parts.append("Formatieren schlägt fehl")
        if state["mount_fails"]:
            parts.append("Einhängen schlägt fehl")
        if state["mounted"] and state["active"]:
            stick = fake_storage.find_stick(self._catalog, state["active"])
            if stick is not None and "marker" in stick:
                parts.append("fremder Stick eingehängt")
        if state["plugged"] != fake_storage.DEFAULT_STATE["plugged"]:
            parts.append("Steckzustand geändert")
        if not parts:
            return {"level": "ok", "text": "Speicher normal"}
        return {"level": "changed", "text": ", ".join(parts)}

    def reset(self) -> None:
        with self._lock:
            fake_storage.reset(self._storage, self._mount, self._catalog)
