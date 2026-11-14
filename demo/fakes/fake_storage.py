"""Modell der Demo-Laufwerke fuer Fake-Helfer und Fake-Konsole (nur Standardbibliothek).

Ein Stick ist ein Eintrag in demo/fixtures/storage-catalog.json. Solange er
nicht eingehaengt ist, liegt sein Inhalt unter disks/<kname>. Eingehaengt wird
der Inhalt in den Einhaengepunkt mount/, dort liest ihn das Dashboard. Alle
Zustandsdateien liegen im Speicherordner der Demo ($WORK/storage):

    plugged.json    Steckzustand, siehe DEFAULT_STATE
    drives.json     Laufwerksliste im lsblk-Format, die das Dashboard liest
    disks/<kname>/  Inhalt eines Sticks, solange er nicht eingehaengt ist
    mount/          Einhaengepunkt

StorageError ist ein Fehler im Ablauf (Helfer: Exit 1, Konsole: 400).
StorageRefused ist eine ungueltige Anfrage, etwa ein unbekannter Stick (Exit 64).
"""
import copy
import json
import os
import shutil
import tempfile
from pathlib import Path

CATALOG_PATH = Path(__file__).resolve().parent.parent / "fixtures" / "storage-catalog.json"
STATE_FILE = "plugged.json"
DRIVES_FILE = "drives.json"
MARKER = ".energy-node-volume"
RUNTIME_DIRS = ("runtime", "backups")
DEFAULT_STATE = {"plugged": ["sdb"], "active": "", "mounted": False, "formatted": [],
                 "format_fails": False, "mount_fails": False}
FAULTS = {"format": "format_fails", "mount": "mount_fails"}
LIST_FIELDS = ("kname", "name", "uuid", "fstype", "transport", "size", "used", "ready", "needs_format", "contents",
               "disk", "disk_size", "label", "whole")


class StorageError(Exception):
    """Ein Schritt scheitert, etwa Stick nicht eingesteckt oder Fehler im Fake."""


class StorageRefused(Exception):
    """Die Anfrage ist ungueltig: unbekannter Stick oder unbekannte Uuid."""


def load_catalog(path=CATALOG_PATH):
    with open(path, encoding="utf-8") as handle:
        return json.load(handle)


def find_stick(catalog, kname):
    return next((stick for stick in catalog if stick["kname"] == kname), None)


def _write_json(path, data):
    path = Path(path)
    path.parent.mkdir(parents=True, exist_ok=True)
    handle, tmp = tempfile.mkstemp(dir=path.parent, prefix="." + path.name + ".", suffix=".tmp")
    with os.fdopen(handle, "w", encoding="utf-8") as out:
        json.dump(data, out, ensure_ascii=False)
    os.replace(tmp, path)


def load_state(storage):
    """Zustand aus plugged.json, fehlt die Datei oder ist sie kaputt, gilt DEFAULT_STATE."""
    state = copy.deepcopy(DEFAULT_STATE)
    try:
        loaded = json.loads((Path(storage) / STATE_FILE).read_text(encoding="utf-8"))
    except (OSError, ValueError):
        return state
    if not isinstance(loaded, dict):
        return state
    for key, value in loaded.items():
        if key in DEFAULT_STATE and isinstance(value, type(DEFAULT_STATE[key])):
            state[key] = value
    return state


def is_formatted(stick, state):
    return not stick["needs_format"] or stick["kname"] in state["formatted"]


def _is_partition(stick):
    return stick.get("disk", stick["kname"]) != stick["kname"]


def _group(catalog, kname):
    """Der Stick mit allen Partitionen. Eine Partition wird nie allein gesteckt oder gezogen."""
    stick = find_stick(catalog, kname)
    if stick is None:
        raise StorageRefused(f"unbekannter Stick {kname}")
    if _is_partition(stick):
        raise StorageRefused(f"{kname} ist eine Partition von {stick['disk']}")
    return [kname, *stick.get("members", [])]


