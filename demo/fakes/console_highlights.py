"""Reiter Highlights der Fake-Konsole.

Rendert die Highlights der Changelog-Abschnitte mit dem Vorschau-Skript
scripts/dev/highlight_details_preview.py: Dialog-Ansicht wie in den Neuerungen,
daneben das ganze Bild mit Ausschnittsrahmen. Die Seite liegt unter
/highlights/, die Bilder kommen aus docs/images des Repos unter /docs/images/.
Mit einem Installer-Zustand schreibt "Rendern" ausserdem den echten Changelog
dorthin (real_changelog.py), damit die Seite Neu erkunden des Dashboards ihn zeigt.
Die Oberflaeche steht in console_highlights_ui.py.
"""

import re
import subprocess
import sys
from pathlib import Path

from console_highlights_ui import HIGHLIGHTS_HTML
from fake_console import Log, Tab

PAGE_PATH = "/highlights/"
IMAGE_PREFIX = "/docs/images/"
CHANGELOG = Path("dashboard/CHANGELOG.md")
VERSION_RE = re.compile(r"v\d+\.\d+\.\d+")
TOP_VERSION_RE = re.compile(r"^## (v\d+\.\d+\.\d+)", re.M)
NOT_READABLE = "Changelog nicht lesbar"
NOT_SET_UP = "Installer-Zustand ist nicht eingerichtet"
NOT_WRITTEN = "Changelog für den Installer-Zustand nicht geschrieben"


class HighlightsTab(Tab):
    name = "highlights"
    title = "Highlights"
    html = HIGHLIGHTS_HTML

    def __init__(self, log: Log, repo: Path, installer_state: Path | None = None, dashboard: str = "") -> None:
        self.log = log
        self.repo = repo
        self.installer_state = installer_state
        self.dashboard = dashboard.rstrip("/")
        self.since = self._default_since()
        self.page: str | None = None
        self.count = 0

    def _preview(self):
        scripts = str(self.repo / "scripts" / "dev")
        if scripts not in sys.path:
            sys.path.insert(0, scripts)
        import highlight_details_preview
        return highlight_details_preview

    def _default_since(self) -> str:
        """Erste Version des aktuellen Minor-Zweigs, z. B. v0.10.0 bei v0.10.33."""
        try:
            text = (self.repo / CHANGELOG).read_text(encoding="utf-8")
        except OSError:
            return ""
        match = TOP_VERSION_RE.search(text)
        if not match:
            return ""
        major, minor, _ = match.group(1)[1:].split(".")
        return f"v{major}.{minor}.0"

    def state(self) -> dict:
        return {"since": self.since, "rendered": self.page is not None, "count": self.count,
                "explore_url": f"{self.dashboard}/?panel=settings&page=checklist&mode=explore" if self.dashboard and self.installer_state else ""}

    def act(self, action: str, body: dict) -> dict:
        if action != "render":
            return super().act(action, body)
        since = body.get("since", self.since)
        if not isinstance(since, str) or not VERSION_RE.fullmatch(since):
            raise ValueError("Version muss die Form v1.2.3 haben")
        try:
            preview = self._preview()
            # Bildpfade sind relativ zu <Repo>/highlights/, also ../docs/images/...
            self.page = preview.render(self.repo, self.repo / "highlights", since)
        except (OSError, ValueError) as exc:
            raise ValueError(f"{NOT_READABLE}: {exc}") from None
        self.since = since
        self.count += 1
        self.log.add("highlights", f"Highlights seit {since} gerendert")
        self._write_installer_changelog()
        return self.state()

    def _write_installer_changelog(self) -> None:
        state = self.installer_state
        if state is None:
            return
        if not (state / "installed-manifest.json").exists():
            raise ValueError(NOT_SET_UP)
        script = self.repo / "demo" / "fakes" / "real_changelog.py"
        result = subprocess.run([sys.executable, str(script), str(self.repo), str(state)],
                                capture_output=True, text=True)
        if result.returncode != 0:
            raise ValueError(f"{NOT_WRITTEN}: {result.stderr.strip()[-200:]}")
        self.log.add("highlights", "Changelog für Neu erkunden geschrieben")

    def summary(self) -> dict:
        if self.page is None:
            return {"level": "off", "text": "nicht gerendert"}
        return {"level": "ok", "text": f"seit {self.since}"}

    def reset(self) -> None:
        self.since = self._default_since()
        self.page = None

    def serve(self, path: str) -> tuple[str, bytes] | None:
        if path == PAGE_PATH and self.page is not None:
            return "text/html; charset=utf-8", self.page.encode("utf-8")
        if path.startswith(IMAGE_PREFIX):
            name = path[len(IMAGE_PREFIX):]
            images = (self.repo / "docs" / "images").resolve()
            target = (images / name).resolve()
            if target.parent == images and target.suffix == ".png" and target.is_file():
                return "image/png", target.read_bytes()
        return None
