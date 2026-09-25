"""MQTT payload parsing for battery_soc.

Extracts MQTT input handling logic from battery_soc_mqtt.py into a
declarative adapter.
"""

import json
import re
from collections import namedtuple

NUMBER_RE = re.compile(r"-?\d+(\.\d+)?")

# "Kontext (Topic + Feldname)" -> zuletzt gemeldeter Grund, siehe
# warn_extraction_failed().
extraction_warnings = {}

TopicField = namedtuple(
    "TopicField",
    "topic_attr value_field ts_field json_key_attr scale_attr invert_attr unit_attr",
)

# Eine Zeile je Eingangs-Slot. Dasselbe Topic darf in mehreren Leistungs-
# Slots stehen (vorzeichenbehafteter Sensor), jede Zeile liest es mit ihrem
# eigenen JSON-Key, Vorzeichen und ihrer Einheit.
TOPIC_FIELDS = (
    TopicField("charger_power_topic", "charger_power_w", "charger_power_ts",
               "charger_power_json_key", None, "charger_power_invert", None),
    TopicField("inverter_power_topic", "inverter_power_w", "inverter_power_ts",
               "inverter_power_json_key", None, "inverter_power_invert", None),
    TopicField("charger_dc_power_topic", "charger_dc_power_w", "charger_dc_power_ts",
               "charger_dc_power_json_key", None, "charger_dc_power_invert", "charger_dc_power_unit"),
    TopicField("inverter_dc_power_topic", "inverter_dc_power_w", "inverter_dc_power_ts",
               "inverter_dc_power_json_key", None, "inverter_dc_power_invert", "inverter_dc_power_unit"),
    TopicField("bank_a_voltage_topic", "bank_a_voltage_v", "bank_a_voltage_ts",
               "bank_a_voltage_json_key", "bank_a_voltage_scale", None, None),
    TopicField("bank_b_voltage_topic", "bank_b_voltage_v", "bank_b_voltage_ts",
               "bank_b_voltage_json_key", "bank_b_voltage_scale", None, None),
)


def extract_value(payload_str, json_key, context=""):
    """Liest entweder ein plain-float-Payload oder ein JSON-Feld aus.

    Schlaegt der konfigurierte json_key fehl, wird weiterhin None geliefert
    (der Aufrufer laesst den Eingang dann einfach veralten) - aber einmal je
    Topic und Fehlergrund gewarnt. Ohne diese Meldung ist ein falscher Key
    voellig unsichtbar: der Eingang wird nie aktualisiert, gilt nach
    stale_input_s als veraltet, und im Log steht nichts.

    KEIN Rueckfall auf den Regex-Pfad: wer einen Key konfiguriert hat, will
    diesen Key. Die erste Zahl im Rohtext waere bei {"id":0,"apower":12.5}
    die 0 - ein stiller Falschwert statt eines sichtbaren Fehlers."""
    if json_key:
        try:
            data = json.loads(payload_str)
        except Exception:
            warn_extraction_failed(context, json_key, payload_str, "kein gueltiges JSON")
            return None
        if not isinstance(data, dict):
            warn_extraction_failed(context, json_key, payload_str,
                                   "Payload ist kein JSON-Objekt")
            return None
        if json_key not in data:
            available = ", ".join(sorted(data)) or "(keine)"
            warn_extraction_failed(context, json_key, payload_str,
                                   f"Key fehlt, vorhanden: {available}")
            return None
        try:
            return float(data[json_key])
        except (TypeError, ValueError):
            warn_extraction_failed(context, json_key, payload_str,
                                   f"Wert ist keine Zahl: {data[json_key]!r}")
            return None
    match = NUMBER_RE.search(payload_str)
    return float(match.group()) if match else None


def warn_extraction_failed(context, json_key, payload_str, reason):
    """Drosselt auf eine Meldung je Kontext und Grund - der Poll laeuft alle
    paar Sekunden, eine Warnung pro Nachricht waere unbrauchbar. Aendert sich
    der Grund (anderer Payload, korrigierter Key), wird erneut gewarnt."""
    if extraction_warnings.get(context) == reason:
        return
    extraction_warnings[context] = reason
    excerpt = payload_str[:120] + ("..." if len(payload_str) > 120 else "")
    print(f"WARNUNG {context}: JSON-Key '{json_key}' nicht lesbar ({reason}); "
          f"Payload: {excerpt}")


def mark_configured(config, inputs):
    """Setzt SocInputs.<slot>_configured fuer jedes gesetzte Topic und
    uebernimmt die Einheit der DC-Slots ("W" oder "A")."""
    for field in TOPIC_FIELDS:
        if getattr(config, field.topic_attr, ""):
            setattr(inputs, field.topic_attr.replace("_topic", "_configured"), True)
        if field.unit_attr:
            setattr(inputs, field.unit_attr, getattr(config, field.unit_attr, "W"))


def apply_message(config, inputs, topic, payload_str, now):
    """Verarbeitet eine MQTT-Nachricht fuer ALLE Slots dieses Topics.

    Je Slot: Wert lesen (eigener JSON-Key), skalieren (Spannung),
    invertieren (Leistung), mit Zeitstempel schreiben. Liefert True, wenn
    mindestens ein Slot einen Wert bekommen hat."""
    landed = False
    for field in TOPIC_FIELDS:
        if getattr(config, field.topic_attr, "") != topic:
            continue
        json_key = getattr(config, field.json_key_attr, "")
        context = f"{topic} ({field.value_field.replace('_w', '').replace('_v', '')})"
        value = extract_value(payload_str, json_key, context)
        if value is None:
            continue
        if field.scale_attr:
            value *= getattr(config, field.scale_attr, None) or 1.0
        if field.invert_attr and getattr(config, field.invert_attr, False):
            value = -value
        setattr(inputs, field.value_field, value)
        setattr(inputs, field.ts_field, now)
        landed = True
    return landed
