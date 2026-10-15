"""Dashboard-Serie -> Home-Assistant-Quelle.

Die Serien-IDs des Dashboards sind MQTT-unique_ids. Home Assistant kennt
dieselben Entitaeten ueber die Bridge und fuehrt sie im Entity-Registry
unter der Plattform mqtt mit genau dieser unique_id.
"""
from __future__ import annotations

import re
from dataclasses import dataclass

from homeassistant.core import HomeAssistant
from homeassistant.helpers import entity_registry as er

from .const import CUSTOM_UNIQUE_PREFIX

# Die Rollen rechnet der Browser aus. Ihr Gegenstueck in Home Assistant sind
# die Sensoren, die das Dashboard selbst per Discovery anmeldet
# (dashboard/internal/energydiscovery, unique_id energy_node_<object_id>).
# role:load ist der Hauszaehler, sein Sensor house_load_measured gibt es nur,
# wenn der Rolle eine Quelle zugeordnet ist. house_load traegt load_total und
# ist keine Rolle. wallbox und heat_pump melden den Sensor ebenfalls per
# Discovery an, Verbrauchskategorien als energy_node_custom_<id>.
ROLE_SOURCES: dict[str, tuple[str, str | None]] = {
    "role:pv": ("energy_node_pv_power", None),
    "role:grid": ("energy_node_grid_import", "energy_node_grid_export"),
    "role:battery": ("energy_node_battery_charge", "energy_node_battery_discharge"),
    "role:battery_soc": ("energy_node_battery_soc", None),
    "role:load": ("energy_node_house_load_measured", None),
    "role:wallbox": ("energy_node_wallbox", None),
    "role:heat_pump": ("energy_node_heat_pump", None),
}
CUSTOM_ROLE = re.compile(r"^role:custom:(consumer|producer|storage):([a-z0-9_]+)$")
NUMERIC_DOMAINS = ("sensor", "number")


@dataclass(frozen=True)
class SeriesSource:
    """Eine Dashboard-Serie und woraus Home Assistant sie bildet.

    minus ist gesetzt, wenn die Serie eine Differenz zweier Entitaeten ist.
    unit ist die Einheit, die das Dashboard fuer die Serie fuehrt.
    """

    series: str
    unit: str
    plus: str
    minus: str | None = None


def _entity_for(registry: er.EntityRegistry, unique_id: str) -> str | None:
    for domain in NUMERIC_DOMAINS:
        entity_id = registry.async_get_entity_id(domain, "mqtt", unique_id)
        if entity_id:
            return entity_id
    return None


def resolve(hass: HomeAssistant, announced: list[dict]) -> list[SeriesSource]:
    """Nur Serien, fuer die Home Assistant eine Quelle hat."""
    registry = er.async_get(hass)
    sources: list[SeriesSource] = []
    for item in announced:
        series = str(item.get("id") or "")
        unit = str(item.get("unit") or "")
        if not series:
            continue
        if series.startswith("role:"):
            custom = CUSTOM_ROLE.match(series)
            if custom is not None:
                plus = _entity_for(registry, CUSTOM_UNIQUE_PREFIX + custom.group(2))
                if plus is not None:
                    sources.append(SeriesSource(series, unit, plus))
                continue
            mapping = ROLE_SOURCES.get(series)
            if mapping is None:
                continue
            plus = _entity_for(registry, mapping[0])
            minus = _entity_for(registry, mapping[1]) if mapping[1] else None
            if plus is None or (mapping[1] and minus is None):
                continue
        else:
            plus = _entity_for(registry, series)
            minus = None
            if plus is None:
                continue
        sources.append(SeriesSource(series, unit, plus, minus))
    return sources
