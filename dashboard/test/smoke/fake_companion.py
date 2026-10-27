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
import socket
import ssl
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
    def __init__(self, base, context=None):
        self.base = base.rstrip("/")
        self.cookie = ""
        self.context = context
        self.closed = False  # gesetzt von close_stream, gelöscht von reopen
        self._response = None

    def _open(self, request, timeout):
        if self.cookie:
            request.add_header("Cookie", self.cookie)
        return urllib.request.urlopen(request, timeout=timeout, context=self.context)

    def close_stream(self):
        """Beendet die offene Stromantwort, damit stream() zurueckkehrt.

        Nur den Socket herunterfahren: response.close() wuerde auf den Lesevorgang
        des anderen Threads warten.
        """
        self.closed = True
        response = self._response
        if response is None:
            return
        try:
            response.fp.raw._sock.shutdown(socket.SHUT_RDWR)
        except (AttributeError, OSError):
            pass

    def reopen(self):
        """Erlaubt wieder Ströme, nach einem neuen start()."""
        self.closed = False

    def login(self):
        request = urllib.request.Request(self.base + "/api/v1/auth/guest", method="POST")
        with urllib.request.urlopen(request, timeout=10, context=self.context) as response:
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
            self._response = response
            try:
                if self.closed:
                    return  # Stop kam während der Anmeldung: Strom sofort schließen
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
            finally:
                self._response = None


class FakeCompanion:
    def __init__(self, session, days, log=lambda text: None):
        self.session = session
        self.days = days
        self.log = log
        self.peer_id = ""
        self.max_chunk = 500
        self.scale = {role: 1.0 for role in ROLES}
        self.faults = {"delay_ms": 0, "silent": False, "partial": False}
        self.running = False
        self.last_error = ""
        self._stop = threading.Event()
        self._thread = None
        self._lock = threading.Lock()

    def start(self):
        """Startet die Verbindungsschleife im Hintergrund. Mehrfacher Aufruf ist harmlos."""
        with self._lock:
            if self._thread is not None and self._thread.is_alive() and not self._stop.is_set():
                return
            self._stop = threading.Event()
            self.session.reopen()
            self.running = True
            self._thread = threading.Thread(target=self._loop, args=(self._stop,), daemon=True)
            self._thread.start()

    def stop(self):
        self._stop.set()
        self.running = False
        self.peer_id = ""
        self.session.close_stream()

    def state(self):
        return {
            "running": self.running,
            "peer_id": self.peer_id,
            "days": self.days,
            "scale": dict(self.scale),
            "faults": dict(self.faults),
            "error": self.last_error,
        }

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
        self.log("Angebot gesendet")

    def rows(self, series, begin, end):
        role = series.removeprefix("role:")
        if role not in ROLES:
            return []
        last = self.now_hour() - HOUR_MS
        begin = max(begin, self.first_day())
        start = -(-begin // HOUR_MS) * HOUR_MS
        out = []
        for ts in range(start, min(end, last + HOUR_MS), HOUR_MS):
            value = round(hour_values(ts)[role] * self.scale.get(role, 1.0), 1)
            out.append({"series": series, "ts": ts, "min": value, "max": value, "avg": value, "n": 1, "u": "Wh"})
        return out

    def deliver(self, base, seq, final, rows):
        self.session.post("/api/v1/history/exchange/deliver", {**base, "seq": seq, "final": final, "rows": rows})

    def answer(self, payload):
        to, req_id = payload.get("peer"), payload.get("req_id")
        if not to or not req_id:
            return
        tier = payload.get("tier")
        series = str(payload.get("series") or "")
        ranges = payload.get("ranges") or []
        base = {"peer": self.peer_id, "to": to, "req_id": req_id, "tier": tier}
        self.log(f"Nachfrage {series} {tier} {len(ranges)} Bereiche")
        if self.faults["silent"]:
            return
        rows = []
        if tier == TIER:
            for begin, end in ranges:
                rows.extend(self.rows(series, int(begin), int(end)))
        if self.faults["delay_ms"]:
            time.sleep(self.faults["delay_ms"] / 1000)
        if not rows:
            self.deliver(base, 0, True, [])
            self.log("geliefert 0 Zeilen")
            return
        sent = 0
        for seq, start in enumerate(range(0, len(rows), self.max_chunk)):
            part = rows[start:start + self.max_chunk]
            final = start + self.max_chunk >= len(rows) and not self.faults["partial"]
            self.deliver(base, seq, final, part)
            sent += len(part)
            if self.faults["partial"]:
                break
        self.log(f"geliefert {sent} Zeilen")

    def spawn(self, fn, *args):
        def run():
            try:
                fn(*args)
            except Exception as error:  # noqa: BLE001
                print("fake companion:", error, file=sys.stderr)
        threading.Thread(target=run, daemon=True).start()

    def refresher(self, stop):
        while not stop.wait(120):
            self.spawn(self.offer)

    def run_once(self, stop):
        self.session.login()
        announcement = self.session.get_json("/api/v1/history/exchange")
        self.max_chunk = int(announcement.get("max_rows_per_deliver", 500))
        if TIER not in (announcement.get("tiers") or []):
            raise RuntimeError("the dashboard does not announce tier 1h")
        for event, payload in self.session.stream():
            if stop.is_set():
                break
            if event == "hello":
                self.peer_id = str(payload.get("peer_id") or "")
                self.last_error = ""
                self.spawn(self.offer)
            elif event == "peer-joined":
                self.spawn(self.offer)
            elif event == "request":
                self.spawn(self.answer, payload)

    def _loop(self, stop):
        threading.Thread(target=self.refresher, args=(stop,), daemon=True).start()
        while not stop.is_set():
            try:
                self.run_once(stop)
            except (urllib.error.URLError, OSError, RuntimeError) as error:
                if stop.is_set():
                    break
                self.last_error = str(error)
                print("fake companion:", error, "- retry in 3 s", file=sys.stderr)
            stop.wait(3)

    def run(self):
        """Blockiert bis stop() aufgerufen wird (CLI-Weg)."""
        self.running = True
        try:
            self._loop(self._stop)
        finally:
            self.running = False


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
