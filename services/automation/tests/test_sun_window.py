"""Zeitfenster mit Starttag-Semantik, sun_window und Standort-Einstellung."""

import datetime
import json
import os
import sys
import time
from pathlib import Path

import pytest

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

import automation_mqtt as automation

BERLIN = (52.52, 13.405)


@pytest.fixture(autouse=True)
def berlin_timezone():
    """Die Fensterlogik rechnet in Ortszeit - fest auf Europe/Berlin, damit
    die erwarteten Sonnenzeiten nicht von der Zeitzone des Testrechners
    abhaengen."""
    previous = os.environ.get("TZ")
    os.environ["TZ"] = "Europe/Berlin"
    time.tzset()
    yield
    if previous is None:
        os.environ.pop("TZ", None)
    else:
        os.environ["TZ"] = previous
    time.tzset()


def local_ts(day: str, clock: str) -> float:
    return datetime.datetime.fromisoformat(f"{day}T{clock}").timestamp()


def evaluate(cond, now_ts, location=BERLIN):
    return automation.evaluate_condition_raw(cond, balance=None, topic_values={}, now_ts=now_ts, location=location)


def write_rules(tmp_path, conditions, settings=None):
    doc = {"version": 1, "settings": settings or {},
           "rules": [{"id": "r1", "name": "R", "enabled": True, "cooldown_seconds": 300,
                      "conditions": conditions,
                      "actions": [{"type": "notification", "severity": "info", "title": "t", "message": "m"}]}]}
    path = tmp_path / "automation_rules.json"
    path.write_text(json.dumps(doc))
    return str(path)


# --- time_window: der Starttag zaehlt -----------------------------------------

# 2024-06-07 ist ein Freitag (weekday 4).

def test_time_window_across_midnight_counts_the_start_day():
    cond = {"type": "time_window", "start": "22:00", "end": "06:00", "weekdays": [4]}
    assert evaluate(cond, local_ts("2024-06-07", "23:00"))[0] is True
    assert evaluate(cond, local_ts("2024-06-08", "03:00"))[0] is True  # Samstag frueh, Fenster von Freitag
    assert evaluate(cond, local_ts("2024-06-08", "23:00"))[0] is False  # Samstagabend: neues Fenster, Sa nicht gewaehlt
    assert evaluate(cond, local_ts("2024-06-07", "03:00"))[0] is False  # Freitag frueh: Fenster von Donnerstag


def test_time_window_with_equal_start_and_end_is_empty():
    cond = {"type": "time_window", "start": "08:00", "end": "08:00", "weekdays": []}
    assert evaluate(cond, local_ts("2024-06-07", "08:00"))[0] is False


def test_time_window_needs_no_location():
    cond = {"type": "time_window", "start": "08:00", "end": "18:00", "weekdays": []}
    assert evaluate(cond, local_ts("2024-06-07", "12:00"), location=None)[0] is True


# --- sun_window --------------------------------------------------------------

def sun_cond(**overrides):
    cond = {"type": "sun_window", "from": "sunrise", "from_offset_min": 0,
            "to": "sunset", "to_offset_min": 0, "weekdays": []}
    cond.update(overrides)
    return cond


# Berlin 2024-06-21: Aufgang 04:43, Untergang 21:33 MESZ.

def test_sun_window_day_between_sunrise_and_sunset():
    cond = sun_cond()
    assert evaluate(cond, local_ts("2024-06-21", "12:00")) == (True, "")
    assert evaluate(cond, local_ts("2024-06-21", "04:30"))[0] is False
    assert evaluate(cond, local_ts("2024-06-21", "21:45"))[0] is False


def test_sun_window_applies_offsets():
    cond = sun_cond(from_offset_min=30, to_offset_min=-30)
    assert evaluate(cond, local_ts("2024-06-21", "05:00"))[0] is False  # vor Aufgang + 30
    assert evaluate(cond, local_ts("2024-06-21", "05:20"))[0] is True
    assert evaluate(cond, local_ts("2024-06-21", "21:10"))[0] is False  # nach Untergang - 30


def test_sun_window_night_crosses_midnight_and_counts_the_start_day():
    # 2024-06-21 ist ein Freitag.
    cond = sun_cond(**{"from": "sunset", "to": "sunrise", "weekdays": [4]})
    assert evaluate(cond, local_ts("2024-06-21", "23:30"))[0] is True
    assert evaluate(cond, local_ts("2024-06-22", "03:00"))[0] is True  # Samstag frueh, Nacht von Freitag
    assert evaluate(cond, local_ts("2024-06-22", "23:30"))[0] is False  # Samstagnacht nicht gewaehlt
    assert evaluate(cond, local_ts("2024-06-21", "12:00"))[0] is False


