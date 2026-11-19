#!/usr/bin/env python3
"""Beispieldaten fuer einen leeren Knoten anlegen (Startinhalte).

Aufrufe:
  example_apply.py check <devices_dir>
      Exit 0, wenn der Knoten leer ist, sonst Exit 3.
  example_apply.py apply <example.json> <devices_dir> <data_dir>
      Haengt die Beispielgeraete, das Szenario und die Freigabe an.
  example_apply.py publish <host> <port> <user> <topic> <payload>
      Veroeffentlicht eine retained Nachricht. Das Passwort kommt roh auf stdin.

Alle Dateien werden atomar geschrieben (temp plus os.replace). Ein zweiter
Lauf aendert nichts, weil jede ID nur einmal angehaengt wird.
"""
import copy
import json
import os
import socket
import struct
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
from mqtt_probe import connect_packet, recv_exact, remaining_length  # noqa: E402

RULES_FILE = "automation_rules.json"
SEED_FILE = "example-seed.json"
TIMEOUT = 3.0


def _load_json(path, default):
    if not path.exists():
        return copy.deepcopy(default)
    with path.open(encoding="utf-8") as fh:
        return json.load(fh)


def _write_json(path, obj):
    path.parent.mkdir(parents=True, exist_ok=True)
    tmp = path.with_name(path.name + ".tmp")
    tmp.write_text(json.dumps(obj, indent=2, ensure_ascii=False) + "\n", encoding="utf-8")
    os.replace(tmp, path)


def node_is_empty(devices_dir: Path) -> bool:
    devices_dir = Path(devices_dir)
    for path in devices_dir.glob("*_devices.json"):
        if _load_json(path, []) != []:
            return False
    rules = _load_json(devices_dir / RULES_FILE, {})
    return not rules.get("rules") and not rules.get("scenarios")


def apply(example: dict, devices_dir: Path, data_dir: Path) -> None:
    devices_dir = Path(devices_dir)
    data_dir = Path(data_dir)

    for key, items in example.get("devices", {}).items():
        path = devices_dir / f"{key}.json"
        current = _load_json(path, [])
        known = {entry.get("id") for entry in current}
        for item in items:
            if item["id"] not in known:
                current.append(copy.deepcopy(item))
                known.add(item["id"])
        _write_json(path, current)

    rules_path = devices_dir / RULES_FILE
    rules = _load_json(rules_path, {"version": 1, "settings": {}, "rules": []})
    scenarios = rules.setdefault("scenarios", [])
    scenario = example["scenario"]
    if scenario["id"] not in {s.get("id") for s in scenarios}:
        scenarios.append(copy.deepcopy(scenario))
    settings = rules.setdefault("settings", {})
    if not settings.get("publish_allowed_prefixes"):
        settings["publish_allowed_prefixes"] = [example["publish_prefix"]]
        settings["publish_allowed_prefixes_auto"] = True
    _write_json(rules_path, rules)

    _write_json(data_dir / SEED_FILE, example)


def publish_retained(host: str, port: int, user: str, password: str, topic: str, payload: str) -> None:
    topic_bytes = topic.encode("utf-8")
    body = struct.pack("!H", len(topic_bytes)) + topic_bytes + payload.encode("utf-8")
    publish = b"\x31" + remaining_length(len(body)) + body  # PUBLISH, retain, QoS 0
    with socket.create_connection((host, port), timeout=TIMEOUT) as sock:
        sock.settimeout(TIMEOUT)
        sock.sendall(connect_packet(user, password.encode("utf-8")))
        ack = recv_exact(sock, 4)
        if ack[0] != 0x20 or ack[1] != 2:
            raise RuntimeError("kein CONNACK vom Broker")
        if ack[3] != 0:
            raise RuntimeError(f"Broker lehnt die Verbindung ab (Code {ack[3]})")
        sock.sendall(publish)
        sock.sendall(b"\xe0\x00")  # DISCONNECT


def main(argv):
    if len(argv) < 2:
        print(__doc__, file=sys.stderr)
        return 2
    cmd = argv[1]
    if cmd == "check" and len(argv) == 3:
        return 0 if node_is_empty(Path(argv[2])) else 3
    if cmd == "apply" and len(argv) == 5:
        example = json.loads(Path(argv[2]).read_text(encoding="utf-8"))
        apply(example, Path(argv[3]), Path(argv[4]))
        return 0
    if cmd == "publish" and len(argv) == 7:
        host, port, user, topic, payload = argv[2:7]
        password = sys.stdin.buffer.read().decode("utf-8")
        try:
            publish_retained(host, int(port), user, password, topic, payload)
        except (OSError, RuntimeError) as exc:
            print(str(exc), file=sys.stderr)
            return 1
        return 0
    print(__doc__, file=sys.stderr)
    return 2


if __name__ == "__main__":
    sys.exit(main(sys.argv))
