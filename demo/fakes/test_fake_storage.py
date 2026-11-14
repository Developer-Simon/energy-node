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
UUID_SDE = "9c4e7a12-3b58-4d06-a1f9-6e2d8b0c5a37"
UUID_SDE1 = "1d6b3f84-7a20-4e95-8c13-b5f0a92e4d68"
UUID_SDE2 = "e3a85c07-92fd-4b41-b6d8-0a7c1f3e9b52"


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
    assert set(by_name) == {"sdb", "sdc", "sdd", "sde", "sde1", "sde2"}
    assert by_name["sdb"]["uuid"] == UUID_SDB
    assert by_name["sdb"]["fstype"] == "exfat"
    assert by_name["sdb"]["needs_format"] is True
    assert by_name["sdc"]["fstype"] == "ext4"
    assert by_name["sdc"]["needs_format"] is False
    assert "marker" not in by_name["sdc"]
    assert by_name["sdd"]["marker"] != MACHINE


def test_catalog_has_the_multi_partition_stick(catalog):
    by_name = {stick["kname"]: stick for stick in catalog}
    whole, first, second = by_name["sde"], by_name["sde1"], by_name["sde2"]
    assert whole["whole"] is True and whole["members"] == ["sde1", "sde2"]
    assert whole["name"] == "Intenso Installer" and whole["size"] == 34359738368
    assert whole["uuid"] == UUID_SDE and whole["fstype"] == "" and whole["needs_format"] is True
    assert whole["contents"] == "storage.contents.disk" and whole["transport"] == "usb"
    assert first["label"] == "INSTALL" and first["fstype"] == "iso9660" and first["disk"] == "sde"
    assert first["uuid"] == UUID_SDE1 and first["whole"] is False and first["needs_format"] is True
    assert second["label"] == "EFI" and second["fstype"] == "vfat" and second["disk"] == "sde"
    assert second["uuid"] == UUID_SDE2 and second["whole"] is False and second["needs_format"] is True
    assert first["disk_size"] == second["disk_size"] == whole["disk_size"] == 34359738368


def test_sdb_matches_the_fixture_of_the_dashboard_drive_list(catalog):
    fixture = json.loads((fake_storage.CATALOG_PATH.parent / "storage-drives.json").read_text(encoding="utf-8"))
    # The fixture is the unformatted stick, so its uuid is empty until the format.
    assert fixture == fake_storage.drive_list(catalog, fake_storage.DEFAULT_STATE)
    assert fixture[0]["disk"] == "sdb" and fixture[0]["whole"] is False and fixture[0]["label"] == ""


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


def names(root, catalog):
    return [d["kname"] for d in drives(root, catalog)]


def test_plugging_the_multi_partition_stick_lists_the_disk_then_its_partitions(storage, catalog):
    fake_storage.plug(storage, mount_of(storage), catalog, "sde", MACHINE)

    assert fake_storage.load_state(storage)["plugged"] == ["sdb", "sde", "sde1", "sde2"]
    listed = drives(storage, catalog)
    assert [d["kname"] for d in listed] == ["sdb", "sde", "sde1", "sde2"]
    whole, first, second = listed[1:]
    assert whole["whole"] is True and whole["needs_format"] is True and whole["uuid"] == ""
    assert whole["contents"] == "storage.contents.disk" and whole["disk"] == "sde"
    assert first["label"] == "INSTALL" and first["disk"] == "sde" and first["whole"] is False
    assert second["label"] == "EFI" and second["disk_size"] == 34359738368
    assert first["uuid"] == "" and second["uuid"] == ""


def test_list_fields_are_defaulted_for_single_sticks(storage, catalog):
    fake_storage.plug(storage, mount_of(storage), catalog, "sdc", MACHINE)

    sdc = next(d for d in drives(storage, catalog) if d["kname"] == "sdc")
    assert sdc["disk"] == "sdc" and sdc["disk_size"] == 31914983424
    assert sdc["label"] == "" and sdc["whole"] is False


def test_formatting_a_partition_keeps_the_sibling_untouched(storage, catalog):
    fake_storage.plug(storage, mount_of(storage), catalog, "sde", MACHINE)
    (storage / "disks" / "sde1").mkdir(parents=True)
    (storage / "disks" / "sde1" / "installer.txt").write_text("iso", encoding="utf-8")

    assert fake_storage.format_drive(storage, mount_of(storage), catalog, "sde2") == UUID_SDE2

    listed = drives(storage, catalog)
    first, second = listed[2], listed[3]
    assert first["kname"] == "sde1" and first["needs_format"] is True and first["uuid"] == ""
    assert second["kname"] == "sde2" and second["needs_format"] is False
    assert second["fstype"] == "ext4" and second["ready"] is True and second["uuid"] == UUID_SDE2
    assert second["whole"] is False
    assert (storage / "disks" / "sde1" / "installer.txt").read_text(encoding="utf-8") == "iso"


def test_formatting_the_whole_stick_leaves_one_ready_ext4_entry(storage, catalog):
    fake_storage.plug(storage, mount_of(storage), catalog, "sde", MACHINE)
    (storage / "disks" / "sde1").mkdir(parents=True)
    (storage / "disks" / "sde1" / "installer.txt").write_text("iso", encoding="utf-8")

    assert fake_storage.format_drive(storage, mount_of(storage), catalog, "sde") == UUID_SDE

    assert not (storage / "disks" / "sde1" / "installer.txt").exists()
    listed = drives(storage, catalog)
    assert [d["kname"] for d in listed] == ["sdb", "sde"]
    whole = listed[1]
    assert whole["whole"] is False and whole["label"] == "" and whole["fstype"] == "ext4"
    assert whole["ready"] is True and whole["needs_format"] is False and whole["uuid"] == UUID_SDE
    assert "members" not in whole


def test_mounting_a_partition_hides_the_whole_group(storage, catalog):
    fake_storage.plug(storage, mount_of(storage), catalog, "sde", MACHINE)
    fake_storage.format_drive(storage, mount_of(storage), catalog, "sde2")

    fake_storage.mount_drive(storage, mount_of(storage), catalog, UUID_SDE2, MACHINE)

    assert names(storage, catalog) == ["sdb"]
    state = fake_storage.load_state(storage)
    assert state["active"] == "sde2" and state["mounted"] is True
    assert (mount_of(storage) / ".energy-node-volume").read_text(encoding="utf-8").strip() == MACHINE


def test_pulling_the_multi_partition_stick_removes_all_three(storage, catalog):
    fake_storage.plug(storage, mount_of(storage), catalog, "sde", MACHINE)

    fake_storage.pull(storage, mount_of(storage), catalog, "sde")

    assert fake_storage.load_state(storage)["plugged"] == ["sdb"]
    assert names(storage, catalog) == ["sdb"]


def test_a_partition_cannot_be_plugged_or_pulled_alone(storage, catalog):
    with pytest.raises(StorageRefused):
        fake_storage.plug(storage, mount_of(storage), catalog, "sde1", MACHINE)
    fake_storage.plug(storage, mount_of(storage), catalog, "sde", MACHINE)
    with pytest.raises(StorageRefused):
        fake_storage.pull(storage, mount_of(storage), catalog, "sde2")
