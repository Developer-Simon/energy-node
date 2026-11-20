"""Reiter Auto-Lernfunktionen der Fake-Konsole.

Abschnitt Regelprüfung: fuegt Beispielregeln in die Regeldatei der Demo ein,
damit jede Meldung der Regelpruefung auf Knopfdruck erscheint. Die Regeln
tragen die Kennung chk_, Entfernen und Zuruecksetzen loeschen nur diese.

Abschnitt Lerndaten: erzeugt Tagesprofile unter <Datenordner>/autolearn/days,
Version 1 im Format des Dashboards (gzip-JSON). Jede Datei traegt die Angabe
"fake": true, Entfernen loescht nur solche Dateien. Die Quittung "neu aktiv"
setzt der Reiter in settings.json auf [] zurueck, die anderen Felder bleiben.

Die Oberflaeche steht in console_autolearn_ui.py. Jede Aktion ist eine Zeile in
ACTIONS. Eine fehlende oder unlesbare Datei, ein fehlender oder unlesbarer
Ordner und unbekannte Szenarien werden als ValueError geworfen (400).

Die Demo hat keinen Automationsdienst. Damit der Befund "Löst nie aus" und
"Löst sehr oft aus" entstehen kann, veröffentlicht der Reiter den Zustand des
Dienstes selbst, mit den Auslösungen je Regel. Der Broker kommt aus --broker-control.
"""
import copy
import gzip
import json
import math
import os
import random
import re
import tempfile
import threading
import zlib
from datetime import date, datetime, timedelta
from pathlib import Path

from console_remote import RemoteFake
from console_autolearn_ui import AUTOLEARN_HTML
from fake_console import Log, Tab

ACTIONS = ("insert", "remove", "insert_all", "remove_all", "trigger",
           "day_create", "day_remove", "ack_reset")
LEARNING_ACTIONS = ("day_create", "day_remove", "ack_reset")
SOURCE = "autolearn"
PREFIX = "chk_"
UNREADABLE = "Regeldatei nicht lesbar"
RULES_MISSING = "Regeldatei nicht angegeben, die Demo startet die Konsole mit --automation-rules"
RULES_UNWRITABLE = "Regeldatei nicht schreibbar"
DATA_MISSING = "Datenordner nicht angegeben, die Demo startet die Konsole mit --data-dir"
DATA_UNREADABLE = "Datenordner nicht lesbar"
DAYS_UNWRITABLE = "Tagesprofile nicht schreibbar"
DAY_UNREMOVABLE = "Tagesprofil nicht löschbar"
SETTINGS_NAME = "settings.json"
SETTINGS_UNREADABLE = "Einstellungen nicht lesbar"
SETTINGS_UNWRITABLE = "Einstellungen nicht schreibbar"
ACK_TEXT = "Quittung in settings.json geleert. Das Dashboard liest die Datei erst nach einem Neustart, dann erscheint der Hinweis wieder"
STATE_TOPIC = "outstation/automation/state"
STATIC_STATE = {"rules_enabled": 3, "online": True}
TRIGGER_DEFAULT = 4
TRIGGER_MAX = 50

DAYS_DEFAULT = 14
DAYS_MIN = 1
DAYS_MAX = 56
DAY_FILE_VERSION = 1
DAY_DIR = ("autolearn", "days")
DAY_NAME = re.compile(r"\d{4}-\d{2}-\d{2}\.json\.gz")
FAKE_KEY = "fake"
QUARTERS = 96
MINUTES = 1440
STANDBY_W = 2.0
ORIGIN_HAND = "hand"
ORIGIN_RULE = "rule"
ORIGIN_EXT = "ext"
RULE_SURPLUS = "regel_ueberschuss_wallbox"
RULE_NIGHT = "nulleinspeisung"


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


def _dump(data: dict) -> bytes:
    return (json.dumps(data, indent=2, ensure_ascii=False) + "\n").encode("utf-8")


