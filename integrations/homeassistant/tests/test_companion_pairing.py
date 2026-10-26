"""Kopplung gegen einen TLS-Testserver, der den Companion-Port nachbildet."""
import aiohttp
import pytest

from custom_components.energy_node_companion.pairing import (
    PairingFailed,
    async_pair,
    client_proof,
    companion_base,
    server_proof,
)

from .companion_tls import CODE, tls_server  # noqa: F401  (tls_server is a pytest fixture)

pytestmark = pytest.mark.enable_socket

FINGERPRINT = "ab" * 32
NONCE = "00" * 32
TOKEN = "11" * 32


def test_proofs_match_shared_vector():
    assert client_proof(CODE, FINGERPRINT, NONCE) == "b5e9f8c6a6d988192bd0a1260722cc046b84b54246050f6fb9a585d2e03512c6"
    assert server_proof(CODE, FINGERPRINT, NONCE, TOKEN) == "a37bc927a473de888af78e8d6a87ac5760347bf6a813933fd52921489ccd6505"


def test_companion_base():
    assert companion_base("http://node.ts.net:8080") == "https://node.ts.net:8081"
    assert companion_base("http://[fd00::1]:8080/") == "https://[fd00::1]:8081"


async def test_pair_against_tls_server(hass, tls_server):
    base, fingerprint, state = tls_server
    async with aiohttp.ClientSession() as session:
        token = await async_pair(session, base, CODE, fingerprint)
    assert len(token) == 64
    assert len(state["seen"]) == 1
    assert state["seen"][0]["proof"] == client_proof(CODE, fingerprint, state["seen"][0]["nonce"])


async def test_pair_rejected_code(hass, tls_server):
    base, fingerprint, _state = tls_server
    async with aiohttp.ClientSession() as session:
        with pytest.raises(PairingFailed) as failure:
            await async_pair(session, base, "ABCD-EFGH-JKLM", fingerprint)
    assert failure.value.reason == "invalid_code"


async def test_pair_detects_wrong_server_proof(hass, tls_server):
    base, fingerprint, state = tls_server
    state["bad_proof"] = True
    async with aiohttp.ClientSession() as session:
        with pytest.raises(PairingFailed) as failure:
            await async_pair(session, base, CODE, fingerprint)
    assert failure.value.reason == "bad_server_proof"


async def test_pinned_connection_refuses_other_certificate(hass, tls_server):
    base, _fingerprint, _state = tls_server
    async with aiohttp.ClientSession() as session:
        with pytest.raises(PairingFailed) as failure:
            await async_pair(session, base, CODE, "00" * 32)
    assert failure.value.reason == "cannot_connect"
