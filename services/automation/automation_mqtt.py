"""Energie-Automationen: eigenstaendiger Dienst, der die vom Dashboard
publizierte Bilanz (Teil A) sowie beliebige MQTT-Topics gegen Regeln
prueft und darauf MQTT-Publishes/Benachrichtigungen ausloest.

Siehe docs/services/automations-how-it-works.md fuer die volle
Semantik (Halte-/Hysterese-/Sperrzeit, Neustartverhalten, Sicherheit)."""

from __future__ import annotations

import collections
import copy
import datetime
import json
import logging
import os
import re
import sys
import tempfile
import time
import math
from dataclasses import dataclass, field
from typing import Any, Optional

from energy_node_common import appconfig
from energy_node_common.config import ReloadableConfig
from energy_node_common.discovery import entity_config, publish_discovery
from energy_node_common.mqtt import build_client, publish_online_status, publish_json
from energy_node_common.slave import Slave

import ha_template
import regulator
import sun

LOG = logging.getLogger("automation_mqtt")

BALANCE_TOPIC = "outstation/energy_node/energy/balance"


BALANCE_FIELDS = frozenset({
    "grid_export", "grid_import", "pv", "load_total", "base", "wallbox",
    "heat_pump", "battery_charge", "battery_discharge", "gap_applied",
    "autarkie", "eigenverbrauch",
    # Aus der Rolle battery_soc abgeleitet (Dashboard, internal/energy).
    # Brauchen keine eigene Auswertungslogik: sie stehen in der Bilanz.
    "battery_soc", "battery_capacity_kwh", "battery_energy_kwh",
})

# Eigene Kategorien der Energy-Seite: Feld "custom:<id>", Wert aus
# balance["categories"][id]. Die id folgt energyIDPattern im Dashboard.
CUSTOM_FIELD_PREFIX = "custom:"
_CUSTOM_FIELD_RE = re.compile(r"^custom:[a-z0-9_]+$")

_TOPIC_SEGMENT_RE = re.compile(r"^[^+#/]+$")


def is_balance_field(field) -> bool:
    return field in BALANCE_FIELDS or (isinstance(field, str) and bool(_CUSTOM_FIELD_RE.match(field)))


def balance_value(balance: dict, field: str):
    """Wert eines Bilanzfelds oder None. Eine geloeschte Kategorie hat keinen
    Wert und verhaelt sich damit wie ein fehlendes Feld."""
    if field.startswith(CUSTOM_FIELD_PREFIX):
        categories = balance.get("categories")
        return categories.get(field[len(CUSTOM_FIELD_PREFIX):]) if isinstance(categories, dict) else None
    return balance.get(field)


class RuleValidationError(ValueError):
    pass


def _topic_segments_ok(topic: str) -> Optional[str]:
    if not topic:
        return "topic must not be empty"
    if "+" in topic:
        return "topic must not contain the '+' wildcard"
    if "#" in topic:
        return "topic must not contain the '#' wildcard"
    segments = topic.split("/")
    if len(segments) < 2:
        return "topic must have at least two segments"
    for segment in segments:
        if segment == "" or not _TOPIC_SEGMENT_RE.match(segment):
            return f"topic has an empty or invalid segment: {topic!r}"
    return None


def validate_subscribe_topic(topic: str) -> Optional[str]:
    return _topic_segments_ok(topic)


def validate_publish_topic(topic: str) -> Optional[str]:
    error = _topic_segments_ok(topic)
    if error:
        return error
    if topic.startswith("homeassistant/"):
        return "topic must not fake a discovery config (homeassistant/...)"
    if topic.startswith("$SYS/"):
        return "topic must not target broker-internal $SYS/..."
    # Nach dem Topic-Umzug (Spec V9) liegt der Dashboard-eigene Baum unter
    # outstation/energy_node/. Geschuetzt sind genau die retained Topics, die
    # das Dashboard selbst publiziert: die Bilanz, die Erreichbarkeit und die
    # Node-Telemetrie (state / diagnostics). Die settings/-Topics (u. a.
    # simulation_active/set) bleiben erlaubt, damit eine Regel den Broadcast
    # setzen darf.
    if topic in (
        BALANCE_TOPIC,
        "outstation/energy_node/status/online",
        "outstation/energy_node/state",
        "outstation/energy_node/diagnostics",
    ):
        return "topic must not forge the dashboard's own topics (energy balance / availability / node telemetry)"
    return None


_ID_RE = re.compile(r"^[a-z0-9][a-z0-9_-]{0,63}$")
_TIME_RE = re.compile(r"^([01]\d|2[0-3]):[0-5]\d$")

_CONDITION_TYPES = {"balance_threshold", "topic_value", "time_window", "entity_value", "sun_window"}
_SUN_EVENTS = ("sunrise", "sunset")
_SUN_OFFSET_MAX_MIN = 240
_ACTION_TYPES = {"publish", "notification"}


@dataclass
class Settings:
    tick_interval_s: float = 10
    settling_seconds: float = 60
    balance_max_age_s: float = 30
    publish_min_interval_s: float = 30
    regulator_interval_s: float = 10
    publish_allowed_prefixes: list[str] = field(default_factory=list)
    history_limit: int = 10
    history_persist: bool = True
    latitude: Optional[float] = None
    longitude: Optional[float] = None

    @property
    def location(self) -> Optional[tuple[float, float]]:
        if self.latitude is None or self.longitude is None:
            return None
        return (self.latitude, self.longitude)


@dataclass
class RulesDocument:
    version: int
    settings: Settings
    rules: list[dict]
    scenarios: list[dict] = field(default_factory=list)


def _validate_condition(cond: dict, index: int, errors: list[str]) -> dict:
    prefix = f"condition[{index}]"
    ctype = cond.get("type")
    if ctype not in _CONDITION_TYPES:
        errors.append(f"{prefix}: unknown type {ctype!r}")
        return cond
    result = dict(cond)
    if ctype == "balance_threshold":
        if not is_balance_field(result.get("field")):
            errors.append(f"{prefix}: field must be one of {sorted(BALANCE_FIELDS)} or custom:<id>")
        if result.get("comparison") not in ("above", "below"):
            errors.append(f"{prefix}: comparison must be 'above' or 'below'")
        if not isinstance(result.get("threshold"), (int, float)):
            errors.append(f"{prefix}: threshold must be a number")
        result.setdefault("hysteresis", 0)
        result.setdefault("hold_seconds", 0)
    elif ctype == "topic_value":
        if err := validate_subscribe_topic(result.get("topic", "")):
            errors.append(f"{prefix}: {err}")
        if result.get("comparison") not in ("above", "below", "equals", "not_equals"):
            errors.append(f"{prefix}: invalid comparison")
        if result.get("comparison") in ("above", "below") and not isinstance(result.get("value"), (int, float)):
            errors.append(f"{prefix}: value must be a number for above/below")
        if result.get("comparison") in ("equals", "not_equals") and "text" not in result and "value" not in result:
            errors.append(f"{prefix}: equals/not_equals needs text or value")
        result.setdefault("json_key", "")
        result.setdefault("hold_seconds", 0)
    elif ctype == "entity_value":
        if err := validate_subscribe_topic(result.get("topic", "")):
            errors.append(f"{prefix}: {err}")
        entity_id = result.get("entity_id")
        if not isinstance(entity_id, str) or not entity_id:
            errors.append(f"{prefix}: entity_id must be a non-empty string")
        template = result.get("value_template", "")
        if not isinstance(template, str):
            errors.append(f"{prefix}: value_template must be a string")
        elif template and ha_template.parse_value_template(template) is None:
            errors.append(f"{prefix}: value_template is not one of the supported forms")
        if result.get("comparison") not in ("above", "below", "equals", "not_equals"):
            errors.append(f"{prefix}: invalid comparison")
        if result.get("comparison") in ("above", "below") and not isinstance(result.get("value"), (int, float)):
            errors.append(f"{prefix}: value must be a number for above/below")
        if result.get("comparison") in ("equals", "not_equals") and "text" not in result and "value" not in result:
            errors.append(f"{prefix}: equals/not_equals needs text or value")
        result.setdefault("value_template", "")
        result.setdefault("hold_seconds", 0)
    elif ctype == "time_window":
        if not _TIME_RE.match(str(result.get("start", ""))):
            errors.append(f"{prefix}: start must be HH:MM")
        if not _TIME_RE.match(str(result.get("end", ""))):
            errors.append(f"{prefix}: end must be HH:MM")
        _validate_weekdays(result, prefix, errors)
    elif ctype == "sun_window":
        for key in ("from", "to"):
            if result.get(key) not in _SUN_EVENTS:
                errors.append(f"{prefix}: {key} must be 'sunrise' or 'sunset'")
        for key in ("from_offset_min", "to_offset_min"):
            offset = result.setdefault(key, 0)
            if isinstance(offset, bool) or not isinstance(offset, int) \
                    or abs(offset) > _SUN_OFFSET_MAX_MIN:
                errors.append(f"{prefix}: {key} must be a whole number between "
                              f"-{_SUN_OFFSET_MAX_MIN} and {_SUN_OFFSET_MAX_MIN}")
        _validate_weekdays(result, prefix, errors)
    return result


def _validate_weekdays(result: dict, prefix: str, errors: list[str]) -> None:
    weekdays = result.get("weekdays", [])
    if any(not isinstance(d, int) or isinstance(d, bool) or d < 0 or d > 6 for d in weekdays):
        errors.append(f"{prefix}: weekdays must all be 0..6")
    result.setdefault("weekdays", [])


