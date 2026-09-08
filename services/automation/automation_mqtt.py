"""Energie-Automationen: eigenstaendiger Dienst, der die vom Dashboard
publizierte Bilanz (Teil A) sowie beliebige MQTT-Topics gegen Regeln
prueft und darauf MQTT-Publishes/Benachrichtigungen ausloest.

Siehe knowhow/dashboard/automationen-funktionsweise.md fuer die volle
Semantik (Halte-/Hysterese-/Sperrzeit, Neustartverhalten, Sicherheit)."""

from __future__ import annotations

import collections
import datetime
import json
import logging
import math
import os
import re
import sys
import tempfile
import time
from dataclasses import dataclass, field
from typing import Any, Optional

from energy_node_common import appconfig
from energy_node_common.config import ReloadableConfig
from energy_node_common.discovery import entity_config, publish_discovery
from energy_node_common.mqtt import build_client, publish_online_status, publish_json
from energy_node_common.settings import SlaveStatus, settings_status_topic
from energy_node_common.slave import Slave

import ha_template

LOG = logging.getLogger("automation_mqtt")

BALANCE_TOPIC = "outstation/dashboard/energy/balance"


BALANCE_FIELDS = frozenset({
    "grid_export", "grid_import", "pv", "load_total", "base", "wallbox",
    "heat_pump", "battery_charge", "battery_discharge", "gap_applied",
    "autarkie", "eigenverbrauch",
    # Aus der Rolle battery_soc abgeleitet (Dashboard, internal/energy).
    # Brauchen keine eigene Auswertungslogik: sie stehen in der Bilanz.
    "battery_soc", "battery_capacity_kwh", "battery_energy_kwh",
})

_TOPIC_SEGMENT_RE = re.compile(r"^[^+#/]+$")


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
    if topic.startswith("outstation/dashboard/"):
        return "topic must not forge the dashboard's own topics (outstation/dashboard/...)"
    return None


_ID_RE = re.compile(r"^[a-z0-9][a-z0-9_-]{0,63}$")
_TIME_RE = re.compile(r"^([01]\d|2[0-3]):[0-5]\d$")

_CONDITION_TYPES = {"balance_threshold", "topic_value", "time_window", "entity_value"}
_ACTION_TYPES = {"publish", "notification"}


@dataclass
class Settings:
    tick_interval_s: float = 10
    settling_seconds: float = 60
    balance_max_age_s: float = 30
    publish_allowed_prefixes: list[str] = field(default_factory=list)
    history_limit: int = 10
    history_persist: bool = True


@dataclass
class RulesDocument:
    version: int
    settings: Settings
    rules: list[dict]


def _validate_condition(cond: dict, index: int, errors: list[str]) -> dict:
    prefix = f"condition[{index}]"
    ctype = cond.get("type")
    if ctype not in _CONDITION_TYPES:
        errors.append(f"{prefix}: unknown type {ctype!r}")
        return cond
    result = dict(cond)
    if ctype == "balance_threshold":
        if result.get("field") not in BALANCE_FIELDS:
            errors.append(f"{prefix}: field must be one of {sorted(BALANCE_FIELDS)}")
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
        weekdays = result.get("weekdays", [])
        if any(not isinstance(d, int) or d < 0 or d > 6 for d in weekdays):
            errors.append(f"{prefix}: weekdays must all be 0..6")
        result.setdefault("weekdays", [])
    return result


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
        elif source == "balance" and result.get("field") not in BALANCE_FIELDS:
            errors.append(f"{prefix}: field must be one of {sorted(BALANCE_FIELDS)}")
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


