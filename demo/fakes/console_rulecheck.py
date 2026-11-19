"""Reiter Regelprüfung der Fake-Konsole.

Fuegt Beispielregeln in die Regeldatei der Demo ein, damit jede Meldung der
Regelpruefung auf Knopfdruck erscheint. Die Regeln tragen die Kennung chk_,
Entfernen und Zuruecksetzen loeschen nur diese. Die Oberflaeche steht in
console_rulecheck_ui.py.

Jede Aktion ist eine Zeile in ACTIONS. Eine fehlende oder unlesbare Datei
und unbekannte Szenarien werden als ValueError geworfen (400).

Die Demo hat keinen Automationsdienst. Damit der Befund "Löst nie aus" und
"Löst sehr oft aus" entstehen kann, veröffentlicht der Reiter den Zustand des
Dienstes selbst, mit den Auslösungen je Regel. Der Broker kommt aus --broker-control.
"""
import copy
import json
import os
import tempfile
import threading
from pathlib import Path

from console_remote import RemoteFake
from console_rulecheck_ui import RULECHECK_HTML
from fake_console import Log, Tab

ACTIONS = ("insert", "remove", "insert_all", "remove_all", "trigger")
PREFIX = "chk_"
UNREADABLE = "Regeldatei nicht lesbar"
STATE_TOPIC = "outstation/automation/state"
STATIC_STATE = {"rules_enabled": 3, "online": True}
TRIGGER_DEFAULT = 4
TRIGGER_MAX = 50


def _balance(field: str, comparison: str, threshold: float, hysteresis: float = 100,
             hold_seconds: float = 30) -> dict:
    return {"type": "balance_threshold", "field": field, "comparison": comparison,
            "threshold": threshold, "hysteresis": hysteresis, "hold_seconds": hold_seconds}


def _publish(topic: str, payload: str) -> dict:
    return {"type": "publish", "topic": topic, "payload_source": "constant", "payload": payload}


def _rule(rule_id: str, name: str, conditions: list, actions: list, **extra) -> dict:
    # 60 s Abkuehlzeit, die Oberflaeche verlangt mindestens publish_min_interval_s (30 s).
    rule = {"id": rule_id, "name": name, "history_enabled": True, "enabled": True, "cooldown_seconds": 60}
    rule.update(extra)
    rule["conditions"] = conditions
    rule["actions"] = actions
    return rule


