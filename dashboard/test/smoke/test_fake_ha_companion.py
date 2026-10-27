import threading
import time

import pytest

import fake_ha_companion
from fake_ha_companion import HACompanion, HAUser, PairError, main

WRONG_CODE = "AAAAAAAAAAAA"
MARIA_PASSWORD = "maria-smoke-pass-1"
TOM_PASSWORD = "tom-smoke-pass-1"


def wait_until(predicate, timeout=5.0):
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        if predicate():
            return
        time.sleep(0.02)
    raise AssertionError("Bedingung nicht erreicht")


@pytest.fixture
def companions(stub_dashboard):
    created = []

    def make(users=None):
        lines = []
        companion = HACompanion(stub_dashboard.url, lines.append, users=users)
        created.append(companion)
        return companion, lines

    yield make
    for companion in created:
        companion.unpair()


def test_pair_and_auto_verify_from_user_table(stub_dashboard, companions):
    companion, _ = companions()
    companion.pair(stub_dashboard.code)
    wait_until(lambda: companion.state()["connected"])
    stub_dashboard.push_verify("req-1", "maria", MARIA_PASSWORD)
    answer = stub_dashboard.answer("req-1")
    assert answer["ok"] is True
    assert answer["is_admin"] is True
    assert answer["ha_user_id"] == "ha-smoke-maria"


def test_auto_verify_rejects_wrong_password_and_inactive(stub_dashboard, companions):
    users = [
        HAUser("ha-smoke-maria", "maria", MARIA_PASSWORD, "Maria", admin=True),
        HAUser("ha-smoke-tom", "tom", TOM_PASSWORD, "Tom", active=False),
    ]
    companion, _ = companions(users)
    companion.pair(stub_dashboard.code)
    wait_until(lambda: companion.state()["connected"])
    for request_id, username, password in [("req-1", "maria", "falsch"), ("req-2", "tom", TOM_PASSWORD)]:
        stub_dashboard.push_verify(request_id, username, password)
        answer = stub_dashboard.answer(request_id)
        assert answer["ok"] is False
        assert answer["ha_user_id"] == ""
        assert answer["username"] == ""
        assert answer["is_admin"] is False


def test_pair_with_wrong_code_reports_error(stub_dashboard, companions):
    companion, _ = companions()
    with pytest.raises(PairError, match=r"Kopplung abgelehnt \(HTTP 403\): .*Kopplungscode stimmt nicht"):
        companion.pair(WRONG_CODE)
    assert companion.state()["paired"] is False


def test_pair_wrong_code_returns_dashboard_answer(stub_dashboard, companions):
    companion, _ = companions()
    text = companion.pair_wrong_code()
    status, _body = stub_dashboard.pair_requests[-1]
    assert status == 403
    assert "HTTP 403" in text
    assert "Kopplungscode stimmt nicht" in text
    assert companion.state()["paired"] is False


def test_stream_end_by_dashboard_marks_unpaired(stub_dashboard, companions):
    companion, _ = companions()
    companion.pair(stub_dashboard.code)
    wait_until(lambda: companion.state()["connected"])
    stub_dashboard.end_stream()
    wait_until(lambda: not companion.state()["paired"])
    state = companion.state()
    assert state["paired"] is False
    assert state["connected"] is False
    assert state["error"]
    assert companion.token() == ""


def test_drop_and_open_stream_keep_pairing(stub_dashboard, companions):
    companion, _ = companions()
    companion.pair(stub_dashboard.code)
    wait_until(lambda: companion.state()["connected"])
    companion.drop_stream()
    state = companion.state()
    assert state["connected"] is False
    assert state["paired"] is True
    companion.open_stream()
    wait_until(lambda: companion.state()["connected"])
    assert companion.state()["paired"] is True


def cli_argv(stub, tmp_path):
    return [
        "--companion", stub.url,
        "--code", stub.code,
        "--user", "maria",
        "--password", MARIA_PASSWORD,
        "--ha-user-id", "ha-smoke-maria",
        "--admin",
        "--token-file", str(tmp_path / "token.txt"),
    ]