def _write_atomic(path: Path, raw: bytes, failure: str) -> None:
    """Schreibt ueber eine temporaere Datei im selben Ordner und ersetzt dann die Zieldatei."""
    tmp = None
    try:
        handle, tmp = tempfile.mkstemp(dir=path.parent, prefix=f".{path.stem}-", suffix=".tmp")
        with os.fdopen(handle, "wb") as out:
            out.write(raw)
        os.replace(tmp, path)
    except OSError:
        if tmp is not None:
            try:
                os.unlink(tmp)
            except OSError:
                pass
        raise ValueError(failure) from None


def _run(start: int, length: int, power: float, origin: str, rule: str = "") -> dict:
    return {"start": start, "end": start + length, "power": power, "origin": origin, "rule": rule}


def _schedules(rng: random.Random) -> dict:
    """Schaltlaeufe je Geraet, Minuten seit Mitternacht. Die Laeufe eines Geraets ueberlappen nicht."""
    pump = [_run(hour * 60 + rng.randint(0, 40), 10, 350, ORIGIN_EXT)
            for hour in range(6, 23, 3) if rng.random() < 0.8]
    return {
        "demo_heater": [
            _run(rng.randint(380, 420), 40, rng.uniform(1700, 1900), ORIGIN_HAND),
            _run(rng.randint(1020, 1080), rng.randint(120, 200), rng.uniform(1700, 1900),
                 ORIGIN_RULE, RULE_SURPLUS),
        ],
        "demo_boiler": [
            _run(rng.randint(60, 90), 120, 2000, ORIGIN_RULE, RULE_NIGHT),
            _run(rng.randint(720, 780), 60, 2000, ORIGIN_RULE, RULE_NIGHT),
        ],
        "demo_washer": [_run(rng.randint(540, 1000), 110, rng.uniform(450, 650), ORIGIN_HAND)],
        "demo_pump": pump,
    }


def _day_profile(day: date) -> dict:
    """Tagesprofil im Format Version 1. Der Zufall ist je Tag fest, derselbe Tag ergibt dieselben Werte."""
    rng = random.Random(day.isoformat())
    midnight = int(datetime(day.year, day.month, day.day).timestamp())
    schedules = _schedules(rng)
    cloud = rng.uniform(0.55, 1.0)

    power: dict = {}
    on: dict = {}
    for device, runs in schedules.items():
        watts = [STANDBY_W] * MINUTES
        switched = [0] * MINUTES
        for run in runs:
            for minute in range(run["start"], min(run["end"], MINUTES)):
                watts[minute] = run["power"] * rng.uniform(0.97, 1.03)
                switched[minute] = 1
        power[device] = watts
        on[device] = switched

    load = []
    pv = []
    for minute in range(MINUTES):
        base = 220 + (200 if 420 <= minute < 480 or 1080 <= minute < 1260 else 0) + rng.uniform(0, 60)
        load.append(base + sum(power[device][minute] for device in schedules))
        pv.append(3600 * cloud * math.sin(math.pi * (minute - 360) / 840)
                  if 360 <= minute < 1200 else 0.0)
    grid_import = [max(0.0, l - p) for l, p in zip(load, pv)]
    grid_export = [max(0.0, p - l) for l, p in zip(load, pv)]

    def quarters(series: list) -> list:
        return [round(sum(series[q * 15:(q + 1) * 15]) / 15, 1) for q in range(QUARTERS)]

    load_q = quarters(load)
    pv_q = quarters(pv)

    devices = {}
    for device in schedules:
        devices[device] = {
            "seen": [15] * QUARTERS,
            "reach": [15] * QUARTERS,
            "pn": [15] * QUARTERS,
            "on_min": [sum(on[device][q * 15:(q + 1) * 15]) for q in range(QUARTERS)],
            "avg_w": quarters(power[device]),
            "wh": [round(sum(power[device][q * 15:(q + 1) * 15]) / 60, 1) for q in range(QUARTERS)],
            "hand": [0] * QUARTERS,
            "ext": [0] * QUARTERS,
            "rule": [0] * QUARTERS,
        }

    events = []
    actions = []
    for device, runs in schedules.items():
        for run in runs:
            for minute, switched_on in ((run["start"], True), (run["end"], False)):
                if minute >= MINUTES:
                    continue
                q = minute // 15
                at = midnight + minute * 60
                entity = f"{device}/switch"
                events.append({"at": at, "d": device, "e": entity, "on": switched_on,
                               "o": run["origin"], "r": run["rule"],
                               "house": round(load_q[q]), "surplus": round(pv_q[q] - load_q[q])})
                counter = devices[device][run["origin"]]
                counter[q] = min(15, counter[q] + 1)
                if run["origin"] == ORIGIN_RULE:
                    actions.append({"at": at, "r": run["rule"], "d": device, "e": entity,
                                    "want": switched_on, "there": False, "ok": True, "rt": 1.2})
    events.sort(key=lambda event: event["at"])
    actions.sort(key=lambda action: action["at"])

    return {
        FAKE_KEY: True,
        "v": DAY_FILE_VERSION,
        "day": day.isoformat(),
        "devices": devices,
        "site": {
            "seen": [15] * QUARTERS,
            "load_total": load_q,
            "grid_import": quarters(grid_import),
            "grid_export": quarters(grid_export),
            "pv": pv_q,
            "battery_charge": [0.0] * QUARTERS,
            "battery_discharge": [0.0] * QUARTERS,
        },
        "events": events,
        "actions": actions,
    }