SCENARIOS = [
    {
        "id": "duplicate",
        "title": "Doppelte Regel",
        "expect": "Doppelt mit ...",
        "note": "Zwei Regeln mit gleichen Bedingungen und gleicher Aktion.",
        "rules": [
            _rule("chk_dup_a", "Prüfung Doppelt A",
                  [_balance("grid_export", "above", 800)],
                  [_publish("demo/check/dup", "1")]),
            _rule("chk_dup_b", "Prüfung Doppelt B",
                  [_balance("grid_export", "above", 800)],
                  [_publish("demo/check/dup", "1")]),
        ],
    },
    {
        "id": "contradiction",
        "title": "Widersprüchliche Regeln",
        "expect": "Widerspricht ...",
        "note": "Zwei Regeln schalten dasselbe Topic mit entgegengesetzten Werten.",
        "rules": [
            _rule("chk_contra_on", "Prüfung Widerspruch Ein",
                  [_balance("pv", "above", 2000)],
                  [_publish("demo/check/contra", "on")]),
            _rule("chk_contra_off", "Prüfung Widerspruch Aus",
                  [_balance("pv", "above", 2000)],
                  [_publish("demo/check/contra", "off")]),
        ],
    },
    {
        "id": "regulator_conflict",
        "title": "Konflikt mit Regler",
        "expect": "Konkurriert mit einem Regler",
        "note": "Die Regel setzt die Leistungsgrenze, die der Regler der Nulleinspeisung führt.",
        "rules": [
            _rule("chk_regler", "Prüfung Regler",
                  [_balance("load_total", "above", 3000)],
                  [_publish("outstation/apsystems_dach/max_power_limit_w/set", "400")]),
        ],
    },
    {
        "id": "unsatisfiable",
        "title": "Nie erfüllbar",
        "expect": "Kann nie erfüllt sein",
        "note": "Netzbezug muss über 900 und unter 100 liegen, das geht nie zugleich.",
        "rules": [
            _rule("chk_unmoeglich", "Prüfung Unmöglich",
                  [_balance("grid_import", "above", 900, hysteresis=0, hold_seconds=30),
                   _balance("grid_import", "below", 100, hysteresis=0, hold_seconds=30)],
                  [_publish("demo/check/unmoeglich", "1")]),
        ],
    },
    {
        "id": "orphan",
        "title": "Gerät fehlt",
        "expect": "Gerät fehlt",
        "note": "Die Bedingung nennt ein Gerät, das es in der Demo nicht gibt.",
        "rules": [
            _rule("chk_geisterhaft", "Prüfung Geisterhaft",
                  [{"type": "entity_value", "entity_id": "chk_gibt_es_nicht",
                    "topic": "demo/check/geisterhaft", "comparison": "below",
                    "value": 20, "hold_seconds": 30}],
                  [_publish("demo/check/orphan", "1")]),
        ],
    },
    {
        "id": "never_fired",
        "title": "Löst nie aus",
        "expect": "Löst nie aus",
        "note": "Die Konsole meldet die Regel mit 0 Auslösungen. Der Befund erscheint nach etwa 10 Sekunden, Seite neu laden, weil die Schwelle nie erreicht wird.",
        "rules": [
            _rule("chk_nie", "Prüfung Nie",
                  [_balance("grid_export", "above", 99999, hysteresis=0, hold_seconds=30)],
                  [_publish("demo/check/nie", "1")]),
        ],
    },
    {
        "id": "frequent",
        "title": "Löst sehr oft aus",
        "expect": "Löst sehr oft aus",
        "note": "Der Knopf meldet dem Dashboard 4 Auslösungen auf einmal (mit dem Fast-Modus der Demo genügen 3 pro Stunde). Der Befund erscheint nach etwa 10 Sekunden, Seite neu laden.",
        "triggerable": True,
        "rules": [
            _rule("chk_pendeln", "Prüfung Pendeln",
                  [{"type": "topic_value", "topic": "demo/check/pendeln", "comparison": "above",
                    "value": 50, "hysteresis": 0, "hold_seconds": 30}],
                  [_publish("demo/check/pendeln_out", "1")],
                  cooldown_seconds=30),
        ],
    },
]
BY_ID = {scenario["id"]: scenario for scenario in SCENARIOS}


def _load(path: Path) -> dict:
    try:
        data = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, ValueError):
        raise ValueError(UNREADABLE) from None
    if not isinstance(data, dict) or not isinstance(data.get("rules"), list):
        raise ValueError(UNREADABLE)
    return data


def _put(rules: list, rule: dict) -> None:
    """Ersetzt eine Regel mit gleicher Kennung an ihrer Stelle, sonst haengt sie an."""
    for index, existing in enumerate(rules):
        if existing.get("id") == rule["id"]:
            rules[index] = copy.deepcopy(rule)
            return
    rules.append(copy.deepcopy(rule))


