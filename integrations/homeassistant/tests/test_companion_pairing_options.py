"""Kopplung in den Optionen: Code eingeben, Token und Fingerabdruck landen im Eintrag."""
from unittest.mock import AsyncMock, patch

import pytest
from pytest_homeassistant_custom_component.common import MockConfigEntry

from custom_components.energy_node_companion.const import (
    CONF_FINGERPRINT, CONF_HISTORY, CONF_PAIRING_CODE, CONF_PANEL, CONF_STATS, CONF_TOKEN, CONF_URL,
    CONF_VERIFY_SSL, DOMAIN, PANEL_ALL,
)
from custom_components.energy_node_companion.pairing import PairingFailed

FLOW = "custom_components.energy_node_companion.config_flow"
RUN = "custom_components.energy_node_companion.HistoryPeer.run"
URL = "http://node.tail1234.ts.net:8080"
COMPANION = "https://node.tail1234.ts.net:8081"
CODE = "k7pm-3qxd-8hwt"
FINGERPRINT = "ab" * 32
TOKEN = "11" * 32


@pytest.fixture(autouse=True)
def auto_enable_custom_integrations(recorder_mock, enable_custom_integrations):
    yield


async def _setup(hass):
    entry = MockConfigEntry(
        domain=DOMAIN, unique_id=URL, options={},
        data={CONF_URL: URL, CONF_VERIFY_SSL: True},
    )
    entry.add_to_hass(hass)
    with patch(RUN, AsyncMock()):
        assert await hass.config_entries.async_setup(entry.entry_id)
        await hass.async_block_till_done()
    return entry


async def _submit(hass, entry, pairing_code=None):
    result = await hass.config_entries.options.async_init(entry.entry_id)
    data = {CONF_HISTORY: True, CONF_STATS: False, CONF_PANEL: PANEL_ALL}
    if pairing_code is not None:
        data[CONF_PAIRING_CODE] = pairing_code
    with patch(RUN, AsyncMock()):
        result = await hass.config_entries.options.async_configure(result["flow_id"], data)
        await hass.async_block_till_done()
    return result


async def test_options_pair_stores_token_and_fingerprint(hass):
    entry = await _setup(hass)
    fetch = AsyncMock(return_value=FINGERPRINT)
    pair = AsyncMock(return_value=TOKEN)
    with patch(f"{FLOW}.async_fetch_fingerprint", fetch), patch(f"{FLOW}.async_pair", pair):
        result = await _submit(hass, entry, CODE)
    assert result["type"] == "create_entry"
    fetch.assert_awaited_once()
    assert fetch.await_args.args[1] == COMPANION
    pair.assert_awaited_once()
    assert pair.await_args.args[1:] == (COMPANION, CODE, FINGERPRINT)
    assert entry.data[CONF_TOKEN] == TOKEN
    assert entry.data[CONF_FINGERPRINT] == FINGERPRINT
    assert entry.data[CONF_URL] == URL
    assert CONF_PAIRING_CODE not in entry.options
    assert CONF_TOKEN not in entry.options


async def test_options_pair_invalid_code(hass):
    entry = await _setup(hass)
    fetch = AsyncMock(return_value=FINGERPRINT)
    pair = AsyncMock(side_effect=PairingFailed("invalid_code"))
    with patch(f"{FLOW}.async_fetch_fingerprint", fetch), patch(f"{FLOW}.async_pair", pair):
        result = await _submit(hass, entry, CODE)
    assert result["type"] == "form"
    assert result["errors"] == {"pairing_code": "pairing_invalid"}
    assert CONF_TOKEN not in entry.data
    assert CONF_FINGERPRINT not in entry.data


@pytest.mark.parametrize(
    "failure",
    [
        {"fetch": PairingFailed("cannot_connect")},
        {"pair": PairingFailed("cannot_connect")},
        {"pair": PairingFailed("bad_server_proof")},
    ],
)
async def test_options_pair_unreachable(hass, failure):
    entry = await _setup(hass)
    fetch = AsyncMock(side_effect=failure["fetch"]) if "fetch" in failure else AsyncMock(return_value=FINGERPRINT)
    pair = AsyncMock(side_effect=failure["pair"]) if "pair" in failure else AsyncMock(return_value=TOKEN)
    with patch(f"{FLOW}.async_fetch_fingerprint", fetch), patch(f"{FLOW}.async_pair", pair):
        result = await _submit(hass, entry, CODE)
    assert result["type"] == "form"
    assert result["errors"] == {"pairing_code": "pairing_failed"}
    assert CONF_TOKEN not in entry.data
    assert CONF_FINGERPRINT not in entry.data


async def test_options_without_code_do_not_pair(hass):
    entry = await _setup(hass)
    fetch = AsyncMock(return_value=FINGERPRINT)
    pair = AsyncMock(return_value=TOKEN)
    with patch(f"{FLOW}.async_fetch_fingerprint", fetch), patch(f"{FLOW}.async_pair", pair):
        result = await _submit(hass, entry)
    assert result["type"] == "create_entry"
    fetch.assert_not_awaited()
    pair.assert_not_awaited()
    assert CONF_TOKEN not in entry.data