def _validate_location(settings_raw: dict, errors: list[str]) -> tuple[Optional[float], Optional[float]]:
    """Breiten- und Laengengrad sind optional, aber nur gemeinsam sinnvoll.
    Ohne Standort bleibt jede sun_window-Bedingung unerfuellt."""
    latitude = settings_raw.get("latitude")
    longitude = settings_raw.get("longitude")
    if latitude is None and longitude is None:
        return None, None
    ok = True
    for key, value, limit in (("latitude", latitude, 90), ("longitude", longitude, 180)):
        if isinstance(value, bool) or not isinstance(value, (int, float)) or not -limit <= value <= limit:
            errors.append(f"settings.{key} must be a number between -{limit} and {limit}")
            ok = False
    if not ok:
        return None, None
    return float(latitude), float(longitude)


def _validate_action(action: dict, index: int, errors: list[str]) -> dict:
    prefix = f"action[{index}]"
    atype = action.get("type")
    if atype not in _ACTION_TYPES:
        errors.append(f"{prefix}: unknown type {atype!r}")
        return action
    result = dict(action)
    if atype == "publish":
        if err := validate_publish_topic(result.get("topic", "")):
            errors.append(f"{prefix}: {err}")
        result.setdefault("retain", False)
        source = result.get("payload_source")
        if source not in ("constant", "balance", "topic", "toggle"):
            errors.append(f"{prefix}: payload_source must be constant/balance/topic/toggle")
        elif source == "balance" and not is_balance_field(result.get("field")):
            errors.append(f"{prefix}: field must be one of {sorted(BALANCE_FIELDS)} or custom:<id>")
        elif source in ("topic", "toggle") and (err := validate_subscribe_topic(result.get("source_topic", ""))):
            errors.append(f"{prefix}: {err}")
        elif source == "toggle" and not (result.get("payload_on") and result.get("payload_off")):
            errors.append(f"{prefix}: toggle requires payload_on and payload_off")
        result.setdefault("scale", 1)
        result.setdefault("offset", 0)
    elif atype == "notification":
        if result.get("severity") not in ("info", "warning", "critical"):
            errors.append(f"{prefix}: severity must be info/warning/critical")
        if not result.get("title"):
            errors.append(f"{prefix}: title required")
        if not result.get("message"):
            errors.append(f"{prefix}: message required")
    return result


def _validate_rule(rule: dict, index: int, errors: list[str], seen_ids: set[str], min_interval: float) -> dict:
    prefix = f"rule[{index}]"
    result = dict(rule)
    rule_id = result.get("id", "")
    if not _ID_RE.match(str(rule_id)):
        errors.append(f"{prefix}: id must match ^[a-z0-9][a-z0-9_-]{{0,63}}$")
    elif rule_id in seen_ids:
        errors.append(f"{prefix}: duplicate id {rule_id!r}")
    else:
        seen_ids.add(rule_id)
    if not isinstance(result.get("enabled"), bool):
        errors.append(f"{prefix}: enabled must be a boolean")
    result.setdefault("cooldown_seconds", 0)
    result.setdefault("repeat_seconds", 0)
    result.setdefault("history_enabled", False)
    conditions = result.get("conditions", [])
    if not (1 <= len(conditions) <= 8):
        errors.append(f"{prefix}: needs 1..8 conditions")
    result["conditions"] = [_validate_condition(c, i, errors) for i, c in enumerate(conditions)]
    actions = result.get("actions", [])
    if not (1 <= len(actions) <= 8):
        errors.append(f"{prefix}: needs 1..8 actions")
    result["actions"] = [_validate_action(a, i, errors) for i, a in enumerate(actions)]
    has_publish = any(a.get("type") == "publish" for a in result["actions"])
    if has_publish:
        if result["cooldown_seconds"] < min_interval:
            errors.append(f"{prefix}: cooldown_seconds must be >= {min_interval:g} for a rule with a publish action")
        for i, cond in enumerate(result["conditions"]):
            if "hold_seconds" in cond and cond["hold_seconds"] < min_interval:
                errors.append(f"{prefix}.condition[{i}]: hold_seconds must be >= {min_interval:g} for a rule with a publish action")
        if result["repeat_seconds"] > 0 and result["repeat_seconds"] < min_interval:
            errors.append(f"{prefix}: repeat_seconds must be >= {min_interval:g} for a rule with a publish action")
    if result["repeat_seconds"] > 0:
        for i, action in enumerate(result["actions"]):
            if action.get("type") == "publish" and action.get("payload_source") == "toggle":
                errors.append(f"{prefix}: repeat_seconds is not allowed with toggle action[{i}]")
                break
    return result


_SCENARIO_LIMIT = 8
_WHILE_MODES = ("regulate", "hold", "none")
_DIRECTIONS = ("producer", "consumer")
_BASE_SOURCES = ("entity", "balance", "last")


def _number(value) -> bool:
    return isinstance(value, (int, float)) and not isinstance(value, bool)


def _object(value, prefix: str, name: str, errors: list[str]) -> dict:
    if value is None:
        return {}
    if not isinstance(value, dict):
        errors.append(f"{prefix}: {name} must be an object")
        return {}
    return value


def _bounded_list(value, low: int, high: int, label: str, prefix: str, errors: list[str]) -> list:
    if not isinstance(value, list) or not (low <= len(value) <= high):
        errors.append(f"{prefix}: needs {low}..{high} {label}")
        return []
    return value


def _validate_each(items: list, validator, prefix: str, errors: list[str]) -> list:
    """Prueft jedes Element mit eigenem Index; die Fehler bekommen den Pfad davor."""
    result = []
    for i, item in enumerate(items):
        if not isinstance(item, dict):
            errors.append(f"{prefix}[{i}]: must be an object")
            continue
        local: list[str] = []
        result.append(validator(item, i, local))
        errors.extend(f"{prefix}.{message}" for message in local)
    return result


def _validate_phase(raw, name: str, prefix: str, errors: list[str]) -> dict:
    block = _object(raw, prefix, name, errors)
    actions = _bounded_list(block.get("actions", []), 0, 8, "actions", f"{prefix}.{name}", errors)
    return {"actions": _validate_each(actions, _validate_action, f"{prefix}.{name}", errors)}


def _validate_regulator(raw: dict, prefix: str, errors: list[str]) -> dict:
    result = dict(raw)

    output = dict(_object(raw.get("output"), prefix, "output", errors))
    if err := validate_publish_topic(output.get("topic", "")):
        errors.append(f"{prefix}.output: {err}")
    low, high = output.get("min"), output.get("max")
    if not (_number(low) and _number(high)):
        errors.append(f"{prefix}.output: min and max must be numbers")
    elif low >= high:
        errors.append(f"{prefix}.output: output.min must be below output.max")
    output.setdefault("step", None)
    output.setdefault("decimals", None)
    if output["step"] is not None and not (_number(output["step"]) and output["step"] > 0):
        errors.append(f"{prefix}.output: step must be above 0")
    result["output"] = output

    direction = raw.get("direction")
    if direction not in _DIRECTIONS:
        errors.append(f"{prefix}: direction must be producer or consumer")
    result["target_w"] = raw.get("target_w", 0)
    if not _number(result["target_w"]):
        errors.append(f"{prefix}: target_w must be a number")
    result["deadband_w"] = raw.get("deadband_w", 20)
    if not (_number(result["deadband_w"]) and result["deadband_w"] >= 0):
        errors.append(f"{prefix}: deadband_w must be 0 or more")
    result["gain"] = raw.get("gain", 0.8)
    if not (_number(result["gain"]) and 0.1 <= result["gain"] <= 1):
        errors.append(f"{prefix}: gain must be between 0.1 and 1")
    result["max_change_w"] = raw.get("max_change_w")
    if result["max_change_w"] is not None and not (_number(result["max_change_w"]) and result["max_change_w"] > 0):
        errors.append(f"{prefix}: max_change_w must be above 0")
    result["interval_s"] = raw.get("interval_s")
    if result["interval_s"] is not None and not (_number(result["interval_s"]) and 5 <= result["interval_s"] <= 3600):
        errors.append(f"{prefix}: interval_s must be between 5 and 3600")

    base = dict(_object(raw.get("base"), prefix, "base", errors))
    source = base.get("source")
    if source not in _BASE_SOURCES:
        errors.append(f"{prefix}.base: source must be entity, balance or last")
    elif source == "entity":
        if err := validate_subscribe_topic(base.get("topic", "")):
            errors.append(f"{prefix}.base: {err}")
        entity_id = base.get("entity_id")
        if not isinstance(entity_id, str) or not entity_id.strip():
            errors.append(f"{prefix}.base: entity_id must not be empty")
        template = base.get("value_template", "")
        if not isinstance(template, str) or (template and ha_template.parse_value_template(template) is None):
            errors.append(f"{prefix}.base: value_template is not one of the supported forms")
        base.setdefault("value_template", "")
    elif source == "balance" and not is_balance_field(base.get("field")):
        errors.append(f"{prefix}.base: field must be one of {sorted(BALANCE_FIELDS)} or custom:<id>")
    result["base"] = base

    fallback = raw.get("fallback")
    payload = fallback.get("payload") if isinstance(fallback, dict) else None
    has_payload = isinstance(payload, str) and payload.strip() != ""
    if direction == "producer" and not has_payload:
        errors.append(f"{prefix}: fallback.payload is required for a producer")
    elif fallback is not None and not has_payload:
        errors.append(f"{prefix}: fallback.payload must be a non-empty string")
    result["fallback"] = fallback if isinstance(fallback, dict) else None
    return result


