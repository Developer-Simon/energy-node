"""Tests for the demo drive model: plugging, mounting and the drive list the dashboard reads."""

import json
import sys
from pathlib import Path

import pytest

sys.path.insert(0, str(Path(__file__).resolve().parent))

import fake_storage  # noqa: E402
from fake_storage import StorageError, StorageRefused  # noqa: E402

MACHINE = "machine-abc"
UUID_SDB = "6f1c2a4e-8b3d-4c5a-9e7f-2d4b6a8c0e1f"
UUID_SDC = "2b7e9d10-4f3a-4c6e-8a51-9d0f7e3b2c48"
UUID_SDD = "c85a0e3f-61d2-4b8e-9f04-7a3c2e1d5b96"


@pytest.fixture
def storage(tmp_path):
    root = tmp_path / "storage"
    (root / "mount").mkdir(parents=True)
    return root


@pytest.fixture
def catalog():
    return fake_storage.load_catalog()


def mount_of(root):
    return root / "mount"


def drives(root, catalog):
    return json.loads((root / "drives.json").read_text(encoding="utf-8"))


def test_catalog_has_the_demo_sticks(catalog):
    by_name = {stick["kname"]: stick for stick in catalog}
    assert set(by_name) == {"sdb", "sdc", "sdd"}
    assert by_name["sdb"]["uuid"] == UUID_SDB
    assert by_name["sdb"]["fstype"] == "exfat"
    assert by_name["sdb"]["needs_format"] is True
    assert by_name["sdc"]["fstype"] == "ext4"
    assert by_name["sdc"]["needs_format"] is False
    assert "marker" not in by_name["sdc"]
    assert by_name["sdd"]["marker"] != MACHINE


def test_sdb_matches_the_fixture_of_the_dashboard_drive_list(catalog):
    fixture = json.loads((fake_storage.CATALOG_PATH.parent / "storage-drives.json").read_text(encoding="utf-8"))
    sdb = next(stick for stick in catalog if stick["kname"] == "sdb")
    # The fixture is the unformatted stick, so its uuid is empty until the format.
    assert fixture == [{key: ("" if key == "uuid" else sdb[key]) for key in fixture[0]}]


def test_default_state_plugs_only_the_demo_drive(storage, catalog):
    state = fake_storage.load_state(storage)

    assert state["plugged"] == ["sdb"]
    assert state["mounted"] is False
    assert [d["kname"] for d in fake_storage.drive_list(catalog, state)] == ["sdb"]


def test_unformatted_drive_is_not_mounted(storage, catalog):
    with pytest.raises(StorageError):
        fake_storage.mount_drive(storage, mount_of(storage), catalog, UUID_SDB, MACHINE)
    assert list(mount_of(storage).iterdir()) == []


def test_format_then_mount_creates_marker_and_runtime_dirs(storage, catalog):
    assert fake_storage.format_drive(storage, mount_of(storage), catalog, "sdb") == UUID_SDB
    fake_storage.mount_drive(storage, mount_of(storage), catalog, UUID_SDB, MACHINE)

    mount = mount_of(storage)
    assert (mount / ".energy-node-volume").read_text(encoding="utf-8").strip() == MACHINE
    assert (mount / "runtime").is_dir()
    assert (mount / "backups").is_dir()
    state = fake_storage.load_state(storage)
    assert state["mounted"] is True and state["active"] == "sdb"
    listed = drives(storage, catalog)[0]
    assert listed["needs_format"] is False and listed["uuid"] == UUID_SDB


def test_mount_fails_and_format_fails_leave_no_trace(storage, catalog):
    fake_storage.set_fault(storage, "format", True)
    with pytest.raises(StorageError):
        fake_storage.format_drive(storage, mount_of(storage), catalog, "sdb")
    assert fake_storage.load_state(storage)["formatted"] == []

    fake_storage.set_fault(storage, "format", False)
    fake_storage.format_drive(storage, mount_of(storage), catalog, "sdb")
    fake_storage.set_fault(storage, "mount", True)
    with pytest.raises(StorageError):
        fake_storage.mount_drive(storage, mount_of(storage), catalog, UUID_SDB, MACHINE)
    assert not (mount_of(storage) / ".energy-node-volume").exists()
    assert fake_storage.load_state(storage)["mounted"] is False


