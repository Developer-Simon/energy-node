import threading
import time

import pytest

from conftest import STUB_CODE
from console_companion import ACTIONS, CompanionTab
from fake_console import Log
from fake_companion import ROLES


def wait_until(predicate, timeout=5.0):
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        if predicate():
            return
        time.sleep(0.02)
    raise AssertionError("Bedingung nicht erreicht")


@pytest.fixture
def make_tab(stub_dashboard, stub_exchange):
    created = []

    def make():
        log = Log()
        tab = CompanionTab(log, stub_dashboard.url, stub_exchange.url)
        created.append(tab)
        return tab, log

    yield make
    for tab in created:
        try:
            tab.act("unpair", {})
        except ValueError:
            pass
        tab.act("history_stop", {})


def texts(log):
    return [entry["text"] for entry in log.since(0)[1]]


def test_state_has_ha_and_history_sections(make_tab):
    tab, _ = make_tab()

    state = tab.state()

    assert set(state) == {"ha", "history"}
    assert state["ha"]["paired"] is False
    assert state["history"]["running"] is False


def test_pair_action_pairs_and_reports_state(make_tab):
    tab, _ = make_tab()

    state = tab.act("pair", {"code": STUB_CODE})

    assert state["ha"]["paired"] is True
    assert state["ha"]["users"]


def test_pair_error_becomes_value_error_and_log_entry(make_tab):
    tab, log = make_tab()

    with pytest.raises(ValueError) as caught:
        tab.act("pair", {"code": "AAAA-BBBB-CCCC"})

    assert "HTTP 403" in str(caught.value)
    assert any("HTTP 403" in text for text in texts(log))
    assert tab.state()["ha"]["paired"] is False


def test_user_save_generates_id_and_requires_username(make_tab):
    tab, _ = make_tab()

    state = tab.act("user_save", {
        "ha_user_id": "", "username": "lena", "password": "lena-pass-1",
        "name": "", "admin": False, "active": True})

    lena = [user for user in state["ha"]["users"] if user["username"] == "lena"]
    assert lena == [{
        "ha_user_id": "ha-lena", "username": "lena", "password": "lena-pass-1",
        "name": "lena", "admin": False, "active": True}]

    with pytest.raises(ValueError, match="Benutzername fehlt"):
        tab.act("user_save", {"ha_user_id": "", "username": "", "password": "x",
                              "name": "", "admin": False, "active": True})


def test_history_actions_validate_ranges(make_tab):
    tab, _ = make_tab()

    with pytest.raises(ValueError):
        tab.act("history_start", {"days": 0})
    with pytest.raises(ValueError):
        tab.act("history_start", {"days": 801})
    with pytest.raises(ValueError):
        tab.act("history_scale", {"role": "pv", "factor": -1})
    with pytest.raises(ValueError):
        tab.act("history_scale", {"role": "pv", "factor": 101})
    with pytest.raises(ValueError):
        tab.act("history_scale", {"role": "unbekannt", "factor": 1})
    with pytest.raises(ValueError):
        tab.act("history_faults", {"delay_ms": 60001, "silent": False, "partial": False})
    assert tab.state()["history"]["running"] is False


def test_history_scale_and_faults_reach_the_peer(make_tab, stub_exchange):
    tab, _ = make_tab()
    tab.act("history_start", {"days": 3})
    wait_until(lambda: stub_exchange.offers)

    state = tab.act("history_scale", {"role": "pv", "factor": 2.5})
    assert state["history"]["scale"]["pv"] == 2.5
    assert set(state["history"]["scale"]) == set(ROLES)

    faults = {"delay_ms": 250, "silent": True, "partial": False}
    state = tab.act("history_faults", faults)
    assert state["history"]["faults"] == faults

    tab.act("history_faults", {"delay_ms": 0, "silent": False, "partial": False})
    tab.act("history_stop", {})
    assert tab.state()["history"]["running"] is False


def test_concurrent_user_saves_keep_table_consistent(make_tab):
    tab, _ = make_tab()
    errors = []

    def save(index):
        try:
            tab.act("user_save", {"ha_user_id": "", "username": f"gast{index}",
                                  "password": "x", "name": "", "admin": False, "active": True})
        except Exception as error:  # noqa: BLE001
            errors.append(error)

    threads = [threading.Thread(target=save, args=(index,)) for index in range(20)]
    for thread in threads:
        thread.start()
    for thread in threads:
        thread.join(5)

    assert errors == []
    usernames = {user["username"] for user in tab.state()["ha"]["users"]}
    assert usernames >= {"maria", "tom"} | {f"gast{index}" for index in range(20)}


def test_unknown_action_is_value_error(make_tab):
    tab, _ = make_tab()

    with pytest.raises(ValueError):
        tab.act("gibt_es_nicht", {})
    assert "pair" in ACTIONS
    assert "history_faults" in ACTIONS
