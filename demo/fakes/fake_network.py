"""Simulierte Netz-Verben von energy-node-dashboard-system-action fuer die Demo.

fake_journal_helper.py reicht network-status, wifi-scan, wifi-connect,
wifi-forget, network-settings und hotspot-secret an run() weiter. Der Schluessel
"network" im Zustand haelt Schalter der Umgebung: lan (Kabel steckt), wifi
(WLAN-Modul vorhanden), password_ok (das WLAN nimmt Passwoerter an) und
werkstatt (das Netz FritzBox Werkstatt ist erreichbar). Alles andere folgt daraus und aus den Dateien
unten, auch der Notfall-Hotspot: er ist offen, wenn ein WLAN-Modul da ist,
weder Kabel noch WLAN-Verbindung bestehen und der Hotspot-Schalter an ist.
Ausgabe und Fortschritt folgen dem Format des echten Helpers, damit der
Go-Parser in dashboard/internal/netmgr sie liest.

Drei Dateien neben dem Demo-Zustand halten, was sich im Lauf aendert:
- network-links.json: gespeicherte WLAN-Profile und das verbundene Netz. Fehlt
  sie, gibt es weder Profile noch Verbindung. Ein erfolgreicher Connect fuegt
  sein Profil hinzu und verbindet, wifi-forget entfernt das Profil.
- network-settings.json: Ersatzmodus und Hotspot-Schalter aus network-settings.
  Fehlt sie, gelten parallel und an. Der Abruf network-status zeigt sie.
- network-switch.json: der letzte Connect. Ein Connect legt sie an, jeder
  Abruf von network-status rechnet den Ablauf aus started_at neu aus: Schritte
  je 1,3 s, bei Erfolg nach 3,9 s, bei Fehlschlag nach 2,2 s mit
  zurueckgenommenem Netz. Das Passwort landet nie in der Datei. Der Schluessel
  _fake traegt das Ergebnis und ist nie Teil der Ausgabe.

Ein Passwort gilt wie im echten Helper: 8 bis 63 druckbare ASCII-Zeichen oder
64 Hex-Ziffern. Der Fehlschlag mit auth tritt ein, wenn das Passwort mit "falsch"
beginnt oder password_ok aus ist. Ein Connect auf ein Netz, das es nicht mehr gibt
(werkstatt aus), scheitert mit not_found.
"""
import datetime
import json
import os
import re
import time
import uuid as uuid_mod
from dataclasses import dataclass
from pathlib import Path

VERBS = ("network-status", "wifi-scan", "wifi-connect", "wifi-forget", "network-settings",
         "hotspot-secret")
SECTIONS = ("managed", "country", "wifidev", "rfkill", "devices", "addr", "route", "dns",
            "speed", "wifi", "profiles", "hotspot", "settings", "switch")
STAGED_NAME = "network-connect.staged.json"
PROGRESS_NAME = "network-switch.json"
LINKS_NAME = "network-links.json"
SETTINGS_NAME = "network-settings.json"
FALLBACK_MODES = ("parallel", "standby")
DEFAULT_SETTINGS = {"fallback": "parallel", "hotspot": True}
# The emergency hotspot of the demo node. The real helper reads hotspot.json,
# which bootstrap 67 writes once. Without a state file there is no node, so 66.
HOTSPOT_SSID = "EnergyNode-4F2A"
HOTSPOT_PASSWORD = "Notfall2026Demo"
FAIL_DETAIL = {"auth": "Passwort abgelehnt", "not_found": "Netz nicht gefunden"}
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

@dataclass(frozen=True)
class Spec:
    managed: bool = True
    wifi_dev: bool = True
    lan_up: bool = False
    password_ok: bool = True
    werkstatt: bool = True


DEFAULT_ENV = {"lan": True, "wifi": True, "password_ok": True, "werkstatt": True}


def env_of(value):
    """Return the environment switches of the state value, or the defaults if it is broken.

    Switches missing in an older state file keep their default.
    """
    if (isinstance(value, dict) and isinstance(value.get("lan"), bool)
            and isinstance(value.get("wifi"), bool)):
        env = dict(DEFAULT_ENV)
        for name in DEFAULT_ENV:
            if isinstance(value.get(name), bool):
                env[name] = value[name]
        return env
    return dict(DEFAULT_ENV)


