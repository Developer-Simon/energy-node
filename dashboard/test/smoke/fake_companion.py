#!/usr/bin/env python3
"""Fake Companion fuer den Smoke-Test der Energiestatistik.

Meldet sich wie die Home-Assistant-Integration (integrations/homeassistant,
peer.py) als Peer "companion" am Verlauf-Austausch des Dashboards an, bietet
Stundenwerte der Stufe 1h an und beantwortet Nachfragen. Die Werte sind
erfundene, aber stimmige Tageskurven (PV-Glocke mit Jahreszeit und Wetter,
Grundlast mit Morgen- und Abendspitze, Batterie, Netz), je Stunde
deterministisch. So haben Tag, Woche, Monat und Jahr etwas zu zeigen.

  fake_companion.py http://127.0.0.1:18100 [--days 400] [--stop-after SEK]

Nur Standardbibliothek. Strg-C beendet.
"""
import argparse
import json
import math
import os
import random
import sys
import threading
import time
import urllib.error
import urllib.request

HOUR_MS = 3_600_000
DAY_MS = 24 * HOUR_MS
TIER = "1h"
ROLES = ["pv", "load_total", "grid_import", "grid_export", "battery_charge", "battery_discharge"]


def hour_values(ts_ms):
    """Wh je Rolle fuer die UTC-Stunde ab ts_ms. Deterministisch."""
    rng = random.Random(ts_ms // HOUR_MS)
    day = ts_ms // DAY_MS
    day_rng = random.Random(day)
    local = ((ts_ms % DAY_MS) / HOUR_MS + 2) % 24  # grob Ortszeit (UTC+2)
    doy = (day % 365)
    season = 0.55 + 0.45 * math.sin((doy - 80) / 365 * 2 * math.pi)  # Hochsommer ~1
    weather = 0.25 + 0.75 * day_rng.random() ** 0.7
    bell = max(0.0, math.sin((local - 6) / 14 * math.pi)) if 6 <= local <= 20 else 0.0
    pv = 4300 * season * weather * bell ** 1.3 * (0.9 + 0.2 * rng.random())
    base = 330 + 80 * rng.random()
    morning = 620 * math.exp(-((local - 7.2) ** 2) / 1.6)
    evening = 2100 * math.exp(-((local - 19.3) ** 2) / 2.8)
    noon = 350 * math.exp(-((local - 12.5) ** 2) / 2.0)
    load = (base + morning + evening + noon) * (0.85 + 0.3 * rng.random())
    surplus = max(0.0, pv - load)
    deficit = max(0.0, load - pv)
    charge = min(surplus, 1800.0)
    discharge = min(deficit, 1100.0) if 17 <= local < 23 else 0.0
    values = {
        "pv": pv,
        "load_total": load,
        "battery_charge": charge,
        "battery_discharge": discharge,
        "grid_export": surplus - charge,
        "grid_import": deficit - discharge,
    }
    return {role: round(value, 1) for role, value in values.items()}


class Session:
    def __init__(self, base):
        self.base = base.rstrip("/")
        self.cookie = ""

    def _open(self, request, timeout):
        if self.cookie:
            request.add_header("Cookie", self.cookie)
        return urllib.request.urlopen(request, timeout=timeout)

    def login(self):
        request = urllib.request.Request(self.base + "/api/v1/auth/guest", method="POST")
        with urllib.request.urlopen(request, timeout=10) as response:
            cookies = response.headers.get_all("Set-Cookie") or []
        self.cookie = "; ".join(item.split(";", 1)[0] for item in cookies)
        if not self.cookie:
            raise RuntimeError("guest login: no session cookie")

    def get_json(self, path):
        with self._open(urllib.request.Request(self.base + path), 10) as response:
            return json.load(response)

    def post(self, path, body):
        request = urllib.request.Request(
            self.base + path, data=json.dumps(body).encode(), method="POST",
            headers={"Content-Type": "application/json"})
        with self._open(request, 10) as response:
            return response.status

    def stream(self):
        request = urllib.request.Request(self.base + "/api/v1/history/exchange/stream")
        with self._open(request, 120) as response:
            event, data = "", []
            for raw in response:
                line = raw.decode().rstrip("\r\n")
                if line:
                    if line.startswith("event:"):
                        event = line[6:].strip()
                    elif line.startswith("data:"):
                        data.append(line[5:].lstrip())
                    continue
                if event and data:
                    try:
                        yield event, json.loads("\n".join(data))
                    except ValueError:
                        pass
                event, data = "", []


class FakeCompanion:
    def __init__(self, session, days):
        self.session = session
        self.days = days
        self.peer_id = ""
        self.max_chunk = 500

    def now_hour(self):
        return int(time.time() * 1000) // HOUR_MS * HOUR_MS

    def first_day(self):
        return self.now_hour() // DAY_MS * DAY_MS - self.days * DAY_MS

    def census(self):
        """Stunden je UTC-Tag. Nur abgeschlossene Stunden (bis vor der laufenden)."""
        start = self.first_day()
        last_hour = self.now_hour() - HOUR_MS
        today = self.now_hour() // DAY_MS * DAY_MS
        counts = []
        for day in range(start, today + DAY_MS, DAY_MS):
            hours = max(0, min(24, (last_hour - day) // HOUR_MS + 1))
            counts.append(int(hours))
        return {"from": start, "step": DAY_MS, "n": counts}

    def offer(self):
        if not self.peer_id:
            return
        census = self.census()
        coverage = {TIER: {f"role:{role}": census for role in ROLES}}
        self.session.post("/api/v1/history/exchange/offer", {
            "peer": self.peer_id, "label": "Fake Home Assistant", "kind": "companion", "coverage": coverage})

    def rows(self, series, begin, end):
        role = series.removeprefix("role:")
        if role not in ROLES:
            return []
        last = self.now_hour() - HOUR_MS
        begin = max(begin, self.first_day())
        start = -(-begin // HOUR_MS) * HOUR_MS
        out = []
        for ts in range(start, min(end, last + HOUR_MS), HOUR_MS):
            value = hour_values(ts)[role]
            out.append({"series": series, "ts": ts, "min": value, "max": value, "avg": value, "n": 1, "u": "Wh"})
        return out

    def answer(self, payload):
        to, req_id = payload.get("peer"), payload.get("req_id")
        if not to or not req_id:
            return
        base = {"peer": self.peer_id, "to": to, "req_id": req_id, "tier": payload.get("tier")}
        rows = []
        if payload.get("tier") == TIER:
            for begin, end in payload.get("ranges") or []:
                rows.extend(self.rows(str(payload.get("series") or ""), int(begin), int(end)))
        if not rows:
            self.session.post("/api/v1/history/exchange/deliver", {**base, "seq": 0, "final": True, "rows": []})
            return
        for seq, start in enumerate(range(0, len(rows), self.max_chunk)):
            self.session.post("/api/v1/history/exchange/deliver", {
                **base, "seq": seq, "final": start + self.max_chunk >= len(rows),
                "rows": rows[start:start + self.max_chunk]})

    def spawn(self, fn, *args):
        def run():
            try:
                fn(*args)
            except Exception as error:  # noqa: BLE001
                print("fake companion:", error, file=sys.stderr)
        threading.Thread(target=run, daemon=True).start()

    def refresher(self):
        while True:
            time.sleep(120)
            self.spawn(self.offer)

    def run_once(self):
        self.session.login()
        announcement = self.session.get_json("/api/v1/history/exchange")
        self.max_chunk = int(announcement.get("max_rows_per_deliver", 500))
        if TIER not in (announcement.get("tiers") or []):
            raise RuntimeError("the dashboard does not announce tier 1h")
        for event, payload in self.session.stream():
            if event == "hello":
                self.peer_id = str(payload.get("peer_id") or "")
                self.spawn(self.offer)
            elif event == "peer-joined":
                self.spawn(self.offer)
            elif event == "request":
                self.spawn(self.answer, payload)

    def run(self):
        threading.Thread(target=self.refresher, daemon=True).start()
        while True:
            try:
                self.run_once()
            except (urllib.error.URLError, OSError, RuntimeError) as error:
                print("fake companion:", error, "- retry in 3 s", file=sys.stderr)
            time.sleep(3)


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("base")
    parser.add_argument("--days", type=int, default=400)
    parser.add_argument("--stop-after", type=float, default=0)
    args = parser.parse_args()
    companion = FakeCompanion(Session(args.base), args.days)
    if args.stop_after:
        threading.Timer(args.stop_after, lambda: os._exit(0)).start()
    try:
        companion.run()
    except KeyboardInterrupt:
        pass


if __name__ == "__main__":
    main()