def _validate_while(raw, prefix: str, errors: list[str], min_interval: float) -> dict:
    block = _object(raw, prefix, "while", errors)
    mode = block.get("mode", "none")
    if mode not in _WHILE_MODES:
        errors.append(f"{prefix}: while.mode must be regulate/hold/none")
        return {"mode": "none"}
    if mode == "none":
        return {"mode": "none"}
    if mode == "regulate":
        regulator = block.get("regulator")
        if not isinstance(regulator, dict):
            errors.append(f"{prefix}: while.regulator is required for mode regulate")
            return {"mode": mode}
        return {"mode": mode, "regulator": _validate_regulator(regulator, f"{prefix}.while.regulator", errors)}

    actions = _bounded_list(block.get("actions", []), 1, 8, "actions", f"{prefix}.while", errors)
    actions = _validate_each(actions, _validate_action, f"{prefix}.while", errors)
    repeat = block.get("repeat_seconds", 0)
    if not (_number(repeat) and repeat > 0):
        errors.append(f"{prefix}.while: repeat_seconds must be above 0 for mode hold")
    elif repeat < min_interval and any(a.get("type") == "publish" for a in actions):
        errors.append(f"{prefix}.while: repeat_seconds must be >= {min_interval:g} for a hold with a publish action")
    for i, action in enumerate(actions):
        if action.get("type") == "publish" and action.get("payload_source") == "toggle":
            errors.append(f"{prefix}.while: hold does not allow a toggle action[{i}]")
            break
    return {"mode": mode, "actions": actions, "repeat_seconds": repeat}


def _validate_scenario(scenario: dict, index: int, errors: list[str], seen_ids: set[str], min_interval: float) -> dict:
    prefix = f"scenario[{index}]"
    result = dict(scenario)
    scenario_id = result.get("id", "")
    if not _ID_RE.match(str(scenario_id)):
        errors.append(f"{prefix}: id must match ^[a-z0-9][a-z0-9_-]{{0,63}}$")
    elif scenario_id in seen_ids:
        errors.append(f"{prefix}: duplicate id {scenario_id!r}")
    else:
        seen_ids.add(scenario_id)
    name = result.get("name")
    if not isinstance(name, str) or not name.strip():
        errors.append(f"{prefix}: name must not be empty")
    if not isinstance(result.get("enabled"), bool):
        errors.append(f"{prefix}: enabled must be a boolean")
    result["history_enabled"] = result.get("history_enabled", False)
    if not isinstance(result["history_enabled"], bool):
        errors.append(f"{prefix}: history_enabled must be a boolean")

    when = _object(result.get("when"), prefix, "when", errors)
    conditions = _bounded_list(when.get("conditions", []), 1, 8, "conditions", f"{prefix}.when", errors)
    result["when"] = {"conditions": _validate_each(conditions, _validate_condition, f"{prefix}.when", errors)}

    until = result.get("until")
    if until is None:
        result["until"] = None
    else:
        until_block = _object(until, prefix, "until", errors)
        conditions = _bounded_list(until_block.get("conditions", []), 1, 8, "conditions", f"{prefix}.until", errors)
        result["until"] = {"conditions": _validate_each(conditions, _validate_condition, f"{prefix}.until", errors)}

    result["on_start"] = _validate_phase(result.get("on_start"), "on_start", prefix, errors)
    result["on_end"] = _validate_phase(result.get("on_end"), "on_end", prefix, errors)
    result["while"] = _validate_while(result.get("while"), prefix, errors, min_interval)
    return result


def _regulated_topic(scenario: dict) -> Optional[str]:
    while_block = scenario.get("while") or {}
    if while_block.get("mode") != "regulate":
        return None
    output = (while_block.get("regulator") or {}).get("output") or {}
    topic = output.get("topic")
    return topic if isinstance(topic, str) and topic else None


def load_and_validate(path: str) -> RulesDocument:
    with open(path, "r", encoding="utf-8") as handle:
        try:
            raw = json.load(handle)
        except json.JSONDecodeError as exc:
            raise RuleValidationError(f"invalid JSON: {exc}") from exc

    errors: list[str] = []
    if raw.get("version") != 1:
        errors.append("version must be 1")

    settings_raw = raw.get("settings", {}) or {}
    history_limit_raw = settings_raw.get("history_limit", 10)
    if isinstance(history_limit_raw, bool) or not isinstance(history_limit_raw, (int, float)) \
            or not (1 <= history_limit_raw <= 200):
        errors.append("settings.history_limit must be a number between 1 and 200")
        history_limit = 10
    else:
        history_limit = int(history_limit_raw)
    intervals: dict[str, float] = {}
    for key, default in (("publish_min_interval_s", 30), ("regulator_interval_s", 10)):
        raw_value = settings_raw.get(key, default)
        if isinstance(raw_value, bool) or not isinstance(raw_value, (int, float)) \
                or not (5 <= raw_value <= 3600):
            errors.append(f"settings.{key} must be a number between 5 and 3600")
            intervals[key] = default
        else:
            intervals[key] = raw_value
    latitude, longitude = _validate_location(settings_raw, errors)
    settings = Settings(
        tick_interval_s=settings_raw.get("tick_interval_s", 10),
        settling_seconds=settings_raw.get("settling_seconds", 60),
        balance_max_age_s=settings_raw.get("balance_max_age_s", 30),
        publish_min_interval_s=intervals["publish_min_interval_s"],
        regulator_interval_s=intervals["regulator_interval_s"],
        publish_allowed_prefixes=list(settings_raw.get("publish_allowed_prefixes", [])),
        history_limit=history_limit,
        history_persist=bool(settings_raw.get("history_persist", True)),
        latitude=latitude,
        longitude=longitude,
    )

    rules_raw = raw.get("rules", [])
    if len(rules_raw) > 16:
        errors.append("at most 16 rules are allowed")

    seen_ids: set[str] = set()
    rules = [_validate_rule(r, i, errors, seen_ids, settings.publish_min_interval_s) for i, r in enumerate(rules_raw)]

    scenarios_raw = raw.get("scenarios", [])
    if not isinstance(scenarios_raw, list):
        errors.append("scenarios must be a list")
        scenarios_raw = []
    if len(scenarios_raw) > _SCENARIO_LIMIT:
        errors.append(f"at most {_SCENARIO_LIMIT} scenarios are allowed")
    scenarios: list[dict] = []
    regulated: dict[str, str] = {}
    for i, scenario_raw in enumerate(scenarios_raw):
        if not isinstance(scenario_raw, dict):
            errors.append(f"scenario[{i}]: must be an object")
            continue
        scenario = _validate_scenario(scenario_raw, i, errors, seen_ids, settings.publish_min_interval_s)
        scenarios.append(scenario)
        topic = _regulated_topic(scenario)
        if topic is None:
            continue
        if topic in regulated:
            errors.append(f"scenario[{i}]: output.topic is already regulated by scenario {regulated[topic]!r}")
        else:
            regulated[topic] = scenario.get("id")

    if errors:
        raise RuleValidationError("; ".join(errors))

    return RulesDocument(version=raw["version"], settings=settings, rules=rules, scenarios=scenarios)


def extract_json_value(payload: str, json_key: str) -> Optional[float]:
    if not json_key:
        try:
            return float(payload.strip())
        except (TypeError, ValueError):
            return None
    try:
        data = json.loads(payload)
    except (TypeError, ValueError):
        return None
    if not isinstance(data, dict) or json_key not in data:
        return None
    try:
        return float(data[json_key])
    except (TypeError, ValueError):
        return None


def extract_json_text(payload: str, json_key: str) -> Optional[str]:
    if not json_key:
        return payload.strip()
    try:
        data = json.loads(payload)
    except (TypeError, ValueError):
        return None
    if not isinstance(data, dict) or json_key not in data:
        return None
    return str(data[json_key])


def _compare(value: float, comparison: str, threshold: float) -> bool:
    if comparison == "above":
        return value > threshold
    if comparison == "below":
        return value < threshold
    return False


def _local_at(day: datetime.date, clock: str) -> float:
    hour, minute = (int(part) for part in clock.split(":"))
    return datetime.datetime.combine(day, datetime.time(hour, minute)).timestamp()


def _time_window_bounds(cond: dict, day: datetime.date, location) -> tuple:
    start = _local_at(day, cond["start"])
    end = _local_at(day, cond["end"])
    if end < start:
        # Ueber Mitternacht, z. B. 22:00 -> 06:00: das Ende liegt am Folgetag.
        end = _local_at(day + datetime.timedelta(days=1), cond["end"])
    return (start, end), ""


def _sun_event(day: datetime.date, event: str, location) -> Optional[float]:
    times = sun.sun_times(day, *location)
    if times is None:
        return None
    return times[0] if event == "sunrise" else times[1]


def _sun_window_bounds(cond: dict, day: datetime.date, location) -> tuple:
    if location is None:
        return None, "location_missing"
    start_event = _sun_event(day, cond["from"], location)
    end_event = _sun_event(day, cond["to"], location)
    if start_event is None or end_event is None:
        return None, "no_sun_event"
    start = start_event + cond.get("from_offset_min", 0) * 60
    end = end_event + cond.get("to_offset_min", 0) * 60
    if end < start:
        # Das naechste Ende nach dem Start, z. B. Untergang -> Aufgang am Folgetag.
        next_event = _sun_event(day + datetime.timedelta(days=1), cond["to"], location)
        if next_event is None:
            return None, "no_sun_event"
        end = next_event + cond.get("to_offset_min", 0) * 60
    return (start, end), ""


_WINDOW_BOUNDS = {"time_window": _time_window_bounds, "sun_window": _sun_window_bounds}


