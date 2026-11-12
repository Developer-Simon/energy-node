"""Simulierte Netz-Verben von energy-node-dashboard-system-action fuer die Demo.

fake_journal_helper.py reicht network-status, wifi-scan, wifi-connect und
wifi-forget an run() weiter. Der Szenario-Schluessel "network" im Zustand
bestimmt den Ausgangszustand. Ausgabe und Fortschritt folgen dem Format des
echten Helpers, damit der Go-Parser in dashboard/internal/netmgr sie liest.

Zwei Dateien neben dem Demo-Zustand halten, was sich im Lauf aendert:
- network-links.json: gespeicherte WLAN-Profile und das verbundene Netz. Fehlt
  sie, gilt der Ausgangszustand des Szenarios. Ein erfolgreicher Connect fuegt
  sein Profil hinzu und verbindet, wifi-forget entfernt das Profil.
- network-switch.json: der letzte Connect. Ein Connect legt sie an, jeder
  Abruf von network-status rechnet den Ablauf aus started_at neu aus: Schritte
  je 1,3 s, bei Erfolg nach 3,9 s, bei Fehlschlag nach 2,2 s mit
  zurueckgenommenem Netz. Das Passwort landet nie in der Datei. Der Schluessel
  _fake traegt das Ergebnis und ist nie Teil der Ausgabe.

Ein Passwort gilt wie im echten Helper: 8 bis 63 druckbare ASCII-Zeichen oder
64 Hex-Ziffern. Der Fehlschlag tritt ein, wenn das Passwort mit "falsch" beginnt
oder im Szenario error das Netz FritzBox Werkstatt gewaehlt ist.
"""
import datetime
import json
import os
import re
import time
import uuid as uuid_mod
from dataclasses import dataclass
from pathlib import Path

VERBS = ("network-status", "wifi-scan", "wifi-connect", "wifi-forget")
SECTIONS = ("managed", "country", "wifidev", "rfkill", "devices", "addr", "route", "dns",
            "speed", "wifi", "profiles", "hotspot", "settings", "switch")
STAGED_NAME = "network-connect.staged.json"
PROGRESS_NAME = "network-switch.json"
LINKS_NAME = "network-links.json"
STEPS = ("associate", "address", "gateway")
STEP_SECONDS = 1.3
REQUEST_LIMIT = 1024
BAD_PASSWORD_PREFIX = "falsch"
WERKSTATT = "FritzBox Werkstatt"
WERKSTATT_UUID = "5b0c4e9a-7d1f-4a2e-9c3b-1f2e3d4c5b6a"
HOTSPOT_UUID = "0e6a1c2b-3d4f-4a5b-8c6d-7e8f9a0b1c2d"
HOTSPOT_NAME = "energy-node-hotspot"
LAN_ADDRESS = "192.168.178.40"
WIFI_ADDRESS = "192.168.178.52"
HOTSPOT_ADDRESS = "10.42.0.1"
GATEWAY = "192.168.178.1"
SETTINGS_JSON = '{"fallback":"parallel","hotspot":true}'
UUID_RE = re.compile(r"[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}")
PASSWORD_RE = re.compile(r"[\x20-\x7e]{8,63}|[0-9a-fA-F]{64}")

# SSID, SECURITY, dBm, frequency in MHz, channel. The values follow the draft NETS.
NETS = (
    ("FritzBox Werkstatt", "WPA2", -54, 5180, 36),
    ("FritzBox Werkstatt 2.4", "WPA2", -61, 2437, 6),
    ("Gäste", "WPA2", -67, 2412, 1),
    ("Halle Süd", "WPA3", -78, 2462, 11),
    ("Nachbar_EG", "WPA2", -86, 2437, 6),
    ("Drucker-Setup", "", -72, 2412, 1),
)

