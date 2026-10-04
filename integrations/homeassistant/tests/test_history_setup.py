"""Einrichtung: Config Flow und Start des Peers."""
import json
from pathlib import Path
from unittest.mock import AsyncMock, patch

import aiohttp
import pytest
from homeassistant.config_entries import ConfigEntryState
from homeassistant.helpers import device_registry as dr
from pytest_homeassistant_custom_component.common import MockConfigEntry

from custom_components.energy_node_companion.const import CONF_URL, CONF_VERIFY_SSL, DOMAIN

BASE = Path(__file__).resolve().parents[1] / "custom_components/energy_node_companion"
FLOW = "custom_components.energy_node_companion.config_flow.DashboardClient"
GOOD = {"protocol": 1, "series": []}


@pytest.fixture(autouse=True)
def auto_enable_custom_integrations(recorder_mock, enable_custom_integrations):
    # manifest.json haengt am Recorder, also braucht jeder Setup-Test ihn.
    yield


async def _submit(hass, url="https://node.local:8443/", verify=True):
    first = await hass.config_entries.flow.async_init(DOMAIN, context={"source": "user"})
    return await hass.config_entries.flow.async_configure(
        first["flow_id"], {CONF_URL: url, CONF_VERIFY_SSL: verify}
    )


async def test_flow_creates_entry_with_normalised_url(hass):
    with (
        patch(f"{FLOW}.login", AsyncMock()),
        patch(f"{FLOW}.announcement", AsyncMock(return_value=GOOD)),
        patch("custom_components.energy_node_companion.async_setup_entry", AsyncMock(return_value=True)),
    ):
        result = await _submit(hass)
    assert result["type"] == "create_entry"
    assert result["title"] == "node.local:8443"
    assert result["data"] == {CONF_URL: "https://node.local:8443", CONF_VERIFY_SSL: True}


@pytest.mark.parametrize(
    ("login", "announcement", "error"),
    [
        (AsyncMock(side_effect=aiohttp.ClientError()), AsyncMock(return_value=GOOD), "cannot_connect"),
        (AsyncMock(), AsyncMock(return_value={"protocol": 2, "series": []}), "wrong_protocol"),
        (AsyncMock(), AsyncMock(return_value={"protocol": 1}), "dashboard_too_old"),
    ],
)
async def test_flow_reports_errors(hass, login, announcement, error):
    with patch(f"{FLOW}.login", login), patch(f"{FLOW}.announcement", announcement):
        result = await _submit(hass)
    assert result["type"] == "form"
    assert result["errors"] == {"base": error}


async def test_flow_rejects_url_without_scheme(hass):
    result = await _submit(hass, url="node.local:8443")
    assert result["errors"] == {CONF_URL: "invalid_url"}


async def test_flow_aborts_for_a_configured_dashboard(hass):
    MockConfigEntry(domain=DOMAIN, unique_id="https://node.local:8443").add_to_hass(hass)
    result = await _submit(hass)
    assert result["type"] == "abort"
    assert result["reason"] == "already_configured"


def _mqtt_device(hass, url="http://energy-node.tail1234.ts.net/"):
    """Das Geraet, das das Dashboard per MQTT Discovery anlegt (Task 4)."""
    mqtt_entry = MockConfigEntry(domain="mqtt")
    mqtt_entry.add_to_hass(hass)
    dr.async_get(hass).async_get_or_create(
        config_entry_id=mqtt_entry.entry_id,
        identifiers={("mqtt", "energy_node")},
        name="Energy Node",
        configuration_url=url,
    )


def _suggested(result, key):
    for field in result["data_schema"].schema:
        if field == key:
            return (field.description or {}).get("suggested_value")
    raise AssertionError(f"{key} not in form")


def _entry(hass, url="http://old.tail1234.ts.net:8080"):
    entry = MockConfigEntry(
        domain=DOMAIN, unique_id=url, title=url.split("//")[1],
        data={CONF_URL: url, CONF_VERIFY_SSL: True},
    )
    entry.add_to_hass(hass)
    return entry


async def test_flow_suggests_the_tailscale_name_on_the_dashboard_port(hass):
    _mqtt_device(hass)
    result = await hass.config_entries.flow.async_init(DOMAIN, context={"source": "user"})
    assert _suggested(result, CONF_URL) == "http://energy-node.tail1234.ts.net:8080"


async def test_flow_without_mqtt_device_has_no_suggestion(hass):
    result = await hass.config_entries.flow.async_init(DOMAIN, context={"source": "user"})
    assert result["type"] == "form"
    assert _suggested(result, CONF_URL) is None


async def test_reconfigure_changes_url_and_keeps_the_entry(hass):
    entry = _entry(hass)
    result = await entry.start_reconfigure_flow(hass)
    assert _suggested(result, CONF_URL) == "http://old.tail1234.ts.net:8080"
    with (
        patch(f"{FLOW}.login", AsyncMock()),
        patch(f"{FLOW}.announcement", AsyncMock(return_value=GOOD)),
        patch("custom_components.energy_node_companion.async_setup_entry", AsyncMock(return_value=True)),
    ):
        result = await hass.config_entries.flow.async_configure(
            result["flow_id"], {CONF_URL: "http://new.tail1234.ts.net:8081/", CONF_VERIFY_SSL: True}
        )
    assert result["type"] == "abort"
    assert result["reason"] == "reconfigure_successful"
    assert entry.data[CONF_URL] == "http://new.tail1234.ts.net:8081"
    assert entry.unique_id == "http://new.tail1234.ts.net:8081"
    assert entry.title == "new.tail1234.ts.net:8081"


async def test_reconfigure_rejects_the_address_of_another_entry(hass):
    _entry(hass, "http://other.tail1234.ts.net:8080")
    entry = _entry(hass)
    result = await entry.start_reconfigure_flow(hass)
    result = await hass.config_entries.flow.async_configure(
        result["flow_id"], {CONF_URL: "http://other.tail1234.ts.net:8080", CONF_VERIFY_SSL: True}
    )
    assert result["type"] == "abort"
    assert result["reason"] == "already_configured"


async def test_setup_starts_the_peer_and_unload_stops_it(hass):
    entry = MockConfigEntry(
        domain=DOMAIN, unique_id="https://node.local:8443",
        data={CONF_URL: "https://node.local:8443", CONF_VERIFY_SSL: True},
    )
    entry.add_to_hass(hass)
    with patch("custom_components.energy_node_companion.HistoryPeer.run", AsyncMock()) as run:
        assert await hass.config_entries.async_setup(entry.entry_id)
        await hass.async_block_till_done()
        run.assert_awaited_once()
        assert entry.state is ConfigEntryState.LOADED
        assert await hass.config_entries.async_unload(entry.entry_id)
    assert entry.state is ConfigEntryState.NOT_LOADED


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
    assert strings == json.loads((BASE / "translations/en.json").read_text())
    assert keys(strings) == keys(json.loads((BASE / "translations/de.json").read_text()))


def test_manifest():
    manifest = json.loads((BASE / "manifest.json").read_text())
    assert manifest["domain"] == DOMAIN
    assert manifest["requirements"] == []
    assert "recorder" in manifest["dependencies"]
