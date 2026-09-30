import json
import os
import time
from dataclasses import dataclass, field
from typing import Dict, Optional


@dataclass(frozen=True)
class StoredMeta:
    """Was neben dem Zaehler in state.json steht.

    saved_at fehlt bei Dateien aus der Zeit vor dem Speicherintervall. Solche
    Dateien gelten als sauber geschrieben, damit der erste Start nach dem
    Update nichts wiederherstellt."""
    saved_at: Optional[float]
    clean: bool
    last_current_a: Dict[str, float] = field(default_factory=dict)


def _currents(raw):
    if not isinstance(raw, dict):
        return {}
    return {name: float(value) for name, value in raw.items()
            if isinstance(value, (int, float)) and not isinstance(value, bool)}


def load_state(config, state):
    """Liest config.state_file in state. None, wenn die Datei fehlt oder kaputt ist.

    Neues Format (units-Dict) -> state.load_dict, sonst Altformat."""
    try:
        data = json.loads(config.state_file.read_text())
    except Exception:
        return None  # Keine/kaputte Datei -> Start mit Defaults
    if not isinstance(data, dict):
        return None

    stored = data.get("units")
    if isinstance(stored, dict):
        state.load_dict(data)
    else:
        state.load_legacy_dict(data, config.topology)

    saved_at = data.get("saved_at")
    if not isinstance(saved_at, (int, float)) or isinstance(saved_at, bool):
        return StoredMeta(saved_at=None, clean=True)
    return StoredMeta(saved_at=float(saved_at), clean=data.get("clean") is True,
                      last_current_a=_currents(data.get("last_current_a")))


def save_state(config, state, *, now=None, clean=False, last_current_a=None):
    """Schreibt state atomar nach config.state_file. False statt Exception.

    tmp-Datei, fsync, os.replace: ein Absturz mitten im Schreiben laesst die
    alte Datei stehen statt einer halben."""
    data = state.to_dict()
    data["saved_at"] = time.time() if now is None else now
    data["clean"] = clean
    data["last_current_a"] = dict(last_current_a or {})
    path = config.state_file
    tmp = path.with_name(path.name + ".tmp")
    try:
        path.parent.mkdir(parents=True, exist_ok=True)
        with open(tmp, "w") as handle:
            handle.write(json.dumps(data))
            handle.flush()
            os.fsync(handle.fileno())
        os.replace(tmp, path)
        return True
    except Exception:
        return False  # Persistenz ist nett, aber kein Grund zum Absturz
