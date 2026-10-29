"""JSON-Client fuer Fakes, die in eigenen Prozessen laufen (nur Standardbibliothek)."""

import json
import threading
import time
import urllib.error
import urllib.request

from fakelog import Log


class RemoteFake:
    """Spricht mit einem Fake ueber HTTP und uebernimmt dessen Protokoll ins Konsolen-Log."""

    def __init__(self, base_url: str, source: str, log: Log, timeout: float = 3.0) -> None:
        self.base_url = base_url.rstrip("/")
        self.source = source
        self.log = log
        self.timeout = timeout
        self._cursor = 0

    def get(self, path: str) -> dict:
        return self._request(urllib.request.Request(f"{self.base_url}/{path}"))

    def post(self, path: str, body: dict) -> dict:
        req = urllib.request.Request(
            f"{self.base_url}/{path}",
            data=json.dumps(body).encode(),
            method="POST",
            headers={"Content-Type": "application/json"},
        )
        return self._request(req)

    def _request(self, req: urllib.request.Request) -> dict:
        try:
            with urllib.request.urlopen(req, timeout=self.timeout) as resp:
                return json.loads(resp.read().decode())
        except urllib.error.HTTPError as exc:
            text = None
            try:
                payload = json.loads(exc.read().decode())
                if isinstance(payload, dict):
                    text = payload.get("error")
            except (OSError, ValueError):
                pass
            if text:
                raise ValueError(text) from None
            raise ValueError(f"HTTP {exc.code}") from None
        except OSError as exc:
            # URLError und Timeout sind Unterklassen von OSError
            raise OSError(str(exc)) from exc

    def pump_log(self) -> None:
        """Holt neue Protokolleintraege des Fakes. Fehler werden beim naechsten Mal erneut versucht."""
        try:
            data = self.get(f"log?since={self._cursor}")
            if data.get("last", 0) < self._cursor:
                # Fake wurde neu gestartet: Zaehler beginnt wieder bei 0
                self._cursor = 0
                data = self.get("log?since=0")
            for e in data.get("entries", []):
                self.log.add(self.source, e.get("text", ""), e.get("level", "info"))
            self._cursor = data.get("last", self._cursor)
        except (OSError, ValueError):
            return

    def start(self, interval: float = 1.0) -> None:
        def loop() -> None:
            while True:
                self.pump_log()
                time.sleep(interval)

        threading.Thread(target=loop, daemon=True).start()
