"""Home-Assistant-Benutzer fuer das Dashboard: Namen und Anmeldepruefung.

Das Dashboard kennt keine eigenen Konten fuer Home-Assistant-Benutzer. Es
fragt die Integration, ob ein Benutzername und ein Passwort zu einem aktiven
Home-Assistant-Benutzer gehoeren, und uebernimmt dessen Rechte.
"""
from __future__ import annotations

from homeassistant.auth.providers.homeassistant import InvalidAuth
from homeassistant.core import HomeAssistant

PROVIDER_TYPE = "homeassistant"


async def async_username(hass: HomeAssistant, user) -> str:
    """Benutzername aus dem Provider "homeassistant", sonst "ha-" + Benutzer-ID-Anfang."""
    for credentials in user.credentials:
        if credentials.auth_provider_type == PROVIDER_TYPE and credentials.data.get("username"):
            return credentials.data["username"]
    return f"ha-{user.id[:8]}"


async def async_identity(hass: HomeAssistant, user) -> dict:
    return {
        "ha_user_id": user.id,
        "username": await async_username(hass, user),
        "name": user.name,
        "is_admin": user.is_admin,
    }


async def async_check_login(hass: HomeAssistant, username: str, password: str) -> dict:
    """Prueft Benutzername und Passwort gegen den Provider und liefert die Identitaet des Benutzers."""
    provider = next((p for p in hass.auth.auth_providers if p.type == PROVIDER_TYPE), None)
    if provider is None:
        return {"ok": False}
    try:
        await provider.async_validate_login(username, password)
    except InvalidAuth:
        return {"ok": False}
    credentials = await provider.async_get_or_create_credentials({"username": username})
    user = await hass.auth.async_get_user_by_credentials(credentials)
    if user is None or not user.is_active or user.system_generated:
        return {"ok": False}
    return {"ok": True, **await async_identity(hass, user)}
