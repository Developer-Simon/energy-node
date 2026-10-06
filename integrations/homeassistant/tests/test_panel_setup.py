"""Panel in der Seitenleiste und die Option dafuer."""
import json
from pathlib import Path
from unittest.mock import AsyncMock, patch

import pytest
from homeassistant.components.frontend import DATA_PANELS
from pytest_homeassistant_custom_component.common import MockConfigEntry

from custom_components.energy_node_companion.const import (
    CONF_HISTORY, CONF_PANEL, CONF_URL, CONF_VERIFY_SSL, DOMAIN, PANEL_ADMINS, PANEL_ALL, PANEL_OFF, panel_url_path,
)

BASE = Path(__file__).resolve().parents[1] / "custom_components/energy_node_companion"
PANEL_JS = BASE / "www/energy-node-panel.js"


@pytest.fixture(autouse=True)
def auto_enable_custom_integrations(recorder_mock, enable_custom_integrations):
    yield


async def _setup(hass, options=None, run=None):
    entry = MockConfigEntry(
        domain=DOMAIN, unique_id="http://node.tail1234.ts.net:8080", options=options or {},
        data={CONF_URL: "http://node.tail1234.ts.net:8080", CONF_VERIFY_SSL: True},
    )
    entry.add_to_hass(hass)
    with patch("custom_components.energy_node_companion.HistoryPeer.run", run or AsyncMock()):
        assert await hass.config_entries.async_setup(entry.entry_id)
        await hass.async_block_till_done()
    return entry


def _panel(hass, entry):
    return hass.data.get(DATA_PANELS, {}).get(panel_url_path(entry.entry_id))


async def test_panel_for_all_users(hass):
    entry = await _setup(hass)
    panel = _panel(hass, entry)
    assert panel is not None
    assert panel.require_admin is False
    assert panel.sidebar_title == "Energy Node"
    assert panel.config["entry_id"] == entry.entry_id
    custom = panel.config["_panel_custom"]
    assert custom["name"] == "energy-node-panel"
    assert custom["module_url"].startswith("/energy_node_static/energy-node-panel.js?v=")
    assert custom["embed_iframe"] is False


async def test_admins_option_requires_admin(hass):
    entry = await _setup(hass, {CONF_PANEL: PANEL_ADMINS})
    assert _panel(hass, entry).require_admin is True


async def test_off_option_registers_no_panel(hass):
    entry = await _setup(hass, {CONF_PANEL: PANEL_OFF})
    assert _panel(hass, entry) is None


async def test_unload_removes_the_panel(hass):
    entry = await _setup(hass)
    assert await hass.config_entries.async_unload(entry.entry_id)
    await hass.async_block_till_done()
    assert _panel(hass, entry) is None


async def _options(hass, entry, data):
    result = await hass.config_entries.options.async_init(entry.entry_id)
    assert result["type"] == "form"
    with patch("custom_components.energy_node_companion.HistoryPeer.run", AsyncMock()):
        result = await hass.config_entries.options.async_configure(result["flow_id"], data)
        await hass.async_block_till_done()
    return result


def _suggested(result, key):
    field = next(field for field in result["data_schema"].schema if field == key)
    return (field.description or {}).get("suggested_value")


async def test_options_flow_switches_visibility(hass):
    entry = await _setup(hass)
    result = await _options(hass, entry, {CONF_PANEL: PANEL_ADMINS})
    assert result["type"] == "create_entry"
    assert entry.options == {CONF_HISTORY: True, CONF_PANEL: PANEL_ADMINS}
    assert _panel(hass, entry).require_admin is True


async def test_options_flow_shows_the_current_values(hass):
    entry = await _setup(hass, {CONF_HISTORY: False, CONF_PANEL: PANEL_ADMINS})
    result = await hass.config_entries.options.async_init(entry.entry_id)
    assert _suggested(result, CONF_HISTORY) is False
    assert _suggested(result, CONF_PANEL) == PANEL_ADMINS


async def test_options_flow_starts_with_everything_on(hass):
    entry = await _setup(hass)
    result = await hass.config_entries.options.async_init(entry.entry_id)
    assert _suggested(result, CONF_HISTORY) is True
    assert _suggested(result, CONF_PANEL) == PANEL_ALL


async def test_options_flow_needs_history_or_panel(hass):
    entry = await _setup(hass)
    result = await _options(hass, entry, {CONF_HISTORY: False, CONF_PANEL: PANEL_OFF})
    assert result["type"] == "form"
    assert result["errors"] == {"base": "nothing_enabled"}
    assert entry.options == {}


async def test_history_off_starts_no_peer(hass):
    run = AsyncMock()
    entry = await _setup(hass, {CONF_HISTORY: False}, run=run)
    run.assert_not_awaited()
    assert _panel(hass, entry) is not None


async def test_panel_only_off_still_supplies_history(hass):
    run = AsyncMock()
    entry = await _setup(hass, {CONF_PANEL: PANEL_OFF}, run=run)
    run.assert_awaited_once()
    assert _panel(hass, entry) is None


def test_panel_script_defines_the_element():
    source = PANEL_JS.read_text(encoding="utf-8")
    assert "customElements.define('energy-node-panel'" in source
    assert "energy_node_companion/panel_session/" in source


def test_panel_script_refreshes_on_reconnect():
    # HA-Neustart: Panel-Sitzungen liegen nur im Speicher. Das Panel muss nach
    # dem Wiederverbinden neu ausstellen und den iframe neu laden.
    source = PANEL_JS.read_text(encoding="utf-8")
    assert "hass.connected" in source
    assert "visibilitychange" in source
    assert "REFRESH_MS = 30 * 60 * 1000" in source


def test_panel_script_fills_the_viewport():
    # ha-panel-custom hat keine feste Hoehe. Mit height: 100% blieb der
    # iframe bei seinen 150px Standardhoehe.
    source = PANEL_JS.read_text(encoding="utf-8")
    host = source[source.index(":host {"):source.index("}", source.index(":host {"))]
    assert "height: 100%" not in host
    assert "100dvh" in host


def test_manifest_keeps_frontend_optional():
    manifest = json.loads((BASE / "manifest.json").read_text())
    assert "http" in manifest["dependencies"]
    assert "frontend" not in manifest["dependencies"]
    assert {"frontend", "panel_custom"} <= set(manifest["after_dependencies"])


def test_default_mode_is_all():
    assert PANEL_ALL == "all"