def run_cli(argv, outcome):
    try:
        main(argv)
        outcome.append("returned")
    except SystemExit as exit_request:
        outcome.append(exit_request.code)


def test_cli_main_keeps_output_lines(stub_dashboard, capsys, tmp_path):
    token_file = tmp_path / "token.txt"
    outcome = []
    runner = threading.Thread(target=run_cli, args=(cli_argv(stub_dashboard, tmp_path), outcome), daemon=True)
    runner.start()
    wait_until(lambda: stub_dashboard.open_streams == 1)
    stub_dashboard.end_stream()
    runner.join(10)
    assert not runner.is_alive()
    assert outcome == ["returned"]
    lines = capsys.readouterr().out.splitlines()
    assert lines == ["Gekoppelt, Token geschrieben", "Pruefstrom offen", "Pruefstrom beendet"]
    assert token_file.read_text() in stub_dashboard.tokens


def test_cli_main_exits_1_when_stream_dies(stub_dashboard, capsys, tmp_path):
    outcome = []
    runner = threading.Thread(target=run_cli, args=(cli_argv(stub_dashboard, tmp_path), outcome), daemon=True)
    runner.start()
    wait_until(lambda: stub_dashboard.open_streams == 1)
    stub_dashboard.kill_stream()
    runner.join(10)
    assert not runner.is_alive()
    assert outcome == [1]
    assert "FEHLER" in capsys.readouterr().err


def test_manual_mode_queues_and_resolves(stub_dashboard, companions):
    companion, _ = companions()
    companion.pair(stub_dashboard.code)
    wait_until(lambda: companion.state()["connected"])
    companion.set_manual(True)
    stub_dashboard.push_verify("req-1", "tom", TOM_PASSWORD)
    wait_until(lambda: companion.state()["pending"])
    assert companion.state()["pending"] == [{"id": "req-1", "username": "tom"}]
    companion.resolve("req-1", "yes")
    answer = stub_dashboard.answer("req-1")
    assert answer["ok"] is True
    assert answer["ha_user_id"] == "ha-smoke-tom"
    assert companion.state()["pending"] == []


def test_manual_no_and_timeout(stub_dashboard, companions):
    companion, _ = companions()
    companion.pair(stub_dashboard.code)
    wait_until(lambda: companion.state()["connected"])
    companion.set_manual(True)
    stub_dashboard.push_verify("req-no", "tom", TOM_PASSWORD)
    wait_until(lambda: len(companion.state()["pending"]) == 1)
    companion.resolve("req-no", "no")
    assert stub_dashboard.answer("req-no")["ok"] is False
    stub_dashboard.push_verify("req-timeout", "tom", TOM_PASSWORD)
    wait_until(lambda: len(companion.state()["pending"]) == 1)
    companion.resolve("req-timeout", "timeout")
    time.sleep(0.3)
    assert companion.state()["pending"] == []
    assert all(answer[0] != "req-timeout" for answer in stub_dashboard.answers)


def test_resolve_unknown_id_raises_value_error(stub_dashboard, companions):
    companion, _ = companions()
    with pytest.raises(ValueError):
        companion.resolve("gibt-es-nicht", "yes")
    companion.set_manual(True)
    with pytest.raises(ValueError):
        companion.resolve("gibt-es-nicht", "vielleicht")


def test_late_manual_answer_is_logged_not_raised(stub_dashboard, companions):
    companion, lines = companions()
    companion.pair(stub_dashboard.code)
    wait_until(lambda: companion.state()["connected"])
    companion.set_manual(True)
    stub_dashboard.set_verify_status(404)
    stub_dashboard.push_verify("req-1", "tom", TOM_PASSWORD)
    wait_until(lambda: companion.state()["pending"])
    companion.resolve("req-1", "yes")
    assert any("abgelaufen" in line for line in lines)