def _active_window(cond: dict, now_ts: float, location) -> tuple:
    """Liefert (bounds, reason) fuer das Fenster, das jetzt gilt, oder das
    heutige, wenn keines gilt. Geprueft werden das heutige und das gestrige
    Fenster - ein Fenster ueber Mitternacht gehoert zu seinem Starttag, und
    nur dessen Wochentag zaehlt fuer "weekdays"."""
    bounds_for = _WINDOW_BOUNDS[cond["type"]]
    weekdays = cond.get("weekdays") or []
    today = datetime.date.fromtimestamp(now_ts)
    today_bounds, today_reason = None, ""
    for day in (today, today - datetime.timedelta(days=1)):
        bounds, reason = bounds_for(cond, day, location)
        if day == today:
            today_bounds, today_reason = bounds, reason
        if bounds is None:
            continue
        start, end = bounds
        if start <= now_ts < end and (not weekdays or day.weekday() in weekdays):
            return bounds, ""
    return None, today_reason if today_bounds is None else ""


def _window_display_bounds(cond: dict, now_ts: float, location) -> Optional[tuple]:
    bounds, _ = _active_window(cond, now_ts, location)
    if bounds is not None:
        return bounds
    return _WINDOW_BOUNDS[cond["type"]](cond, datetime.date.fromtimestamp(now_ts), location)[0]


def evaluate_condition_raw(cond: dict, *, balance: Optional[dict], topic_values: dict, now_ts: float,
                           location: Optional[tuple] = None) -> tuple:
    ctype = cond["type"]

    if ctype == "balance_threshold":
        if balance is None:
            return False, "balance_stale"
        value = balance_value(balance, cond["field"])
        if value is None:
            return False, "field_missing"
        return _compare(value, cond["comparison"], cond["threshold"]), ""

    if ctype == "topic_value":
        payload = topic_values.get(cond["topic"])
        if payload is None:
            return False, "topic_unknown"
        comparison = cond["comparison"]
        if comparison in ("above", "below"):
            value = extract_json_value(payload, cond.get("json_key", ""))
            if value is None:
                return False, "value_unparseable"
            return _compare(value, comparison, cond["value"]), ""
        text = extract_json_text(payload, cond.get("json_key", ""))
        if text is None:
            return False, "value_unparseable"
        expected = cond.get("text", str(cond.get("value", "")))
        equal = text == expected
        return (equal if comparison == "equals" else not equal), ""

    if ctype == "entity_value":
        payload = topic_values.get(cond["topic"])
        if payload is None:
            return False, "topic_unknown"
        text = ha_template.extract_value(payload, cond.get("value_template", ""))
        if not text:
            # "" bedeutet: Pfad fehlt und kein default(0). None bedeutet: der
            # Wert ist kein Skalar. Beides ist fuer einen Vergleich unbrauchbar.
            return False, "value_unparseable"
        comparison = cond["comparison"]
        if comparison in ("above", "below"):
            try:
                value = float(text.strip())
            except (TypeError, ValueError):
                return False, "value_unparseable"
            return _compare(value, comparison, cond["value"]), ""
        expected = cond.get("text", str(cond.get("value", "")))
        equal = text == expected
        return (equal if comparison == "equals" else not equal), ""

    if ctype in _WINDOW_BOUNDS:
        bounds, reason = _active_window(cond, now_ts, location)
        return bounds is not None, reason

    return False, "unknown_type"


def condition_value(cond: dict, *, balance: Optional[dict], topic_values: dict, now_ts: float,
                    location: Optional[tuple] = None) -> tuple:
    """Liefert (value, target) fuer die Live-Anzeige im Dashboard.

    value ist der Wert, mit dem die Regel gerade gerechnet hat, oder None,
    wenn keiner vorlag (Bilanz veraltet, Topic nie gesehen, Payload nicht
    lesbar). target ist die Schwelle, gegen die verglichen wurde, oder None,
    wo es keine gibt. Rein beschreibend - die Auswertung selbst passiert
    unveraendert in evaluate_condition_raw."""
    ctype = cond["type"]

    if ctype == "balance_threshold":
        value = None if balance is None else balance_value(balance, cond["field"])
        return value, cond.get("threshold")

    if ctype == "topic_value":
        payload = topic_values.get(cond["topic"])
        numeric = cond["comparison"] in ("above", "below")
        target = cond.get("value") if numeric else cond.get("text", cond.get("value"))
        if payload is None:
            return None, target
        if numeric:
            return extract_json_value(payload, cond.get("json_key", "")), target
        return extract_json_text(payload, cond.get("json_key", "")), target

    if ctype == "entity_value":
        numeric = cond["comparison"] in ("above", "below")
        target = cond.get("value") if numeric else cond.get("text", cond.get("value"))
        payload = topic_values.get(cond["topic"])
        if payload is None:
            return None, target
        text = ha_template.extract_value(payload, cond.get("value_template", ""))
        if not text:
            return None, target
        if not numeric:
            return text, target
        try:
            return float(text.strip()), target
        except (TypeError, ValueError):
            return None, target

    if ctype == "time_window":
        local = time.localtime(now_ts)
        return f"{local.tm_hour:02d}:{local.tm_min:02d}", None

    if ctype == "sun_window":
        bounds = _window_display_bounds(cond, now_ts, location)
        if bounds is None:
            return None, None
        start, end = (time.strftime("%H:%M", time.localtime(ts)) for ts in bounds)
        return f"{start} – {end}", None

    return None, None


def resolve_publish_value(action: dict, *, balance: Optional[dict], topic_values: dict) -> tuple:
    source = action["payload_source"]
    if source == "constant":
        return str(action.get("payload", "")), None

    if source == "toggle":
        payload = topic_values.get(action.get("source_topic", ""))
        if payload is None:
            return None, "source_topic_unknown"
        text = ha_template.extract_value(payload, action.get("value_template", ""))
        if not text:
            return None, "value_unparseable"
        payload_on = action.get("payload_on", "")
        payload_off = action.get("payload_off", "")
        return (payload_off if text == payload_on else payload_on), None

    if source == "balance":
        if balance is None:
            return None, "balance_stale"
        base_value = balance_value(balance, action["field"])
        if base_value is None:
            return None, "field_missing"
    else:  # "topic"
        payload = topic_values.get(action.get("source_topic", ""))
        if payload is None:
            return None, "source_topic_unknown"
        base_value = extract_json_value(payload, action.get("source_json_key", ""))
        if base_value is None:
            return None, "value_unparseable"

    value = base_value * action.get("scale", 1) + action.get("offset", 0)
    _, payload = regulator.quantize(
        value,
        step=action.get("step"),
        minimum=action.get("min"),
        maximum=action.get("max"),
        decimals=action.get("decimals"),
    )
    return payload, None


class ActionExecutor:
    def __init__(self, publish_fn, publish_allowed_prefixes: list):
        self.publish_fn = publish_fn
        self.publish_allowed_prefixes = publish_allowed_prefixes

    def preview(self, action: dict, *, balance: Optional[dict], topic_values: dict) -> dict:
        """Resolves the action's effect without executing it. Called every
        engine tick (even for rules that are not currently firing) so the
        state document can show 'would publish X' - see SPEC:335/337."""
        if action["type"] == "notification":
            return {"severity": action["severity"], "title": action["title"],
                    "message": action["message"], "blocked": False, "reason": None}

        topic = action["topic"]
        payload, block_reason = resolve_publish_value(action, balance=balance, topic_values=topic_values)
        if block_reason is not None:
            return {"topic": topic, "payload": None, "blocked": True, "reason": block_reason}

        allowed = any(topic.startswith(prefix) for prefix in self.publish_allowed_prefixes)
        if not allowed:
            reason = "no publish_allowed_prefixes configured" if not self.publish_allowed_prefixes else "topic does not match any allowed prefix"
            return {"topic": topic, "payload": payload, "blocked": True, "reason": reason}

        return {"topic": topic, "payload": payload, "blocked": False, "reason": None}

    def execute(self, action: dict, *, balance: Optional[dict], topic_values: dict, event_sink) -> dict:
        outcome = self.preview(action, balance=balance, topic_values=topic_values)
        if action["type"] == "notification":
            event_sink(f"{outcome['severity']}: {outcome['title']} - {outcome['message']}")
            return outcome
        if outcome["blocked"]:
            event_sink(f"blockiert: Publish auf {outcome.get('topic')} ({outcome.get('reason')})")
            return outcome
        self.publish_fn(outcome["topic"], outcome["payload"], action.get("retain", False))
        event_sink(f"veroeffentlicht: {outcome['topic']} = {outcome['payload']}")
        return outcome


@dataclass
class ConditionRuntime:
    since: Optional[float] = None
    latched: bool = False


@dataclass
class RuleRuntime:
    conditions: list = field(default_factory=list)
    fired_this_episode: bool = False
    last_fired_at: Optional[float] = None
    fire_count: int = 0


def _apply_hysteresis(cond: dict, cond_rt: ConditionRuntime, balance: Optional[dict]) -> Optional[bool]:
    """Returns the hysteresis-latched met/not-met state for
    balance_threshold conditions with hysteresis > 0, or None if
    hysteresis doesn't apply (caller then uses the raw comparison)."""
    if cond["type"] != "balance_threshold" or cond.get("hysteresis", 0) <= 0 or balance is None:
        return None
    value = balance_value(balance, cond["field"])
    if value is None:
        return None
    threshold, hysteresis = cond["threshold"], cond["hysteresis"]
    if cond["comparison"] == "above":
        cond_rt.latched = (value >= threshold - hysteresis) if cond_rt.latched else (value > threshold)
    else:
        cond_rt.latched = (value <= threshold + hysteresis) if cond_rt.latched else (value < threshold)
    return cond_rt.latched


