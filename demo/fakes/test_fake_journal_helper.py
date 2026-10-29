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
