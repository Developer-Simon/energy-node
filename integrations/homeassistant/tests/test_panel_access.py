"""Panel-Sitzungen und Sichtbarkeit, ohne Home Assistant."""
from types import SimpleNamespace

from custom_components.energy_node_companion.const import PANEL_ADMINS, PANEL_ALL, PANEL_OFF
from custom_components.energy_node_companion.panel_access import PanelSessions, may_use_panel


class Clock:
    def __init__(self):
        self.now = 1000.0

    def __call__(self):
        return self.now


def test_issued_token_validates_to_its_user():
    sessions = PanelSessions(ttl_s=60, clock=Clock())
    token = sessions.issue("user-1")
    assert len(token) >= 40
    assert sessions.validate(token) == "user-1"


def test_tokens_are_unique():
    sessions = PanelSessions(ttl_s=60, clock=Clock())
    assert sessions.issue("u") != sessions.issue("u")


def test_token_expires():
    clock = Clock()
    sessions = PanelSessions(ttl_s=60, clock=clock)
    token = sessions.issue("user-1")
    clock.now += 59
    assert sessions.validate(token) == "user-1"
    clock.now += 2
    assert sessions.validate(token) is None


def test_unknown_and_empty_tokens_are_refused():
    sessions = PanelSessions(ttl_s=60, clock=Clock())
    assert sessions.validate("") is None
    assert sessions.validate("nope") is None


def test_expired_tokens_are_pruned_on_issue():
    clock = Clock()
    sessions = PanelSessions(ttl_s=60, clock=clock)
    for _ in range(100):
        sessions.issue("u")
    clock.now += 61
    sessions.issue("u")
    assert len(sessions) == 1


def test_visibility():
    admin = SimpleNamespace(is_admin=True)
    user = SimpleNamespace(is_admin=False)
    assert may_use_panel(user, PANEL_ALL)
    assert may_use_panel(admin, PANEL_ADMINS)
    assert not may_use_panel(user, PANEL_ADMINS)
    assert not may_use_panel(admin, PANEL_OFF)
    assert not may_use_panel(admin, "unknown")