def spec_of(env):
    env = env_of(env)
    return Spec(wifi_dev=env["wifi"], lan_up=env["lan"], password_ok=env["password_ok"],
                werkstatt=env["werkstatt"])


def paths_for(state_path):
    """Return (staged request, progress file, links file) next to the demo state, or (None, None, None)."""
    if not state_path:
        return None, None, None
    base = Path(state_path).parent
    return base / "data" / STAGED_NAME, base / PROGRESS_NAME, base / LINKS_NAME


def settings_path_for(links):
    """Return the settings file next to the links file, or None without a state file."""
    return None if links is None else Path(links).with_name(SETTINGS_NAME)


def load_settings(links):
    """Return the stored fallback and hotspot settings, or the defaults if missing or broken."""
    doc = _read_json(settings_path_for(links))
    if (isinstance(doc, dict) and doc.get("fallback") in FALLBACK_MODES
            and isinstance(doc.get("hotspot"), bool)):
        return {"fallback": doc["fallback"], "hotspot": doc["hotspot"]}
    return dict(DEFAULT_SETTINGS)


def _settings_text(settings):
    return json.dumps(settings, separators=(",", ":"), ensure_ascii=False)


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

def default_links():
    """Return the links a fresh node starts with: no saved profile and no connected network."""
    return {"profiles": [], "connected": None}


def _valid_links(doc):
    if not isinstance(doc, dict) or "connected" not in doc or not isinstance(doc.get("profiles"), list):
        return False
    if doc["connected"] is not None and not isinstance(doc["connected"], str):
        return False
    return all(isinstance(p, list) and len(p) == 3 and all(isinstance(v, str) for v in p)
               for p in doc["profiles"])


def load_links(path):
    doc = _read_json(path)
    return doc if _valid_links(doc) else default_links()


def effective_links(spec, links, settings):
    """Return (profiles, connected) as the node sees them, including the emergency hotspot.

    In the standby mode the saved network stays unconnected as long as the
    cable is plugged in, in the parallel mode it is connected alongside.
    The hotspot opens by itself when there is a WLAN module but no cable and no
    WLAN connection, and the hotspot switch is on. Plugging the cable or
    connecting a network closes it again.
    """
    profiles = [tuple(p) for p in links["profiles"]]
    connected = links["connected"] if spec.wifi_dev else None
    if connected == WERKSTATT and not spec.werkstatt:
        connected = None  # the access point is gone, the connection is lost
    if connected and spec.lan_up and settings["fallback"] == "standby":
        connected = None  # standby: Wi-Fi stays off while the cable has a network
    if spec.wifi_dev and not connected and not spec.lan_up and settings["hotspot"]:
        profiles.append((HOTSPOT_UUID, "no", HOTSPOT_NAME))
        connected = HOTSPOT_NAME
    return profiles, connected


def _profile_uuid(ssid):
    return str(uuid_mod.uuid5(uuid_mod.NAMESPACE_URL, "energy-node-fake:" + ssid))


def _with_success(links, ssid):
    profiles = [list(p) for p in links["profiles"]]
    if not any(p[2] == ssid for p in profiles):
        profiles.append([_profile_uuid(ssid), "yes", ssid])
    return {"profiles": profiles, "connected": ssid}


# ---- the switch ---------------------------------------------------------------

def new_switch(ssid, error, now):
    """Return the progress file written by a connect at time now.

    error is "" for a connect that works, else the failure code auth or not_found.
    """
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
        "_fake": {"fail": bool(error), "error": error, "applied": False},
    }


def switch_view(doc, now):
    """Return the progress as the helper would print it at time now, or None."""
    if doc is None:
        return None
    fail = bool(doc.get("_fake", {}).get("fail"))
    code = doc.get("_fake", {}).get("error") or "auth"
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
            step["detail"] = FAIL_DETAIL.get(code, "Verbindung fehlgeschlagen")
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
    view["error"] = (code if fail else "") if finished else ""
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


