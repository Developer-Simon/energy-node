"""Tests for the fake system helper: state file, event file and system actions."""

import json
import os
import subprocess
import sys
import time
from pathlib import Path

import pytest

sys.path.insert(0, str(Path(__file__).resolve().parent))

import fake_journal_helper  # noqa: E402
import fake_storage  # noqa: E402

HELPER = str(Path(__file__).resolve().parent / "fake_journal_helper.py")


@pytest.fixture
def epoch_env(tmp_path):
    epoch_file = tmp_path / "epoch"
    epoch_file.write_text(str(time.time() - 120))
    return {"FAKE_JOURNAL_EPOCH": str(epoch_file), "PATH": os.environ.get("PATH", "")}


def run_helper(env, *args):
    return subprocess.run([sys.executable, HELPER, *args], env=env, capture_output=True, text=True)


def write_state(tmp_path, state):
    path = tmp_path / "state.json"
    path.write_text(json.dumps(state))
    return str(path)


def read_events(path):
    if not Path(path).exists():
        return []
    return [json.loads(line) for line in Path(path).read_text().splitlines() if line.strip()]


def with_state(env, state_path, events_path=None):
    env = dict(env, FAKE_SYSTEM_STATE=state_path)
    if events_path is not None:
        env["FAKE_SYSTEM_EVENTS"] = events_path
    return env


def test_without_state_file_output_is_unchanged(tmp_path, epoch_env):
    result = run_helper(epoch_env, "read-journal", "dashboard", "15", "100")
    assert result.returncode == 0
    assert len(result.stdout.splitlines()) > 0
    caddy = run_helper(epoch_env, "read-journal", "caddy", "15", "100")
    assert caddy.returncode == 0
    assert caddy.stdout == ""


def test_broken_state_file_falls_back_to_defaults(tmp_path, epoch_env):
    broken = tmp_path / "state.json"
    broken.write_text('{"journal": ')
    plain = run_helper(epoch_env, "read-journal", "dashboard", "15", "100")
    with_broken = run_helper(with_state(epoch_env, str(broken)), "read-journal", "dashboard", "15", "100")
    assert with_broken.returncode == 0
    assert with_broken.stdout == plain.stdout


def test_errors_mode_sets_priority_3(tmp_path, epoch_env):
    state = write_state(tmp_path, {"journal": {"dashboard": "errors"}})
    result = run_helper(with_state(epoch_env, state), "read-journal", "dashboard", "15", "100")
    assert result.returncode == 0
    rows = [json.loads(line) for line in result.stdout.splitlines()]
    assert rows
    assert all(row["PRIORITY"] == "3" for row in rows)
    assert all(row["MESSAGE"] for row in rows)


def test_empty_mode_prints_nothing(tmp_path, epoch_env):
    state = write_state(tmp_path, {"journal": {"dashboard": "empty"}})
    result = run_helper(with_state(epoch_env, state), "read-journal", "dashboard", "15", "100")
    assert result.returncode == 0
    assert result.stdout == ""


def test_inject_replaces_line_at_index(tmp_path, epoch_env):
    state = write_state(tmp_path, {"inject": [
        {"key": "dashboard", "index": 100, "priority": 3, "message": "Testfehler"},
    ]})
    result = run_helper(with_state(epoch_env, state), "read-journal", "dashboard", "15", "100")
    assert result.returncode == 0
    rows = [json.loads(line) for line in result.stdout.splitlines()]
    matching = [row for row in rows if row["__CURSOR"] == "s=100"]
    assert len(matching) == 1
    assert matching[0]["MESSAGE"] == "Testfehler (#100)"
    assert matching[0]["PRIORITY"] == "3"


def test_denied_writes_sudo_message_and_event(tmp_path, epoch_env):
    state = write_state(tmp_path, {"helper": "denied"})
    events = str(tmp_path / "events.jsonl")
    result = run_helper(with_state(epoch_env, state, events), "read-journal", "dashboard", "15", "100")
    assert result.returncode == 1
    assert result.stderr.startswith("sudo:")
    recorded = read_events(events)
    assert len(recorded) == 1
    assert recorded[0]["action"] == "read-journal"
    assert recorded[0]["result"] == "denied"


def test_fail_mode_exit_1(tmp_path, epoch_env):
    state = write_state(tmp_path, {"helper": "fail"})
    events = str(tmp_path / "events.jsonl")
    result = run_helper(with_state(epoch_env, state, events), "read-journal", "dashboard", "15", "100")
    assert result.returncode == 1
    assert "simulated failure" in result.stderr
    assert read_events(events)[-1]["result"] == "fail"