def _evaluate_one(cond: dict, cond_rt: ConditionRuntime, *, balance, topic_values, now_ts, location):
    raw_met, reason = evaluate_condition_raw(cond, balance=balance, topic_values=topic_values, now_ts=now_ts,
                                             location=location)
    latched = _apply_hysteresis(cond, cond_rt, balance)
    if latched is not None:
        raw_met = latched

    if not raw_met:
        cond_rt.since = None
        return raw_met, False, reason

    if cond_rt.since is None:
        cond_rt.since = now_ts
    hold_seconds = cond.get("hold_seconds", 0)
    held_met = (now_ts - cond_rt.since) >= hold_seconds
    return raw_met, held_met, reason


@dataclass
class ConditionsResult:
    all_raw: bool
    all_held: bool
    saw_stale: bool
    reports: list


def evaluate_conditions(conditions: list, runtimes: list, *, balance: Optional[dict], topic_values: dict,
                        now_ts: float, location) -> ConditionsResult:
    """Evaluates all conditions of one rule or scenario against their runtimes
    and builds the report list shown in the dashboard."""
    reports = []
    all_raw = True
    all_held = True
    saw_stale = False
    for cond, cond_rt in zip(conditions, runtimes):
        raw_met, held_met, reason = _evaluate_one(
            cond, cond_rt, balance=balance, topic_values=topic_values, now_ts=now_ts, location=location)
        if reason == "balance_stale":
            saw_stale = True
        all_raw = all_raw and raw_met
        all_held = all_held and held_met
        hold_seconds = cond.get("hold_seconds", 0)
        remaining = max(0.0, hold_seconds - (now_ts - cond_rt.since)) if cond_rt.since is not None else float(hold_seconds)
        # value/target sind rein fuer die Anzeige im Dashboard - siehe
        # docs/superpowers/specs/2026-08-09-automations-visueller-editor-design.md.
        # met/since/hold_remaining bleiben unveraendert, darauf bauen Spec A und C auf.
        value, target = condition_value(cond, balance=balance, topic_values=topic_values, now_ts=now_ts,
                                        location=location)
        reports.append({"met": held_met, "raw_met": raw_met, "value": value,
                        "target": target, "since": cond_rt.since,
                        "hold_remaining": remaining})
    return ConditionsResult(all_raw=all_raw, all_held=all_held, saw_stale=saw_stale, reports=reports)


class Engine:
    """Ausgewertet wird jeder Tick fuer jede Regel neu; Zustand lebt nur in
    self._runtime (RAM, kein Neustart-ueberlebender Speicher - siehe
    docs/services/automations-how-it-works.md, Abschnitt 3)."""

    def __init__(self, executor: ActionExecutor, *, started_at: float, event_sink, history_sink=None):
        self.executor = executor
        self.started_at = started_at
        self.event_sink = event_sink
        self.history_sink = history_sink
        self._runtime: dict = {}

    def _runtime_for(self, rule_id: str, condition_count: int) -> RuleRuntime:
        rt = self._runtime.setdefault(rule_id, RuleRuntime())
        while len(rt.conditions) < condition_count:
            rt.conditions.append(ConditionRuntime())
        return rt

    def tick(self, doc: RulesDocument, *, balance: Optional[dict], balance_age: Optional[float],
              topic_values: dict, now_ts: float) -> dict:
        settling = (now_ts - self.started_at) < doc.settings.settling_seconds
        balance_stale = balance_age is None or balance_age > doc.settings.balance_max_age_s
        effective_balance = None if balance_stale else balance

        results = {}
        for rule in doc.rules:
            rule_id = rule["id"]
            rt = self._runtime_for(rule_id, len(rule["conditions"]))

            if not rule["enabled"]:
                rt.fired_this_episode = False
                results[rule_id] = self._rule_result(rt, rule, "disabled", None, [], preview=[], cooldown_remaining=0.0)
                continue

            evaluation = evaluate_conditions(rule["conditions"], rt.conditions, balance=effective_balance,
                                             topic_values=topic_values, now_ts=now_ts,
                                             location=doc.settings.location)
            condition_reports = evaluation.reports
            all_raw = evaluation.all_raw
            all_held = evaluation.all_held
            saw_stale = evaluation.saw_stale

            if not all_raw:
                rt.fired_this_episode = False

            cooldown_remaining = max(0.0, rule.get("cooldown_seconds", 0) - (now_ts - rt.last_fired_at)) if rt.last_fired_at is not None else 0.0

            preview = [self.executor.preview(a, balance=effective_balance, topic_values=topic_values) for a in rule["actions"]]

            if settling:
                results[rule_id] = self._rule_result(rt, rule, "settling", None, condition_reports, preview, cooldown_remaining)
            elif saw_stale:
                results[rule_id] = self._rule_result(rt, rule, "balance_stale", "balance_stale", condition_reports, preview, cooldown_remaining)
            elif not all_raw:
                results[rule_id] = self._rule_result(rt, rule, "conditions_not_met", None, condition_reports, preview, cooldown_remaining)
            elif rt.fired_this_episode:
                # Edge-triggered: still true, already fired this episode -
                # do not refire, just keep showing the live preview. Checked
                # before cooldown so the same still-true episode keeps
                # reporting "fired" instead of flipping to "cooldown" the
                # instant its own cooldown window starts ticking down.
                # repeat_seconds > 0 re-runs the action chain every
                # repeat_seconds while the conditions keep holding.
                if rule.get("repeat_seconds", 0) > 0 and all_held and now_ts - rt.last_fired_at >= rule["repeat_seconds"]:
                    outcome_result, fired_actions = self._fire(rule, rt, balance=effective_balance,
                                                               topic_values=topic_values, now_ts=now_ts,
                                                               record_history=False)
                    cooldown_remaining = max(0.0, rule.get("cooldown_seconds", 0) - (now_ts - rt.last_fired_at))
                    results[rule_id] = self._rule_result(rt, rule, outcome_result, None, condition_reports, fired_actions, cooldown_remaining)
                else:
                    results[rule_id] = self._rule_result(rt, rule, "fired", None, condition_reports, preview, cooldown_remaining)
            elif cooldown_remaining > 0:
                results[rule_id] = self._rule_result(rt, rule, "cooldown", None, condition_reports, preview, cooldown_remaining)
            elif not all_held:
                results[rule_id] = self._rule_result(rt, rule, "hold_pending", None, condition_reports, preview, cooldown_remaining)
            else:
                outcome_result, fired_actions = self._fire(rule, rt, balance=effective_balance,
                                                           topic_values=topic_values, now_ts=now_ts,
                                                           record_history=True)
                cooldown_remaining = max(0.0, rule.get("cooldown_seconds", 0) - (now_ts - rt.last_fired_at))
                results[rule_id] = self._rule_result(rt, rule, outcome_result, None, condition_reports, fired_actions, cooldown_remaining)

        return results

    def _fire(self, rule: dict, rt: RuleRuntime, *, balance, topic_values, now_ts: float,
              record_history: bool) -> tuple[str, list]:
        rule_id = rule["id"]
        fired_actions = []
        any_blocked = False
        any_error = False
        for action in rule["actions"]:
            try:
                outcome = self.executor.execute(action, balance=balance, topic_values=topic_values, event_sink=self.event_sink)
            except Exception as exc:  # noqa: BLE001 - a broker/publish failure must not crash the tick
                outcome = {"blocked": True, "reason": str(exc)}
                any_error = True
            fired_actions.append(outcome)
            if outcome.get("blocked"):
                any_blocked = True
        rt.last_fired_at = now_ts
        rt.fired_this_episode = True
        rt.fire_count += 1
        outcome_result = "error" if any_error else ("blocked" if any_blocked else "fired")
        if record_history and self.history_sink is not None:
            history_actions = [{**outcome, "type": action["type"]}
                                for action, outcome in zip(rule["actions"], fired_actions)]
            self.history_sink(rule_id, {"result": outcome_result, "reason": None,
                                         "test": False, "actions": history_actions})
        return outcome_result, fired_actions

    @staticmethod
    def _rule_result(rt: RuleRuntime, rule: dict, result: str, reason: Optional[str], conditions: list,
                      preview: list, cooldown_remaining: float) -> dict:
        return {
            "enabled": rule["enabled"], "result": result, "reason": reason,
            "fired_at": rt.last_fired_at, "fire_count": rt.fire_count,
            "cooldown_remaining": cooldown_remaining,
            "conditions": conditions, "actions": preview,
        }


@dataclass
class ScenarioRuntime:
    phase: str = "idle"
    since: Optional[float] = None
    when: list = field(default_factory=list)
    until: list = field(default_factory=list)
    definition: Optional[dict] = None
    last_value: Optional[float] = None
    last_sent_at: Optional[float] = None
    last_step_at: Optional[float] = None
    last_hold_at: Optional[float] = None
    net: Optional[float] = None
    error: Optional[float] = None
    base: Optional[float] = None
    reason: Optional[str] = None


_SCENARIO_ACTIVE_PHASES = ("active", "fallback")


def _finite_float(value) -> Optional[float]:
    if value is None or isinstance(value, bool):
        return None
    try:
        number = float(value)
    except (TypeError, ValueError):
        return None
    return number if math.isfinite(number) else None