def _encode(data: dict) -> bytes:
    return gzip.compress(json.dumps(data, separators=(",", ":"), ensure_ascii=False).encode("utf-8"))


def _read_day(path: Path):
    """Inhalt einer Tagesdatei, None wenn sie nicht gelesen werden kann."""
    try:
        data = json.loads(gzip.decompress(path.read_bytes()).decode("utf-8"))
    except (OSError, EOFError, zlib.error, ValueError):
        return None
    return data if isinstance(data, dict) else None


def _is_fake(path: Path) -> bool:
    data = _read_day(path)
    return data is not None and data.get(FAKE_KEY) is True


def _days_arg(body: dict) -> int:
    days = body.get("days", DAYS_DEFAULT)
    if type(days) is not int or not DAYS_MIN <= days <= DAYS_MAX:
        raise ValueError("Anzahl der Tage ist ungültig")
    return days


class AutolearnTab(Tab):
    name = "autolearn"
    title = "Auto-Lernfunktionen"
    html = AUTOLEARN_HTML

    def __init__(self, log: Log, rules_path: Path | None, broker_control: str = "",
                 data_dir: Path | None = None) -> None:
        self._log = log
        self._path = Path(rules_path) if rules_path else None
        self._data_path = Path(data_dir) if data_dir else None
        self._lock = threading.Lock()
        # Ohne Steuer-URL des Brokers wird nichts veroeffentlicht
        self._remote = RemoteFake(broker_control, "regelpruefung", log) if broker_control else None
        self._counts: dict[str, int] = {}

    # Regeldatei

    def _rules(self) -> dict:
        if self._path is None:
            raise ValueError(RULES_MISSING)
        return _load(self._path)

    def _write(self, data: dict) -> None:
        _write_atomic(self._path, _dump(data), RULES_UNWRITABLE)

    def _post_state(self, payload: dict) -> None:
        """Veroeffentlicht den Zustand retained. Fehler gehen ins Protokoll, nie als Ausnahme."""
        if self._remote is None:
            return
        try:
            self._remote.post("publish", {"topic": STATE_TOPIC, "payload": json.dumps(payload),
                                          "retain": True})
        except (OSError, ValueError) as exc:
            self._log.add(SOURCE, f"Zustand des Automationsdienstes nicht veröffentlicht: {exc}",
                          "error")

    def _publish_state(self) -> None:
        """Zustand aus der Regeldatei und den Auslösungen. Aufrufer halten den Lock."""
        if self._remote is None or self._path is None:
            return
        try:
            rules = self._rules()["rules"]
        except ValueError as exc:
            self._log.add(SOURCE, f"Zustand des Automationsdienstes nicht veröffentlicht: {exc}",
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

    def _scenarios(self, rules: list) -> list:
        present = {rule.get("id") for rule in rules}
        return [{"id": s["id"], "title": s["title"], "expect": s["expect"], "note": s["note"],
                 "inserted": all(rule["id"] in present for rule in s["rules"]),
                 "triggerable": bool(s.get("triggerable"))} for s in SCENARIOS]

    def _rules_or_none(self) -> list:
        """Regeln fuer die Antwort einer Lernaktion. Eine unlesbare Regeldatei zaehlt als leer."""
        with self._lock:
            try:
                return self._rules()["rules"]
            except ValueError:
                return []

    # Lerndaten

    def _data_dir(self) -> Path:
        if self._data_path is None:
            raise ValueError(DATA_MISSING)
        if not (self._data_path.is_dir() and os.access(self._data_path, os.R_OK | os.X_OK)):
            raise ValueError(DATA_UNREADABLE)
        return self._data_path

    def _days_dir(self, create: bool = False) -> Path:
        folder = self._data_dir().joinpath(*DAY_DIR)
        if create:
            try:
                folder.mkdir(parents=True, exist_ok=True)
            except OSError:
                raise ValueError(DAYS_UNWRITABLE) from None
        return folder

    def _day_create(self, body: dict) -> str:
        days = _days_arg(body)
        folder = self._days_dir(create=True)
        today = date.today()
        created = skipped = 0
        for offset in range(1, days + 1):
            day = today - timedelta(days=offset)
            path = folder / f"{day.isoformat()}.json.gz"
            if path.exists() and not _is_fake(path):
                skipped += 1
                continue
            _write_atomic(path, _encode(_day_profile(day)), DAYS_UNWRITABLE)
            created += 1
        text = f"{created} Tagesprofile erzeugt"
        if skipped:
            text += f", {skipped} vorhandene Datei ohne Kennzeichnung übersprungen"
        return text

    def _day_remove(self, body: dict) -> str:
        folder = self._days_dir()
        removed = kept = 0
        if folder.is_dir():
            for path in sorted(folder.iterdir()):
                if not DAY_NAME.fullmatch(path.name):
                    continue
                if not _is_fake(path):
                    kept += 1
                    continue
                try:
                    path.unlink()
                except OSError:
                    raise ValueError(DAY_UNREMOVABLE) from None
                removed += 1
        text = f"{removed} Tagesprofile entfernt"
        if kept:
            text += f", {kept} ohne Kennzeichnung bleiben"
        return text

    def _ack_reset(self, body: dict) -> str:
        path = self._data_dir() / SETTINGS_NAME
        try:
            data = json.loads(path.read_text(encoding="utf-8"))
        except (OSError, ValueError):
            raise ValueError(SETTINGS_UNREADABLE) from None
        if not isinstance(data, dict):
            raise ValueError(SETTINGS_UNREADABLE)
        data["learning_acknowledged"] = []
        _write_atomic(path, _dump(data), SETTINGS_UNWRITABLE)
        return ACK_TEXT

    # Aktionen

    def act(self, action: str, body: dict) -> list:
        if action not in ACTIONS:
            raise ValueError(f"unbekannte Aktion {action}")
        if not isinstance(body, dict):
            raise ValueError("ungültiger Body")
        if action in LEARNING_ACTIONS:
            text = {"day_create": self._day_create, "day_remove": self._day_remove,
                    "ack_reset": self._ack_reset}[action](body)
            self._log.add(SOURCE, text)
            return self._scenarios(self._rules_or_none())
        with self._lock:
            data = self._rules()
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
            self._log.add(SOURCE, text)
        return self.state()

    def state(self) -> list:
        with self._lock:
            return self._scenarios(self._rules()["rules"])

    def summary(self) -> dict:
        try:
            state = self.state()
        except ValueError as exc:
            return {"level": "error", "text": str(exc)}
        count = sum(1 for s in state if s["inserted"])
        if count:
            return {"level": "changed", "text": f"{count} von {len(state)} eingefügt"}
        return {"level": "ok", "text": "keine Beispiele eingefügt"}

    def reset(self) -> None:
        with self._lock:
            if self._path is not None:
                data = self._rules()
                data["rules"] = [rule for rule in data["rules"]
                                 if not str(rule.get("id", "")).startswith(PREFIX)]
                self._write(data)
            self._counts.clear()
            self._post_state(STATIC_STATE)