def test_system_action_ok_and_fail_are_recorded(tmp_path, epoch_env):
    events = str(tmp_path / "events.jsonl")
    ok_state = write_state(tmp_path, {"actions": "ok"})
    ok = run_helper(with_state(epoch_env, ok_state, events), "reboot")
    assert ok.returncode == 0
    assert read_events(events)[-1]["action"] == "reboot"
    assert read_events(events)[-1]["result"] == "ok"

    fail_state = write_state(tmp_path, {"actions": "fail"})
    fail = run_helper(with_state(epoch_env, fail_state, events), "reboot")
    assert fail.returncode == 1
    assert read_events(events)[-1]["result"] == "fail"


def test_normal_read_journal_writes_no_event(tmp_path, epoch_env):
    state = write_state(tmp_path, {})
    events = str(tmp_path / "events.jsonl")
    result = run_helper(with_state(epoch_env, state, events), "read-journal", "dashboard", "15", "100")
    assert result.returncode == 0
    assert read_events(events) == []


def test_unknown_action_still_exits_64(tmp_path, epoch_env):
    result = run_helper(epoch_env, "no-such-action")
    assert result.returncode == 64


def test_import_does_not_run_main():
    assert hasattr(fake_journal_helper, "main")


def test_hang_mode_records_event_and_continues(tmp_path, epoch_env, monkeypatch, capsys):
    state = write_state(tmp_path, {"helper": "hang"})
    events = str(tmp_path / "events.jsonl")
    monkeypatch.setenv("FAKE_SYSTEM_STATE", state)
    monkeypatch.setenv("FAKE_SYSTEM_EVENTS", events)
    monkeypatch.setenv("FAKE_JOURNAL_EPOCH", epoch_env["FAKE_JOURNAL_EPOCH"])
    monkeypatch.setattr(fake_journal_helper, "HANG_SECONDS", 0)
    code = fake_journal_helper.main(["read-journal", "dashboard", "15", "100"])
    assert code == 0
    assert read_events(events)[0]["result"] == "hang"


def test_system_action_without_state_file_still_exits_64(epoch_env):
    result = run_helper(epoch_env, "reboot")
    assert result.returncode == 64


@pytest.mark.parametrize("action", ["stop-services", "start-services", "restart-dashboard"])
def test_restore_verbs_succeed_without_state_and_are_recorded(tmp_path, epoch_env, action):
    events = str(tmp_path / "events.jsonl")
    env = dict(epoch_env, FAKE_SYSTEM_EVENTS=events)
    result = run_helper(env, action)
    assert result.returncode == 0
    assert read_events(events)[-1]["action"] == action
    assert read_events(events)[-1]["result"] == "ok"


@pytest.mark.parametrize("action", [
    "reboot", "poweroff", "apply-bridge-config", "apply-app-config",
    "restart-mosquitto", "tailscale-up", "tailscale-logout", "tailscale-restart",
])
def test_other_system_actions_still_exit_64_without_state(epoch_env, action):
    result = run_helper(epoch_env, action)
    assert result.returncode == 64


def test_restore_verbs_follow_actions_fail_with_state(tmp_path, epoch_env):
    state = write_state(tmp_path, {"actions": "fail"})
    events = str(tmp_path / "events.jsonl")
    for action in ("stop-services", "start-services", "restart-dashboard"):
        result = run_helper(with_state(epoch_env, state, events), action)
        assert result.returncode == 1
        assert read_events(events)[-1]["result"] == "fail"


def test_restore_verbs_denied_with_state(tmp_path, epoch_env):
    state = write_state(tmp_path, {"helper": "denied"})
    result = run_helper(with_state(epoch_env, state), "stop-services")
    assert result.returncode == 1
    assert result.stderr.startswith("sudo:")


def test_storage_format_prints_the_uuid_and_mount_writes_the_marker(tmp_path, epoch_env):
    env = {**epoch_env, "FAKE_STORAGE_MOUNT": str(tmp_path / "mount")}
    result = run_helper(env, "storage", "format", "sdb")
    assert result.returncode == 0, result.stderr
    assert result.stdout.strip() == fake_journal_helper.STORAGE_DRIVE_UUID
    assert not (tmp_path / "mount" / ".energy-node-volume").exists()

    result = run_helper(env, "storage", "mount", fake_journal_helper.STORAGE_DRIVE_UUID)
    assert result.returncode == 0, result.stderr
    marker = tmp_path / "mount" / ".energy-node-volume"
    assert marker.read_text().strip() == fake_journal_helper.machine_id()
    assert (tmp_path / "mount" / "runtime").is_dir()
    assert (tmp_path / "mount" / "backups").is_dir()