def _validate_rule(rule: dict, index: int, errors: list[str], seen_ids: set[str]) -> dict:
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
        if result["cooldown_seconds"] < 30:
            errors.append(f"{prefix}: cooldown_seconds must be >= 30 for a rule with a publish action")
        for i, cond in enumerate(result["conditions"]):
            if "hold_seconds" in cond and cond["hold_seconds"] < 30:
                errors.append(f"{prefix}.condition[{i}]: hold_seconds must be >= 30 for a rule with a publish action")
    return result


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
    settings = Settings(
        tick_interval_s=settings_raw.get("tick_interval_s", 10),
        settling_seconds=settings_raw.get("settling_seconds", 60),
        balance_max_age_s=settings_raw.get("balance_max_age_s", 30),
        publish_allowed_prefixes=list(settings_raw.get("publish_allowed_prefixes", [])),
        history_limit=history_limit,
        history_persist=bool(settings_raw.get("history_persist", True)),
    )

    rules_raw = raw.get("rules", [])
    if len(rules_raw) > 16:
        errors.append("at most 16 rules are allowed")

    seen_ids: set[str] = set()
    rules = [_validate_rule(r, i, errors, seen_ids) for i, r in enumerate(rules_raw)]

    if errors:
        raise RuleValidationError("; ".join(errors))

    return RulesDocument(version=raw["version"], settings=settings, rules=rules)


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


def _time_window_met(cond: dict, now_ts: float, weekday: int) -> bool:
    weekdays = cond.get("weekdays") or []
    if weekdays and weekday not in weekdays:
        return False
    local = time.localtime(now_ts)
    minutes_now = local.tm_hour * 60 + local.tm_min
    start_h, start_m = (int(part) for part in cond["start"].split(":"))
    end_h, end_m = (int(part) for part in cond["end"].split(":"))
    start_minutes = start_h * 60 + start_m
    end_minutes = end_h * 60 + end_m
    if start_minutes <= end_minutes:
        return start_minutes <= minutes_now < end_minutes
    # Crosses midnight, e.g. 22:00 -> 06:00.
    return minutes_now >= start_minutes or minutes_now < end_minutes


def evaluate_condition_raw(cond: dict, *, balance: Optional[dict], topic_values: dict, now_ts: float, weekday: int) -> tuple:
    ctype = cond["type"]

    if ctype == "balance_threshold":
        if balance is None:
            return False, "balance_stale"
        value = balance.get(cond["field"])
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

    if ctype == "time_window":
        return _time_window_met(cond, now_ts, weekday), ""

    return False, "unknown_type"


def condition_value(cond: dict, *, balance: Optional[dict], topic_values: dict, now_ts: float) -> tuple:
    """Liefert (value, target) fuer die Live-Anzeige im Dashboard.

    value ist der Wert, mit dem die Regel gerade gerechnet hat, oder None,
    wenn keiner vorlag (Bilanz veraltet, Topic nie gesehen, Payload nicht
    lesbar). target ist die Schwelle, gegen die verglichen wurde, oder None,
    wo es keine gibt. Rein beschreibend - die Auswertung selbst passiert
    unveraendert in evaluate_condition_raw."""
    ctype = cond["type"]

    if ctype == "balance_threshold":
        value = None if balance is None else balance.get(cond["field"])
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
        base_value = balance.get(action["field"])
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
    step = action.get("step")
    if step:
        # Floor, not round-to-nearest: a surplus-driven setpoint (e.g. wallbox
        # charge power from grid_export) must never round up past what is
        # actually available.
        value = math.floor(value / step) * step
    if action.get("min") is not None:
        value = max(value, action["min"])
    if action.get("max") is not None:
        value = min(value, action["max"])
    decimals = action.get("decimals")
    if decimals is not None:
        value = round(value, decimals)
        return f"{value:.{decimals}f}", None
    return str(value), None


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


