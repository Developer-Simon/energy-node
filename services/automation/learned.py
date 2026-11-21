"""Lernwerte in Automationsregeln: Bezugsangabe pruefen und aufloesen.

Ein Lernwert liegt als retained MQTT-Topic vor, Payload {"value", "n", "at"}.
Eine Regel verweist darauf mit einer Bezugsangabe, z. B.
{"learned": "site/day_start", "min": "23:00", "max": "01:00", "fallback": "00:00"}.

Dieses Modul ist reine Rechnung ohne MQTT. Es prueft die Bezugsangabe und
liefert den Wert, mit dem die Regel rechnet, zusammen mit seiner Herkunft
(learned, clamped oder fallback).
"""

from __future__ import annotations

import json
import math
import re
from typing import Optional

LEARNED_PREFIX = "outstation/energy_node/learned/"
LEARNED_KEY_RE = re.compile(r"^(device/[^/#+\s]{1,96}|site)/[a-z0-9_]{1,40}$")
DEFAULT_MAX_AGE_S = 604800
FUTURE_SKEW_S = 300

LEARNABLE = {
    "balance_threshold": ("threshold", "hold_seconds"),
    "topic_value": ("value", "hold_seconds"),
    "entity_value": ("value", "hold_seconds"),
    "time_window": ("start", "end", "hold_seconds"),
    "sun_window": ("hold_seconds",),
    "learned_state": ("hold_seconds",),
}

_TIME_FIELDS = ("start", "end")
_REF_KEYS = {"learned", "min", "max", "fallback", "offset", "max_age_s"}
_CLOCK_RE = re.compile(r"([01][0-9]|2[0-3]):([0-5][0-9])")
_DAY_MINUTES = 1440
_MAX_TIME_OFFSET_MIN = 720


def field_kind(field: str) -> str:
    return "time" if field in _TIME_FIELDS else "number"


def is_ref(value) -> bool:
    return isinstance(value, dict) and "learned" in value


def learned_topic(key: str) -> str:
    return LEARNED_PREFIX + key


def _is_number(value) -> bool:
    if not isinstance(value, (int, float)) or isinstance(value, bool):
        return False
    try:
        return math.isfinite(value)
    except OverflowError:
        # Ganzzahlen mit mehr als rund 308 Stellen passen nicht in ein float.
        return False


def _clock_minutes(value) -> Optional[int]:
    if not isinstance(value, str):
        return None
    match = _CLOCK_RE.fullmatch(value)
    if match is None:
        return None
    return int(match.group(1)) * 60 + int(match.group(2))


def _clock_text(minutes: int) -> str:
    minutes %= _DAY_MINUTES
    return f"{minutes // 60:02d}:{minutes % 60:02d}"


def validate_ref(ref: dict, kind: str, prefix: str, errors: list[str]) -> None:
    for key in sorted(set(ref) - _REF_KEYS):
        errors.append(f"{prefix}: unknown key {key!r}")

    key = ref.get("learned")
    if not isinstance(key, str) or LEARNED_KEY_RE.fullmatch(key) is None:
        errors.append(f"{prefix}: learned must be a key like site/day_start or device/<id>/usual_on")

    if kind == "time":
        _validate_clock_bounds(ref, prefix, errors)
        offset = ref.get("offset")
        if "offset" in ref and not (
            isinstance(offset, int) and not isinstance(offset, bool)
            and abs(offset) <= _MAX_TIME_OFFSET_MIN
        ):
            errors.append(f"{prefix}: offset must be whole minutes up to {_MAX_TIME_OFFSET_MIN}")
    else:
        _validate_number_bounds(ref, prefix, errors)
        if "offset" in ref and not _is_number(ref["offset"]):
            errors.append(f"{prefix}: offset must be a number")

    if "max_age_s" in ref and (not _is_number(ref["max_age_s"]) or ref["max_age_s"] <= 0):
        errors.append(f"{prefix}: max_age_s must be a number above 0")


def _validate_clock_bounds(ref: dict, prefix: str, errors: list[str]) -> None:
    low = _clock_minutes(ref.get("min"))
    high = _clock_minutes(ref.get("max"))
    fallback = _clock_minutes(ref.get("fallback"))
    for name, minutes in (("min", low), ("max", high), ("fallback", fallback)):
        if minutes is None:
            errors.append(f"{prefix}: {name} must be HH:MM")
    if low is None or high is None or fallback is None:
        return
    if (fallback - low) % _DAY_MINUTES > (high - low) % _DAY_MINUTES:
        errors.append(f"{prefix}: fallback must lie between min and max")