def test_save_user_reports_user_changed_when_paired(stub_dashboard, companions):
    companion, _ = companions()
    companion.pair(stub_dashboard.code)
    wait_until(lambda: companion.state()["connected"])
    companion.save_user(HAUser("ha-smoke-tom", "tom", TOM_PASSWORD, "Tom", admin=True))
    body = stub_dashboard.user_changes[-1]
    assert body["ha_user_id"] == "ha-smoke-tom"
    assert body["admin"] is True
    assert body["active"] is True


def test_remove_user_reports_removed_without_username(stub_dashboard, companions):
    companion, _ = companions()
    companion.pair(stub_dashboard.code)
    wait_until(lambda: companion.state()["connected"])
    companion.remove_user("ha-smoke-tom")
    assert stub_dashboard.user_changes[-1] == {"ha_user_id": "ha-smoke-tom", "removed": True}
    assert "ha-smoke-tom" not in companion.users


def test_user_changes_while_unpaired_do_not_call_dashboard(stub_dashboard, companions):
    companion, _ = companions()
    before = len(stub_dashboard.user_changes)
    companion.save_user(HAUser("ha-smoke-anna", "anna", "anna-pass", "Anna"))
    companion.remove_user("ha-smoke-tom")
    assert len(stub_dashboard.user_changes) == before
    assert "ha-smoke-anna" in companion.users


def test_parallel_open_stream_starts_one_stream(stub_dashboard, companions):
    companion, _ = companions()
    companion.pair(stub_dashboard.code)
    wait_until(lambda: companion.state()["connected"])
    companion.drop_stream()
    wait_until(lambda: stub_dashboard.open_streams == 0)
    barrier = threading.Barrier(8)

    def opener():
        barrier.wait()
        companion.open_stream()

    openers = [threading.Thread(target=opener) for _ in range(8)]
    for thread in openers:
        thread.start()
    for thread in openers:
        thread.join(10)
    wait_until(lambda: companion.state()["connected"])
    time.sleep(0.3)
    assert stub_dashboard.open_streams == 1


def test_old_stream_thread_cannot_touch_new_stream(stub_dashboard, companions, monkeypatch):
    monkeypatch.setattr(fake_ha_companion, "STOP_JOIN", 0.2)
    companion, lines = companions()
    companion.pair(stub_dashboard.code)
    wait_until(lambda: companion.state()["connected"])
    token = companion.token()
    stub_dashboard.hold_verify()
    stub_dashboard.push_verify("req-1", "tom", TOM_PASSWORD)
    assert stub_dashboard.hold_entered.wait(5)
    companion.drop_stream()
    companion.open_stream()
    wait_until(lambda: companion.state()["connected"])
    stub_dashboard.release_verify()
    wait_until(lambda: lines.count("Pruefstrom beendet") == 1)
    state = companion.state()
    assert state["paired"] is True
    assert state["connected"] is True
    assert companion.token() == token


def test_user_table_changes_during_verifies_keep_stream(stub_dashboard, companions):
    companion, _ = companions()
    companion.pair(stub_dashboard.code)
    wait_until(lambda: companion.state()["connected"])

    def churn():
        # Direkt unter dem Lock, damit viele Aenderungen auf die Pruefungen treffen
        for i in range(20000):
            with companion._lock:
                companion.users[f"ha-x{i}"] = HAUser(f"ha-x{i}", f"x{i}", "pw", "X")
                if i >= 100:
                    companion.users.pop(f"ha-x{i - 100}", None)

    writer = threading.Thread(target=churn)
    writer.start()
    for i in range(30):
        stub_dashboard.push_verify(f"req-{i}", "maria", MARIA_PASSWORD)
    for i in range(30):
        assert stub_dashboard.answer(f"req-{i}")["ok"] is True
    writer.join(30)
    state = companion.state()
    assert state["paired"] is True
    assert state["connected"] is True