def _settle(progress, links, now):
    """Applies a finished successful switch once: the network becomes connected and saved."""
    doc = load_progress(progress)
    view = switch_view(doc, now)
    if view is None or view["result"] != "ok" or doc["_fake"].get("applied"):
        return
    if links is not None:
        _write_json(links, _with_success(load_links(links), view["ssid"]))
    doc["_fake"]["applied"] = True
    _write_json(progress, doc)


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
        if ssid == WERKSTATT and not spec.werkstatt:
            continue
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


def _switch_text(progress, now):
    view = switch_view(progress, now)
    return json.dumps(view, separators=(",", ":"), ensure_ascii=False) if view else "{}"


def status_text(env, progress, now, links=None, settings=None):
    """Return the network-status output for the environment switches, the stored progress, links and settings."""
    spec = spec_of(env)
    if links is None:
        links = default_links()
    if settings is None:
        settings = dict(DEFAULT_SETTINGS)
    profiles, connected = effective_links(spec, links, settings)
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
        "settings": _settings_text(settings),
        "switch": _switch_text(progress, now),
    }
    out = []
    for name in SECTIONS:
        out.append("##%s\n" % name)
        if bodies[name]:
            out.append(bodies[name] + "\n")
    return "".join(out)


# ---- verbs ----------------------------------------------------------------------

def _connect(env, staged, progress, links, now):
    """Return (exit code, output)."""
    spec = spec_of(env)
    if not spec.wifi_dev:
        return 1, ""
    view = switch_view(load_progress(progress), now)
    if view is not None and view["result"] == "":
        return 75, ""
    if staged is None or not staged.is_file() or staged.is_symlink():
        return 65, ""
    with open(staged, "rb") as handle:
        raw = handle.read(REQUEST_LIMIT + 1)
    request = check_request(raw)
    if request is None:
        return 65, ""
    staged.unlink()
    _settle(progress, links, now)
    if request["ssid"] == WERKSTATT and not spec.werkstatt:
        error = "not_found"
    elif request["password"].startswith(BAD_PASSWORD_PREFIX) or not spec.password_ok:
        error = "auth"
    else:
        error = ""
    _write_json(progress, new_switch(request["ssid"], error, now))
    return 0, ""


def _forget(uuid, links):
    """Return (exit code, output)."""
    if links is None:
        return 1, ""
    doc = load_links(links)
    known = [p for p in doc["profiles"] if p[0] == uuid]
    if not UUID_RE.fullmatch(uuid) or not known:
        return 65, ""
    profiles = [p for p in doc["profiles"] if p[0] != uuid]
    connected = None if doc["connected"] == known[0][2] else doc["connected"]
    _write_json(links, {"profiles": profiles, "connected": connected})
    return 0, ""


def _settings_write(args, links):
    """Return (exit code, output) for network-settings <fallback> <on|off>."""
    if len(args) != 2:
        return 64, ""
    fallback, hotspot = args
    if fallback not in FALLBACK_MODES or hotspot not in ("on", "off"):
        return 64, ""
    path = settings_path_for(links)
    if path is None:
        return 1, ""
    _write_json(path, {"fallback": fallback, "hotspot": hotspot == "on"})
    return 0, ""


def _hotspot_secret(args, links):
    """Return (exit code, output) for hotspot-secret, which prints ssid and password."""
    if args:
        return 64, ""
    if links is None:
        return 66, ""
    return 0, "%s\n%s\n" % (HOTSPOT_SSID, HOTSPOT_PASSWORD)


def run(verb, args, env, staged, progress, links, now=None):
    """Answer one network verb. Return (exit code, stdout text).

    env holds the two environment switches lan and wifi of the demo state.
    """
    now = time.time() if now is None else now
    if verb == "network-status":
        if args:
            return 64, ""
        _settle(progress, links, now)
        text = status_text(env, load_progress(progress), now, load_links(links), load_settings(links))
        return 0, text
    if verb == "network-settings":
        return _settings_write(args, links)
    if verb == "hotspot-secret":
        return _hotspot_secret(args, links)
    if verb == "wifi-scan":
        if args:
            return 64, ""
        return (0, "") if spec_of(env).wifi_dev else (1, "")
    if verb == "wifi-connect":
        if args:
            return 64, ""
        return _connect(env, staged, progress, links, now)
    if verb == "wifi-forget":
        if len(args) != 1:
            return 64, ""
        return _forget(args[0], links)
    return 64, ""