def test_sun_window_same_event_twice_spans_the_offsets():
    cond = sun_cond(to="sunrise", to_offset_min=120)
    assert evaluate(cond, local_ts("2024-06-21", "06:00"))[0] is True
    assert evaluate(cond, local_ts("2024-06-21", "07:00"))[0] is False


def test_sun_window_without_location_is_not_met():
    assert evaluate(sun_cond(), local_ts("2024-06-21", "12:00"), location=None) == (False, "location_missing")


def test_sun_window_polar_day_reports_no_sun_event():
    assert evaluate(sun_cond(), local_ts("2024-06-21", "12:00"), location=(78.22, 15.65)) == (False, "no_sun_event")


def test_condition_value_shows_the_active_sun_window():
    value, target = automation.condition_value(
        sun_cond(), balance=None, topic_values={}, now_ts=local_ts("2024-06-21", "12:00"), location=BERLIN)
    assert value == "04:43 – 21:33"
    assert target is None


def test_condition_value_shows_last_nights_window_after_midnight():
    cond = sun_cond(**{"from": "sunset", "to": "sunrise"})
    value, _ = automation.condition_value(
        cond, balance=None, topic_values={}, now_ts=local_ts("2024-06-22", "03:00"), location=BERLIN)
    assert value == "21:33 – 04:43"


def test_condition_value_is_none_without_location():
    value, _ = automation.condition_value(
        sun_cond(), balance=None, topic_values={}, now_ts=local_ts("2024-06-21", "12:00"), location=None)
    assert value is None


# --- Validierung --------------------------------------------------------------

def test_load_accepts_a_sun_window_and_fills_defaults(tmp_path):
    doc = automation.load_and_validate(write_rules(tmp_path, [{"type": "sun_window", "from": "sunset", "to": "sunrise"}]))
    cond = doc.rules[0]["conditions"][0]
    assert cond["from_offset_min"] == 0
    assert cond["to_offset_min"] == 0
    assert cond["weekdays"] == []


@pytest.mark.parametrize("bad", [
    {"from": "noon"},
    {"to": "dusk"},
    {"from_offset_min": 241},
    {"to_offset_min": -241},
    {"from_offset_min": 1.5},
    {"weekdays": [7]},
])
def test_load_rejects_invalid_sun_window(tmp_path, bad):
    with pytest.raises(automation.RuleValidationError):
        automation.load_and_validate(write_rules(tmp_path, [sun_cond(**bad)]))


def test_load_reads_the_location(tmp_path):
    doc = automation.load_and_validate(write_rules(tmp_path, [sun_cond()], settings={"latitude": 52.52, "longitude": 13.405}))
    assert doc.settings.location == (52.52, 13.405)


def test_location_is_none_when_not_set(tmp_path):
    doc = automation.load_and_validate(write_rules(tmp_path, [sun_cond()]))
    assert doc.settings.location is None


@pytest.mark.parametrize("settings", [
    {"latitude": 91, "longitude": 0},
    {"latitude": 0, "longitude": -181},
    {"latitude": 52.5},
    {"longitude": 13.4},
    {"latitude": "52", "longitude": 13.4},
    {"latitude": True, "longitude": 13.4},
])
def test_load_rejects_invalid_location(tmp_path, settings):
    with pytest.raises(automation.RuleValidationError):
        automation.load_and_validate(write_rules(tmp_path, [sun_cond()], settings=settings))


# --- Engine reicht den Standort durch ------------------------------------------

def test_tick_uses_the_location_from_settings():
    executor = automation.ActionExecutor(publish_fn=lambda *args: None, publish_allowed_prefixes=[])
    engine = automation.Engine(executor, started_at=0, event_sink=lambda message: None)
    rule = {"id": "r1", "name": "R", "enabled": True, "cooldown_seconds": 0, "conditions": [sun_cond()],
            "actions": [{"type": "notification", "severity": "info", "title": "t", "message": "m"}]}
    now = local_ts("2024-06-21", "12:00")
    with_location = automation.RulesDocument(
        version=1, settings=automation.Settings(settling_seconds=0, latitude=52.52, longitude=13.405), rules=[rule])
    without = automation.RulesDocument(version=1, settings=automation.Settings(settling_seconds=0), rules=[rule])

    assert engine.tick(with_location, balance=None, balance_age=None, topic_values={}, now_ts=now)["r1"]["result"] == "fired"
    engine2 = automation.Engine(executor, started_at=0, event_sink=lambda message: None)
    assert engine2.tick(without, balance=None, balance_age=None, topic_values={}, now_ts=now)["r1"]["result"] == "conditions_not_met"