def test_unmount_removes_nothing(storage, catalog):
    fake_storage.format_drive(storage, mount_of(storage), catalog, "sdb")
    fake_storage.mount_drive(storage, mount_of(storage), catalog, UUID_SDB, MACHINE)

    fake_storage.unmount_drive(storage, mount_of(storage))

    assert (mount_of(storage) / ".energy-node-volume").exists()
    assert (mount_of(storage) / "runtime").is_dir()
    assert fake_storage.load_state(storage)["mounted"] is False


def test_pulling_the_active_drive_empties_the_mount_and_plugging_back_restores_it(storage, catalog):
    fake_storage.format_drive(storage, mount_of(storage), catalog, "sdb")
    fake_storage.mount_drive(storage, mount_of(storage), catalog, UUID_SDB, MACHINE)
    (mount_of(storage) / "runtime" / "state.json").write_text("{}", encoding="utf-8")

    fake_storage.pull(storage, mount_of(storage), catalog, "sdb")

    assert list(mount_of(storage).iterdir()) == []
    assert drives(storage, catalog) == []

    fake_storage.plug(storage, mount_of(storage), catalog, "sdb", MACHINE)

    assert (mount_of(storage) / ".energy-node-volume").read_text(encoding="utf-8").strip() == MACHINE
    assert (mount_of(storage) / "runtime" / "state.json").read_text(encoding="utf-8") == "{}"
    assert fake_storage.load_state(storage)["mounted"] is True


def test_pulling_an_idle_drive_keeps_the_mount(storage, catalog):
    fake_storage.plug(storage, mount_of(storage), catalog, "sdc", MACHINE)
    fake_storage.pull(storage, mount_of(storage), catalog, "sdc")

    assert fake_storage.load_state(storage)["plugged"] == ["sdb"]


def test_foreign_drive_takes_the_mount_with_its_own_marker(storage, catalog):
    fake_storage.format_drive(storage, mount_of(storage), catalog, "sdb")
    fake_storage.mount_drive(storage, mount_of(storage), catalog, UUID_SDB, MACHINE)

    fake_storage.plug(storage, mount_of(storage), catalog, "sdd", MACHINE)

    marker = (mount_of(storage) / ".energy-node-volume").read_text(encoding="utf-8").strip()
    assert marker == "fremde-kennung-4711"
    state = fake_storage.load_state(storage)
    assert state["active"] == "sdd" and state["mounted"] is True
    # The previous drive went back onto its own stick, it is not lost.
    assert (storage / "disks" / "sdb" / ".energy-node-volume").exists()


def test_format_wipes_the_stick_and_mount_of_the_active_drive(storage, catalog):
    fake_storage.format_drive(storage, mount_of(storage), catalog, "sdb")
    fake_storage.mount_drive(storage, mount_of(storage), catalog, UUID_SDB, MACHINE)

    fake_storage.format_drive(storage, mount_of(storage), catalog, "sdb")

    assert list(mount_of(storage).iterdir()) == []
    assert fake_storage.load_state(storage)["active"] == ""


def test_format_of_an_unplugged_or_unknown_stick(storage, catalog):
    with pytest.raises(StorageError):
        fake_storage.format_drive(storage, mount_of(storage), catalog, "sdc")
    with pytest.raises(StorageRefused):
        fake_storage.format_drive(storage, mount_of(storage), catalog, "sdz")
    with pytest.raises(StorageRefused):
        fake_storage.mount_drive(storage, mount_of(storage), catalog, "00000000-0000-0000-0000-000000000000", MACHINE)


def test_reset_returns_to_the_start_state(storage, catalog):
    fake_storage.plug(storage, mount_of(storage), catalog, "sdd", MACHINE)
    fake_storage.set_fault(storage, "mount", True)

    fake_storage.reset(storage, mount_of(storage), catalog)

    assert fake_storage.load_state(storage) == fake_storage.DEFAULT_STATE
    assert [d["kname"] for d in drives(storage, catalog)] == ["sdb"]
    assert list(mount_of(storage).iterdir()) == []