def _validate_number_bounds(ref: dict, prefix: str, errors: list[str]) -> None:
    numbers = {name: ref.get(name) for name in ("min", "max", "fallback")}
    for name, value in numbers.items():
        if not _is_number(value):
            errors.append(f"{prefix}: {name} must be a number")
    if not all(_is_number(value) for value in numbers.values()):
        return
    if numbers["min"] > numbers["max"]:
        errors.append(f"{prefix}: min must not be above max")
    elif not numbers["min"] <= numbers["fallback"] <= numbers["max"]:
        errors.append(f"{prefix}: fallback must lie between min and max")


def _fresh_value(payload: Optional[str], kind: str, now_ts: float, max_age_s: float):
    """Liefert den Wert aus dem Payload, oder None bei fehlendem, altem,
    zukuenftigem oder falsch typisiertem Wert. Das Alter rechnet aus `at`."""
    if payload is None:
        return None
    try:
        data = json.loads(payload)
    except (TypeError, ValueError, RecursionError, OverflowError):
        # Verschachteltes JSON kann den Parser an die Rekursionsgrenze bringen.
        return None
    if not isinstance(data, dict):
        return None
    at = data.get("at")
    if not _is_number(at) or at - now_ts > FUTURE_SKEW_S or now_ts - at > max_age_s:
        return None
    value = data.get("value")
    if kind == "time":
        return value if _clock_minutes(value) is not None else None
    if kind == "state":
        return value if isinstance(value, str) and value else None
    return value if _is_number(value) else None


def resolve_ref(ref: dict, kind: str, payload: Optional[str], now_ts: float) -> tuple[object, str]:
    """Liefert (Wert, Herkunft) fuer eine Bezugsangabe der Art `time` oder `number`.

    Der Lernwert wird auf min bis max geklemmt, danach kommt der Versatz dazu.
    Uhrzeiten liegen auf dem Kreis: ausserhalb des Bogens geht der Wert auf die
    naehere Grenze, bei gleichem Abstand auf min."""
    offset = ref.get("offset", 0)
    learned = _fresh_value(payload, kind, now_ts, ref.get("max_age_s", DEFAULT_MAX_AGE_S))
    if kind == "time":
        return _resolve_clock(ref, learned, offset)
    return _resolve_number(ref, learned, offset)


def _resolve_number(ref: dict, learned, offset) -> tuple[object, str]:
    if learned is None:
        return ref["fallback"] + offset, "fallback"
    if learned < ref["min"]:
        return ref["min"] + offset, "clamped"
    if learned > ref["max"]:
        return ref["max"] + offset, "clamped"
    return learned + offset, "learned"


def _resolve_clock(ref: dict, learned, offset) -> tuple[str, str]:
    low = _clock_minutes(ref["min"])
    high = _clock_minutes(ref["max"])
    if learned is None:
        return _clock_text(_clock_minutes(ref["fallback"]) + offset), "fallback"
    value = _clock_minutes(learned)
    if (value - low) % _DAY_MINUTES <= (high - low) % _DAY_MINUTES:
        return _clock_text(value + offset), "learned"
    to_high = (value - high) % _DAY_MINUTES
    to_low = (low - value) % _DAY_MINUTES
    if to_low <= to_high:
        return _clock_text(low + offset), "clamped"
    return _clock_text(high + offset), "clamped"


def resolve_condition(cond: dict, topic_values: dict, now_ts: float) -> tuple[dict, dict]:
    """Kopie der Bedingung mit den aufgeloesten Feldern und {feld: herkunft}.

    `topic_values` bildet das volle Topic auf den Payload-Text ab. Bei
    `learned_state` ersetzt die Aufloesung nichts, der Zustand kommt ueber
    `state_value`."""
    resolved = dict(cond)
    sources: dict[str, str] = {}
    for field in LEARNABLE.get(cond.get("type"), ()):
        ref = cond.get(field)
        if not is_ref(ref):
            continue
        payload = topic_values.get(learned_topic(ref["learned"]))
        resolved[field], sources[field] = resolve_ref(ref, field_kind(field), payload, now_ts)
    return resolved, sources


def state_value(cond: dict, topic_values: dict, now_ts: float) -> tuple[str, str]:
    """Zustand eines `learned_state` als (Wert, Herkunft). Fehlt oder altert der
    Lernwert, gilt `fallback`."""
    payload = topic_values.get(learned_topic(cond["learned"]))
    value = _fresh_value(payload, "state", now_ts, cond.get("max_age_s", DEFAULT_MAX_AGE_S))
    if value is None:
        return cond["fallback"], "fallback"
    return value, "learned"


def cond_learned_keys(cond: dict) -> set[str]:
    keys: set[str] = set()
    if cond.get("type") == "learned_state" and isinstance(cond.get("learned"), str):
        keys.add(cond["learned"])
    for field in LEARNABLE.get(cond.get("type"), ()):
        ref = cond.get(field)
        if is_ref(ref) and isinstance(ref["learned"], str):
            keys.add(ref["learned"])
    return keys