def regulator_base_value(regulator: dict, *, balance: Optional[dict], topic_values: dict,
                         last_value: Optional[float]) -> Optional[float]:
    base = regulator["base"]
    source = base["source"]
    if source == "entity":
        payload = topic_values.get(base.get("topic", ""))
        if payload is None:
            return None
        return _finite_float(ha_template.extract_value(payload, base.get("value_template", "")))
    if source == "balance":
        if balance is None:
            return None
        return _finite_float(balance_value(balance, base["field"]))
    if last_value is not None:
        return float(last_value)
    output = regulator["output"]
    return float(output["max"] if regulator["direction"] == "producer" else output["min"])


class ScenarioEngine:
    """Zustandsmaschine fuer Szenarien (idle, active, fallback, disabled).
    Der Zustand lebt nur im RAM, wie bei Engine."""

    def __init__(self, executor: ActionExecutor, *, started_at: float, event_sink, history_sink=None):
        self.executor = executor
        self.started_at = started_at
        self.event_sink = event_sink
        self.history_sink = history_sink
        self._runtime: dict[str, ScenarioRuntime] = {}

    def tick(self, doc: RulesDocument, *, balance: Optional[dict], balance_age: Optional[float],
             topic_values: dict, now_ts: float) -> dict:
        settling = (now_ts - self.started_at) < doc.settings.settling_seconds
        balance_stale = balance_age is None or balance_age > doc.settings.balance_max_age_s
        effective_balance = None if balance_stale else balance
        location = doc.settings.location
        ctx = {"balance": effective_balance, "topic_values": topic_values}

        results = {}
        seen = set()
        settings = doc.settings
        for scenario in doc.scenarios:
            sid = scenario["id"]
            seen.add(sid)
            rt = self._runtime.setdefault(sid, ScenarioRuntime())

            if not scenario["enabled"]:
                if rt.phase in _SCENARIO_ACTIVE_PHASES:
                    self._end(sid, rt, **ctx)
                rt.phase = "disabled"
                rt.reason = None
                rt.when = []
                rt.until = []
                when_eval, until_eval = self._evaluate(scenario, rt, now_ts=now_ts, location=location, **ctx)
            else:
                when_eval, until_eval = self._evaluate(scenario, rt, now_ts=now_ts, location=location, **ctx)
                self._advance(sid, scenario, rt, when_eval, until_eval, settling=settling, now_ts=now_ts,
                              settings=settings, **ctx)

            results[sid] = self._report(rt, scenario, when_eval, until_eval)

        for sid in list(self._runtime):
            if sid in seen:
                continue
            rt = self._runtime.pop(sid)
            if rt.phase in _SCENARIO_ACTIVE_PHASES:
                self._end(sid, rt, **ctx)
        return results

    def _evaluate(self, scenario: dict, rt: ScenarioRuntime, *, balance, topic_values, now_ts, location):
        when_conditions = scenario["when"]["conditions"]
        while len(rt.when) < len(when_conditions):
            rt.when.append(ConditionRuntime())
        when_eval = evaluate_conditions(when_conditions, rt.when, balance=balance, topic_values=topic_values,
                                        now_ts=now_ts, location=location)
        until_eval = None
        if scenario["until"] is None:
            rt.until = []
        else:
            until_conditions = scenario["until"]["conditions"]
            while len(rt.until) < len(until_conditions):
                rt.until.append(ConditionRuntime())
            until_eval = evaluate_conditions(until_conditions, rt.until, balance=balance,
                                             topic_values=topic_values, now_ts=now_ts, location=location)
        return when_eval, until_eval

    def _advance(self, sid: str, scenario: dict, rt: ScenarioRuntime, when_eval, until_eval, *,
                 settling: bool, now_ts: float, settings, balance, topic_values) -> None:
        ctx = {"balance": balance, "topic_values": topic_values}
        if rt.phase not in _SCENARIO_ACTIVE_PHASES:
            rt.reason = None
            if settling:
                rt.reason = "settling"
            elif when_eval.saw_stale:
                rt.reason = "balance_stale"
            elif when_eval.all_held:
                self._start(sid, scenario, rt, now_ts=now_ts, **ctx)
                self._run_while(scenario, rt, now_ts=now_ts, settings=settings, **ctx)
            return

        if until_eval is None:
            ends = not when_eval.all_raw and not when_eval.saw_stale
        else:
            ends = until_eval.all_held
        if ends:
            self._end(sid, rt, **ctx)
            return
        rt.reason = None
        self._run_while(scenario, rt, now_ts=now_ts, settings=settings, **ctx)

    def _start(self, sid: str, scenario: dict, rt: ScenarioRuntime, *, now_ts: float, balance, topic_values) -> None:
        rt.phase = "active"
        rt.since = now_ts
        rt.last_hold_at = None
        rt.definition = copy.deepcopy(scenario)
        outcomes = self._run_actions(scenario["on_start"]["actions"], balance=balance, topic_values=topic_values)
        self._record(sid, "started", outcomes)

    def _end(self, sid: str, rt: ScenarioRuntime, *, balance, topic_values) -> None:
        on_end = (rt.definition or {}).get("on_end", {"actions": []})
        outcomes = self._run_actions(on_end["actions"], balance=balance, topic_values=topic_values)
        self._record(sid, "ended", outcomes)
        rt.phase = "idle"
        rt.since = None
        rt.definition = None
        rt.last_value = None
        rt.last_step_at = None
        rt.last_hold_at = None
        rt.when = []
        rt.until = []

    def _run_while(self, scenario: dict, rt: ScenarioRuntime, *, now_ts: float, settings, balance,
                   topic_values) -> None:
        while_block = scenario["while"]
        if while_block["mode"] == "hold":
            if rt.last_hold_at is None or now_ts - rt.last_hold_at >= while_block["repeat_seconds"]:
                rt.last_hold_at = now_ts
                self._run_actions(while_block["actions"], balance=balance, topic_values=topic_values)
        elif while_block["mode"] == "regulate":
            self._regulate(scenario["id"], scenario, rt, now_ts=now_ts, settings=settings,
                           balance=balance, topic_values=topic_values)

    def _regulate(self, sid: str, scenario: dict, rt: ScenarioRuntime, *, now_ts: float, settings,
                  balance, topic_values) -> None:
        regulator_def = scenario["while"]["regulator"]
        interval = regulator_def.get("interval_s")
        if interval is None:
            interval = settings.regulator_interval_s
        interval = max(interval, settings.tick_interval_s)
        if rt.last_step_at is not None and now_ts - rt.last_step_at < interval:
            return
        rt.last_step_at = now_ts

        output_topic = regulator_def["output"]["topic"]
        base = regulator_base_value(regulator_def, balance=balance, topic_values=topic_values,
                                    last_value=rt.last_value)
        if balance is None or base is None:
            reason = "balance_stale" if balance is None else "base_missing"
            rt.base = base
            rt.reason = reason
            if rt.phase != "fallback":
                rt.phase = "fallback"
                outcomes = []
                fallback = regulator_def.get("fallback")
                if fallback:
                    outcomes = self._run_actions([{
                        "type": "publish", "topic": output_topic, "payload_source": "constant",
                        "payload": fallback["payload"], "retain": False,
                    }], balance=balance, topic_values=topic_values)
                    if not outcomes[0].get("blocked"):
                        rt.last_value = _finite_float(fallback["payload"])
                self._record(sid, "fallback", outcomes, reason=reason)
            return

        if rt.phase == "fallback":
            rt.phase = "active"
            self._record(sid, "recovered", [])

        net = (balance_value(balance, "grid_import") or 0) - (balance_value(balance, "grid_export") or 0)
        step = regulator.compute_step(regulator_def, net=net, base=base, last_value=rt.last_value)
        rt.net = net
        rt.error = step.error
        rt.base = base
        rt.reason = step.reason
        if step.payload is None:
            return

        outcomes = self._run_actions([{
            "type": "publish", "topic": output_topic, "payload_source": "constant",
            "payload": step.payload, "retain": False,
        }], balance=balance, topic_values=topic_values)
        outcome = outcomes[0]
        if outcome.get("blocked"):
            rt.reason = outcome.get("reason")
            return
        rt.last_value = step.value
        rt.last_sent_at = now_ts
        deadband = regulator_def["deadband_w"]
        if deadband > 0 and abs(step.error) > 5 * deadband:
            self._record(sid, "jump", outcomes)

    def _run_actions(self, actions: list, *, balance, topic_values) -> list[dict]:
        outcomes = []
        for action in actions:
            try:
                outcome = self.executor.execute(action, balance=balance, topic_values=topic_values,
                                                event_sink=self.event_sink)
            except Exception as exc:  # noqa: BLE001 - a broker/publish failure must not crash the tick
                outcome = {"blocked": True, "reason": str(exc)}
            outcomes.append({**outcome, "type": action["type"]})
        return outcomes

    def _record(self, sid: str, result: str, outcomes: list, reason: Optional[str] = None) -> None:
        if self.history_sink is not None:
            self.history_sink(sid, {"result": result, "reason": reason, "test": False, "actions": outcomes})

    @staticmethod
    def _report(rt: ScenarioRuntime, scenario: dict, when_eval, until_eval) -> dict:
        return {
            "enabled": scenario["enabled"],
            "phase": rt.phase,
            "since": rt.since,
            "net": rt.net,
            "error": rt.error,
            "output": rt.last_value if rt.phase in _SCENARIO_ACTIVE_PHASES else None,
            "base": rt.base,
            "last_sent_at": rt.last_sent_at,
            "reason": rt.reason,
            "when": when_eval.reports,
            "until": until_eval.reports if until_eval is not None else [],
        }


