"""Header-Regeln des Panel-Proxys, ohne Home Assistant."""
from multidict import CIMultiDict

from custom_components.energy_node_companion.const import panel_url_path, proxy_prefix
from custom_components.energy_node_companion.proxy_headers import (
    guest_cookie,
    is_logout,
    own_session,
    response_headers,
    session_cookie_name,
    upstream_headers,
)

PREFIX = "/api/energy_node_companion/proxy/01K6ABCDEF"


def _up(incoming, scheme="http", cookie="energy_node_guest_session=g"):
    return upstream_headers(
        CIMultiDict(incoming), scheme=scheme, host="ha.tail1234.ts.net:8123",
        remote="100.64.0.7", prefix=PREFIX, session_cookie=cookie,
    )


def test_prefix_and_panel_path():
    assert proxy_prefix("01K6ABCDEF") == PREFIX
    assert panel_url_path("01K6ABCDEFGHJKMNPQRSTVWXYZ") == "energy-node-rstvwxyz"


def test_cookie_name_follows_the_scheme():
    assert session_cookie_name(False) == "energy_node_guest_session"
    assert session_cookie_name(True) == "energy_node_session"
    assert guest_cookie("tok", True) == "energy_node_session=tok"


def test_own_session_picks_the_cookie_for_the_scheme():
    header = "energy_node_panel=p; energy_node_guest_session=mine; other=x"
    assert own_session(header, secure=False) == "energy_node_guest_session=mine"
    assert own_session(header, secure=True) is None
    assert own_session("energy_node_session=admin", secure=True) == "energy_node_session=admin"


def test_own_session_survives_garbage():
    assert own_session("", secure=False) is None
    assert own_session('bad"cookie; ;;=', secure=False) is None


def test_upstream_sets_forwarding_and_one_cookie():
    headers = _up({"Accept": "text/html", "Cookie": "energy_node_panel=p", "Host": "ha:8123"})
    assert headers["Accept"] == "text/html"
    assert headers.getall("Cookie") == ["energy_node_guest_session=g"]
    assert headers["X-Forwarded-Prefix"] == PREFIX
    assert headers["X-Forwarded-Proto"] == "http"
    assert headers["X-Forwarded-Host"] == "ha.tail1234.ts.net:8123"
    assert headers["X-Forwarded-For"] == "100.64.0.7"
    assert "Host" not in headers


def test_forged_forwarding_headers_are_replaced():
    headers = _up({
        "X-Forwarded-Proto": "https",
        "X-Forwarded-Prefix": "/evil",
        "X-Ingress-Path": "/api/hassio_ingress/x",
        "X-Forwarded-For": "1.2.3.4",
        "Forwarded": "proto=https",
        "Authorization": "Bearer ha-token",
    })
    assert headers.getall("X-Forwarded-Proto") == ["http"]
    assert headers.getall("X-Forwarded-Prefix") == [PREFIX]
    assert headers.getall("X-Forwarded-For") == ["100.64.0.7"]
    for gone in ("X-Ingress-Path", "Forwarded", "Authorization"):
        assert gone not in headers


def test_https_scheme_is_forwarded():
    assert _up({}, scheme="https")["X-Forwarded-Proto"] == "https"


def test_hop_by_hop_and_length_headers_are_dropped():
    headers = _up({
        "Connection": "keep-alive", "Keep-Alive": "5", "Transfer-Encoding": "chunked",
        "Content-Length": "3", "Accept-Encoding": "gzip", "Upgrade": "h2c", "TE": "trailers",
    })
    assert dict(headers).keys() == {"Cookie", "X-Forwarded-Prefix", "X-Forwarded-Proto", "X-Forwarded-Host", "X-Forwarded-For"}


def test_response_keeps_every_set_cookie_and_drops_encoding():
    upstream = CIMultiDict([
        ("Content-Type", "text/event-stream"),
        ("Set-Cookie", "a=1; Path=/x/"),
        ("Set-Cookie", "b=2; Path=/x/"),
        ("Content-Encoding", "gzip"),
        ("Content-Length", "10"),
        ("Transfer-Encoding", "chunked"),
        ("Connection", "close"),
    ])
    headers = response_headers(upstream)
    assert headers.getall("Set-Cookie") == ["a=1; Path=/x/", "b=2; Path=/x/"]
    assert headers["Content-Type"] == "text/event-stream"
    for gone in ("Content-Encoding", "Content-Length", "Transfer-Encoding", "Connection"):
        assert gone not in headers


def test_logout_path():
    assert is_logout("api/v1/auth/logout")
    assert is_logout("/api/v1/auth/logout/")
    assert not is_logout("api/v1/auth/login")
