#!/usr/bin/env python3
"""Welche Systempakete wuerde apt-get upgrade jetzt aktualisieren?

Eine Stelle fuer die Zaehlung: plan.sh (Vorschau des Installers),
diagnose.sh und der Dashboard-Updater (dashboard/internal/updaterhost), der
dieses Skript aus dem Kandidaten-Bundle aufruft.

Die Pruefung ruft von sich aus nie apt-get update. Sie simuliert auf den
vorhandenen Paketlisten, die apt-daily taeglich auffrischt, und nennt deren
Stand. So braucht sie weder root noch Netz und wartet auf keinen Lock. Nur
mit --refresh (Diagnose, "jetzt neu abrufen") holt sie die Listen vorher
frisch, ueber sudo; scheitert das, endet sie mit Exit 2 und nennt den
Fehler auf stderr.

Ausgabe (eine Zeile JSON) bzw. Rueckgabe von pending():
  {"count": 2, "checked_at": "2026-10-04T06:12:00+00:00",
   "packages": [{"name": "libssl3", "from": "3.0.11", "to": "3.0.13"}, ...]}
oder null/None, wenn apt-get fehlt, scheitert oder zu lange braucht.
checked_at ist "", wenn der Stand unbekannt ist.

  EN_ROOT     Praefix vor /var/lib/apt (Tests)
  EN_APT_GET  apt-get-Programm (Vorgabe: apt-get aus PATH)
  EN_SUDO     Praefix fuer apt-get update (Vorgabe "sudo -n", "" = keines)
  EN_APT_LOCK_PAUSE  Sekunden zwischen zwei Versuchen bei belegtem Lock
"""
import datetime
import json
import os
import pathlib
import re
import shlex
import subprocess
import sys
import time

TIMEOUT_SECONDS = 120
REFRESH_TIMEOUT_SECONDS = 600
# apt-get update nimmt den Listen-Lock ohne Wartezeit, apt-daily haelt ihn
# gerade nach dem Booten: bis zu 30 Versuche (Vorgabe fuenf Minuten).
LOCK_ATTEMPTS = 30


class RefreshFailed(Exception):
    """apt-get update ist gescheitert; die Meldung ist apts stderr."""


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


def refresh():
    """Holt die Paketlisten frisch (apt-get update ueber sudo)."""
    apt_get = os.environ.get("EN_APT_GET", "apt-get")
    sudo = shlex.split(os.environ.get("EN_SUDO", "sudo -n"))
    command = sudo + ["env", "DEBIAN_FRONTEND=noninteractive", "LC_ALL=C", apt_get, "-qq", "update"]
    pause = float(os.environ.get("EN_APT_LOCK_PAUSE", "10"))
    for attempt in range(LOCK_ATTEMPTS):
        try:
            result = subprocess.run(command, capture_output=True, text=True, timeout=REFRESH_TIMEOUT_SECONDS)
        except (OSError, subprocess.TimeoutExpired) as exc:
            raise RefreshFailed(str(exc))
        if result.returncode == 0:
            return
        if "Could not get lock" not in result.stderr or attempt == LOCK_ATTEMPTS - 1:
            raise RefreshFailed(result.stderr.strip() or "apt-get update exit %d" % result.returncode)
        time.sleep(pause)


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
    if "--refresh" in sys.argv[1:]:
        try:
            refresh()
        except RefreshFailed as exc:
            print(exc, file=sys.stderr)
            sys.exit(2)
    print(json.dumps(pending(), ensure_ascii=False))
    sys.exit(0)