class EventRing:
    def __init__(self, maxlen: int = 10):
        self._items = collections.deque(maxlen=maxlen)

    def push(self, message: str, at: float) -> None:
        self._items.appendleft({"at": at, "message": message})

    def as_list(self) -> list:
        return list(self._items)


def _atomic_write_json(path: str, data) -> None:
    """Best-effort atomares Schreiben: ein Absturz mitten im Schreiben darf
    dem Dashboard (ein zweiter, unabhaengig laufender Prozess, der dieselbe
    Datei liest) niemals eine halb geschriebene Datei zeigen."""
    directory = os.path.dirname(path) or "."
    os.makedirs(directory, exist_ok=True)
    fd, tmp_path = tempfile.mkstemp(dir=directory, prefix=".automation_history-")
    try:
        with os.fdopen(fd, "w", encoding="utf-8") as handle:
            json.dump(data, handle)
        os.replace(tmp_path, path)
    except Exception:
        try:
            os.remove(tmp_path)
        except OSError:
            pass
        raise


class RuleHistory:
    """Persistente Auslöser-Historie je Regel, begrenzt auf die letzten
    ``limit`` Eintraege beim jeweiligen record()-Aufruf. Anders als
    EventRing (RAM, ein String-Ring fuer alle Regeln zusammen) ist das hier
    strukturiert, je Regel getrennt und ueberlebt einen Dienst-Neustart -
    siehe docs/superpowers/specs/2026-08-10-automations-ausloeser-verlauf-design.md.
    path=None schaltet die Persistenz komplett aus (reines RAM) - fuer Tests,
    und im Dienst selbst ueber settings.history_persist=false erreichbar."""

    def __init__(self, path: Optional[str]):
        self.path = path
        self._items: dict = {}
        self._load()

    def _load(self) -> None:
        if not self.path:
            return
        try:
            with open(self.path, "r", encoding="utf-8") as handle:
                raw = json.load(handle)
            if isinstance(raw, dict):
                self._items = {str(rule_id): list(events) for rule_id, events in raw.items()
                               if isinstance(events, list)}
        except Exception:
            pass  # kaputte/fehlende Datei -> leer starten, wie battery_soc.load_state

    def record(self, rule_id: str, event: dict, limit: int) -> None:
        items = self._items.setdefault(rule_id, [])
        items.insert(0, event)
        del items[limit:]
        self._save()

    def as_list(self, rule_id: str) -> list:
        return list(self._items.get(rule_id, []))

    def _save(self) -> None:
        if not self.path:
            return
        try:
            _atomic_write_json(self.path, self._items)
        except Exception:
            pass  # Persistenz ist ein Nice-to-have, kein Show-Stopper


def build_state_document(rule_results: dict, events: list, *, at: float, online: bool) -> dict:
    return {
        "at": at, "online": online, "rules": rule_results, "events": events,
        "rules_total": len(rule_results),
        "rules_enabled": sum(1 for r in rule_results.values() if r.get("enabled") is True),
        "rules_fired_total": sum(r.get("fire_count", 0) for r in rule_results.values()),
    }


def build_history_document(history_by_rule: dict, *, at: float) -> dict:
    """Payload fuer das eigenstaendige history-Topic: nur Regeln mit
    history_enabled, damit das Topic nicht mit allen Regeln aufgeblaeht wird
    (siehe docs/superpowers/plans/2026-08-26-automations-verlauf-live-topic.md).
    rule_count ist der einzige skalare Wert fuer HA's value_template - die
    eigentliche Liste steckt im rohen Payload, den das Dashboard direkt
    liest (wie bei state/last_event/status/test_result)."""
    return {"at": at, "rules": history_by_rule, "rule_count": len(history_by_rule)}


class ChangeGatedPublisher:
    def __init__(self, publish_fn, heartbeat_seconds: float = 60):
        self.publish_fn = publish_fn
        self.heartbeat_seconds = heartbeat_seconds
        self._last_payload: Optional[str] = None
        self._last_published_at: Optional[float] = None

    def maybe_publish(self, topic: str, payload_dict: dict, now_ts: float) -> bool:
        payload = json.dumps(payload_dict, sort_keys=True)
        due_heartbeat = self._last_published_at is None or (now_ts - self._last_published_at) >= self.heartbeat_seconds
        if payload == self._last_payload and not due_heartbeat:
            return False
        self.publish_fn(topic, payload)
        self._last_payload = payload
        self._last_published_at = now_ts
        return True


DEFAULT_NODE_DEVICE_ID = "energy_node"


def device_block(service_id: str, node_device_id: str) -> dict:
    """HA-Geraet des Dienstes, ueber via_device am Node aufgehaengt - gleiches
    Muster wie Shelly und APsystems, damit die Device Map es unter den Node
    zeichnet."""
    return {"identifiers": [service_id], "name": "Energie-Automationen", "manufacturer": "Energy Node", "via_device": node_device_id}


def discovery_object_ids(device_id: str, base_topic: str, device_block: dict) -> list:
    state_topic = f"{base_topic}/state"
    state_config = entity_config(device_id, base_topic, "state", "Automations-Zustand", device_block,
                                  state_topic=state_topic, entity_category="diagnostic",
                                  value_template="{{ value_json.rules_enabled }}",
                                  json_attributes_topic=state_topic,
                                  unit_of_measurement="Regeln", state_class="measurement")
    last_event_topic = f"{base_topic}/last_event"
    last_event_config = entity_config(device_id, base_topic, "last_event", "Letztes Ereignis", device_block,
                                       state_topic=last_event_topic,
                                       value_template="{{ value_json.message }}",
                                       json_attributes_topic=last_event_topic)
    # Ohne einen Discovery-Eintrag abonniert der Dashboard-Client
    # settings/status nie (er folgt nur state_topics discoverter Entities),
    # und die Rule-Editor-UI (Task 12) koennte einen abgelehnten Reload nie
    # sehen - deshalb hier als eigene diagnostische Entity mitgefuehrt.
    status_topic = f"{base_topic}/settings/status"
    status_config = entity_config(device_id, base_topic, "status", "Automations-Laufzeitstatus", device_block,
                                   state_topic=status_topic, entity_category="diagnostic",
                                   value_template="{{ value_json.runtime_status }}",
                                   json_attributes_topic=status_topic)
    # Retained, damit das Dashboard nach einem Reload das letzte Testergebnis
    # noch sieht; die Oberflaeche verwirft Ergebnisse, die aelter sind als die
    # eigene Anfrage.
    test_result_topic = f"{base_topic}/test/result"
    test_result_config = entity_config(device_id, base_topic, "test_result", "Letzter Test", device_block,
                                        state_topic=test_result_topic, entity_category="diagnostic",
                                        value_template="{{ value_json.status }}",
                                        json_attributes_topic=test_result_topic)
    # Eigenes Topic statt Teil von state_config: die Verlaufslisten wuerden
    # das ohnehin schon pro Tick verglichene state-Dokument aufblaehen, siehe
    # docs/superpowers/plans/2026-08-26-automations-verlauf-live-topic.md.
    history_topic = f"{base_topic}/history"
    history_config = entity_config(device_id, base_topic, "history", "Auslöser-Verlauf", device_block,
                                    state_topic=history_topic, entity_category="diagnostic",
                                    value_template="{{ value_json.rule_count }}",
                                    json_attributes_topic=history_topic)
    return [("sensor", "state", state_config), ("sensor", "last_event", last_event_config),
            ("sensor", "status", status_config), ("sensor", "test_result", test_result_config),
            ("sensor", "history", history_config)]


def load_initial_document(config_store: ReloadableConfig) -> tuple:
    doc, error = config_store.load_or(RulesDocument(version=1, settings=Settings(), rules=[]))
    return doc, (str(error) if error is not None else None)


