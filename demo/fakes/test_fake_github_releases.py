import http.client
import http.server
import json
import sys
import threading
from pathlib import Path

import pytest

sys.path.insert(0, str(Path(__file__).resolve().parent))
import fake_github_releases as fgr  # noqa: E402

ARCHIVE_NAME = "energy-node-v9.9.9-amd64.tar.gz"


@pytest.fixture
def server(tmp_path):
    archive = tmp_path / ARCHIVE_NAME
    archive.write_bytes(b"x" * 1000)
    state = fgr.ReleaseState("v9.9.9", str(archive), 0)
    httpd = http.server.ThreadingHTTPServer(("127.0.0.1", 0), fgr.make_handler(state))
    threading.Thread(target=httpd.serve_forever, daemon=True).start()
    yield httpd.server_address[1], state
    httpd.shutdown()
    httpd.server_close()


def call(port, method, path, body=None):
    conn = http.client.HTTPConnection("127.0.0.1", port, timeout=5)
    payload = None if body is None else json.dumps(body).encode("utf-8")
    headers = {"Content-Type": "application/json"} if payload is not None else {}
    conn.request(method, path, body=payload, headers=headers)
    resp = conn.getresponse()
    data = resp.read()
    conn.close()
    return resp.status, resp, data


def test_latest_returns_tag_by_default(server):
    port, _ = server
    status, _, data = call(port, "GET", "/repos/example/energy-node/releases/latest")
    assert status == 200
    assert json.loads(data)["tag_name"] == "v9.9.9"


def test_none_mode_offers_old_tag_and_empty_list(server):
    port, _ = server
    call(port, "POST", "/_fake/release", {"mode": "none"})
    _, _, latest = call(port, "GET", "/repos/example/energy-node/releases/latest")
    assert json.loads(latest)["tag_name"] == "v0.0.1"
    _, _, releases = call(port, "GET", "/repos/example/energy-node/releases")
    assert json.loads(releases) == []


def test_error_and_ratelimit_status(server):
    port, _ = server
    call(port, "POST", "/_fake/release", {"mode": "error"})
    status, _, _ = call(port, "GET", "/repos/example/energy-node/releases/latest")
    assert status == 500
    call(port, "POST", "/_fake/release", {"mode": "ratelimit"})
    status, resp, data = call(port, "GET", "/repos/example/energy-node/releases/latest")
    assert status == 403
    assert resp.getheader("X-RateLimit-Remaining") == "0"
    assert json.loads(data) == {"message": "API rate limit exceeded"}


def test_invalid_tag_is_400(server):
    port, _ = server
    status, _, data = call(port, "POST", "/_fake/release", {"tag": "9.9"})
    assert status == 400
    assert json.loads(data)["error"] == "Version muss die Form v1.2.3 haben"


def test_half_abort_delivers_partial_body(server):
    port, _ = server
    call(port, "POST", "/_fake/download", {"seconds": 0, "abort": "half"})
    conn = http.client.HTTPConnection("127.0.0.1", port, timeout=5)
    conn.request("GET", f"/download/{ARCHIVE_NAME}")
    resp = conn.getresponse()
    length = int(resp.getheader("Content-Length"))
    try:
        body = resp.read()
    except http.client.IncompleteRead as exc:
        body = exc.partial
    conn.close()
    assert 0 < len(body) < length


def test_reset_restores_start_values(server):
    port, _ = server
    call(port, "POST", "/_fake/release", {"tag": "v9.9.10", "mode": "none"})
    call(port, "POST", "/_fake/download", {"seconds": 7, "abort": "start"})
    status, _, data = call(port, "POST", "/_fake/reset")
    assert status == 200
    state = json.loads(data)
    assert state["tag"] == "v9.9.9"
    assert state["mode"] == "update"
    assert state["seconds"] == 0
    assert state["abort"] == "never"


def test_log_records_check_answers(server):
    port, _ = server
    call(port, "GET", "/repos/example/energy-node/releases/latest")
    _, _, data = call(port, "GET", "/_fake/log?since=0")
    texts = [entry["text"] for entry in json.loads(data)["entries"]]
    assert "Update-Check beantwortet mit v9.9.9" in texts


def test_without_control_calls_behaviour_is_unchanged(server):
    port, _ = server
    _, _, data = call(port, "GET", "/repos/example/energy-node/releases")
    releases = json.loads(data)
    assert len(releases) == 1
    asset = releases[0]["assets"][0]
    assert asset["name"] == ARCHIVE_NAME
    assert asset["size"] == 1000
