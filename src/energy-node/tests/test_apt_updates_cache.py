"""`apt list --upgradable` parst jedes Mal den kompletten APT-Cache und ist
auf dem Pi 1 der teuerste Diagnose-Aufruf. Es wird daher hoechstens 1x/Tag
wirklich ausgefuehrt. Siehe docs/knowledge/performance-und-ressourcen.md 5.2.
"""

import sys
from pathlib import Path

import pytest

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

import energy_node_mqtt as node


@pytest.fixture(autouse=True)
def _reset_apt_cache():
    node._apt_updates_cache.update(ts=0.0, value=None)
    yield
    node._apt_updates_cache.update(ts=0.0, value=None)


def _stub_runner(monkeypatch, values):
    seq = list(values)
    calls = {"n": 0}

    def fake_run():
        calls["n"] += 1
        return seq.pop(0)

    monkeypatch.setattr(node, "_run_apt_updates_pending", fake_run)
    return calls


def test_ttl_is_one_day():
    assert node.APT_UPDATES_TTL_S == 86_400


def test_result_is_cached_within_the_ttl(monkeypatch):
    calls = _stub_runner(monkeypatch, [3, 3])

    first = node.read_apt_updates_pending(now=1_000.0)
    second = node.read_apt_updates_pending(now=1_000.0 + node.APT_UPDATES_TTL_S - 1)

    assert first == 3
    assert second == 3
    assert calls["n"] == 1


def test_result_is_refetched_after_the_ttl(monkeypatch):
    calls = _stub_runner(monkeypatch, [3, 9])

    first = node.read_apt_updates_pending(now=1_000.0)
    second = node.read_apt_updates_pending(now=1_000.0 + node.APT_UPDATES_TTL_S)

    assert first == 3
    assert second == 9
    assert calls["n"] == 2


def test_failed_run_is_not_cached(monkeypatch):
    calls = _stub_runner(monkeypatch, [None, 5])

    first = node.read_apt_updates_pending(now=1_000.0)
    second = node.read_apt_updates_pending(now=1_000.0 + 1)

    assert first is None
    assert second == 5
    assert calls["n"] == 2


def test_stale_refetch_failure_serves_last_good_value(monkeypatch):
    calls = _stub_runner(monkeypatch, [4, None])

    first = node.read_apt_updates_pending(now=1_000.0)
    second = node.read_apt_updates_pending(now=1_000.0 + node.APT_UPDATES_TTL_S + 1)

    assert first == 4
    assert second == 4
    assert calls["n"] == 2
