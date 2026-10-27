"""Gemeinsamer TLS-Testserver fuer den Companion-Port (Kopplung und Benutzermeldungen)."""
import asyncio
import datetime
import ipaddress
import json
import secrets
import ssl

import pytest
from aiohttp import web
from aiohttp.test_utils import TestServer
from cryptography import x509
from cryptography.hazmat.primitives import hashes, serialization
from cryptography.hazmat.primitives.asymmetric import ec
from cryptography.x509.oid import NameOID

from custom_components.energy_node_companion.pairing import client_proof, fingerprint_of, server_proof

CODE = "k7pm-3qxd-8hwt"


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
    state["events"] ist die Warteschlange des Pruefstroms: (Name, Daten) werden gesendet,
    None beendet den Strom. state["stream_status"] != 200 beantwortet den Strom mit diesem Status.
    state["verified"] und state["changes"] sammeln Antworten und Benutzermeldungen des Links.
    """
    cert_path, key_path, der = _self_signed_certificate(tmp_path)
    fingerprint = fingerprint_of(der)
    state = {
        "bad_proof": False,
        "seen": [],
        "events": asyncio.Queue(),
        "stream_status": 200,
        "verified": [],
        "changes": [],
    }

    async def pair(request):
        body = await request.json()
        state["seen"].append(body)
        if body.get("proof") != client_proof(CODE, fingerprint, body.get("nonce", "")):
            return web.json_response({"code": "pairing_invalid"}, status=403)
        token = secrets.token_hex(32)
        proof = "00" * 32 if state["bad_proof"] else server_proof(CODE, fingerprint, body["nonce"], token)
        return web.json_response({"token": token, "proof": proof})

    async def stream(request):
        if state["stream_status"] != 200:
            return web.json_response({"code": "companion_unauthorized"}, status=state["stream_status"])
        response = web.StreamResponse(headers={"Content-Type": "text/event-stream"})
        await response.prepare(request)
        while True:
            item = await state["events"].get()
            if item is None:
                break
            name, payload = item
            await response.write(f"event: {name}\ndata: {json.dumps(payload)}\n\n".encode())
        return response

    async def verified(request):
        state["verified"].append((request.match_info["request_id"], await request.json()))
        return web.Response(status=204)

    async def changed(request):
        state["changes"].append(await request.json())
        return web.Response(status=204)

    app = web.Application()
    app.router.add_post("/api/v1/companion/pair", pair)
    app.router.add_get("/api/v1/companion/stream", stream)
    app.router.add_post("/api/v1/companion/verify/{request_id}", verified)
    app.router.add_post("/api/v1/companion/user-changed", changed)
    context = ssl.create_default_context(ssl.Purpose.CLIENT_AUTH)
    context.load_cert_chain(cert_path, key_path)
    server = TestServer(app, host="127.0.0.1")
    await server.start_server(ssl=context)
    try:
        yield f"https://127.0.0.1:{server.port}", fingerprint, state
    finally:
        # Den Strom beenden, sonst wartet der Server beim Schliessen auf ihn.
        await state["events"].put(None)
        await server.close()