def test_storage_refuses_bad_disk_names_and_unknown_uuids(tmp_path, epoch_env):
    env = {**epoch_env, "FAKE_STORAGE_MOUNT": str(tmp_path / "mount")}
    assert run_helper(env, "storage", "format", "../sdb").returncode == 64
    assert run_helper(env, "storage", "mount", "00000000-0000-0000-0000-000000000000").returncode == 64
    assert run_helper(env, "storage", "bogus").returncode == 64
    assert not (tmp_path / "mount").exists()


def test_storage_mount_and_unmount_work_on_the_demo_drive(tmp_path, epoch_env):
    env = {**epoch_env, "FAKE_STORAGE_MOUNT": str(tmp_path / "mount")}
    uuid = fake_journal_helper.STORAGE_DRIVE_UUID
    assert run_helper(env, "storage", "format", "sdb").returncode == 0
    assert run_helper(env, "storage", "mount", uuid).returncode == 0
    assert run_helper(env, "storage", "unmount").returncode == 0
    assert (tmp_path / "mount" / ".energy-node-volume").exists()


def test_storage_mount_refuses_an_unformatted_drive_and_a_failing_fault(tmp_path, epoch_env):
    env = {**epoch_env, "FAKE_STORAGE_MOUNT": str(tmp_path / "mount")}
    uuid = fake_journal_helper.STORAGE_DRIVE_UUID
    assert run_helper(env, "storage", "mount", uuid).returncode == 1
    assert run_helper(env, "storage", "format", "sdb").returncode == 0
    fake_storage.set_fault(tmp_path, "mount", True)
    result = run_helper(env, "storage", "mount", uuid)
    assert result.returncode == 1
    assert "Einhängen" in result.stderr
    fake_storage.set_fault(tmp_path, "mount", False)
    fake_storage.set_fault(tmp_path, "format", True)
    assert run_helper(env, "storage", "format", "sdb").returncode == 1


def test_apply_app_config_copies_the_staged_file_to_the_config(tmp_path, epoch_env):
    staged = tmp_path / "staged.json"
    target = tmp_path / "config.json"
    staged.write_text('{"paths": {"runtime_dir": "/x"}}\n')
    target.write_text("{}\n")
    state = write_state(tmp_path, {"actions": "ok"})
    events = tmp_path / "events.jsonl"
    env = {**epoch_env, "FAKE_SYSTEM_STATE": state, "FAKE_SYSTEM_EVENTS": str(events),
           "FAKE_STAGED_CONFIG": str(staged), "FAKE_APP_CONFIG_TARGET": str(target)}
    result = run_helper(env, "apply-app-config")
    assert result.returncode == 0, result.stderr
    assert target.read_text() == staged.read_text()
    assert read_events(events)[-1]["result"] == "ok"


def test_restart_dashboard_leaves_the_request_file_for_the_supervisor(tmp_path, epoch_env):
    request = tmp_path / "restart-request"
    state = write_state(tmp_path, {"actions": "ok"})
    env = {**epoch_env, "FAKE_SYSTEM_STATE": state, "FAKE_RESTART_REQUEST": str(request)}
    result = run_helper(env, "restart-dashboard")
    assert result.returncode == 0, result.stderr
    assert request.read_text() == "restart\n"


def test_restart_dashboard_without_request_path_stays_a_noop(tmp_path, epoch_env):
    result = run_helper(epoch_env, "restart-dashboard")
    assert result.returncode == 0, result.stderr
    assert not (tmp_path / "restart-request").exists()


def test_storage_unmount_then_mount_another_uuid_swaps_the_drive(tmp_path, epoch_env):
    # What a replace sends to the helper: unmount, then mount the new uuid on the same point.
    env = {**epoch_env, "FAKE_STORAGE_MOUNT": str(tmp_path / "mount")}
    sdb = fake_journal_helper.STORAGE_DRIVE_UUID
    sdc = "2b7e9d10-4f3a-4c6e-8a51-9d0f7e3b2c48"
    assert run_helper(env, "storage", "format", "sdb").returncode == 0
    assert run_helper(env, "storage", "mount", sdb).returncode == 0
    (tmp_path / "mount" / "runtime" / "state.json").write_text("sdb-data")
    fake_storage.plug(tmp_path, tmp_path / "mount", fake_storage.load_catalog(), "sdc", fake_journal_helper.machine_id())

    assert run_helper(env, "storage", "unmount").returncode == 0
    result = run_helper(env, "storage", "mount", sdc)
    assert result.returncode == 0, result.stderr

    assert list((tmp_path / "mount" / "runtime").iterdir()) == []
    assert (tmp_path / "disks" / "sdb" / "runtime" / "state.json").read_text() == "sdb-data"
    assert run_helper(env, "storage", "unmount").returncode == 0
    assert run_helper(env, "storage", "mount", sdb).returncode == 0
    assert (tmp_path / "mount" / "runtime" / "state.json").read_text() == "sdb-data"