# Last failed switch of the error scenario, as a fixed record.
STATIC_ERROR = {
    "ssid": WERKSTATT,
    "started_at": "2026-11-12T13:02:40Z",
    "steps": [{"key": "associate", "state": "fail", "detail": "Passwort abgelehnt"},
              {"key": "address", "state": "wait"},
              {"key": "gateway", "state": "wait"}],
    "internet": "unknown",
    "result": "fail",
    "error": "auth",
    "address": "",
    "rolled_back": True,
    "finished_at": "2026-11-12T13:02:42Z",
}


@dataclass(frozen=True)
class Spec:
    label: str
    note: str
    managed: bool = True
    wifi_dev: bool = True
    lan_up: bool = False
    wifi_link: str = ""  # start state: "", "werkstatt" or "hotspot"
    saved: bool = False  # start state: the Werkstatt profile exists
    static_error: bool = False


SCENARIOS = {
    "lan": Spec("Nur LAN", "Kabel steckt, WLAN nicht eingerichtet", lan_up=True),
    "both": Spec("LAN mit WLAN-Ersatz", "Kabel aktiv, WLAN parallel bereit",
                 lan_up=True, wifi_link="werkstatt", saved=True),
    "wifi": Spec("Nur WLAN", "Kein Kabel, Node hängt im WLAN", wifi_link="werkstatt", saved=True),
    "error": Spec("WLAN scheitert", "Kabel aktiv, WLAN lehnt das Passwort ab",
                  lan_up=True, saved=True, static_error=True),
    "hotspot": Spec("Notfall-Hotspot", "Weder LAN noch WLAN, Hotspot offen", wifi_link="hotspot"),
    "nowifi": Spec("Kein WLAN-Modul", "Gerät ohne WLAN, zum Beispiel ein Thin Client",
                   wifi_dev=False, lan_up=True),
    "unmanaged": Spec("Ohne NetworkManager", "Nur die Adressen aus ip, keine WLAN-Liste",
                      managed=False, lan_up=True),
}
SCENARIO_IDS = tuple(SCENARIOS)
DEFAULT_SCENARIO = "lan"
# Scenario after a finished successful connect, and after forgetting the connected network.
SUCCESS_SCENARIO = {"lan": "both", "hotspot": "wifi"}
FORGET_SCENARIO = {"both": "lan"}


def spec_of(scenario):
    return SCENARIOS.get(scenario, SCENARIOS[DEFAULT_SCENARIO])


def paths_for(state_path):
    """Return (staged request, progress file, links file) next to the demo state, or (None, None, None)."""
    if not state_path:
        return None, None, None
    base = Path(state_path).parent
    return base / "data" / STAGED_NAME, base / PROGRESS_NAME, base / LINKS_NAME