class RuleCheckTab(Tab):
    name = "rulecheck"
    title = "Regelprüfung"
    html = RULECHECK_HTML

    def __init__(self, log: Log, rules_path: Path, broker_control: str = "") -> None:
        self._log = log
        self._path = Path(rules_path)
        self._lock = threading.Lock()
        # Ohne Steuer-URL des Brokers wird nichts veroeffentlicht
        self._remote = RemoteFake(broker_control, "regelpruefung", log) if broker_control else None
        self._counts: dict[str, int] = {}

    def _post_state(self, payload: dict) -> None:
        """Veroeffentlicht den Zustand retained. Fehler gehen ins Protokoll, nie als Ausnahme."""
        if self._remote is None:
            return
        try:
            self._remote.post("publish", {"topic": STATE_TOPIC, "payload": json.dumps(payload),
                                          "retain": True})
        except (OSError, ValueError) as exc:
            self._log.add("regelpruefung", f"Zustand des Automationsdienstes nicht veröffentlicht: {exc}",
                          "error")

    def _publish_state(self) -> None:
        """Zustand aus der Regeldatei und den Auslösungen. Aufrufer halten den Lock."""
        if self._remote is None:
            return
        try:
            rules = _load(self._path)["rules"]
        except ValueError as exc:
            self._log.add("regelpruefung", f"Zustand des Automationsdienstes nicht veröffentlicht: {exc}",
                          "error")
            return
        self._post_state({
            "rules_enabled": sum(1 for rule in rules if rule.get("enabled")),
            "online": True,
            "rules": {rule["id"]: {"trigger_count": self._counts.get(rule["id"], 0)}
                      for rule in rules if "id" in rule},
        })

    def _trigger(self, body: dict) -> str:
        scenario = BY_ID.get(body.get("scenario"))
        if scenario is None or not scenario.get("triggerable"):
            raise ValueError("Szenario ist ungültig")
        times = body.get("times", TRIGGER_DEFAULT)
        if type(times) is not int or not 1 <= times <= TRIGGER_MAX:
            raise ValueError("Anzahl ist ungültig")
        for rule in scenario["rules"]:
            self._counts[rule["id"]] = self._counts.get(rule["id"], 0) + times
        return f"{times} Auslösungen gemeldet: {scenario['title']}"

    def _write(self, data: dict) -> None:
        """Schreibt atomar: temporaere Datei im selben Ordner, dann os.replace."""
        tmp = None
        try:
            handle, tmp = tempfile.mkstemp(dir=self._path.parent, prefix=".automation-rules-", suffix=".tmp")
            with os.fdopen(handle, "w", encoding="utf-8") as out:
                json.dump(data, out, indent=2, ensure_ascii=False)
            os.replace(tmp, self._path)
        except OSError:
            if tmp is not None:
                try:
                    os.unlink(tmp)
                except OSError:
                    pass
            raise ValueError("Regeldatei nicht schreibbar") from None

    def _inserted(self, rules: list, scenario: dict) -> bool:
        present = {rule.get("id") for rule in rules}
        return all(rule["id"] in present for rule in scenario["rules"])

    def act(self, action: str, body: dict) -> list:
        if action not in ACTIONS:
            raise ValueError(f"unbekannte Aktion {action}")
        if not isinstance(body, dict):
            raise ValueError("ungültiger Body")
        with self._lock:
            data = _load(self._path)
            rules = data["rules"]
            if action in ("insert", "remove"):
                scenario = BY_ID.get(body.get("scenario"))
                if scenario is None:
                    raise ValueError("Szenario ist ungültig")
                if action == "insert":
                    for rule in scenario["rules"]:
                        _put(rules, rule)
                    text = f"Beispiel eingefügt: {scenario['title']}"
                else:
                    ids = {rule["id"] for rule in scenario["rules"]}
                    rules[:] = [rule for rule in rules if rule.get("id") not in ids]
                    text = f"Beispiel entfernt: {scenario['title']}"
            elif action == "insert_all":
                for scenario in SCENARIOS:
                    for rule in scenario["rules"]:
                        _put(rules, rule)
                text = "Alle Beispiele eingefügt"
            elif action == "trigger":
                text = self._trigger(body)
            else:
                rules[:] = [rule for rule in rules if not str(rule.get("id", "")).startswith(PREFIX)]
                text = "Alle Beispiele entfernt"
            if action != "trigger":
                self._write(data)
            self._publish_state()
            self._log.add("regelpruefung", text)
        return self.state()

    def state(self) -> list:
        with self._lock:
            rules = _load(self._path)["rules"]
            return [{"id": s["id"], "title": s["title"], "expect": s["expect"], "note": s["note"],
                     "inserted": self._inserted(rules, s),
                     "triggerable": bool(s.get("triggerable"))} for s in SCENARIOS]

    def summary(self) -> dict:
        try:
            state = self.state()
        except ValueError:
            return {"level": "error", "text": UNREADABLE}
        count = sum(1 for s in state if s["inserted"])
        if count:
            return {"level": "changed", "text": f"{count} von {len(state)} eingefügt"}
        return {"level": "ok", "text": "keine Beispiele eingefügt"}

    def reset(self) -> None:
        with self._lock:
            data = _load(self._path)
            data["rules"] = [rule for rule in data["rules"]
                             if not str(rule.get("id", "")).startswith(PREFIX)]
            self._write(data)
            self._counts.clear()
            self._post_state(STATIC_STATE)
