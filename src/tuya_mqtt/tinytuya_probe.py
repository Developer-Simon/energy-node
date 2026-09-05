#!/usr/bin/env python3
"""Small JSON adapter for the TinyTuya setup assistant."""

from __future__ import annotations

import json
import sys
from typing import Any

import tinytuya


def fail(message: str) -> None:
    print(json.dumps({"ok": False, "error": message}, ensure_ascii=True), flush=True)
    raise SystemExit(0)


def text(value: Any) -> str:
    return str(value or "").strip()


def number(value: Any, default: float = 3.3) -> float:
    try:
        return float(value)
    except (TypeError, ValueError):
        return default


def error_text(value: Any) -> str:
    if isinstance(value, (dict, list)):
        return json.dumps(value, ensure_ascii=True)
    return text(value)


def redact_error(message: str, payload: dict[str, Any]) -> str:
    for key in ("access_secret", "local_key"):
        secret = text(payload.get(key))
        if secret:
            message = message.replace(secret, "[redacted]")
    return message or "TinyTuya-Abfrage fehlgeschlagen"


def cloud_devices(request: dict[str, Any]) -> list[dict[str, Any]]:
    region = text(request.get("region"))
    access_id = text(request.get("access_id"))
    access_secret = text(request.get("access_secret"))
    if not region or not access_id or not access_secret:
        raise ValueError("Region, Access ID und Access Secret sind erforderlich")

    cloud = tinytuya.Cloud(apiRegion=region, apiKey=access_id, apiSecret=access_secret)
    result = cloud.getdevices()
    if isinstance(result, dict):
        detail = result.get("Error") or result.get("error") or result.get("msg")
        if result.get("success") is False or detail:
            raise RuntimeError(f"Tuya Cloud: {error_text(detail)}")
        result = result.get("result", result.get("devices", []))
    if not isinstance(result, list):
        raise RuntimeError("Tuya-Cloud lieferte keine Geräteliste")

    devices = []
    for item in result:
        if not isinstance(item, dict):
            continue
        device_id = text(item.get("id") or item.get("device_id"))
        if not device_id:
            continue
        devices.append(
            {
                "device_id": device_id,
                "name": text(item.get("name")) or device_id,
                "local_key": text(item.get("key") or item.get("local_key")),
                "ip": text(item.get("ip") or item.get("last_ip")),
                "version": number(item.get("version")),
            }
        )
    return devices


def local_status(request: dict[str, Any]) -> dict[str, Any]:
    device_id = text(request.get("device_id"))
    local_key = text(request.get("local_key"))
    ip = text(request.get("ip"))
    version = number(request.get("version"))
    if not device_id or not local_key or not ip:
        raise ValueError("Device ID, Local Key und IP-Adresse sind erforderlich")
    if version < 3:
        raise ValueError("Die Protokollversion muss mindestens 3.0 sein")

    device = tinytuya.Device(device_id, ip, local_key)
    device.set_version(version)
    if hasattr(device, "set_socketTimeout"):
        device.set_socketTimeout(5)
    status = device.status()
    if not isinstance(status, dict):
        raise RuntimeError("Das Gerät lieferte keine gültige Statusantwort")
    detail = status.get("Error") or status.get("Err") or status.get("error")
    if detail:
        raise RuntimeError(f"Lokaler Tuya-Status: {error_text(detail)}")

    raw_dps = status.get("dps", {})
    if not isinstance(raw_dps, dict):
        raw_dps = {}
    dps = [
        {"id": str(dp_id), "type": type(value).__name__, "value": value}
        for dp_id, value in raw_dps.items()
    ]
    return {"dps": dps, "raw": status}


def main() -> None:
    payload: dict[str, Any] = {}
    try:
        request = json.load(sys.stdin)
        if not isinstance(request, dict):
            fail("Ungültige Probe-Anfrage")
        operation = request.get("operation")
        payload = request.get("request")
        if not isinstance(payload, dict):
            fail("Ungültige Probe-Anfrage")
        if operation == "devices":
            print(json.dumps({"ok": True, "devices": cloud_devices(payload)}, ensure_ascii=True), flush=True)
            return
        if operation == "status":
            print(json.dumps({"ok": True, "status": local_status(payload)}, ensure_ascii=True), flush=True)
            return
        fail("Unbekannte TinyTuya-Operation")
    except ValueError as exc:
        fail(str(exc))
    except Exception as exc:
        fail(redact_error(str(exc), payload))


if __name__ == "__main__":
    main()
