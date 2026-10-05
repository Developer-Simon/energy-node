#!/usr/bin/env python3
"""Welche Systempakete wuerde apt-get upgrade jetzt aktualisieren?

Eine Stelle fuer die Zaehlung: plan.sh (Vorschau des Installers),
diagnose.sh und der Dashboard-Updater (dashboard/internal/updaterhost), der
dieses Skript aus dem Kandidaten-Bundle aufruft.

Die Pruefung ruft nie apt-get update. Sie simuliert auf den vorhandenen
Paketlisten, die apt-daily taeglich auffrischt, und nennt deren Stand. So
braucht sie weder root noch Netz und wartet auf keinen Lock.

Ausgabe (eine Zeile JSON) bzw. Rueckgabe von pending():
  {"count": 2, "checked_at": "2026-10-04T06:12:00+00:00",
   "packages": [{"name": "libssl3", "from": "3.0.11", "to": "3.0.13"}, ...]}
oder null/None, wenn apt-get fehlt, scheitert oder zu lange braucht.
checked_at ist "", wenn der Stand unbekannt ist.

  EN_ROOT     Praefix vor /var/lib/apt (Tests)
  EN_APT_GET  apt-get-Programm (Vorgabe: apt-get aus PATH)
"""
import datetime
import json
import os
import pathlib
import re
import subprocess
import sys

TIMEOUT_SECONDS = 120
INST = re.compile(r"^Inst (\S+) (?:\[(\S+)\] )?\((\S+)")


def checked_at(root=None):
    """Stand der Paketlisten: der Stempel, den apt nach jedem gelungenen
    update setzt, sonst die neueste Release-Datei der Listen."""
    root = os.environ.get("EN_ROOT", "") if root is None else root
    apt = pathlib.Path(root + "/var/lib/apt")
    stamp = apt / "periodic" / "update-success-stamp"
    times = []
    if stamp.is_file():
        times = [stamp.stat().st_mtime]
    else:
        lists = apt / "lists"
        if lists.is_dir():
            times = [p.stat().st_mtime for p in lists.iterdir()
                     if p.name.endswith("_InRelease") or p.name.endswith("_Release")]
    if not times:
        return ""
    moment = datetime.datetime.fromtimestamp(max(times), datetime.timezone.utc)
    return moment.isoformat(timespec="seconds")


def parse(simulation):
    packages = []
    for line in simulation.splitlines():
        match = INST.match(line)
        if match:
            packages.append({"name": match.group(1), "from": match.group(2) or "", "to": match.group(3)})
    return packages


def pending(root=None):
    apt_get = os.environ.get("EN_APT_GET", "apt-get")
    env = dict(os.environ, LC_ALL="C")
    try:
        result = subprocess.run(
            [apt_get, "-s", "-o", "Debug::NoLocking=1", "upgrade"],
            capture_output=True, text=True, env=env, timeout=TIMEOUT_SECONDS,
        )
    except (OSError, subprocess.TimeoutExpired):
        return None
    if result.returncode != 0:
        return None
    packages = parse(result.stdout)
    return {"count": len(packages), "checked_at": checked_at(root), "packages": packages}


if __name__ == "__main__":
    print(json.dumps(pending(), ensure_ascii=False))
    sys.exit(0)
