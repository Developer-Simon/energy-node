"""Dashboard-Serie -> Home-Assistant-Entitaet ueber die MQTT-unique_id."""
from homeassistant.helpers import entity_registry as er

from custom_components.energy_node_companion.series_map import SeriesSource, resolve


def _mqtt(hass, unique_id, object_id, domain="sensor", platform="mqtt"):
    er.async_get(hass).async_get_or_create(domain, platform, unique_id, suggested_object_id=object_id)


async def test_roles_and_entities_resolve_via_mqtt_unique_id(hass):
    _mqtt(hass, "energy_node_pv_power", "pv")
    _mqtt(hass, "energy_node_grid_import", "imp")
    _mqtt(hass, "energy_node_grid_export", "exp")
    _mqtt(hass, "energy_node_house_load", "load")
    _mqtt(hass, "shelly_temp", "temp")
    _mqtt(hass, "shelly_setpoint", "setpoint", domain="number")

    sources = resolve(hass, [
        {"id": "role:pv", "unit": "W"},
        {"id": "role:grid", "unit": "W"},
        {"id": "role:battery", "unit": "W"},
        {"id": "role:load", "unit": "W"},
        {"id": "shelly_temp", "unit": "°C"},
        {"id": "shelly_setpoint", "unit": ""},
        {"id": "unknown", "unit": ""},
    ])

    assert sources == [
        SeriesSource("role:pv", "W", "sensor.pv"),
        SeriesSource("role:grid", "W", "sensor.imp", "sensor.exp"),
        SeriesSource("shelly_temp", "°C", "sensor.temp"),
        SeriesSource("shelly_setpoint", "", "number.setpoint"),
    ]


async def test_role_load_is_never_mapped(hass):
    # house_load traegt load_total, role:load im Browser den gemessenen
    # Anteil der Rolle load. Das ist nicht dieselbe Groesse.
    _mqtt(hass, "energy_node_house_load", "load")
    assert resolve(hass, [{"id": "role:load", "unit": "W"}]) == []


async def test_same_unique_id_from_another_platform_is_ignored(hass):
    _mqtt(hass, "energy_node_battery_soc", "soc", platform="template")
    assert resolve(hass, [{"id": "role:battery_soc", "unit": "%"}]) == []


async def test_derived_role_needs_both_sides(hass):
    _mqtt(hass, "energy_node_battery_charge", "charge")
    assert resolve(hass, [{"id": "role:battery", "unit": "W"}]) == []