def test_replace_swaps_the_mount_and_keeps_each_stick_apart(storage, catalog):
    # The sdb -> sdc replace: unmount the old stick, then mount the new uuid on the same point.
    fake_storage.format_drive(storage, mount_of(storage), catalog, "sdb")
    fake_storage.mount_drive(storage, mount_of(storage), catalog, UUID_SDB, MACHINE)
    (mount_of(storage) / "runtime" / "state.json").write_text("sdb-data", encoding="utf-8")
    fake_storage.plug(storage, mount_of(storage), catalog, "sdc", MACHINE)

    fake_storage.unmount_drive(storage, mount_of(storage))
    fake_storage.mount_drive(storage, mount_of(storage), catalog, UUID_SDC, MACHINE)

    mount = mount_of(storage)
    assert (mount / ".energy-node-volume").read_text(encoding="utf-8").strip() == MACHINE
    assert list((mount / "runtime").iterdir()) == []
    assert (storage / "disks" / "sdb" / "runtime" / "state.json").read_text(encoding="utf-8") == "sdb-data"
    state = fake_storage.load_state(storage)
    assert state["active"] == "sdc" and state["mounted"] is True

    (mount / "runtime" / "state.json").write_text("sdc-data", encoding="utf-8")
    fake_storage.unmount_drive(storage, mount)
    fake_storage.mount_drive(storage, mount, catalog, UUID_SDB, MACHINE)

    assert (mount / "runtime" / "state.json").read_text(encoding="utf-8") == "sdb-data"
    assert (storage / "disks" / "sdc" / "runtime" / "state.json").read_text(encoding="utf-8") == "sdc-data"


def test_replace_from_a_missing_drive_keeps_its_data_on_its_stick(storage, catalog):
    # The old stick is pulled while mounted: the mount is empty, its data stays on the stick.
    fake_storage.format_drive(storage, mount_of(storage), catalog, "sdb")
    fake_storage.mount_drive(storage, mount_of(storage), catalog, UUID_SDB, MACHINE)
    (mount_of(storage) / "runtime" / "state.json").write_text("sdb-data", encoding="utf-8")
    fake_storage.pull(storage, mount_of(storage), catalog, "sdb")
    fake_storage.plug(storage, mount_of(storage), catalog, "sdc", MACHINE)

    fake_storage.mount_drive(storage, mount_of(storage), catalog, UUID_SDC, MACHINE)

    assert (mount_of(storage) / ".energy-node-volume").read_text(encoding="utf-8").strip() == MACHINE
    assert (storage / "disks" / "sdb" / "runtime" / "state.json").read_text(encoding="utf-8") == "sdb-data"
    assert fake_storage.load_state(storage)["active"] == "sdc"


def test_failed_mount_of_the_new_stick_leaves_the_old_one_mountable(storage, catalog):
    # Replace undoes a failed mount by mounting the old uuid again: its data must come back.
    fake_storage.format_drive(storage, mount_of(storage), catalog, "sdb")
    fake_storage.mount_drive(storage, mount_of(storage), catalog, UUID_SDB, MACHINE)
    (mount_of(storage) / "runtime" / "state.json").write_text("sdb-data", encoding="utf-8")
    fake_storage.plug(storage, mount_of(storage), catalog, "sdc", MACHINE)
    fake_storage.unmount_drive(storage, mount_of(storage))
    fake_storage.set_fault(storage, "mount", True)
    with pytest.raises(StorageError):
        fake_storage.mount_drive(storage, mount_of(storage), catalog, UUID_SDC, MACHINE)
    fake_storage.set_fault(storage, "mount", False)

    fake_storage.mount_drive(storage, mount_of(storage), catalog, UUID_SDB, MACHINE)

    assert (mount_of(storage) / "runtime" / "state.json").read_text(encoding="utf-8") == "sdb-data"
    assert fake_storage.load_state(storage)["active"] == "sdb"
