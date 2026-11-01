"""Reiter MQTT-Broker der Fake-Konsole.

Spricht mit dem Steuerport von minibroker.py (RemoteFake) und uebernimmt dessen
Protokoll ins Konsolen-Log. Jede Aktion ist ein Eintrag in ACTIONS und wird als
POST /<aktion> an den Broker weitergereicht. Die Oberflaeche steht in
console_mqtt_ui.py.
"""

from console_mqtt_ui import MQTT_HTML
from console_remote import RemoteFake
from fake_console import Log, Tab

ACTIONS = ("simulate", "device", "service", "publish", "disconnect", "login")
UNREACHABLE = "MQTT-Broker nicht erreichbar"


def _count(n, one, many):
    return f"1 {one}" if n == 1 else f"{n} {many}"


class MqttTab(Tab):
    name = "mqtt"
    title = "MQTT-Broker"
    html = MQTT_HTML
    sources = ("mqtt",)

    def __init__(self, log: Log, control_url: str) -> None:
        self._remote = RemoteFake(control_url, "mqtt", log)
        self._remote.start()

    def state(self) -> dict:
        try:
            data = self._remote.get("state")
        except OSError:
            return {"reachable": False}
        return {**data, "reachable": True}

    def act(self, action: str, body: dict) -> dict:
        if action not in ACTIONS:
            raise ValueError(f"unbekannte Aktion {action}")
        try:
            data = self._remote.post(action, body)
        except OSError:
            raise ValueError(UNREACHABLE) from None
        return {**data, "reachable": True}

    def reset(self) -> None:
        try:
            self._remote.post("reset", {})
        except OSError:
            # Ein nicht erreichbarer Broker darf den Reset der anderen Reiter nicht abbrechen
            pass

    def summary(self) -> dict:
        try:
            state = self._remote.get("state")
        except OSError:
            return {"level": "error", "text": "nicht erreichbar"}
        offline = [device for device in state["devices"] if not device["online"]]
        deviating = [service for service in state["services"] if service["mode"] != service["initial_mode"]]
        rejected = sum(1 for service in deviating if service["mode"] == "reject")
        silent = sum(1 for service in deviating if service["mode"] == "silent")
        simulating = state["simulate"] != state["initial_simulate"]
        login = state["login"]
        login_changed = (login["required"], login["user"]) != (login["initial_required"], login["initial_user"])
        changed = bool(offline or deviating or simulating or login_changed)

        parts = []
        if rejected:
            parts.append(_count(rejected, "Dienst abgelehnt", "Dienste abgelehnt"))
        if silent:
            parts.append(_count(silent, "Dienst schweigt", "Dienste schweigen"))
        if offline:
            parts.append(_count(len(offline), "Gerät offline", "Geräte offline"))
        if login_changed:
            parts.append("Anmeldung verlangt" if login["required"] else "Anmeldung offen")
        if not parts:
            parts.append("Simulation läuft" if state["simulate"] else "Simulation aus")
        return {"level": "changed" if changed else "ok", "text": ", ".join(parts)}