def _listed(stick, formatted):
    """Ein Eintrag der Laufwerksliste. Felder, die der Katalog nicht kennt, bekommen ihren Standard."""
    entry = {field: stick.get(field) for field in LIST_FIELDS}
    entry["disk"] = stick.get("disk", stick["kname"])
    entry["disk_size"] = stick.get("disk_size", stick["size"])
    entry["label"] = stick.get("label", "")
    entry["whole"] = stick.get("whole", False)
    entry["uuid"] = stick["uuid"] if formatted else ""
    entry["needs_format"] = not formatted
    entry["ready"] = bool(stick["ready"] or formatted)
    entry["fstype"] = "ext4" if (stick["needs_format"] and formatted) else stick["fstype"]
    entry["contents"] = "" if formatted else stick["contents"]
    return entry


def drive_list(catalog, state):
    """Laufwerke, die gerade eingesteckt sind, im Format der Laufwerksliste des Dashboards.

    Eine Platte mit Partitionen erscheint als Gesamteintrag, gefolgt von ihren Partitionen.
    Ist der Gesamteintrag formatiert, bleibt nur ein normaler ext4-Eintrag. Ist eine Partition
    eingehaengt, verschwindet die ganze Gruppe.
    """
    drives = []
    for stick in catalog:
        kname = stick["kname"]
        if kname not in state["plugged"] or _is_partition(stick):
            continue
        members = [find_stick(catalog, member) for member in stick.get("members", [])]
        if not members:
            drives.append(_listed(stick, is_formatted(stick, state)))
            continue
        if kname in state["formatted"]:
            single = _listed(stick, True)
            single.update(whole=False, label="")
            drives.append(single)
            continue
        group = [kname, *stick["members"]]
        if state["mounted"] and state["active"] in group:
            continue
        drives.append(_listed(stick, False))
        drives.extend(_listed(member, is_formatted(member, state)) for member in members)
    return drives


def _commit(storage, catalog, state):
    _write_json(Path(storage) / STATE_FILE, state)
    _write_json(Path(storage) / DRIVES_FILE, drive_list(catalog, state))


def _entries(path):
    path = Path(path)
    return sorted(path.iterdir()) if path.is_dir() else []


def _remove(path):
    if path.is_dir() and not path.is_symlink():
        shutil.rmtree(path)
    else:
        path.unlink()


def _clear(path):
    for item in _entries(path):
        _remove(item)


def _move(src, dst):
    if dst.exists() or dst.is_symlink():
        _remove(dst)
    shutil.move(str(src), str(dst))


def _park(storage, mount, kname):
    """Legt den Inhalt des Einhaengepunkts auf dem Stick kname ab."""
    content = _entries(mount)
    if not content:
        return
    disk = Path(storage) / "disks" / kname
    disk.mkdir(parents=True, exist_ok=True)
    for item in content:
        _move(item, disk / item.name)


def _attach(storage, mount, kname, marker):
    """Holt den Inhalt des Sticks in den Einhaengepunkt und schreibt Kennung und Verzeichnisse."""
    mount = Path(mount)
    mount.mkdir(parents=True, exist_ok=True)
    disk = Path(storage) / "disks" / kname
    for item in _entries(disk):
        _move(item, mount / item.name)
    (mount / MARKER).write_text(marker + "\n", encoding="utf-8")
    for name in RUNTIME_DIRS:
        (mount / name).mkdir(exist_ok=True)


def _make_active(storage, mount, state, kname):
    """Der Einhaengepunkt gehoert danach dem Stick kname, der bisherige Inhalt bleibt auf seinem Stick."""
    if state["active"] and state["active"] != kname:
        _park(storage, mount, state["active"])
    elif not state["active"]:
        _clear(mount)


