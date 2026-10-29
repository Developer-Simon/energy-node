"""Reiter System der Fake-Konsole.

Schreibt Journal-Modi, Helper-Antwort, Antwort auf Systemaktionen und eingefuegte
Log-Zeilen in die Zustandsdatei des Fake-Journal-Helpers und liest dessen
Ereignisdatei. Es wird nichts wirklich neu gestartet oder ausgeschaltet. Die
Oberflaeche steht in console_system_ui.py.

Jede Aktion ist eine Zeile in ACTIONS. Ungueltige Eingaben und nicht
schreibbare Zustandsdateien werden als ValueError geworfen (400).
"""
import copy
import json
import os
import tempfile
import threading
import time
from collections import deque
from pathlib import Path

from console_system_ui import system_html
from fake_console import Log, Tab
from fake_journal_helper import (ACTION_RESULTS, DEFAULT_STATE, EMPTY_KEYS, HELPER_MODES, JOURNAL_MODES,
                                 KEYS, journal_mode, load_state)

ACTIONS = ("journal", "inject", "helper", "actions")
ACTION_LABELS = {"read-journal": "Log lesen", "restart-dashboard": "Dashboard-Neustart", "reboot": "Neustart",
                 "poweroff": "Ausschalten", "apply-bridge-config": "Bridge-Konfiguration anwenden",
                 "apply-app-config": "App-Konfiguration anwenden", "restart-mosquitto": "Mosquitto-Neustart",
                 "tailscale-up": "Tailscale anmelden", "tailscale-logout": "Tailscale abmelden",
                 "tailscale-restart": "Tailscale-Neustart"}
RESULT_LABELS = {"ok": "Erfolg", "fail": "Fehler", "denied": "verweigert", "hang": "hängt"}
MAX_MESSAGE = 500
PRIORITIES = {"error": 3, "info": 6}
HELPER_TEXT = {"denied": "Helper verweigert", "hang": "Helper hängt", "fail": "Helper fehlerhaft"}
RECENT_SIZE = 5


def _choice(body: dict, name: str, options) -> str:
    value = body.get(name)
    if not isinstance(value, str) or value not in options:
        raise ValueError(f"{name} ist ungültig")
    return value


def _key(body: dict) -> str:
    return _choice(body, "key", KEYS)


def _default_mode(key: str) -> str:
    return "empty" if key in EMPTY_KEYS else "normal"


