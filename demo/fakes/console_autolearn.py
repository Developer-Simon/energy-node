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

DAYS_DEFAULT = 21
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
# Geraete der Demo mit ihren Schaltern, wie die Registry sie hat (Discovery der Demo-Fixture).
DEMO_ENTITIES = {
    "plug_balkon": "plug_balkon_steckdose",
    "plug_entfeuchter": "plug_entfeuchter_steckdose",
    "relais_keller": "relais_keller_switch_1",
    "waermepumpe": "waermepumpe_mode",
}
DEMO_WATTS = {"plug_balkon": 60.0, "plug_entfeuchter": 300.0, "relais_keller": 500.0, "waermepumpe": 1500.0}
DRY_LONG_WATTS = 1200.0
LONG_DRY_RESIDUES = (6, 15)          # zwei Tage je 21, an denen der Entfeuchter Stunden laeuft
DEMO_RULE = "regel_akku_leer_wp_aus"  # schaltet die Waermepumpe aus
RELAY_EVENING_MIN = 19 * 60          # Kellerrelais am Abend, Minuten seit Mitternacht
RELAY_JITTER_MIN = 4.0
PLUG_OFF_MIN = 22 * 60 + 30          # Balkonsteckdose um 22:30 von Hand aus
PLUG_JITTER_MIN = 8.0
DAY_SECONDS = 86400


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


def _switch(sec: int, device: str, on: bool, origin: str, rule: str = "") -> dict:
    return {"sec": sec, "d": device, "on": on, "o": origin, "r": rule}


def _sec(minute: float) -> int:
    return int(round(minute * 60))


def _schedules(day: date, rng: random.Random) -> tuple:
    """Schaltvorgaenge des Tages, Sekunden seit Mitternacht, und die Leistung je Geraet.

    Vier Muster, dieselben wie die Gewohnheiten, die der Dashboard-Seed (SeedDemo) schreibt:
    Balkonsteckdose um 22:30 von Hand aus, an 18 von 21 Tagen, nach einem Lauf
    des Kellerrelais. Entfeuchter etwa 40 Minuten, an zwei Tagen Stunden. Wärmepumpe
    von der Regel ausgeschaltet, werktags sofort wieder von Hand ein.
    """
    skip = day.toordinal() % 7 == 3          # drei von 21 Tagen ohne Balkongewohnheit
    long_dry = day.toordinal() % 21 in LONG_DRY_RESIDUES
    weekday = day.weekday() < 5
    switches = []

    # Wärmepumpe: die Regel schaltet sie zwischen 10 und 17 Uhr aus.
    off = _sec(rng.uniform(600, 1020))
    switches.append(_switch(off, "waermepumpe", False, ORIGIN_RULE, DEMO_RULE))
    if weekday:
        switches.append(_switch(off + rng.randint(60, 240), "waermepumpe", True, ORIGIN_HAND))
    else:
        switches.append(_switch(off + rng.randint(150, 180) * 60, "waermepumpe", True, ORIGIN_HAND))

    # Entfeuchter: von Hand gestartet, meist 35 bis 45 Minuten.
    start = _sec(rng.uniform(600, 1020))
    if long_dry:
        run = _sec(rng.uniform(300, 360))
    else:
        run = rng.randint(35, 45) * 60
    switches.append(_switch(start, "plug_entfeuchter", True, ORIGIN_HAND))
    switches.append(_switch(start + run, "plug_entfeuchter", False, ORIGIN_HAND))

    # Kellerrelais von Hand am Abend, die Balkonsteckdose folgt 30 bis 50 Sekunden später.
    # Das Relais geht 1 bis 2,5 Stunden später wieder aus, zu keiner festen Zeit.
    relay = _sec(rng.gauss(RELAY_EVENING_MIN, RELAY_JITTER_MIN))
    switches.append(_switch(relay, "relais_keller", True, ORIGIN_HAND))
    switches.append(_switch(relay + _sec(rng.uniform(60, 150)), "relais_keller", False, ORIGIN_HAND))
    if not skip:
        switches.append(_switch(relay + round(rng.uniform(30, 50)), "plug_balkon", True, ORIGIN_HAND))
        switches.append(_switch(_sec(rng.gauss(PLUG_OFF_MIN, PLUG_JITTER_MIN)), "plug_balkon", False,
                                ORIGIN_HAND))

    switches.sort(key=lambda s: s["sec"])
    watts = dict(DEMO_WATTS)
    if long_dry:
        watts["plug_entfeuchter"] = DRY_LONG_WATTS
    return switches, watts


def _on_minutes(switches: list, device: str) -> list:
    """Minuten mit eingeschaltetem Gerät. Ein Gerät ist am Morgen aus, bis ein Schaltvorgang kommt."""
    changes = [s for s in switches if s["d"] == device]
    on = [0] * MINUTES
    state, index = False, 0
    for minute in range(MINUTES):
        while index < len(changes) and changes[index]["sec"] <= minute * 60:
            state = changes[index]["on"]
            index += 1
        on[minute] = 1 if state else 0
    return on


def _day_profile(day: date) -> dict:
    """Tagesprofil im Format Version 1. Der Zufall ist je Tag fest, derselbe Tag ergibt dieselben Werte."""
    rng = random.Random(day.isoformat())
    midnight = int(datetime(day.year, day.month, day.day).timestamp())
    switches, watts_of = _schedules(day, rng)
    cloud = rng.uniform(0.55, 1.0)

    power: dict = {}
    on: dict = {}
    for device in DEMO_ENTITIES:
        switched = _on_minutes(switches, device)
        watts = [STANDBY_W] * MINUTES
        for minute in range(MINUTES):
            if switched[minute]:
                watts[minute] = watts_of[device] * rng.uniform(0.97, 1.03)
        power[device] = watts
        on[device] = switched

    load = []
    pv = []
    for minute in range(MINUTES):
        base = 220 + (200 if 420 <= minute < 480 or 1080 <= minute < 1260 else 0) + rng.uniform(0, 60)
        load.append(base + sum(power[device][minute] for device in DEMO_ENTITIES))
        pv.append(3600 * cloud * math.sin(math.pi * (minute - 360) / 840)
                  if 360 <= minute < 1200 else 0.0)
    grid_import = [max(0.0, l - p) for l, p in zip(load, pv)]
    grid_export = [max(0.0, p - l) for l, p in zip(load, pv)]

    def quarters(series: list) -> list:
        return [round(sum(series[q * 15:(q + 1) * 15]) / 15, 1) for q in range(QUARTERS)]

    load_q = quarters(load)
    pv_q = quarters(pv)

    devices = {}
    for device in DEMO_ENTITIES:
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
    for switch in switches:
        if switch["sec"] >= DAY_SECONDS:
            continue
        q = min(switch["sec"] // 900, QUARTERS - 1)
        at = midnight + switch["sec"]
        device = switch["d"]
        events.append({"at": at, "d": device, "e": DEMO_ENTITIES[device], "on": switch["on"],
                       "o": switch["o"], "r": switch["r"],
                       "house": round(load_q[q]), "surplus": round(pv_q[q] - load_q[q])})
        counter = devices[device][switch["o"]]
        counter[q] = min(15, counter[q] + 1)
        if switch["o"] == ORIGIN_RULE:
            actions.append({"at": at, "r": switch["r"], "d": device, "e": DEMO_ENTITIES[device],
                            "want": switch["on"], "there": False, "ok": True, "rt": 1.2})
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
