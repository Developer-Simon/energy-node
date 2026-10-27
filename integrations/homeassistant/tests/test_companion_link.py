"""Benutzer-Anmeldung, Pruefkanal und Benutzermeldungen fuer das Dashboard (Home-Assistant-Konten)."""
import asyncio
from contextlib import suppress
from unittest.mock import Mock, patch

import aiohttp
import pytest
from homeassistant.auth.const import GROUP_ID_USER
from pytest_homeassistant_custom_component.common import MockConfigEntry

from custom_components.energy_node_companion.const import (
    CONF_FINGERPRINT, CONF_HISTORY, CONF_STATS, CONF_TOKEN, CONF_URL, CONF_VERIFY_SSL, DOMAIN,
)
from custom_components.energy_node_companion.ha_users import async_check_login, async_username
from custom_components.energy_node_companion.link import CompanionLink

from .companion_tls import tls_server  # noqa: F401  (tls_server is a pytest fixture)

TOKEN = "11" * 32
ENTRY_URL = "https://node.local:8443"
COMPANION = "custom_components.energy_node_companion.companion_base"


@pytest.fixture(autouse=True)
def auto_enable_custom_integrations(recorder_mock, enable_custom_integrations):
    yield


async def _until(condition):
    async with asyncio.timeout(5):
        while not condition():
            await asyncio.sleep(0.02)


@pytest.fixture
async def ha_user(hass, local_auth):
    """Home-Assistant-Benutzer "anna" mit Passwort, verknuepft ueber den Provider "homeassistant".

    Der erste Benutzer einer Installation wird Besitzer, daher legt die Fixture
    zuerst einen Besitzer an. Anna ist danach ein gewoehnlicher Benutzer.
    """
    await hass.auth.async_create_user("Besitzer")
    user = await hass.auth.async_create_user("Anna", group_ids=[GROUP_ID_USER])
    await local_auth.async_add_auth("anna", "geheim-123")
    credentials = await local_auth.async_get_or_create_credentials({"username": "anna"})
    await hass.auth.async_link_user(user, credentials)
    return user


async def test_check_login_accepts_valid_credentials(hass, ha_user):
    result = await async_check_login(hass, "anna", "geheim-123")
    assert result["ok"] is True
    assert result["ha_user_id"] == ha_user.id
    assert result["username"] == "anna"
    assert result["name"] == "Anna"
    assert result["is_admin"] is False


async def test_check_login_rejects_wrong_password(hass, ha_user):
    assert await async_check_login(hass, "anna", "falsch") == {"ok": False}


async def test_check_login_rejects_inactive_user(hass, ha_user):
    await hass.auth.async_deactivate_user(ha_user)
    assert await async_check_login(hass, "anna", "geheim-123") == {"ok": False}


async def test_username_falls_back_to_id_prefix(hass):
    user = await hass.auth.async_create_user("Ohne Konto")
    assert await async_username(hass, user) == f"ha-{user.id[:8]}"


@pytest.mark.enable_socket
async def test_link_answers_verify_events(hass, tls_server, ha_user):  # noqa: F811
    base, fingerprint, state = tls_server
    async with aiohttp.ClientSession() as session:
        link = CompanionLink(hass, session, base, TOKEN, fingerprint, Mock())
        task = asyncio.create_task(link.run())
        try:
            state["events"].put_nowait(("verify", {"id": "abc123", "username": "anna", "password": "geheim-123"}))
            await _until(lambda: state["verified"])
            await hass.async_block_till_done(wait_background_tasks=True)
        finally:
            task.cancel()
            with suppress(asyncio.CancelledError):
                await task
    request_id, body = state["verified"][0]
    assert request_id == "abc123"
    assert body["ok"] is True
    assert body["ha_user_id"] == ha_user.id
    assert body["username"] == "anna"


@pytest.mark.enable_socket
async def test_link_reports_unpaired_on_401(hass, tls_server):  # noqa: F811
    base, fingerprint, state = tls_server
    state["stream_status"] = 401
    on_unpaired = Mock()
    async with aiohttp.ClientSession() as session:
        link = CompanionLink(hass, session, base, TOKEN, fingerprint, on_unpaired)
        await asyncio.wait_for(link.run(), 5)
    on_unpaired.assert_called_once_with()


@pytest.mark.enable_socket
async def test_user_removed_is_reported(hass, tls_server, ha_user):  # noqa: F811
    base, fingerprint, state = tls_server
    user_id = ha_user.id
    entry = MockConfigEntry(
        domain=DOMAIN,
        unique_id=ENTRY_URL,
        data={CONF_URL: ENTRY_URL, CONF_VERIFY_SSL: True, CONF_TOKEN: TOKEN, CONF_FINGERPRINT: fingerprint},
        options={CONF_HISTORY: False, CONF_STATS: False},
    )
    entry.add_to_hass(hass)
    with patch(COMPANION, return_value=base):
        assert await hass.config_entries.async_setup(entry.entry_id)
    await hass.auth.async_remove_user(ha_user)
    await _until(lambda: state["changes"])
    assert state["changes"] == [{"ha_user_id": user_id, "removed": True}]
    assert await hass.config_entries.async_unload(entry.entry_id)


async def test_setup_without_token_starts_no_link(hass):
    entry = MockConfigEntry(
        domain=DOMAIN,
        unique_id=ENTRY_URL,
        data={CONF_URL: ENTRY_URL, CONF_VERIFY_SSL: True},
        options={CONF_HISTORY: False, CONF_STATS: False},
    )
    entry.add_to_hass(hass)
    assert await hass.config_entries.async_setup(entry.entry_id)
    assert entry.runtime_data.link is None
