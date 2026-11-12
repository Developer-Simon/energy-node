import json
from types import SimpleNamespace

import pytest

from console_storage import StorageTab
from console_storage_ui import storage_html
from fake_console import Log
from fake_storage import StorageError, format_drive, load_catalog, load_state, mount_drive

MACHINE = "machine-abc"
UUID_SDB = "6f1c2a4e-8b3d-4c5a-9e7f-2d4b6a8c0e1f"


@pytest.fixture
def paths(tmp_path):
    root = tmp_path / "storage"
    (root / "mount").mkdir(parents=True)
    return SimpleNamespace(root=root, mount=root / "mount")


@pytest.fixture
def log():
    return Log()


@pytest.fixture
def tab(paths, log):
    return StorageTab(log, paths.root, machine_id_value=MACHINE)


def entries(log):
    return [(e["source"], e["text"], e["level"]) for e in log.since(0)[1]]


def drive_names(paths):
    return [d["kname"] for d in json.loads((paths.root / "drives.json").read_text(encoding="utf-8"))]


def test_state_lists_the_sticks_and_the_start_state(tab):
    state = tab.state()

    by_kname = {stick["kname"]: stick for stick in state["sticks"]}
    assert set(by_kname) == {"sdb", "sdc", "sdd"}
    assert by_kname["sdb"]["plugged"] is True
    assert by_kname["sdc"]["plugged"] is False
    assert by_kname["sdd"]["foreign"] is True
    assert state["format_fails"] is False and state["mount_fails"] is False
    assert "sdb" in state["plugged_text"]
    assert state["mounted_text"] == "Nichts eingehängt"


def test_plugging_in_writes_the_drive_list_and_logs(tab, paths, log):
    tab.act("plug", {"kname": "sdc", "plugged": True})

    assert drive_names(paths) == ["sdb", "sdc"]
    assert entries(log) == [("storage", "sdc eingesteckt", "info")]


def test_pulling_the_active_drive_empties_the_mount(tab, paths):
    format_drive(paths.root, paths.mount, load_catalog(), "sdb")
    mount_drive(paths.root, paths.mount, load_catalog(), UUID_SDB, MACHINE)
    assert tab.state()["mounted_text"].startswith("Eingehängt: sdb")

    tab.act("plug", {"kname": "sdb", "plugged": False})

    assert list(paths.mount.iterdir()) == []
    assert drive_names(paths) == []
    assert tab.state()["mounted_text"] == "Nichts eingehängt"


def test_plugging_the_active_drive_back_restores_the_marker(tab, paths):
    format_drive(paths.root, paths.mount, load_catalog(), "sdb")
    mount_drive(paths.root, paths.mount, load_catalog(), UUID_SDB, MACHINE)
    tab.act("plug", {"kname": "sdb", "plugged": False})

    tab.act("plug", {"kname": "sdb", "plugged": True})

    assert (paths.mount / ".energy-node-volume").read_text(encoding="utf-8").strip() == MACHINE
    assert (paths.mount / "runtime").is_dir()
    assert "Kennung passt" in tab.state()["mounted_text"]


def test_foreign_stick_mounts_with_a_foreign_marker(tab, paths, log):
    tab.act("plug", {"kname": "sdd", "plugged": True})

    state = tab.state()
    assert "fremde Kennung" in state["mounted_text"]
    assert tab.summary() == {"level": "changed", "text": "fremder Stick eingehängt, Steckzustand geändert"}
    assert ("storage", "sdd eingesteckt, fremde Kennung eingehängt", "warn") in entries(log)


def test_faults_are_written_and_logged(tab, paths, log):
    tab.act("fault", {"name": "format", "on": True})
    tab.act("fault", {"name": "mount", "on": True})

    state = load_state(paths.root)
    assert state["format_fails"] is True and state["mount_fails"] is True
    assert tab.state()["format_fails"] is True
    assert ("storage", "Formatieren schlägt fehl: an", "info") in entries(log)

    with pytest.raises(StorageError):
        format_drive(paths.root, paths.mount, load_catalog(), "sdb")


def test_invalid_inputs_are_value_errors(tab):
    with pytest.raises(ValueError):
        tab.act("plug", {"kname": "sdz", "plugged": True})
    with pytest.raises(ValueError):
        tab.act("plug", {"kname": "sdc", "plugged": "ja"})
    with pytest.raises(ValueError):
        tab.act("plug", {"kname": "sdb", "plugged": True})
    with pytest.raises(ValueError):
        tab.act("plug", {"kname": "sdc", "plugged": False})
    with pytest.raises(ValueError):
        tab.act("fault", {"name": "power", "on": True})
    with pytest.raises(ValueError):
        tab.act("bogus", {})
    with pytest.raises(ValueError):
        tab.act("fault", "keine Liste")


def test_summary_texts(tab):
    assert tab.summary() == {"level": "ok", "text": "Speicher normal"}

    tab.act("fault", {"name": "mount", "on": True})

    assert tab.summary() == {"level": "changed", "text": "Einhängen schlägt fehl"}


def test_reset_returns_to_the_start(tab, paths):
    tab.act("plug", {"kname": "sdc", "plugged": True})
    tab.act("fault", {"name": "format", "on": True})

    tab.reset()

    assert drive_names(paths) == ["sdb"]
    assert load_state(paths.root)["format_fails"] is False
    assert tab.summary() == {"level": "ok", "text": "Speicher normal"}


def test_fragment_markers():
    html = storage_html(load_catalog())
    for marker in ('id="tab-storage"', "data-stick", "data-fault", "data-status-plugged",
                   "data-status-mounted", "Einstecken"):
        assert marker in html
        assert marker in StorageTab.html
