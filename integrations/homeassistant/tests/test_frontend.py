"""Die Lovelace-Karte muss ausgeliefert und im Frontend angemeldet werden."""
from pathlib import Path
from types import SimpleNamespace
from unittest.mock import AsyncMock, patch

from custom_components.battery_soc import _async_register_frontend
from custom_components.battery_soc.const import DOMAIN, FRONTEND_CARD_FILES


def test_card_files_ship_with_the_integration():
    www = Path(__file__).resolve().parents[1] / "custom_components/battery_soc/www"
    for name in FRONTEND_CARD_FILES:
        assert (www / name).is_file(), f"{name} fehlt unter www/"


def test_card_registers_itself_for_lovelace():
    www = Path(__file__).resolve().parents[1] / "custom_components/battery_soc/www"
    source = (www / "battery-soc-card.js").read_text(encoding="utf-8")
    assert "customElements.define('battery-soc-card'" in source
    assert "window.customCards" in source


async def test_register_frontend_registers_static_paths_and_extra_js():
    """Mit vorhandenem hass.http meldet die Karte sich beim Frontend an."""
    fake_http = SimpleNamespace(async_register_static_paths=AsyncMock())
    fake_hass = SimpleNamespace(http=fake_http, data={})

    with patch("custom_components.battery_soc.add_extra_js_url") as add_url:
        await _async_register_frontend(fake_hass)

    fake_http.async_register_static_paths.assert_awaited_once()
    assert add_url.call_count == len(FRONTEND_CARD_FILES)
    assert fake_hass.data[f"{DOMAIN}_frontend_registered"] is True


async def test_register_frontend_is_a_noop_without_http():
    """Ohne hass.http (http-Komponente nicht aufgesetzt) darf nichts platzen.

    Das ist genau der Fall, der vorher jeden Config-Entry abgeschossen hat:
    AttributeError auf hass.http.async_register_static_paths.
    """
    fake_hass = SimpleNamespace(http=None, data={})

    await _async_register_frontend(fake_hass)

    assert fake_hass.data == {}


async def test_register_frontend_runs_once():
    """Ein zweiter Aufruf registriert nicht doppelt - der zweite Versuch waere ein Fehler."""
    fake_http = SimpleNamespace(async_register_static_paths=AsyncMock())
    fake_hass = SimpleNamespace(http=fake_http, data={f"{DOMAIN}_frontend_registered": True})

    with patch("custom_components.battery_soc.add_extra_js_url") as add_url:
        await _async_register_frontend(fake_hass)

    fake_http.async_register_static_paths.assert_not_awaited()
    add_url.assert_not_called()