class Engine:
    """Ausgewertet wird jeder Tick fuer jede Regel neu; Zustand lebt nur in
    self._runtime (RAM, kein Neustart-ueberlebender Speicher - siehe
    knowhow/dashboard/automationen-funktionsweise.md #Neustartverhalten)."""

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

    def _apply_hysteresis(self, cond: dict, cond_rt: ConditionRuntime, balance: Optional[dict]) -> Optional[bool]:
        """Returns the hysteresis-latched met/not-met state for
        balance_threshold conditions with hysteresis > 0, or None if
        hysteresis doesn't apply (caller then uses the raw comparison)."""
        if cond["type"] != "balance_threshold" or cond.get("hysteresis", 0) <= 0 or balance is None:
            return None
        value = balance.get(cond["field"])
        if value is None:
            return None
        threshold, hysteresis = cond["threshold"], cond["hysteresis"]
        if cond["comparison"] == "above":
            cond_rt.latched = (value >= threshold - hysteresis) if cond_rt.latched else (value > threshold)
        else:
            cond_rt.latched = (value <= threshold + hysteresis) if cond_rt.latched else (value < threshold)
        return cond_rt.latched

    def _evaluate_one(self, cond: dict, cond_rt: ConditionRuntime, *, balance, topic_values, now_ts, weekday):
        raw_met, reason = evaluate_condition_raw(cond, balance=balance, topic_values=topic_values, now_ts=now_ts, weekday=weekday)
        latched = self._apply_hysteresis(cond, cond_rt, balance)
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

    def tick(self, doc: RulesDocument, *, balance: Optional[dict], balance_age: Optional[float],
              topic_values: dict, now_ts: float, weekday: int) -> dict:
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

            condition_reports = []
            all_raw = True
            all_held = True
            saw_stale = False
            for cond, cond_rt in zip(rule["conditions"], rt.conditions):
                raw_met, held_met, reason = self._evaluate_one(
                    cond, cond_rt, balance=effective_balance, topic_values=topic_values, now_ts=now_ts, weekday=weekday)
                if reason == "balance_stale":
                    saw_stale = True
                all_raw = all_raw and raw_met
                all_held = all_held and held_met
                hold_seconds = cond.get("hold_seconds", 0)
                remaining = max(0.0, hold_seconds - (now_ts - cond_rt.since)) if cond_rt.since is not None else float(hold_seconds)
                # value/target sind rein fuer die Anzeige im Dashboard - siehe
                # docs/superpowers/specs/2026-08-09-automations-visueller-editor-design.md.
                # met/since/hold_remaining bleiben unveraendert, darauf bauen Spec A und C auf.
                value, target = condition_value(cond, balance=effective_balance,
                                                 topic_values=topic_values, now_ts=now_ts)
                condition_reports.append({"met": held_met, "raw_met": raw_met, "value": value,
                                          "target": target, "since": cond_rt.since,
                                          "hold_remaining": remaining})

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
                results[rule_id] = self._rule_result(rt, rule, "fired", None, condition_reports, preview, cooldown_remaining)
            elif cooldown_remaining > 0:
                results[rule_id] = self._rule_result(rt, rule, "cooldown", None, condition_reports, preview, cooldown_remaining)
            elif not all_held:
                results[rule_id] = self._rule_result(rt, rule, "hold_pending", None, condition_reports, preview, cooldown_remaining)
            else:
                fired_actions = []
                any_blocked = False
                any_error = False
                for action in rule["actions"]:
                    try:
                        outcome = self.executor.execute(action, balance=effective_balance, topic_values=topic_values, event_sink=self.event_sink)
                    except Exception as exc:  # noqa: BLE001 - a broker/publish failure must not crash the tick
                        outcome = {"blocked": True, "reason": str(exc)}
                        any_error = True
                    fired_actions.append(outcome)
                    if outcome.get("blocked"):
                        any_blocked = True
                rt.last_fired_at = now_ts
                rt.fired_this_episode = True
                rt.fire_count += 1
                cooldown_remaining = max(0.0, rule.get("cooldown_seconds", 0) - (now_ts - rt.last_fired_at))
                outcome_result = "error" if any_error else ("blocked" if any_blocked else "fired")
                if self.history_sink is not None:
                    history_actions = [{**outcome, "type": action["type"]}
                                        for action, outcome in zip(rule["actions"], fired_actions)]
                    self.history_sink(rule_id, {"result": outcome_result, "reason": None,
                                                 "test": False, "actions": history_actions})
                results[rule_id] = self._rule_result(rt, rule, outcome_result, None, condition_reports, fired_actions, cooldown_remaining)

        return results

    @staticmethod
    def _rule_result(rt: RuleRuntime, rule: dict, result: str, reason: Optional[str], conditions: list,
                      preview: list, cooldown_remaining: float) -> dict:
        return {
            "enabled": rule["enabled"], "result": result, "reason": reason,
            "fired_at": rt.last_fired_at, "fire_count": rt.fire_count,
            "cooldown_remaining": cooldown_remaining,
            "conditions": conditions, "actions": preview,
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
    try:
        return config_store.load(), None
    except RuleValidationError as exc:
        return RulesDocument(version=1, settings=Settings(), rules=[]), str(exc)


class AutomationService:
    def __init__(self, service_config, mqtt_config, config_store, app_config=None, service_name: str = "automation", history_path: Optional[str] = None, started_at: Optional[float] = None):
        self.service_config = service_config
        self.mqtt_config = mqtt_config
        self.config_store = config_store
        self.app_config = app_config
        self.service_name = service_name
        self.doc, self.startup_error = load_initial_document(config_store)
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

    def _publish_startup_rejection(self, client, error: str) -> None:
        status = SlaveStatus(poll_interval_s=self.doc.settings.tick_interval_s,
                              diagnostic_poll_multiplier=1,
                              actual_poll_interval_s=self.doc.settings.tick_interval_s,
                              runtime_status="rejected", error=error)
        publish_json(client, settings_status_topic(self.service_config.service_id), status.to_dict())

    def on_connect(self, client, userdata, flags, reason_code, properties=None):
        publish_online_status(client, self.base_topic, online=True, reason="connected")
        # clean_session=True: der Broker verwirft bei jedem (Re-)Connect alle
        # frueheren Subscriptions dieser Client-ID, auch bei einem
        # automatischen Reconnect nach einem Broker-Neustart. Der Cache muss
        # daher hier verworfen werden, sonst subscribed apply_subscriptions()
        # bei unveraendertem Dokument nach einem Reconnect auf gar nichts.
        self.subscribed_topics = set()
        self.apply_subscriptions(self.doc)
        device_block = {"identifiers": [self.service_config.service_id], "name": "Energie-Automationen", "manufacturer": "Energy Node"}
        for component, object_id, config in discovery_object_ids(self.service_config.service_id, self.base_topic, device_block):
            publish_discovery(client, self.service_config.service_id, component, object_id, config)
        self.slave.start(client)
        if self.startup_error is not None:
            self._publish_startup_rejection(client, self.startup_error)
            self.startup_error = None  # only report once, on the first connect

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
        weekday = time.localtime(now).tm_wday
        results = self.engine.tick(self.doc, balance=self.last_balance, balance_age=balance_age,
                                    topic_values=self.topic_values, now_ts=now, weekday=weekday)
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
                self.slave.apply_config_defaults(
                    poll_interval_s=new_service_config.poll_interval_s,
                    diagnostic_multiplier=new_service_config.diagnostic_poll_multiplier,
                )

        self.config_store.commit(new_doc)
        self.doc = new_doc
        self.executor.publish_allowed_prefixes = new_doc.settings.publish_allowed_prefixes
        self.history.path = self._history_path if new_doc.settings.history_persist else None
        self.apply_subscriptions(new_doc)

    def run(self):
        self.client = build_client(client_id=f"{self.service_config.service_id}-service", host=self.mqtt_config.host, port=self.mqtt_config.port,
                                    user=self.mqtt_config.username, password=self.mqtt_config.password(), will_topic=f"{self.base_topic}/status/online")
        self.client.enable_logger(LOG)
        self.client.on_connect = self.on_connect
        self.client.on_message = self.on_message

        self.slave = Slave(service_id=self.service_config.service_id, poll_core=self.poll_core,
                            default_poll_interval_s=self.doc.settings.tick_interval_s,
                            on_config_reload=self.reload_config,
                            on_poll_error=lambda exc: LOG.error("Scheduler-Fehler: %s", exc))

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