def format_drive(storage, mount, catalog, kname):
    """Loescht den Stick und gibt seine Uuid zurueck. Der Einhaengepunkt wird geleert, wenn der Stick dort liegt.

    Formatiert wird der Gesamteintrag einer Platte mit Partitionen, dann gehen die Partitionen mit.
    Eine Partition allein formatiert nur sich selbst.
    """
    stick = find_stick(catalog, kname)
    if stick is None:
        raise StorageRefused(f"unbekannter Stick {kname}")
    state = load_state(storage)
    if kname not in state["plugged"]:
        raise StorageError(f"{kname} ist nicht eingesteckt")
    if state["format_fails"]:
        raise StorageError("Formatieren fehlgeschlagen")
    targets = [kname, *stick.get("members", [])]
    if state["active"] in targets:
        _clear(mount)
        state["active"] = ""
        state["mounted"] = False
    for target in targets:
        _clear(Path(storage) / "disks" / target)
    if stick["needs_format"] and kname not in state["formatted"]:
        state["formatted"].append(kname)
    _commit(storage, catalog, state)
    return stick["uuid"]


def mount_drive(storage, mount, catalog, uuid, machine_id):
    """Haengt den Stick mit der Uuid ein und schreibt Kennung und Verzeichnisse."""
    stick = next((s for s in catalog if s["uuid"] == uuid), None)
    if stick is None:
        raise StorageRefused(f"unbekannte Uuid {uuid}")
    kname = stick["kname"]
    state = load_state(storage)
    if kname not in state["plugged"]:
        raise StorageError(f"{kname} ist nicht eingesteckt")
    if not is_formatted(stick, state):
        raise StorageError(f"{kname} ist nicht formatiert")
    if state["mount_fails"]:
        raise StorageError("Einhängen fehlgeschlagen")
    _make_active(storage, mount, state, kname)
    _attach(storage, mount, kname, stick.get("marker") or machine_id)
    state["active"] = kname
    state["mounted"] = True
    _commit(storage, catalog, state)


def unmount_drive(storage, mount):
    """Markiert den Einhaengepunkt als ausgehaengt. Es wird nichts entfernt."""
    state = load_state(storage)
    state["mounted"] = False
    _commit(storage, load_catalog(), state)


def plug(storage, mount, catalog, kname, machine_id):
    """Steckt einen Stick ein. Der Stick, der zuletzt eingehaengt war, bekommt seinen Inhalt zurueck.
    Ein fremder Stick haengt sich selbst am Einhaengepunkt ein, mit seiner eigenen Kennung.
    Eine Platte mit Partitionen steckt mit allen Partitionen zusammen."""
    group = _group(catalog, kname)
    stick = find_stick(catalog, kname)
    state = load_state(storage)
    if kname in state["plugged"]:
        raise StorageError(f"{kname} ist schon eingesteckt")
    state["plugged"].extend(group)
    if "marker" in stick:
        _make_active(storage, mount, state, kname)
        _attach(storage, mount, kname, stick["marker"])
        state["active"] = kname
        state["mounted"] = True
    elif state["active"] in group:
        _attach(storage, mount, state["active"], machine_id)
        state["mounted"] = True
    _commit(storage, catalog, state)


def pull(storage, mount, catalog, kname):
    """Zieht einen Stick ab. Liegt sein Inhalt im Einhaengepunkt, bleibt er auf dem Stick und der Punkt ist leer.
    Eine Platte mit Partitionen zieht alle Partitionen mit."""
    group = _group(catalog, kname)
    state = load_state(storage)
    if kname not in state["plugged"]:
        raise StorageError(f"{kname} ist nicht eingesteckt")
    if state["active"] in group:
        _park(storage, mount, state["active"])
        state["mounted"] = False
    state["plugged"] = [plugged for plugged in state["plugged"] if plugged not in group]
    _commit(storage, catalog, state)


def set_fault(storage, name, on):
    """Schaltet den Fehler fuer format oder mount an oder aus."""
    if name not in FAULTS:
        raise StorageRefused(f"unbekannter Fehler {name}")
    state = load_state(storage)
    state[FAULTS[name]] = bool(on)
    _commit(storage, load_catalog(), state)


def reset(storage, mount, catalog):
    """Startzustand: nur der Demo-Stick steckt, der Einhaengepunkt und die Sticks sind leer."""
    _clear(mount)
    _clear(Path(storage) / "disks")
    _commit(storage, catalog, copy.deepcopy(DEFAULT_STATE))
