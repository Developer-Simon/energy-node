"""Laufzeitzustand eines geladenen Eintrags (entry.runtime_data)."""
from __future__ import annotations

from dataclasses import dataclass

import aiohttp

from .client import DashboardClient
from .link import CompanionLink
from .panel_access import PanelSessions


@dataclass
class EnergyNodeData:
    client: DashboardClient
    # Dieselbe Sitzung wie der Client, mit DummyCookieJar: der Proxy darf
    # Set-Cookie eines Browsers nie fuer andere Anfragen aufheben.
    session: aiohttp.ClientSession
    base_url: str
    panel_mode: str
    panel_sessions: PanelSessions
    # Pruefkanal zum Companion-Port. Nur mit Token, also nach der Kopplung.
    link: CompanionLink | None = None
