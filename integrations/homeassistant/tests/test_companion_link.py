"""Benutzer-Anmeldung und Benutzernamen fuer das Dashboard (Home-Assistant-Konten)."""
import pytest
from homeassistant.auth.const import GROUP_ID_USER

from custom_components.energy_node_companion.ha_users import async_check_login, async_username


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