class AutomationService:
    def __init__(self, service_config, mqtt_config, config_store, app_config=None, service_name: str = "automation", history_path: Optional[str] = None, started_at: Optional[float] = None):
        self.service_config = service_config
        self.mqtt_config = mqtt_config
        self.config_store = config_store
        self.app_config = app_config
        self.service_name = service_name
        self.doc, self.startup_error = load_initial_document(config_store)
        if self.startup_error is not None:
            LOG.error("Regeldatei abgelehnt, warte auf config/reload: %s", self.startup_error)
        self.base_topic = f"outstation/{service_config.service_id}"
        self.test_command_topic = f"{self.base_topic}/test/set"
        self.test_result_topic = f"{self.base_topic}/test/result"
        self.executor = ActionExecutor(publish_fn=self._publish_action,
                                        publish_allowed_prefixes=self.doc.settings.publish_allowed_prefixes)
        self._history_path = history_path
        self.history = RuleHistory(history_path if self.doc.settings.history_persist else None)
        if started_at is None:
            started_at = time.time()
        self.engine = Engine(self.executor, started_at=started_at, event_sink=self._on_event,
                              history_sink=self._on_fired)
        self.events = EventRing(maxlen=10)
        self.topic_values: dict = {}
        self.subscribed_topics: set = set()
        self.last_balance = None
        self.last_balance_at = None
        self.client = None
        self.slave = None
        self.state_publisher = ChangeGatedPublisher(self._publish_raw_json, heartbeat_seconds=60)
        self.last_event_publisher = ChangeGatedPublisher(self._publish_raw_json, heartbeat_seconds=60)
        self.history_publisher = ChangeGatedPublisher(self._publish_raw_json, heartbeat_seconds=60)

    def required_topics(self, doc: RulesDocument) -> set:
        topics = {BALANCE_TOPIC, self.test_command_topic}
        for rule in doc.rules:
            for cond in rule["conditions"]:
                if cond["type"] in ("topic_value", "entity_value"):
                    topics.add(cond["topic"])
            for action in rule["actions"]:
                if action["type"] == "publish" and action.get("payload_source") in ("topic", "toggle"):
                    topics.add(action["source_topic"])
        return topics

    def apply_subscriptions(self, doc: RulesDocument) -> None:
        wanted = self.required_topics(doc)
        for topic in wanted - self.subscribed_topics:
            self.client.subscribe(topic)
        for topic in self.subscribed_topics - wanted:
            self.client.unsubscribe(topic)
        self.subscribed_topics = wanted

    def _publish_action(self, topic: str, payload: str, retain: bool) -> None:
        self.client.publish(topic, payload, qos=0, retain=retain)

    def _publish_raw_json(self, topic: str, payload: str) -> None:
        self.client.publish(topic, payload, qos=0, retain=True)

    def _on_event(self, message: str) -> None:
        self.events.push(message, at=time.time())

    def _on_fired(self, rule_id: str, event: dict) -> None:
        rule = next((r for r in self.doc.rules if r["id"] == rule_id), None)
        if not rule or not rule.get("history_enabled"):
            return
        self.history.record(rule_id, {**event, "at": time.time()}, limit=int(self.doc.settings.history_limit))

    def on_connect(self, client, userdata, flags, reason_code, properties=None):
        publish_online_status(client, self.base_topic, online=True, reason="connected")
        # clean_session=True: der Broker verwirft bei jedem (Re-)Connect alle
        # frueheren Subscriptions dieser Client-ID, auch bei einem
        # automatischen Reconnect nach einem Broker-Neustart. Der Cache muss
        # daher hier verworfen werden, sonst subscribed apply_subscriptions()
        # bei unveraendertem Dokument nach einem Reconnect auf gar nichts.
        self.subscribed_topics = set()
        self.apply_subscriptions(self.doc)
        node_device_id = DEFAULT_NODE_DEVICE_ID
        if getattr(self, "app_config", None) is not None:
            node_device_id = self.app_config.dashboard.node_device_id or DEFAULT_NODE_DEVICE_ID
        block = device_block(self.service_config.service_id, node_device_id)
        for component, object_id, config in discovery_object_ids(self.service_config.service_id, self.base_topic, block):
            publish_discovery(client, self.service_config.service_id, component, object_id, config)
        self.slave.start(client)

    def on_message(self, client, userdata, msg):
        topic = msg.topic
        payload = msg.payload.decode(errors="replace")
        if self.slave.handle_message(client, topic, payload):
            return
        if topic == self.test_command_topic:
            result = self.run_test_action(payload, time.time())
            self._publish_raw_json(self.test_result_topic, json.dumps(result, sort_keys=True))
            return
        if topic == BALANCE_TOPIC:
            try:
                data = json.loads(payload)
            except (TypeError, ValueError):
                return
            self.last_balance = data.get("balance")
            self.last_balance_at = time.time()
            return
        self.topic_values[topic] = payload

    def run_test_action(self, payload: str, now_ts: float) -> dict:
        """Fuehrt genau eine Aktion einer Regel aus - ohne Bedingungen,
        Sperrzeit und ohne den enabled-Schalter zu beachten.

        Die Zustandsmaschine der Regel bleibt bewusst unberuehrt
        (fire_count/last_fired_at/fired_this_episode), sonst wuerde das Testen
        genau das Verhalten verfaelschen, das geprueft werden soll. Folge:
        nach einem Test laeuft keine Sperrzeit an."""
        result = {"at": now_ts, "rule_id": None, "action_index": None,
                  "status": "error", "topic": None, "payload": None, "reason": None}

        try:
            request = json.loads(payload)
        except (TypeError, ValueError):
            result["reason"] = "payload is not valid JSON"
            return result
        if not isinstance(request, dict):
            result["reason"] = "payload is not a JSON object"
            return result

        rule_id = request.get("rule_id")
        action_index = request.get("action_index")
        if not isinstance(rule_id, str) or not rule_id:
            result["reason"] = "rule_id must be a non-empty string"
            return result
        if not isinstance(action_index, int) or isinstance(action_index, bool):
            result["reason"] = "action_index must be an integer"
            return result
        result["rule_id"] = rule_id
        result["action_index"] = action_index

        rule = next((r for r in self.doc.rules if r["id"] == rule_id), None)
        if rule is None:
            result["reason"] = f"unknown rule_id {rule_id!r}"
            return result
        if not 0 <= action_index < len(rule["actions"]):
            result["reason"] = f"action_index {action_index} out of range (rule has {len(rule['actions'])} actions)"
            return result

        balance_age = None if self.last_balance_at is None else now_ts - self.last_balance_at
        stale = balance_age is None or balance_age > self.doc.settings.balance_max_age_s
        balance = None if stale else self.last_balance

        try:
            outcome = self.executor.execute(
                rule["actions"][action_index], balance=balance, topic_values=self.topic_values,
                event_sink=lambda message: self._on_event(f"Test: {message}"))
        except Exception as exc:  # noqa: BLE001 - ein Broker-Fehler darf den Dienst nicht beenden
            result["reason"] = str(exc)
            return result

        if rule.get("history_enabled"):
            self.history.record(rule_id, {
                "at": now_ts, "result": "blocked" if outcome.get("blocked") else "fired",
                "reason": None, "test": True,
                "actions": [{**outcome, "type": rule["actions"][action_index]["type"]}],
            }, limit=int(self.doc.settings.history_limit))

        result["topic"] = outcome.get("topic")
        result["payload"] = outcome.get("payload")
        result["status"] = "blocked" if outcome.get("blocked") else "published"
        result["reason"] = outcome.get("reason")
        return result

    def poll_core(self):
        now = time.time()
        balance_age = None if self.last_balance_at is None else now - self.last_balance_at
        results = self.engine.tick(self.doc, balance=self.last_balance, balance_age=balance_age,
                                    topic_values=self.topic_values, now_ts=now)
        state_doc = build_state_document(results, self.events.as_list(), at=now, online=True)
        self.state_publisher.maybe_publish(f"{self.base_topic}/state", state_doc, now)
        history_by_rule = {rule["id"]: self.history.as_list(rule["id"])
                            for rule in self.doc.rules if rule.get("history_enabled")}
        history_doc = build_history_document(history_by_rule, at=now)
        self.history_publisher.maybe_publish(f"{self.base_topic}/history", history_doc, now)
        newest = self.events.as_list()
        if newest:
            self.last_event_publisher.maybe_publish(f"{self.base_topic}/last_event", newest[0], now)
        self.slave.note_update(self.client)

    def reload_config(self):
        """config.json und die Geraetedatei gemeinsam neu laden.

        Zuerst beide Kandidaten laden, dann beide uebernehmen: ein Fehler
        in einer der beiden Dateien laesst beide unveraendert, und der
        Slave meldet runtime_status: rejected mit dem Grund.
        """
        new_doc = self.config_store.load_candidate()  # raises RuleValidationError on an invalid file

        if hasattr(self, 'app_config') and self.app_config is not None:
            new_app_config = appconfig.load(self.app_config.path)
            new_service_config = new_app_config.service(self.service_name)
            self.app_config = new_app_config
            self.service_config = new_service_config
            self.mqtt_config = new_app_config.mqtt
            logging.getLogger().setLevel(new_app_config.log_level)
            if hasattr(self, 'slave') and self.slave is not None:
                self.slave.apply_rates(
                    poll_interval_s=new_service_config.poll_interval_s,
                    diagnostic_poll_multiplier=new_service_config.diagnostic_poll_multiplier,
                )

        self.config_store.commit(new_doc)
        self.doc = new_doc
        self.executor.publish_allowed_prefixes = new_doc.settings.publish_allowed_prefixes
        self.history.path = self._history_path if new_doc.settings.history_persist else None
        self.apply_subscriptions(new_doc)

    def _make_slave(self) -> Slave:
        return Slave(service_id=self.service_config.service_id, poll_core=self.poll_core,
                     poll_interval_s=self.doc.settings.tick_interval_s,
                     on_config_reload=self.reload_config,
                     on_poll_error=lambda exc: LOG.error("Scheduler-Fehler: %s", exc),
                     config_store=self.config_store)

    def run(self):
        self.client = build_client(client_id=f"{self.service_config.service_id}-service", host=self.mqtt_config.host, port=self.mqtt_config.port,
                                    user=self.mqtt_config.username, password=self.mqtt_config.password(), will_topic=f"{self.base_topic}/status/online")
        self.client.enable_logger(LOG)
        self.client.on_connect = self.on_connect
        self.client.on_message = self.on_message

        self.slave = self._make_slave()

        self.client.loop_start()
        LOG.info("Automations-Dienst gestartet (%d Regeln, tick=%ss)", len(self.doc.rules), self.doc.settings.tick_interval_s)
        try:
            while True:
                time.sleep(3600)
        except KeyboardInterrupt:
            pass
        finally:
            self.slave.stop()
            publish_online_status(self.client, self.base_topic, online=False, reason="shutdown")
            self.client.loop_stop()
            self.client.disconnect()


def main() -> None:
    try:
        config = appconfig.load(appconfig.config_path_from_argv())
    except appconfig.ConfigError as exc:
        print(f"Konfigurationsfehler: {exc}", file=sys.stderr)
        raise SystemExit(1)

    logging.basicConfig(level=config.log_level)
    rules_path = config.rules_config()
    config_store = ReloadableConfig(rules_path, load_and_validate)
    AutomationService(config.service("automation"), config.mqtt, config_store, app_config=config, service_name="automation", history_path=str(config.history_file())).run()


if __name__ == "__main__":
    main()
