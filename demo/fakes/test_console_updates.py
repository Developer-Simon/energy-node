import http.server
import json
import shutil
import socket
import threading
from pathlib import Path

import pytest

import fake_github_releases as fgr
from console_updates import INSTALLER_FILES, InstallerState, UpdatesTab
from console_updates_ui import updates_html
from fake_console import Log

FIXTURE = Path(__file__).resolve().parents[1] / "fixtures" / "installed-state"


@pytest.fixture
def release_state(tmp_path):
    archive = tmp_path / "energy-node-v9.9.9-amd64.tar.gz"
    archive.write_bytes(b"x" * 100)
    return fgr.ReleaseState("v9.9.9", str(archive), 0)


@pytest.fixture
def github(release_state):
    httpd = http.server.ThreadingHTTPServer(("127.0.0.1", 0), fgr.make_handler(release_state))
    threading.Thread(target=httpd.serve_forever, daemon=True).start()
    yield httpd.server_address[1]
    httpd.shutdown()
    httpd.server_close()


@pytest.fixture
def installer(tmp_path):
    state_dir = tmp_path / "installer-state"
    shutil.copytree(FIXTURE, state_dir)
    shutil.copytree(FIXTURE, tmp_path / "fixture")
    return InstallerState(state_dir, tmp_path / "fixture")


@pytest.fixture
def tab(github, installer):
    return UpdatesTab(Log(), f"http://127.0.0.1:{github}", installer)


def test_set_version_writes_manifest_and_component(installer):
    installer.set_version("v0.9.0")
    state = installer.state()
    assert state["version"] == "v0.9.0"
    assert state["matches_fixture"] is False
    manifest = json.loads((installer.state_dir / "installed-manifest.json").read_text())
    assert manifest["version"] == "v0.9.0"
    assert manifest["components"]["dashboard"] == "v0.9.0"


def test_invalid_version_is_value_error(installer):
    with pytest.raises(ValueError, match="Version muss die Form v1.2.3 haben"):
        installer.set_version("0.9.0")


def test_remove_and_previous_toggle(installer):
    installer.remove_installed()
    assert installer.state()["version"] == ""
    installer.set_previous(False)
    assert installer.state()["previous"] is False
    installer.set_previous(True)
    assert installer.state()["previous"] is True


def test_reset_restores_fixture(installer):
    installer.set_version("v0.9.0")
    installer.reset()
    assert installer.state()["matches_fixture"] is True
    for name in INSTALLER_FILES:
        assert (installer.state_dir / name).read_bytes() == (FIXTURE / name).read_bytes()


def test_summary_texts(tab, release_state, installer):
    assert tab.summary() == {"level": "ok", "text": "bietet v9.9.9"}
    tab.act("release", {"mode": "none"})
    assert tab.summary() == {"level": "changed", "text": "kein Update"}
    tab.act("installer_version", {"version": "v0.9.0"})
    assert tab.summary() == {"level": "changed", "text": "kein Update, Installer verändert"}


def test_unreachable_fake_is_error_level_and_value_error(installer):
    probe = socket.socket()
    probe.bind(("127.0.0.1", 0))
    port = probe.getsockname()[1]
    probe.close()
    tab = UpdatesTab(Log(), f"http://127.0.0.1:{port}", installer)
    assert tab.summary() == {"level": "error", "text": "nicht erreichbar"}
    assert tab.state()["reachable"] is False
    with pytest.raises(ValueError, match="GitHub-Fake nicht erreichbar"):
        tab.act("release", {"mode": "none"})


def test_installer_actions_without_installer_are_value_errors(github):
    tab = UpdatesTab(Log(), f"http://127.0.0.1:{github}", None)
    for action in ("installer_version", "installer_none", "installer_previous", "installer_reset"):
        with pytest.raises(ValueError, match="Installer-Zustand ist nicht eingerichtet"):
            tab.act(action, {})
    assert tab.state()["installer"] is None


def test_reset_resets_fake_and_installer(tab, release_state, installer):
    tab.act("release", {"mode": "error"})
    tab.act("installer_version", {"version": "v0.9.0"})
    tab.reset()
    assert release_state.state()["mode"] == "update"
    assert installer.state()["matches_fixture"] is True


def test_fragment_markers(installer):
    html = updates_html(18284, installer is not None)
    for marker in ('id="tab-updates"', "data-tag", "data-mode", "data-seconds",
                   "data-abort", "data-installed", "data-previous"):
        assert marker in html
    assert 'data-group="installer"' not in updates_html(18284, False)
