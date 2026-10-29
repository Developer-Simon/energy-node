import json
import time
from types import SimpleNamespace

import pytest

from console_system import SystemTab
from fake_console import Log
from fake_journal_helper import DEFAULT_STATE, journal_mode, load_state


@pytest.fixture
def paths(tmp_path):
    epoch = tmp_path / "epoch"
    epoch.write_text(str(time.time() - 1000))
    return SimpleNamespace(dir=tmp_path, state=tmp_path / "state.json",
                           events=tmp_path / "events.jsonl", epoch=epoch)


@pytest.fixture
def log():
    return Log()


@pytest.fixture
def tab(paths, log):
    return SystemTab(log, paths.state, paths.events, paths.epoch)


def entries(log):
    return [(e["source"], e["text"], e["level"]) for e in log.since(0)[1]]


def test_journal_mode_is_written_atomically_and_read_by_helper(tab, paths):
    tab.act("journal", {"key": "dashboard", "mode": "errors"})

    assert load_state(paths.state)["journal"]["dashboard"] == "errors"
    assert list(paths.dir.glob("*.tmp")) == []


def test_invalid_inputs_are_value_errors(tab):
    with pytest.raises(ValueError):
        tab.act("journal", {"key": "quatsch", "mode": "errors"})
    with pytest.raises(ValueError):
        tab.act("journal", {"key": "dashboard", "mode": "x"})
    with pytest.raises(ValueError):
        tab.act("inject", {"key": "dashboard", "message": "", "priority": "error"})
    with pytest.raises(ValueError):
        tab.act("inject", {"key": "dashboard", "message": "x" * 501, "priority": "error"})
    with pytest.raises(ValueError):
        tab.act("inject", {"key": "dashboard", "message": "ok", "priority": "warn"})


def test_unwritable_state_is_value_error(tmp_path, log, paths):
    tab = SystemTab(log, tmp_path / "missing" / "state.json", paths.events, paths.epoch)

    with pytest.raises(ValueError):
        tab.act("helper", {"mode": "denied"})


def test_inject_picks_next_free_index(tab, paths):
    tab.act("inject", {"key": "dashboard", "message": "eins", "priority": "error"})
    tab.act("inject", {"key": "dashboard", "message": "zwei", "priority": "info"})

    indexes = [item["index"] for item in load_state(paths.state)["inject"]]
    assert indexes[1] == indexes[0] + 1
    assert indexes[0] >= 1000


def test_pump_events_logs_labels_and_levels(tab, paths, log):
    lines = [
        {"t": 1, "action": "reboot", "args": [], "result": "ok"},
        {"t": 2, "action": "read-journal", "args": ["dashboard", "15", "100"], "result": "denied"},
    ]
    paths.events.write_text("".join(json.dumps(line) + "\n" for line in lines))

    tab.pump_events()

    assert entries(log) == [("system", "Neustart, Erfolg", "info"),
                            ("system", "Log lesen, verweigert", "warn")]
    assert tab.state()["recent"][0] == {"label": "Log lesen", "result": "denied", "t": 2}


def test_pump_events_keeps_partial_line(tab, paths, log):
    first = json.dumps({"t": 1, "action": "reboot", "args": [], "result": "ok"})
    paths.events.write_text(first + "\n" + '{"t": 2, "act')

    tab.pump_events()
    assert entries(log) == [("system", "Neustart, Erfolg", "info")]

    with paths.events.open("a", encoding="utf-8") as handle:
        handle.write('ion": "reboot", "args": [], "result": "ok"}\n')
    tab.pump_events()

    assert len(entries(log)) == 2


def test_summary_texts(tab):
    assert tab.summary() == {"level": "ok", "text": "Logs normal"}

    tab.act("helper", {"mode": "denied"})

    assert tab.summary() == {"level": "changed", "text": "Helper verweigert"}


def test_default_modes_count_as_unchanged(tab):
    tab.act("journal", {"key": "caddy", "mode": "empty"})

    assert tab.summary()["level"] == "ok"

    tab.act("journal", {"key": "dashboard", "mode": "errors"})

    assert tab.summary() == {"level": "changed", "text": "1 Dienst verändert"}


def test_reset_writes_default_state(tab, paths):
    tab.act("helper", {"mode": "hang"})

    tab.reset()

    assert json.loads(paths.state.read_text(encoding="utf-8")) == DEFAULT_STATE
    assert journal_mode(load_state(paths.state), "caddy") == "empty"


def test_fragment_markers():
    for marker in ('id="tab-system"', "data-journal", "data-inject-key", "data-helper",
                   "data-actions", "Zeile einfügen"):
        assert marker in SystemTab.html
