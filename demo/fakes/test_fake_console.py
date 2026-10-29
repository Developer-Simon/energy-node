import json
import sys
import threading
import urllib.error
import urllib.request
from pathlib import Path

import pytest

sys.path.insert(0, str(Path(__file__).resolve().parent))
import fake_console  # noqa: E402
from fake_console import Log, Tab  # noqa: E402


class EchoTab(Tab):
    name = "echo"
    title = "Echo"
    html = "<p>echo</p>"

    def __init__(self):
        self.count = 0

    def state(self):
        return {"n": self.count}

    def act(self, action, body):
        if action == "inc":
            self.count += 1
            return self.state()
        if action == "bad":
            raise ValueError("kaputt")
        raise ValueError(f"unbekannte Aktion {action}")


@pytest.fixture
def base_url():
    server = fake_console.make_server([EchoTab()], Log(), 0)
    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()
    yield f"http://127.0.0.1:{server.server_address[1]}"
    server.shutdown()
    server.server_close()


def request(url, method="GET", body=None):
    data = None if body is None else json.dumps(body).encode()
    req = urllib.request.Request(url, data=data, method=method)
    if data is not None:
        req.add_header("Content-Type", "application/json")
    try:
        with urllib.request.urlopen(req) as resp:
            return resp.status, resp.headers.get("Content-Type", ""), resp.read()
    except urllib.error.HTTPError as err:
        return err.code, err.headers.get("Content-Type", ""), err.read()


def test_log_since_returns_only_newer_entries():
    log = Log()
    log.add("ha", "eins")
    log.add("ha", "zwei")
    log.add("verlauf", "drei")
    last, entries = log.since(1)
    assert last == 3
    assert [e["n"] for e in entries] == [2, 3]
    assert entries[1]["source"] == "verlauf"
    assert entries[1]["text"] == "drei"
    assert len(entries[0]["t"]) == 8  # HH:MM:SS


def test_log_is_a_ring_buffer():
    log = Log(size=3)
    for i in range(5):
        log.add("ha", f"eintrag {i}")
    last, entries = log.since(0)
    assert last == 5
    assert [e["text"] for e in entries] == ["eintrag 2", "eintrag 3", "eintrag 4"]


def test_state_and_act_roundtrip(base_url):
    status, _, body = request(f"{base_url}/api/echo/inc", method="POST", body={})
    assert status == 200
    assert json.loads(body) == {"n": 1}
    status, _, body = request(f"{base_url}/api/echo/state")
    assert status == 200
    assert json.loads(body) == {"n": 1}


def test_value_error_becomes_400_with_text(base_url):
    status, _, body = request(f"{base_url}/api/echo/bad", method="POST", body={})
    assert status == 400
    assert json.loads(body) == {"error": "kaputt"}


def test_unknown_tab_is_404(base_url):
    status, ctype, body = request(f"{base_url}/api/nope/state")
    assert status == 404
    assert "application/json" in ctype
    assert "error" in json.loads(body)


def test_tabs_listing_has_html_fragment(base_url):
    status, _, body = request(f"{base_url}/api/tabs")
    assert status == 200
    assert json.loads(body) == [{"name": "echo", "title": "Echo", "html": "<p>echo</p>", "sources": []}]


def test_index_page_is_served(base_url):
    status, ctype, body = request(f"{base_url}/")
    assert status == 200
    assert ctype.startswith("text/html")
    assert b"<html" in body.lower()


def test_log_endpoint_returns_entries_since():
    server = fake_console.make_server([], Log(), 0)
    server.log.add("ha", "start")
    server.log.add("ha", "ende")
    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()
    try:
        url = f"http://127.0.0.1:{server.server_address[1]}/api/log?since=1"
        status, _, body = request(url)
        data = json.loads(body)
        assert status == 200
        assert data["last"] == 2
        assert [e["text"] for e in data["entries"]] == ["ende"]
    finally:
        server.shutdown()
        server.server_close()


def test_busy_port_exits_with_message(capsys):
    server = fake_console.make_server([], Log(), 0)
    port = server.server_address[1]
    try:
        assert fake_console.main(["--port", str(port)]) == 2
    finally:
        server.server_close()
    assert "belegt" in capsys.readouterr().err


class StatusTab(Tab):
    name = "st"
    title = "Status"
    html = "<p>st</p>"
    sources = ("st",)

    def __init__(self):
        self.resets = 0

    def summary(self):
        return {"level": "changed", "text": "verändert"}

    def reset(self):
        self.resets += 1


class BrokenTab(Tab):
    name = "kaputt"
    title = "Kaputt"
    html = "<p>kaputt</p>"

    def summary(self):
        raise OSError("weg")

    def reset(self):
        raise ValueError("geht nicht")


@pytest.fixture
def status_url():
    log = Log()
    st = StatusTab()
    server = fake_console.make_server([st, BrokenTab()], log, 0)
    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()
    yield f"http://127.0.0.1:{server.server_address[1]}", log, st
    server.shutdown()
    server.server_close()


def test_tabs_list_contains_sources(status_url):
    base, _, _ = status_url
    _, _, body = request(f"{base}/api/tabs")
    tabs = json.loads(body)
    assert tabs[0]["sources"] == ["st"]
    assert tabs[1]["sources"] == []


def test_status_reports_summary_and_unreachable(status_url):
    base, _, _ = status_url
    status, _, body = request(f"{base}/api/status")
    assert status == 200
    assert json.loads(body) == [
        {"name": "st", "level": "changed", "text": "verändert"},
        {"name": "kaputt", "level": "error", "text": "nicht erreichbar"},
    ]


def test_reset_calls_every_tab_and_logs_failures(status_url):
    base, log, st = status_url
    status, _, body = request(f"{base}/api/reset", method="POST", body={})
    assert status == 200
    assert json.loads(body)[0] == {"name": "st", "level": "changed", "text": "verändert"}
    assert st.resets == 1
    _, entries = log.since(0)
    assert ("konsole", "Kaputt: geht nicht", "error") in [
        (e["source"], e["text"], e["level"]) for e in entries]
    last = entries[-1]
    assert (last["source"], last["text"], last["level"]) == (
        "konsole", "Alles auf Start zurückgesetzt", "info")
