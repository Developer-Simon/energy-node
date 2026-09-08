"""Python-Entsprechung der Value-Template-Teilmenge des Dashboards.

Kanonisch ist Go: dashboard/internal/registry/registry.go, Funktionen
parseValueTemplate / extractValueParsed / isTruthyJSONValue. Dieses Modul
baut sie nach - einschliesslich ihrer Eigenheiten, nicht bereinigt.
Abgesichert durch die geteilte Fixture
dashboard/internal/registry/testdata/value-template-cases.json, die von
tests/test_ha_template.py und von value_template_test.go gelesen wird.

Der Modulname ist bewusst nicht value_template.py: "value_template" ist der
Name eines Regelfeldes, und eine lokale Variable dieses Namens wuerde das
Modul in automation_mqtt.py verdecken.
"""

from __future__ import annotations

import datetime
import json
import re
from dataclasses import dataclass
from typing import Any, Optional

# Wortgleich mit registry.go:1310-1311.
_BOOLEAN_FORM = re.compile(
    r"^\{\{\s*'([^']*)'\s+if\s+value_json\.([A-Za-z0-9_]+)\s+else\s+'([^']*)'\s*\}\}$"
)
_PATH_FORM = re.compile(
    r"^\{\{\s*value_json\.([A-Za-z0-9_]+)(?:\[['\"]([^'\"]+)['\"]\])?"
    r"(?:\s*\|\s*default\(0\))?(?:\s*\|\s*int)?(?:\s*\|\s*timestamp_local)?\s*\}\}$"
)


@dataclass(frozen=True)
class ParsedValueTemplate:
    path: tuple[str, ...]
    when_true: str = ""
    when_false: str = ""
    default_zero: bool = False
    integer: bool = False
    timestamp_local: bool = False


def parse_value_template(template: str) -> Optional[ParsedValueTemplate]:
    """None, wenn die Form nicht unterstuetzt wird - auch bei leerem Template
    (Gos parseValueTemplate gibt dort ok=false zurueck)."""
    if not template:
        return None
    trimmed = template.strip()

    if match := _BOOLEAN_FORM.match(trimmed):
        return ParsedValueTemplate(
            path=(match.group(2),), when_true=match.group(1), when_false=match.group(3)
        )

    if match := _PATH_FORM.match(trimmed):
        path = (match.group(1),) if not match.group(2) else (match.group(1), match.group(2))
        # Go liest diese drei Flags mit strings.Contains aus dem ganzen String,
        # nicht aus den Regex-Gruppen: "{{ value_json.x|int }}" passt auf die
        # Regex, setzt das Flag aber NICHT. Bewusst nachgebaut - die Fixture
        # haelt beide Schreibweisen fest.
        return ParsedValueTemplate(
            path=path,
            default_zero="| default(0)" in trimmed,
            integer="| int" in trimmed,
            timestamp_local="| timestamp_local" in trimmed,
        )

    return None


def is_truthy(value: Any) -> bool:
    """Gos isTruthyJSONValue (registry.go:1393): false, null, der leere String
    und 0 sind unwahr, alles andere wahr."""
    if isinstance(value, bool):
        return value
    if value is None:
        return False
    if isinstance(value, str):
        return value != ""
    if isinstance(value, (int, float)):
        return value != 0
    return True


def extract_value(payload: str, template: str) -> Optional[str]:
    """Gos extractValue. Liefert None nur, wenn der aufgeloeste Wert kein
    Skalar ist - dort druckt Go seine eigene Map-Darstellung, die hier
    bewusst nicht nachgebaut wird."""
    parsed_template = parse_value_template(template)
    if parsed_template is None:
        # Kein oder unlesbares Template: Go gibt die rohe Nutzlast zurueck.
        return payload

    try:
        # parse_int=float: Go dekodiert JSON-Zahlen fuer interface{}/map[string]any
        # immer als float64, unabhaengig vom Dezimalpunkt (encoding/json).
        # 1000000 in der Nutzlast landet in Go als float64(1000000) und faellt
        # damit unter dieselbe Exponential-Formatierung wie 1000000.0.
        parsed_payload = json.loads(payload, parse_int=float)
    except (TypeError, ValueError):
        return payload
    if not isinstance(parsed_payload, dict):
        # Auch ein gueltiges JSON-Array faellt hier auf die rohe Nutzlast
        # zurueck - genau wie Gos Type-Assertion auf map[string]any.
        return payload

    value, found = _lookup(parsed_payload, parsed_template.path)
    if not found or value is None:
        # Go prueft dies vor der Bool-Form: ein fehlender oder nullwertiger
        # Pfad liefert "" und faellt NICHT in when_true/when_false, selbst
        # wenn das Template die Bool-Form ist (registry.go:1277-1283).
        if not parsed_template.default_zero:
            return ""
        value = 0.0

    if parsed_template.when_true:
        return parsed_template.when_true if is_truthy(value) else parsed_template.when_false

    if parsed_template.integer:
        value = _integer_value(value)

    if parsed_template.timestamp_local:
        numeric = _numeric_value(value)
        if numeric is None:
            return ""
        return datetime.datetime.fromtimestamp(int(numeric)).astimezone().isoformat()

    return format_go_value(value)