def _stamp(ts):
    return datetime.datetime.fromtimestamp(ts, datetime.timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")


def _parse(stamp):
    parsed = datetime.datetime.strptime(stamp, "%Y-%m-%dT%H:%M:%SZ")
    return parsed.replace(tzinfo=datetime.timezone.utc).timestamp()


def _esc(text):
    return text.replace("\\", "\\\\").replace(":", "\\:")


def _read_json(path):
    if path is None:
        return None
    try:
        return json.loads(Path(path).read_text(encoding="utf-8"))
    except (OSError, ValueError):
        return None


def _write_json(path, doc):
    tmp = Path(str(path) + ".tmp")
    with open(tmp, "w", encoding="utf-8") as handle:
        json.dump(doc, handle, separators=(",", ":"), ensure_ascii=False)
    os.chmod(tmp, 0o644)
    os.replace(tmp, path)


# ---- saved profiles and the connected network -------------------------------

def default_links(spec):
    """Return the links a scenario starts with: saved profiles and the connected SSID."""
    profiles = []
    if spec.saved:
        profiles.append([WERKSTATT_UUID, "yes", WERKSTATT])
    if spec.wifi_link == "hotspot":
        profiles.append([HOTSPOT_UUID, "no", HOTSPOT_NAME])
    connected = {"werkstatt": WERKSTATT, "hotspot": HOTSPOT_NAME}.get(spec.wifi_link)
    return {"profiles": profiles, "connected": connected}


def _valid_links(doc):
    if not isinstance(doc, dict) or "connected" not in doc or not isinstance(doc.get("profiles"), list):
        return False
    if doc["connected"] is not None and not isinstance(doc["connected"], str):
        return False
    return all(isinstance(p, list) and len(p) == 3 and all(isinstance(v, str) for v in p)
               for p in doc["profiles"])


def load_links(path, spec):
    doc = _read_json(path)
    return doc if _valid_links(doc) else default_links(spec)


def _profile_uuid(ssid):
    return str(uuid_mod.uuid5(uuid_mod.NAMESPACE_URL, "energy-node-fake:" + ssid))


def _with_success(links, ssid):
    profiles = [list(p) for p in links["profiles"]]
    if not any(p[2] == ssid for p in profiles):
        profiles.append([_profile_uuid(ssid), "yes", ssid])
    return {"profiles": profiles, "connected": ssid}


# ---- the switch ---------------------------------------------------------------

def new_switch(ssid, fail, now):
    """Return the progress file written by a connect at time now."""
    return {
        "ssid": ssid,
        "started_at": _stamp(now),
        "steps": [{"key": key, "state": "wait"} for key in STEPS],
        "internet": "unknown",
        "result": "",
        "error": "",
        "address": "",
        "rolled_back": False,
        "finished_at": "",
        "_fake": {"fail": fail, "applied": False},
    }


def switch_view(doc, now):
    """Return the progress as the helper would print it at time now, or None."""
    if doc is None:
        return None
    fail = bool(doc.get("_fake", {}).get("fail"))
    elapsed = max(0.0, now - _parse(doc["started_at"]))
    steps = []
    for index, key in enumerate(STEPS):
        begin, end = index * STEP_SECONDS, (index + 1) * STEP_SECONDS
        step = {"key": key, "state": "wait"}
        if fail and index > 0 or elapsed < begin:
            pass
        elif elapsed < end:
            step["state"] = "run"
        elif fail:
            step["state"] = "fail"
            step["detail"] = "Passwort abgelehnt"
        else:
            step["state"] = "done"
            if key == "address":
                step["detail"] = WIFI_ADDRESS
            if key == "gateway":
                step["detail"] = GATEWAY
        steps.append(step)
    finish_at = (2 if fail else 3) * STEP_SECONDS
    finished = elapsed >= finish_at
    view = {k: v for k, v in doc.items() if k != "_fake"}
    view["steps"] = steps
    view["internet"] = "ok" if finished and not fail else "unknown"
    view["address"] = WIFI_ADDRESS if not fail and elapsed >= 2 * STEP_SECONDS else ""
    view["result"] = ("fail" if fail else "ok") if finished else ""
    view["error"] = ("auth" if fail else "") if finished else ""
    view["rolled_back"] = bool(fail and finished)
    view["finished_at"] = _stamp(_parse(doc["started_at"]) + finish_at) if finished else ""
    return view


def load_progress(path):
    """Return the stored progress document, or None if missing or broken."""
    doc = _read_json(path)
    if not isinstance(doc, dict) or not isinstance(doc.get("started_at"), str):
        return None
    if not isinstance(doc.get("_fake"), dict):
        return None
    return doc


def _settle(scenario, progress, links, now):
    """Applies a finished successful switch once and return the scenario after it.

    The network becomes connected and saved, and a lan or hotspot scenario moves on to both or wifi.
    """
    doc = load_progress(progress)
    view = switch_view(doc, now)
    if view is None or view["result"] != "ok" or doc["_fake"].get("applied"):
        return scenario
    if links is not None:
        _write_json(links, _with_success(load_links(links, spec_of(scenario)), view["ssid"]))
    doc["_fake"]["applied"] = True
    _write_json(progress, doc)
    return SUCCESS_SCENARIO.get(scenario, scenario)


def check_request(raw):
    """Return ssid, password and security of a staged request that follows the rules, else None."""
    if len(raw) > REQUEST_LIMIT:
        return None
    try:
        data = json.loads(raw.decode("utf-8"))
    except ValueError:
        return None
    if not isinstance(data, dict):
        return None
    ssid = data.get("ssid")
    password = data.get("password")
    uuid = data.get("profile_uuid", "")
    hidden = data.get("hidden", False)
    security = data.get("security", "")
    if not all(isinstance(v, str) for v in (ssid, password, uuid, security)):
        return None
    if not isinstance(hidden, bool):
        return None
    if not 1 <= len(ssid.encode("utf-8")) <= 32:
        return None
    if any(ord(c) < 0x20 or ord(c) == 0x7F for c in ssid):
        return None
    if password and not PASSWORD_RE.fullmatch(password):
        return None
    if uuid and not UUID_RE.fullmatch(uuid):
        return None
    return {"ssid": ssid, "password": password, "security": security}


# ---- rendering ------------------------------------------------------------------

def _wan(connected):
    return bool(connected) and connected != HOTSPOT_NAME


def _addresses(spec, connected):
    entries = [{"ifname": "lo", "addr_info": [{"family": "inet", "local": "127.0.0.1"}]}]
    lan = [{"family": "inet", "local": LAN_ADDRESS}] if spec.lan_up else []
    entries.append({"ifname": "eth0", "addr_info": lan})
    if spec.wifi_dev:
        if connected == HOTSPOT_NAME:
            wlan = [{"family": "inet", "local": HOTSPOT_ADDRESS}]
        elif connected:
            wlan = [{"family": "inet", "local": WIFI_ADDRESS}]
        else:
            wlan = []
        entries.append({"ifname": "wlan0", "addr_info": wlan})
    return json.dumps(entries, separators=(",", ":"))


def _routes(spec, connected):
    routes = []
    if spec.lan_up:
        routes.append({"dst": "default", "gateway": GATEWAY, "dev": "eth0", "metric": 100})
    if _wan(connected):
        routes.append({"dst": "default", "gateway": GATEWAY, "dev": "wlan0", "metric": 600})
    return json.dumps(routes, separators=(",", ":"))


def _devices(spec, connected):
    if not spec.managed:
        return ""
    rows = ["eth0:ethernet:%s" % ("connected:Kabel" if spec.lan_up else "unavailable:--")]
    if spec.wifi_dev:
        rows.append("wlan0:wifi:connected:" + _esc(connected) if connected else "wlan0:wifi:disconnected:--")
    return "\n".join(rows)


def _wifi_list(spec, connected):
    if not spec.managed or not spec.wifi_dev:
        return ""
    rows = []
    for ssid, security, dbm, freq, channel in NETS:
        current = "*" if connected and ssid == connected else " "
        signal = (dbm + 100) * 2
        rows.append("%s:%s:%d:%d MHz:%d:%s" % (current, _esc(ssid), signal, freq, channel, security or "--"))
    return "\n".join(rows)


def _profile_text(profiles):
    return "\n".join("%s\t%s\t%s" % (uuid, auto, _esc(ssid)) for uuid, auto, ssid in profiles)


def _hotspot(connected):
    active = "yes" if connected == HOTSPOT_NAME else "no"
    clients = 1 if connected == HOTSPOT_NAME else 0
    return "active %s\nclients %d" % (active, clients)


def _switch_text(spec, progress, now):
    view = switch_view(progress, now)
    if view is None and spec.static_error:
        view = STATIC_ERROR
    return json.dumps(view, separators=(",", ":"), ensure_ascii=False) if view else "{}"


def status_text(scenario, progress, now, links=None):
    """Return the network-status output for a scenario, the stored progress and the links."""
    spec = spec_of(scenario)
    if links is None:
        links = default_links(spec)
    connected = links["connected"] if spec.managed and spec.wifi_dev else None
    profiles = [tuple(p) for p in links["profiles"]] if spec.managed else []
    bodies = {
        "managed": "yes" if spec.managed else "no",
        "country": "DE",
        "wifidev": "wlan0" if spec.wifi_dev else "",
        "rfkill": "0 0" if spec.wifi_dev else "",
        "devices": _devices(spec, connected),
        "addr": _addresses(spec, connected),
        "route": _routes(spec, connected),
        "dns": GATEWAY if spec.lan_up or _wan(connected) else "",
        "speed": "eth0 1000" if spec.lan_up else "",
        "wifi": _wifi_list(spec, connected),
        "profiles": _profile_text(profiles),
        "hotspot": _hotspot(connected),
        "settings": SETTINGS_JSON,
        "switch": _switch_text(spec, progress, now),
    }
    out = []
    for name in SECTIONS:
        out.append("##%s\n" % name)
        if bodies[name]:
            out.append(bodies[name] + "\n")
    return "".join(out)


# ---- verbs ----------------------------------------------------------------------

def _connect(scenario, staged, progress, links, now):
    """Return (exit code, output, scenario after the call)."""
    spec = spec_of(scenario)
    if not spec.managed:
        return 69, "", scenario
    if not spec.wifi_dev:
        return 1, "", scenario
    view = switch_view(load_progress(progress), now)
    if view is not None and view["result"] == "":
        return 75, "", scenario
    if staged is None or not staged.is_file() or staged.is_symlink():
        return 65, "", scenario
    with open(staged, "rb") as handle:
        raw = handle.read(REQUEST_LIMIT + 1)
    request = check_request(raw)
    if request is None:
        return 65, "", scenario
    staged.unlink()
    scenario = _settle(scenario, progress, links, now)
    fail = request["password"].startswith(BAD_PASSWORD_PREFIX) or (
        scenario == "error" and request["ssid"] == WERKSTATT)
    _write_json(progress, new_switch(request["ssid"], fail, now))
    return 0, "", scenario


def _forget(scenario, uuid, links):
    """Return (exit code, output, scenario after the call)."""
    spec = spec_of(scenario)
    if links is None:
        return 1, "", scenario
    doc = load_links(links, spec)
    known = [p for p in doc["profiles"] if p[0] == uuid]
    if not UUID_RE.fullmatch(uuid) or not known:
        return 65, "", scenario
    profiles = [p for p in doc["profiles"] if p[0] != uuid]
    was_connected = doc["connected"] == known[0][2]
    connected = None if was_connected else doc["connected"]
    _write_json(links, {"profiles": profiles, "connected": connected})
    if was_connected:
        scenario = FORGET_SCENARIO.get(scenario, scenario)
    return 0, "", scenario


def run(verb, args, scenario, staged, progress, links, now=None):
    """Answer one network verb. Return (exit code, stdout text, scenario after the call).

    A finished successful connect and a forget of the connected network can switch the scenario.
    The caller stores the new scenario in the demo state.
    """
    now = time.time() if now is None else now
    if verb == "network-status":
        if args:
            return 64, "", scenario
        scenario = _settle(scenario, progress, links, now)
        spec = spec_of(scenario)
        return 0, status_text(scenario, load_progress(progress), now, load_links(links, spec)), scenario
    if verb == "wifi-scan":
        if args:
            return 64, "", scenario
        spec = spec_of(scenario)
        if not spec.managed:
            return 69, "", scenario
        if not spec.wifi_dev:
            return 1, "", scenario
        return 0, "", scenario
    if verb == "wifi-connect":
        if args:
            return 64, "", scenario
        return _connect(scenario, staged, progress, links, now)
    if verb == "wifi-forget":
        if len(args) != 1:
            return 64, "", scenario
        return _forget(scenario, args[0], links)
    return 64, "", scenario
