"""Die Icon-Integration meldet nur ein Frontend-Modul an - sonst nichts."""
import json
from pathlib import Path
from types import SimpleNamespace
from unittest.mock import AsyncMock, patch

import pytest
from pytest_homeassistant_custom_component.common import MockConfigEntry

from custom_components.energy_node_icons import _async_register_frontend
from custom_components.energy_node_icons.const import DOMAIN, FRONTEND_FILES

BASE = Path(__file__).resolve().parents[1] / "custom_components/energy_node_icons"


def test_module_ships_with_the_integration():
    for name in FRONTEND_FILES:
        assert (BASE / "www" / name).is_file()


def test_manifest_needs_nothing_at_runtime():
    manifest = json.loads((BASE / "manifest.json").read_text())
    assert manifest["domain"] == DOMAIN
    assert manifest["requirements"] == []
    assert manifest["config_flow"] is True
    assert "http" in manifest["after_dependencies"]


def test_translation_keys_match_strings():
    def keys(node, prefix=""):
        out = set()
        for key, value in node.items():
            path = f"{prefix}.{key}" if prefix else key
            out.add(path)
            if isinstance(value, dict):
                out |= keys(value, path)
        return out

    strings = json.loads((BASE / "strings.json").read_text())
    assert keys(strings) == keys(json.loads((BASE / "translations/en.json").read_text()))
    assert keys(strings) == keys(json.loads((BASE / "translations/de.json").read_text()))


async def test_register_frontend_serves_and_loads_the_module():
    fake_http = SimpleNamespace(async_register_static_paths=AsyncMock())
    fake_hass = SimpleNamespace(http=fake_http, data={})

    with patch("custom_components.energy_node_icons.add_extra_js_url") as add_url:
        await _async_register_frontend(fake_hass)

    fake_http.async_register_static_paths.assert_awaited_once()
    assert add_url.call_count == len(FRONTEND_FILES)


async def test_register_frontend_is_a_noop_without_http():
    fake_hass = SimpleNamespace(http=None, data={})
    await _async_register_frontend(fake_hass)
    assert fake_hass.data == {}


async def test_register_frontend_runs_once():
    fake_http = SimpleNamespace(async_register_static_paths=AsyncMock())
    fake_hass = SimpleNamespace(http=fake_http, data={f"{DOMAIN}_frontend_registered": True})

    with patch("custom_components.energy_node_icons.add_extra_js_url") as add_url:
        await _async_register_frontend(fake_hass)

    fake_http.async_register_static_paths.assert_not_awaited()
    add_url.assert_not_called()


async def test_config_flow_creates_one_entry_only(hass):
    first = await hass.config_entries.flow.async_init(DOMAIN, context={"source": "user"})
    done = await hass.config_entries.flow.async_configure(first["flow_id"], {})
    assert done["type"] == "create_entry"

    MockConfigEntry(domain=DOMAIN, unique_id=DOMAIN).add_to_hass(hass)
    second = await hass.config_entries.flow.async_init(DOMAIN, context={"source": "user"})
    assert second["type"] == "abort"
    assert second["reason"] == "single_instance_allowed"
