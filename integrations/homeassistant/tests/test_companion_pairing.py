"""Kopplung gegen einen TLS-Testserver, der den Companion-Port nachbildet."""
import datetime
import ipaddress
import secrets
import ssl

import aiohttp
import pytest
from aiohttp import web
from aiohttp.test_utils import TestServer
from cryptography import x509
from cryptography.hazmat.primitives import hashes, serialization
from cryptography.hazmat.primitives.asymmetric import ec
from cryptography.x509.oid import NameOID

from custom_components.energy_node_companion.pairing import (
    PairingFailed,
    async_pair,
    client_proof,
    companion_base,
    fingerprint_of,
    server_proof,
)

pytestmark = pytest.mark.enable_socket

CODE = "k7pm-3qxd-8hwt"
FINGERPRINT = "ab" * 32
NONCE = "00" * 32
TOKEN = "11" * 32


def test_proofs_match_shared_vector():
    assert client_proof(CODE, FINGERPRINT, NONCE) == "b5e9f8c6a6d988192bd0a1260722cc046b84b54246050f6fb9a585d2e03512c6"
    assert server_proof(CODE, FINGERPRINT, NONCE, TOKEN) == "a37bc927a473de888af78e8d6a87ac5760347bf6a813933fd52921489ccd6505"


def test_companion_base():
    assert companion_base("http://node.ts.net:8080") == "https://node.ts.net:8081"
    assert companion_base("http://[fd00::1]:8080/") == "https://[fd00::1]:8081"


def _self_signed_certificate(directory):
    """Erzeugt ein selbstsigniertes Zertifikat fuer 127.0.0.1 und liefert (cert, key, der)."""
    key = ec.generate_private_key(ec.SECP256R1())
    name = x509.Name([x509.NameAttribute(NameOID.COMMON_NAME, "127.0.0.1")])
    now = datetime.datetime.now(datetime.timezone.utc)
    cert = (
        x509.CertificateBuilder()
        .subject_name(name)
        .issuer_name(name)
        .public_key(key.public_key())
        .serial_number(x509.random_serial_number())
        .not_valid_before(now - datetime.timedelta(minutes=5))
        .not_valid_after(now + datetime.timedelta(days=1))
        .add_extension(x509.SubjectAlternativeName([x509.IPAddress(ipaddress.ip_address("127.0.0.1"))]), critical=False)
        .sign(key, hashes.SHA256())
    )
    cert_path = directory / "cert.pem"
    key_path = directory / "key.pem"
    cert_path.write_bytes(cert.public_bytes(serialization.Encoding.PEM))
    key_path.write_bytes(
        key.private_bytes(
            serialization.Encoding.PEM,
            serialization.PrivateFormat.PKCS8,
            serialization.NoEncryption(),
        )
    )
    return cert_path, key_path, cert.public_bytes(serialization.Encoding.DER)


@pytest.fixture
async def tls_server(tmp_path):
    """Companion-Port mit Testzertifikat: prueft den Client-Nachweis und antwortet wie das Dashboard.

    state["bad_proof"] = True laesst den Server-Nachweis absichtlich falsch rechnen.
    """
    cert_path, key_path, der = _self_signed_certificate(tmp_path)
    fingerprint = fingerprint_of(der)
    state = {"bad_proof": False, "seen": []}

    async def pair(request):
        body = await request.json()
        state["seen"].append(body)
        if body.get("proof") != client_proof(CODE, fingerprint, body.get("nonce", "")):
            return web.json_response({"code": "pairing_invalid"}, status=403)
        token = secrets.token_hex(32)
        proof = "00" * 32 if state["bad_proof"] else server_proof(CODE, fingerprint, body["nonce"], token)
        return web.json_response({"token": token, "proof": proof})

    app = web.Application()
    app.router.add_post("/api/v1/companion/pair", pair)
    context = ssl.create_default_context(ssl.Purpose.CLIENT_AUTH)
    context.load_cert_chain(cert_path, key_path)
    server = TestServer(app, host="127.0.0.1")
    await server.start_server(ssl=context)
    try:
        yield f"https://127.0.0.1:{server.port}", fingerprint, state
    finally:
        await server.close()


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
