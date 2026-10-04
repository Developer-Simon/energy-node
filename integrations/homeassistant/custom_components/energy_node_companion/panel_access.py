"""Wer das Panel benutzen darf und wie der Browser das dem Proxy zeigt.

Ein iframe kann keinen HA-Token mitschicken. Das Panel holt deshalb ueber
eine angemeldete HA-Anfrage ein kurzlebiges Token, das als Cookie unter dem
Proxy-Pfad liegt. Die Tokens leben nur im Speicher: nach einem HA-Neustart
holt das Panel sich ein neues.
"""
from __future__ import annotations

import secrets
import time
from collections.abc import Callable

from .const import PANEL_ADMINS, PANEL_ALL, PANEL_SESSION_TTL_S


class PanelSessions:
    """Panel-Tokens eines Eintrags mit Ablaufzeit."""

    def __init__(self, ttl_s: float = PANEL_SESSION_TTL_S, clock: Callable[[], float] = time.monotonic) -> None:
        self._ttl = ttl_s
        self._clock = clock
        self._sessions: dict[str, tuple[str, float]] = {}

    def __len__(self) -> int:
        return len(self._sessions)

    def issue(self, user_id: str) -> str:
        now = self._clock()
        self._sessions = {token: item for token, item in self._sessions.items() if item[1] > now}
        token = secrets.token_urlsafe(32)
        self._sessions[token] = (user_id, now + self._ttl)
        return token

    def validate(self, token: str) -> str | None:
        item = self._sessions.get(token) if token else None
        if item is None:
            return None
        if item[1] <= self._clock():
            self._sessions.pop(token, None)
            return None
        return item[0]


def may_use_panel(user, mode: str) -> bool:
    """Sichtbarkeit aus den Optionen. Unbekannte Werte sperren."""
    if mode == PANEL_ALL:
        return True
    if mode == PANEL_ADMINS:
        return bool(user.is_admin)
    return False
