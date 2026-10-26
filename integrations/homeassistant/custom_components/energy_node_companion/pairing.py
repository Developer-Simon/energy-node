"""Kopplung mit dem Dashboard ueber den Companion-Port.

Der Kopplungscode verlaesst die Integration nie. Beide Seiten weisen mit
HMAC-SHA256 nach, dass sie den Code kennen, und binden den Nachweis an den
Fingerabdruck des Zertifikats und an eine frische Nonce.
"""
import asyncio
import hashlib
import hmac
import secrets
import ssl
from urllib.parse import urlsplit

import aiohttp

from .const import COMPANION_PORT

PAIR_CONTEXT = "energy-node-pair-v1"
PAIR_TIMEOUT_S = 10
FETCH_TIMEOUT_S = 10


class PairingFailed(Exception):
    """reason: "invalid_code", "cannot_connect" oder "bad_server_proof"."""

    def __init__(self, reason: str) -> None:
        super().__init__(reason)
        self.reason = reason


def normalize_code(text: str) -> str:
    """Grossbuchstaben ohne Bindestriche und Leerzeichen."""
    return text.upper().replace("-", "").replace(" ", "")


def client_proof(code: str, fingerprint: str, nonce: str) -> str:
    message = f"{PAIR_CONTEXT}|client|{fingerprint}|{nonce}"
    return _mac(code, message)


def server_proof(code: str, fingerprint: str, nonce: str, token: str) -> str:
    message = f"{PAIR_CONTEXT}|server|{fingerprint}|{nonce}|{token}"
    return _mac(code, message)


def _mac(code: str, message: str) -> str:
    key = normalize_code(code).encode()
    return hmac.new(key, message.encode(), hashlib.sha256).hexdigest()


def companion_base(url: str) -> str:
    """Macht aus der Dashboard-URL die Basis des Companion-Ports."""
    parts = urlsplit(url)
    host = parts.hostname or ""
    if ":" in host:
        host = f"[{host}]"
    return f"https://{host}:{COMPANION_PORT}"


def fingerprint_of(der: bytes) -> str:
    return hashlib.sha256(der).hexdigest()


async def async_fetch_fingerprint(hass, base: str) -> str:
    """Liest das Zertifikat des Companion-Ports ohne Pruefung und liefert den Fingerabdruck."""
    parts = urlsplit(base)
    try:
        pem = await asyncio.wait_for(
            hass.async_add_executor_job(ssl.get_server_certificate, (parts.hostname, parts.port)),
            timeout=FETCH_TIMEOUT_S,
        )
    except (OSError, TimeoutError) as err:
        raise PairingFailed("cannot_connect") from err
    return fingerprint_of(ssl.PEM_cert_to_DER_cert(pem))


def pinned(fingerprint: str) -> aiohttp.Fingerprint:
    return aiohttp.Fingerprint(bytes.fromhex(fingerprint))


async def async_pair(session: aiohttp.ClientSession, base: str, code: str, fingerprint: str) -> str:
    """Koppelt sich und liefert das Token. Wirft PairingFailed bei jedem Fehler."""
    nonce = secrets.token_hex(32)
    proof = client_proof(code, fingerprint, nonce)
    try:
        async with session.post(
            f"{base}/api/v1/companion/pair",
            json={"nonce": nonce, "proof": proof},
            ssl=pinned(fingerprint),
            timeout=aiohttp.ClientTimeout(total=PAIR_TIMEOUT_S),
        ) as response:
            if response.status == 403:
                raise PairingFailed("invalid_code")
            if response.status != 200:
                raise PairingFailed("cannot_connect")
            answer = await response.json(content_type=None)
    except (aiohttp.ClientError, asyncio.TimeoutError, ValueError) as err:
        raise PairingFailed("cannot_connect") from err

    token = answer.get("token") or ""
    expected = server_proof(code, fingerprint, nonce, token)
    if not token or not hmac.compare_digest(expected, str(answer.get("proof") or "")):
        raise PairingFailed("bad_server_proof")
    return token