def _lookup(payload: dict, path: tuple[str, ...]) -> tuple[Any, bool]:
    current: Any = payload
    for segment in path:
        if not isinstance(current, dict) or segment not in current:
            return None, False
        current = current[segment]
    return current, True


def _numeric_value(value: Any) -> Optional[float]:
    """Gos numericValue: Zahlen direkt, Strings ueber ParseFloat, sonst nichts.
    Ein bool ist in Go keine Zahl - in Python schon, deshalb der Extra-Zweig."""
    if isinstance(value, bool):
        return None
    if isinstance(value, (int, float)):
        return float(value)
    if isinstance(value, str):
        try:
            return float(value.strip())
        except ValueError:
            return None
    return None


def _integer_value(value: Any) -> Any:
    """Gos integerValue: int64(numeric), also Abschneiden Richtung Null.
    Nicht-Zahlen bleiben unveraendert."""
    numeric = _numeric_value(value)
    if numeric is None:
        return value
    return int(numeric)


def format_go_value(value: Any) -> Optional[str]:
    """fmt.Sprintf("%v", value) fuer die Typen, die json.Unmarshal in Go
    liefert. Der Float-Zweig ist strconv.FormatFloat(f, 'g', -1, 64):
    kuerzeste round-trip-faehige Ziffernfolge, Exponentialschreibweise wenn
    der Exponent < -4 oder >= 6 ist (Go setzt eprec bei 'shortest' auf 6)."""
    if isinstance(value, bool):
        return "true" if value else "false"
    if isinstance(value, str):
        return value
    if isinstance(value, int):
        return str(value)
    if isinstance(value, float):
        return _format_go_float(value)
    return None


def _format_go_float(value: float) -> str:
    if value != value:
        return "NaN"
    if value == float("inf"):
        return "+Inf"
    if value == float("-inf"):
        return "-Inf"
    if value == 0:
        return "0"

    digits, dp = _shortest_digits(value)
    sign = "-" if value < 0 else ""
    exponent = dp - 1

    if exponent < -4 or exponent >= 6:
        head, tail = digits[0], digits[1:]
        mantissa = head + ("." + tail if tail else "")
        marker = "+" if exponent >= 0 else "-"
        return f"{sign}{mantissa}e{marker}{abs(exponent):02d}"
    if dp <= 0:
        return sign + "0." + "0" * (-dp) + digits
    if dp >= len(digits):
        return sign + digits + "0" * (dp - len(digits))
    return f"{sign}{digits[:dp]}.{digits[dp:]}"


def _shortest_digits(value: float) -> tuple[str, int]:
    """Kuerzeste Ziffernfolge, die value zurueckliest, plus Dezimalpunkt-
    Position dp: der Wert ist 0.<digits> * 10**dp. Pythons repr() benutzt
    dieselbe Shortest-Round-Trip-Grundlage wie Gos 'shortest', nur eine
    andere Darstellungsregel - die kommt in _format_go_float."""
    text = repr(abs(value))
    if "e" in text:
        mantissa, _, exponent = text.partition("e")
        exponent10 = int(exponent)
    else:
        mantissa, exponent10 = text, 0
    int_part, _, frac_part = mantissa.partition(".")
    raw = int_part + frac_part
    stripped = raw.lstrip("0")
    leading_zeros = len(raw) - len(stripped)
    dp = len(int_part) + exponent10 - leading_zeros
    return (stripped.rstrip("0") or "0"), dp
