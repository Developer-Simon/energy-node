#!/usr/bin/env python3
"""Simulated energy-node-dashboard-system-action for --preset service-logs.

Only read-journal is implemented, with the argument checks of the real helper.
Every other action exits 64, so reboot and poweroff stay harmless locally.

The fake journal has one entry per second since the epoch stored in the file
given by FAKE_JOURNAL_EPOCH. The cursor is "s=<index>". caddy and tailscale
have no entries, like a node where the service is not installed.
"""
import json
import os
import re
import sys
import time

KEYS = {"dashboard", "updater", "automation", "battery-soc", "shelly", "trucki",
        "tuya", "apsystems", "mosquitto", "caddy", "tailscale"}
SINCE = {15, 60, 360, 1440, 10080}
LINES = {100, 200, 500, 1000}
EMPTY_KEYS = {"caddy", "tailscale"}
UNITS = {"dashboard": "energy-node-dashboard.service", "tailscale": "tailscaled.service",
         "mosquitto": "mosquitto.service", "caddy": "caddy.service"}

MESSAGES = [
    (6, "Polling cycle finished in 41 ms"),
    (6, "MQTT message received on outstation/battery_soc/state"),
    (5, "Configuration reloaded"),
    (4, "Retained message for bms/bank_b/voltage is older than 10 minutes"),
    (6, "Published state for shelly/netz_meanwell/status"),
    (3, "Connection to 192.168.1.50 failed: timeout after 5 s"),
    (7, "debug: tick"),
    (6, "Health check ok"),
]


def entry(key, index, epoch):
    prio, message = MESSAGES[(index * 7 + len(key)) % len(MESSAGES)]
    if index % 61 == 0:
        message = "Multi-line message\n  second line with details\n  third line"
    if index % 97 == 0:
        message = "x" * 2300
    row = {
        "__CURSOR": "s=%d" % index,
        "__REALTIME_TIMESTAMP": str(int((epoch + index) * 1_000_000)),
        "PRIORITY": str(prio),
        "_SYSTEMD_UNIT": UNITS.get(key, key + ".service"),
        "MESSAGE": "%s (#%d)" % (message, index),
    }
    if index % 53 == 0:
        row["MESSAGE"] = list(("binary \xff\xfe payload #%d" % index).encode("latin-1"))
    return row


def read_journal(args):
    key, since, lines = args[0], args[1], args[2]
    cursor = args[3] if len(args) == 4 else ""
    if key not in KEYS or not since.isdigit() or int(since) not in SINCE:
        sys.exit(65)
    if not lines.isdigit() or int(lines) not in LINES:
        sys.exit(65)
    if cursor and (len(cursor) > 256 or not re.fullmatch(r"s=[0-9]+", cursor)):
        sys.exit(65)
    if key in EMPTY_KEYS:
        return
    epoch = float(open(os.environ["FAKE_JOURNAL_EPOCH"]).read())
    now_index = int(time.time() - epoch)
    first = max(0, now_index - int(since) * 60)
    if cursor:
        first = max(first, int(cursor[2:]) + 1)
    last = min(now_index, first + int(lines) - 1) if cursor else now_index
    if not cursor:
        first = max(first, last - int(lines) + 1)
    for index in range(first, last + 1):
        print(json.dumps(entry(key, index, epoch)))


def main():
    argv = sys.argv[1:]
    if argv and argv[0] == "read-journal" and len(argv) in (4, 5):
        read_journal(argv[1:])
        return
    sys.exit(64)


main()
