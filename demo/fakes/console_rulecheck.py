"""Reiter Regelprüfung der Fake-Konsole.

Fuegt Beispielregeln in die Regeldatei der Demo ein, damit jede Meldung der
Regelpruefung auf Knopfdruck erscheint. Die Regeln tragen die Kennung chk_,
Entfernen und Zuruecksetzen loeschen nur diese. Die Oberflaeche steht in
console_rulecheck_ui.py.

Jede Aktion ist eine Zeile in ACTIONS. Eine fehlende oder unlesbare Datei
und unbekannte Szenarien werden als ValueError geworfen (400).
"""
import copy
import json
import os
import tempfile
import threading
from pathlib import Path

from console_rulecheck_ui import RULECHECK_HTML
from fake_console import Log, Tab

ACTIONS = ("insert", "remove", "insert_all", "remove_all")
PREFIX = "chk_"
UNREADABLE = "Regeldatei nicht lesbar"


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
        "note": "Die Schwelle liegt so hoch, dass sie nie erreicht wird. Der Hinweis erscheint nach dem ersten Takt der Demo.",
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
        "note": "Löst erst aus, wenn der Wert mehrmals über und unter 50 pendelt. Im Reiter MQTT mehrmals abwechselnd 80 und 20 auf demo/check/pendeln veröffentlichen (mindestens 3 Auslösungen innerhalb einer Stunde).",
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

    def __init__(self, log: Log, rules_path: Path) -> None:
        self._log = log
        self._path = Path(rules_path)
        self._lock = threading.Lock()

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
            else:
                rules[:] = [rule for rule in rules if not str(rule.get("id", "")).startswith(PREFIX)]
                text = "Alle Beispiele entfernt"
            self._write(data)
            self._log.add("regelpruefung", text)
        return self.state()

    def state(self) -> list:
        with self._lock:
            rules = _load(self._path)["rules"]
            return [{"id": s["id"], "title": s["title"], "expect": s["expect"], "note": s["note"],
                     "inserted": self._inserted(rules, s)} for s in SCENARIOS]

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
