"""Reiter Updates der Fake-Konsole.

Zwei Teile: der GitHub-Releases-Fake (fake_github_releases.py, ueber RemoteFake
mit seinen /_fake/-Routen) und der Installer-Zustand, also die Dateien, die das
Dashboard bei jedem Update-Check liest. Der Installer-Zustand liegt in einem
Ordner, der aus der Fixture demo/fixtures/installed-state kopiert wird. Die
Oberflaeche steht in console_updates_ui.py.
"""

import json
import os
import re
import shutil
import tempfile
from pathlib import Path

from console_remote import RemoteFake
from console_updates_ui import updates_html
from fake_console import Log, Tab

INSTALLER_FILES = ("installed-manifest.json", "previous-manifest.json", "selection.json", "changelog.json")
ACTIONS = ("release", "download", "installer_version", "installer_none", "installer_previous", "installer_reset")
VERSION_RE = re.compile(r"v\d+\.\d+\.\d+")
UNREACHABLE = "GitHub-Fake nicht erreichbar"
NOT_SET_UP = "Installer-Zustand ist nicht eingerichtet"
NOT_READABLE = "Installer-Zustand nicht lesbar"
NOT_WRITABLE = "Installer-Zustand nicht schreibbar"
MODE_TEXT = {"none": "kein Update", "error": "Fehler 500", "ratelimit": "Rate-Limit"}


def _read(path: Path) -> bytes | None:
    try:
        return path.read_bytes()
    except FileNotFoundError:
        return None
    except OSError:
        raise ValueError(NOT_READABLE) from None


def _parse_manifest(raw: bytes) -> dict:
    try:
        data = json.loads(raw.decode("utf-8"))
    except (UnicodeDecodeError, ValueError):
        raise ValueError(NOT_READABLE) from None
    if not isinstance(data, dict):
        raise ValueError(NOT_READABLE)
    return data


class InstallerState:
    """Dateien des Installer-Zustands in state_dir, Vorlage ist fixture_dir."""

    def __init__(self, state_dir: Path, fixture_dir: Path) -> None:
        self.state_dir = state_dir
        self.fixture_dir = fixture_dir

    def matches_fixture(self) -> bool:
        for name in INSTALLER_FILES:
            if _read(self.state_dir / name) != _read(self.fixture_dir / name):
                return False
        return True

    def state(self) -> dict:
        version = ""
        raw = _read(self.state_dir / "installed-manifest.json")
        if raw is not None:
            version = str(_parse_manifest(raw).get("version", ""))
        return {
            "version": version,
            "previous": (self.state_dir / "previous-manifest.json").exists(),
            "matches_fixture": self.matches_fixture(),
        }

    def set_version(self, version: str) -> None:
        if not isinstance(version, str) or not VERSION_RE.fullmatch(version):
            raise ValueError("Version muss die Form v1.2.3 haben")
        self.state_dir.mkdir(parents=True, exist_ok=True)
        target = self.state_dir / "installed-manifest.json"
        if not target.exists():
            shutil.copyfile(self.fixture_dir / "installed-manifest.json", target)
        manifest = _parse_manifest(target.read_bytes())
        manifest["version"] = version
        manifest.setdefault("components", {})["dashboard"] = version
        handle, tmp = tempfile.mkstemp(dir=self.state_dir, prefix=".installed-", suffix=".tmp")
        with os.fdopen(handle, "w", encoding="utf-8") as out:
            json.dump(manifest, out, ensure_ascii=False, indent=2)
        os.replace(tmp, target)

    def set_previous(self, present: bool) -> None:
        target = self.state_dir / "previous-manifest.json"
        if present:
            self.state_dir.mkdir(parents=True, exist_ok=True)
            shutil.copyfile(self.fixture_dir / "previous-manifest.json", target)
        else:
            target.unlink(missing_ok=True)

    def remove_installed(self) -> None:
        (self.state_dir / "installed-manifest.json").unlink(missing_ok=True)

    def reset(self) -> None:
        self.state_dir.mkdir(parents=True, exist_ok=True)
        for name in INSTALLER_FILES:
            shutil.copyfile(self.fixture_dir / name, self.state_dir / name)


class UpdatesTab(Tab):
    name = "updates"
    title = "Updates"
    sources = ("updates",)

    def __init__(self, log: Log, updates_url: str, installer: InstallerState | None) -> None:
        self._log = log
        self._installer = installer
        self._remote = RemoteFake(f"{updates_url}/_fake", "updates", log)
        self._remote.start()
        port = updates_url.rstrip("/").rsplit(":", 1)[-1]
        self.html = updates_html(int(port) if port.isdigit() else None, installer is not None)

    def state(self) -> dict:
        try:
            release = self._remote.get("state")
        except OSError:
            release = None
        installer = self._installer.state() if self._installer is not None else None
        return {"reachable": release is not None, "release": release, "installer": installer}

    def act(self, action: str, body: dict) -> dict:
        if action not in ACTIONS:
            raise ValueError(f"unbekannte Aktion {action}")
        if action in ("release", "download"):
            try:
                self._remote.post(action, body)
            except OSError:
                raise ValueError(UNREACHABLE) from None
            return self.state()
        if self._installer is None:
            raise ValueError(NOT_SET_UP)
        try:
            if action == "installer_version":
                version = body.get("version", "")
                self._installer.set_version(version)
                self._log.add("updates", f"Installierte Version {version}")
            elif action == "installer_none":
                self._installer.remove_installed()
                self._log.add("updates", "Keine Installation")
            elif action == "installer_previous":
                present = bool(body.get("present"))
                self._installer.set_previous(present)
                self._log.add("updates", "Vorherige Version vorhanden" if present else "Vorherige Version entfernt")
            else:
                self._installer.reset()
                self._log.add("updates", "Installer-Zustand auf Fixture zurückgesetzt")
        except OSError:
            raise ValueError(NOT_WRITABLE) from None
        return self.state()

    def reset(self) -> None:
        if self._installer is not None:
            try:
                self._installer.reset()
            except OSError:
                raise ValueError(NOT_WRITABLE) from None
        try:
            self._remote.post("reset", {})
        except OSError:
            raise ValueError(UNREACHABLE) from None

    def summary(self) -> dict:
        try:
            release = self._remote.get("state")
        except OSError:
            return {"level": "error", "text": "nicht erreichbar"}

        if release["mode"] == "update":
            text = f"bietet {release['tag']}"
        else:
            text = MODE_TEXT[release["mode"]]
        installer_changed = False
        if self._installer is not None:
            try:
                installer_changed = not self._installer.state()["matches_fixture"]
            except ValueError:
                return {"level": "error", "text": NOT_READABLE}
        if installer_changed:
            text += ", Installer verändert"
        changed = (
            release["mode"] != "update"
            or release["tag"] != release["initial_tag"]
            or release["seconds"] != release["initial_seconds"]
            or release["abort"] != "never"
            or installer_changed
        )
        return {"level": "changed" if changed else "ok", "text": text}
