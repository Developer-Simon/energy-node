"""MQTT payload parsing for battery_soc.

Extracts MQTT input handling logic from battery_soc_mqtt.py into a
declarative adapter.
"""

import json
import re

NUMBER_RE = re.compile(r"-?\d+(\.\d+)?")

# "Kontext (Topic + Feldname)" -> zuletzt gemeldeter Grund, siehe
# warn_extraction_failed().
extraction_warnings = {}

# Mapping of config topic attribute -> (value field, ts field, json_key attr, scale attr)
# scale_attr can be None for power fields (default scale 1.0)
TOPIC_FIELDS = (
    ("charger_power_topic",     "charger_power_w",     "charger_power_ts",     "charger_power_json_key",     None),
    ("inverter_power_topic",    "inverter_power_w",    "inverter_power_ts",    "inverter_power_json_key",    None),
    ("charger_dc_power_topic",  "charger_dc_power_w",  "charger_dc_power_ts",  "charger_dc_power_json_key",  None),
    ("inverter_dc_power_topic", "inverter_dc_power_w", "inverter_dc_power_ts", "inverter_dc_power_json_key", None),
    ("bank_a_voltage_topic",    "bank_a_voltage_v",    "bank_a_voltage_ts",    "bank_a_voltage_json_key",    "bank_a_voltage_scale"),
    ("bank_b_voltage_topic",    "bank_b_voltage_v",    "bank_b_voltage_ts",    "bank_b_voltage_json_key",    "bank_b_voltage_scale"),
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
    """Sets each SocInputs.<field>_configured True when the matching
    config.<topic> attr is non-empty."""
    for topic_attr, _value_field, _ts_field, _json_key_attr, _scale_attr in TOPIC_FIELDS:
        topic = getattr(config, topic_attr, "")
        if topic:
            # Map topic_attr to configured flag name
            # e.g., "charger_power_topic" -> "charger_power_configured"
            field_name = topic_attr.replace("_topic", "_configured")
            setattr(inputs, field_name, True)


def apply_message(config, inputs, topic, payload_str, now):
    """Processes an MQTT message for a topic.

    Finds the TOPIC_FIELDS row whose config.<topic attr> equals topic;
    runs extract_value; on a value, writes value * scale to the value field
    and now to the ts field; returns True if a value landed, else False.
    """
    for topic_attr, value_field, ts_field, json_key_attr, scale_attr in TOPIC_FIELDS:
        config_topic = getattr(config, topic_attr, "")
        if config_topic == topic:
            # Extract the value
            json_key = getattr(config, json_key_attr, "")
            context = f"{topic} ({value_field.replace('_w', '').replace('_v', '')})"
            value = extract_value(payload_str, json_key, context)

            if value is not None:
                # Determine scale
                if scale_attr:
                    scale = getattr(config, scale_attr, None)
                    if not scale:  # Fall back to 1.0 if scale is falsy
                        scale = 1.0
                else:
                    scale = 1.0

                # Write scaled value and timestamp
                setattr(inputs, value_field, value * scale)
                setattr(inputs, ts_field, now)
                return True
            return False

    return False
