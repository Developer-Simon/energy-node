"""Reiter Companion der Fake-Konsole.

Verbindet den gekoppelten Fake-Companion (HA-Seite, Kopplung, Prüfstrom,
Benutzertabelle) und den Verlauf-Peer (FakeCompanion) mit der Konsole. Die
Oberflaeche kommt in einem spaeteren Schritt dazu, hier ist nur die Logik.

Jede Aktion ist eine Zeile in ACTIONS. Fehler werden als ValueError mit dem
Text der Meldung geworfen, Kopplungsfehler zusaetzlich ins Protokoll
geschrieben.
"""
import ssl

from fake_companion import ROLES, FakeCompanion, Session
from fake_console import Log, Tab
from fake_ha_companion import HACompanion, HAUser, PairError
from console_companion_ui import COMPANION_HTML

ACTIONS = (
    "pair", "pair_wrong", "unpair", "drop_stream", "open_stream",
    "user_save", "user_remove", "manual", "resolve",
    "history_start", "history_stop", "history_scale", "history_offer", "history_faults",
)
MIN_DAYS = 1
MAX_DAYS = 800
MAX_FACTOR = 100.0
MAX_DELAY_MS = 60000
DEFAULT_DAYS = 400  # wie der CLI-Weg von fake_companion.py


def _history_context(url):
    """Unverifizierender Kontext fuer https-Dashboards, sonst None."""
    if not url.startswith("https://"):
        return None
    context = ssl.create_default_context()
    context.check_hostname = False
    context.verify_mode = ssl.CERT_NONE
    return context


def _number(body, key, kind, default=None):
    if key not in body:
        if default is None:
            raise ValueError(f"{key} fehlt")
        return default
    try:
        return kind(body[key])
    except (TypeError, ValueError):
        raise ValueError(f"{key} ist ungültig") from None


def _days(value):
    if not MIN_DAYS <= value <= MAX_DAYS:
        raise ValueError(f"Tage müssen zwischen {MIN_DAYS} und {MAX_DAYS} liegen")
    return value


class CompanionTab(Tab):
    name = "companion"
    title = "Companion"
    html = COMPANION_HTML

    def __init__(self, log: Log, companion_url: str, dashboard_url: str) -> None:
        self._log = log
        self._ha = HACompanion(companion_url, log=lambda text: log.add("ha", text))
        session = Session(dashboard_url, context=_history_context(dashboard_url))
        self._history = FakeCompanion(session, DEFAULT_DAYS, log=lambda text: log.add("verlauf", text))

    def state(self) -> dict:
        return {"ha": self._ha.state(), "history": self._history.state()}

    def act(self, action: str, body: dict) -> dict:
        if action not in ACTIONS:
            raise ValueError(f"unbekannte Aktion {action}")
        handler = getattr(self, "_" + action)
        try:
            handler(body)
        except PairError as error:
            self._log.add("ha", str(error))
            raise ValueError(str(error)) from None
        except OSError as error:
            text = f"Companion nicht erreichbar: {error}"
            self._log.add("ha", text)
            raise ValueError(text) from None
        return self.state()

    # HA-Companion

    def _pair(self, body):
        code = str(body.get("code") or "").strip()
        if not code:
            raise ValueError("Kopplungscode fehlt")
        self._ha.pair(code)

    def _pair_wrong(self, body):
        self._log.add("ha", self._ha.pair_wrong_code())

    def _unpair(self, body):
        self._ha.unpair()

    def _drop_stream(self, body):
        self._ha.drop_stream()

    def _open_stream(self, body):
        self._ha.open_stream()

    def _user_save(self, body):
        username = str(body.get("username") or "").strip()
        if not username:
            raise ValueError("Benutzername fehlt")
        ha_user_id = str(body.get("ha_user_id") or "").strip() or "ha-" + username
        name = str(body.get("name") or "").strip() or username
        self._ha.save_user(HAUser(
            ha_user_id, username, str(body.get("password") or ""), name,
            admin=bool(body.get("admin", False)), active=bool(body.get("active", True))))

    def _user_remove(self, body):
        ha_user_id = str(body.get("ha_user_id") or "")
        if not ha_user_id:
            raise ValueError("ha_user_id fehlt")
        self._ha.remove_user(ha_user_id)

    def _manual(self, body):
        self._ha.set_manual(bool(body.get("on", False)))

    def _resolve(self, body):
        request_id = str(body.get("id") or "")
        if not request_id:
            raise ValueError("id fehlt")
        self._ha.resolve(request_id, str(body.get("decision") or ""))

    # Verlauf-Peer

    def _history_start(self, body):
        self._history.days = _days(_number(body, "days", int))
        self._history.start()

    def _history_stop(self, body):
        self._history.stop()

    def _history_scale(self, body):
        role = str(body.get("role") or "")
        if role not in ROLES:
            raise ValueError(f"unbekannte Rolle {role}")
        factor = _number(body, "factor", float)
        if not 0 <= factor <= MAX_FACTOR:
            raise ValueError(f"Faktor muss zwischen 0 und {MAX_FACTOR:g} liegen")
        self._history.scale[role] = factor

    def _history_offer(self, body):
        if body.get("days") is not None:
            self._history.days = _days(_number(body, "days", int))
        if not self._history.running:
            raise ValueError("Der Peer ist nicht angemeldet")
        self._history.spawn(self._history.offer)

    def _history_faults(self, body):
        delay = _number(body, "delay_ms", int, default=0)
        if not 0 <= delay <= MAX_DELAY_MS:
            raise ValueError(f"Verzögerung muss zwischen 0 und {MAX_DELAY_MS} ms liegen")
        self._history.faults = {
            "delay_ms": delay,
            "silent": bool(body.get("silent", False)),
            "partial": bool(body.get("partial", False)),
        }