class SystemTab(Tab):
    name = "system"
    title = "System"
    sources = ("system",)
    html = system_html(RESULT_LABELS)

    def __init__(self, log: Log, state_path: Path, events_path: Path, epoch_path: Path) -> None:
        self._log = log
        self._state_path = Path(state_path)
        self._events_path = Path(events_path)
        self._epoch_path = Path(epoch_path)
        self._offset = 0
        self._recent: deque = deque(maxlen=RECENT_SIZE)
        self._lock = threading.Lock()
        threading.Thread(target=self._pump_loop, daemon=True).start()

    def _pump_loop(self) -> None:
        while True:
            try:
                self.pump_events()
            except OSError:
                pass
            time.sleep(1)

    def _write(self, state: dict) -> None:
        """Schreibt atomar: temporaere Datei im selben Ordner, dann os.replace."""
        tmp = None
        try:
            handle, tmp = tempfile.mkstemp(dir=self._state_path.parent, prefix=".system-state-", suffix=".tmp")
            with os.fdopen(handle, "w", encoding="utf-8") as out:
                json.dump(state, out, ensure_ascii=False)
            os.replace(tmp, self._state_path)
        except OSError:
            if tmp is not None:
                try:
                    os.unlink(tmp)
                except OSError:
                    pass
            raise ValueError("Zustandsdatei nicht schreibbar") from None

    def _epoch(self) -> float:
        try:
            return float(self._epoch_path.read_text())
        except (OSError, ValueError):
            raise ValueError("Journal-Epoche fehlt") from None

    def act(self, action: str, body: dict) -> dict:
        if action not in ACTIONS:
            raise ValueError(f"unbekannte Aktion {action}")
        if not isinstance(body, dict):
            raise ValueError("ungültiger Body")
        with self._lock:
            state = load_state(self._state_path)
            if action == "journal":
                key = _key(body)
                mode = _choice(body, "mode", JOURNAL_MODES)
                state["journal"][key] = mode
                text = f"Journal-Modus {key}: {mode}"
            elif action == "inject":
                key = _key(body)
                message = body.get("message")
                if not isinstance(message, str) or not message.strip():
                    raise ValueError("Nachricht fehlt")
                if len(message) > MAX_MESSAGE:
                    raise ValueError(f"Nachricht ist länger als {MAX_MESSAGE} Zeichen")
                priority = _choice(body, "priority", PRIORITIES)
                index = int(time.time() - self._epoch())
                taken = {item.get("index") for item in state["inject"]
                         if isinstance(item, dict) and item.get("key") == key}
                while index in taken:
                    index += 1
                state["inject"].append({"key": key, "index": index, "message": message,
                                        "priority": PRIORITIES[priority]})
                text = f"Zeile in {key} eingefügt"
            elif action == "helper":
                mode = _choice(body, "mode", HELPER_MODES)
                state["helper"] = mode
                text = f"Helper-Antwort {mode}"
            else:
                result = _choice(body, "result", ACTION_RESULTS)
                state["actions"] = result
                text = f"Antwort auf Systemaktionen {result}"
            self._write(state)
            self._log.add("system", text)
        return self.state()

    def pump_events(self) -> None:
        """Liest neue vollstaendige Zeilen der Ereignisdatei und protokolliert sie."""
        with self._lock:
            try:
                size = self._events_path.stat().st_size
                if size < self._offset:
                    self._offset = 0
                with open(self._events_path, "rb") as handle:
                    handle.seek(self._offset)
                    data = handle.read()
            except OSError:
                return
            end = data.rfind(b"\n")
            if end < 0:
                return
            chunk = data[:end + 1]
            self._offset += len(chunk)
            for raw in chunk.decode("utf-8", errors="replace").splitlines():
                self._handle_event(raw)

    def _handle_event(self, raw: str) -> None:
        try:
            event = json.loads(raw)
        except ValueError:
            return
        if not isinstance(event, dict):
            return
        action = event.get("action")
        result = event.get("result")
        if not isinstance(action, str) or not isinstance(result, str):
            return
        if action not in ACTION_LABELS or result not in RESULT_LABELS:
            return
        self._log.add("system", f"{ACTION_LABELS[action]}, {RESULT_LABELS[result]}",
                      "info" if result == "ok" else "warn")
        self._recent.append({"label": ACTION_LABELS[action], "result": result, "t": event.get("t")})

    def state(self) -> dict:
        state = load_state(self._state_path)
        with self._lock:
            recent = [dict(item) for item in reversed(self._recent)]
        return {
            "journal": {key: journal_mode(state, key) for key in sorted(KEYS)},
            "helper": state["helper"],
            "actions": state["actions"],
            "injected": len(state["inject"]),
            "recent": recent,
        }

    def reset(self) -> None:
        with self._lock:
            self._write(copy.deepcopy(DEFAULT_STATE))

    def summary(self) -> dict:
        state = load_state(self._state_path)
        parts = []
        if state["helper"] != "normal":
            parts.append(HELPER_TEXT.get(state["helper"], "Helper fehlerhaft"))
        changed = [key for key in KEYS if journal_mode(state, key) != _default_mode(key)]
        if len(changed) == 1:
            parts.append("1 Dienst verändert")
        elif changed:
            parts.append(f"{len(changed)} Dienste verändert")
        injected = len(state["inject"])
        if injected == 1:
            parts.append("1 eingefügte Zeile")
        elif injected:
            parts.append(f"{injected} eingefügte Zeilen")
        if state["actions"] != "ok":
            parts.append("Aktionen schlagen fehl")
        if not parts:
            return {"level": "ok", "text": "Logs normal"}
        return {"level": "changed", "text": ", ".join(parts)}
