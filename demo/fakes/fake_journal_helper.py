#!/usr/bin/env python3
"""Simulated energy-node-dashboard-system-action for the demo.

Without any state file it behaves like before: read-journal with the argument
checks of the real helper, every other action exits 64, so reboot and poweroff
stay harmless locally.

With FAKE_SYSTEM_STATE set, the file decides how the helper answers: journal
mode per key, injected lines, helper-wide denied/hang/fail, and the result of
the system actions. With FAKE_SYSTEM_EVENTS set, each call that is denied,
hangs, fails or runs a system action appends one JSON line to that file.

The fake journal has one entry per second since the epoch stored in the file
given by FAKE_JOURNAL_EPOCH. The cursor is "s=<index>". caddy and tailscale
have no entries, like a node where the service is not installed.
"""
import copy
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

ERROR_MESSAGES = [
    "Connection to 192.168.1.50 failed: timeout after 5 s",
    "Failed to publish state: broker not reachable",
    "Verbindung zu 192.168.1.50 fehlgeschlagen: Zeitüberschreitung nach 5 s",
]

JOURNAL_MODES = ("normal", "errors", "empty")
HELPER_MODES = ("normal", "denied", "hang", "fail")
ACTION_RESULTS = ("ok", "fail")
SYSTEM_ACTIONS = ("restart-dashboard", "reboot", "poweroff", "apply-bridge-config", "apply-app-config",
                  "restart-mosquitto", "tailscale-up", "tailscale-logout", "tailscale-restart")
HANG_SECONDS = 15
DENIED_MESSAGE = "sudo: a password is required"
DEFAULT_STATE = {"journal": {}, "inject": [], "helper": "normal", "actions": "ok"}


def load_state(path):
    """Return the state from path, or a copy of DEFAULT_STATE if it is missing or broken."""
    state = copy.deepcopy(DEFAULT_STATE)
    if not path:
        return state
    try:
        with open(path, encoding="utf-8") as handle:
            loaded = json.load(handle)
    except (OSError, ValueError):
        return state
    if not isinstance(loaded, dict):
        return state
    for name, value in loaded.items():
        if name in DEFAULT_STATE and isinstance(value, type(DEFAULT_STATE[name])):
            state[name] = value
    return state


def journal_mode(state, key):
    mode = state["journal"].get(key)
    if mode is not None:
        return mode
    return "empty" if key in EMPTY_KEYS else "normal"


def record(path, action, args, result):
    """Append one event line. Without a path or on a write error nothing happens."""
    if not path:
        return
    try:
        with open(path, "a", encoding="utf-8") as handle:
            handle.write(json.dumps({"t": time.time(), "action": action, "args": args,
                                     "result": result}) + "\n")
    except OSError:
        return


def entry(key, index, epoch, errors=False):
    if errors:
        prio = 3
        message = ERROR_MESSAGES[(index * 7 + len(key)) % len(ERROR_MESSAGES)]
    else:
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


def valid_journal_args(args):
    key, since, lines = args[0], args[1], args[2]
    cursor = args[3] if len(args) == 4 else ""
    if key not in KEYS or not since.isdigit() or int(since) not in SINCE:
        return False
    if not lines.isdigit() or int(lines) not in LINES:
        return False
    if cursor and (len(cursor) > 256 or not re.fullmatch(r"s=[0-9]+", cursor)):
        return False
    return True


def print_journal(state, args):
    key, since, lines = args[0], args[1], args[2]
    cursor = args[3] if len(args) == 4 else ""
    mode = journal_mode(state, key)
    if mode == "empty":
        return
    epoch = float(open(os.environ["FAKE_JOURNAL_EPOCH"]).read())
    now_index = int(time.time() - epoch)
    first = max(0, now_index - int(since) * 60)
    if cursor:
        first = max(first, int(cursor[2:]) + 1)
    last = min(now_index, first + int(lines) - 1) if cursor else now_index
    if not cursor:
        first = max(first, last - int(lines) + 1)
    injected = {}
    for item in state["inject"]:
        if isinstance(item, dict) and item.get("key") == key and isinstance(item.get("index"), int):
            injected[item["index"]] = item
    for index in range(first, last + 1):
        row = entry(key, index, epoch, errors=(mode == "errors"))
        if index in injected:
            item = injected[index]
            row["MESSAGE"] = "%s (#%d)" % (item.get("message", ""), index)
            row["PRIORITY"] = str(item.get("priority", row["PRIORITY"]))
        print(json.dumps(row))


def main(argv=None):
    if argv is None:
        argv = sys.argv[1:]
    has_state = bool(os.environ.get("FAKE_SYSTEM_STATE"))
    state = load_state(os.environ.get("FAKE_SYSTEM_STATE"))
    events = os.environ.get("FAKE_SYSTEM_EVENTS")
    action = argv[0] if argv else ""
    args = argv[1:]

    if state["helper"] == "denied":
        sys.stderr.write(DENIED_MESSAGE + "\n")
        record(events, action, args, "denied")
        return 1

    if action == "read-journal":
        if len(argv) not in (4, 5):
            return 64
        if not valid_journal_args(args):
            return 65
    elif action not in SYSTEM_ACTIONS or not has_state:
        return 64

    if state["helper"] == "hang":
        record(events, action, args, "hang")
        time.sleep(HANG_SECONDS)

    if state["helper"] == "fail":
        if action == "read-journal":
            sys.stderr.write("journalctl: simulated failure\n")
        else:
            sys.stderr.write("simulated failure\n")
        record(events, action, args, "fail")
        return 1

    if action == "read-journal":
        print_journal(state, args)
        return 0

    if state["actions"] == "fail":
        sys.stderr.write("simulated failure\n")
        record(events, action, args, "fail")
        return 1
    record(events, action, args, "ok")
    return 0


if __name__ == "__main__":
    sys.exit(main())
