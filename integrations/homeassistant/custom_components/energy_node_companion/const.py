"""Konstanten der Verlauf-Integration."""

DOMAIN = "energy_node_companion"
CONF_URL = "url"
CONF_VERIFY_SSL = "verify_ssl"

# Protokollversion des Verlauf-Austauschs (exchangeProtocolVersion im
# Dashboard, PROTOCOL in history-exchange.js).
PROTOCOL = 1
TIERS = ("1m", "5m")
# Muss RASTER in history-coverage.js und rasterFor() im Dashboard spiegeln,
# sonst passen die Deckungsraster nicht aufeinander und der Browser plant
# keine einzige Nachfrage. Werte: (Rasterschritt, Fenster) in Millisekunden.
RASTER_MS = {
    "1m": (3_600_000, 7 * 24 * 3_600_000),
    "5m": (21_600_000, 30 * 24 * 3_600_000),
}
BUCKET_S = {"1m": 60, "5m": 300}

PEER_LABEL = "Home Assistant"
# Wie oft das eigene Angebot neu berechnet wird. Ein neues Angebot geht nur
# hinaus, wenn sich das Raster tatsaechlich geaendert hat.
REFRESH_SECONDS = 600
RECONNECT_MIN_S = 2
RECONNECT_MAX_S = 60

# Panel in der Seitenleiste (Plan 2026-10-04-ha-dashboard-panel).
CONF_PANEL = "panel"
PANEL_ALL = "all"
PANEL_ADMINS = "admins"
PANEL_OFF = "off"
PANEL_MODES = (PANEL_ALL, PANEL_ADMINS, PANEL_OFF)
# Name und Symbol des Panels in der Seitenleiste, frei waehlbar.
CONF_PANEL_TITLE = "panel_title"
CONF_PANEL_ICON = "panel_icon"
# Verlaeufe an das Dashboard liefern. Mindestens Verlaeufe oder Panel muss an
# sein, sonst tut der Eintrag nichts.
CONF_HISTORY = "history"

# Cookie, mit dem der Browser dem Proxy zeigt, dass ein HA-Benutzer das Panel
# geoeffnet hat. Gilt nur unter dem Proxy-Pfad des Eintrags.
PANEL_COOKIE = "energy_node_panel"
# Das Panel erneuert die Sitzung alle 30 Minuten (REFRESH_MS in
# www/energy-node-panel.js), lange bevor sie ablaeuft.
PANEL_SESSION_TTL_S = 7200
PANEL_ELEMENT = "energy-node-panel"
PANEL_TITLE = "Energy Node"
PANEL_ICON = "mdi:solar-power-variant"
# Bei jeder Aenderung an www/energy-node-panel.js hochzaehlen (Cache-Bust).
PANEL_JS_VERSION = 2
STATIC_URL = "/energy_node_static"


def proxy_prefix(entry_id: str) -> str:
    """Pfad, unter dem HA das Dashboard dieses Eintrags durchreicht."""
    return f"/api/energy_node_companion/proxy/{entry_id}"


def panel_url_path(entry_id: str) -> str:
    """URL des Panels in der Seitenleiste, stabil fuer einen Eintrag."""
    return f"energy-node-{entry_id[-8:].lower()}"

# Energiestatistik (Spec 2026-10-11-energiestatistik, Abschnitt 2). Die
# Stufe 1h fuehrt Stundenmittel in W (zahlenmaessig Wh), das Raster zaehlt
# abgeschlossene Stunden je UTC-Tag. Muss rasterFor("1h") im Dashboard
# spiegeln.
PEER_KIND = "companion"
STATS_TIER = "1h"
HOUR_MS = 3_600_000
DAY_MS = 86_400_000
STATS_WINDOW_DAYS = 3650
CONF_STATS = "stats"
COUNTER_PREFIX = "counter_"
STATS_ROLES = (
    "pv", "grid_import", "grid_export", "battery_charge", "battery_discharge", "load_total", "wallbox", "heat_pump",
)
# Rolle -> unique_id des Sensors, den das Dashboard per Discovery anmeldet
# (energydiscovery.go). Ohne Zaehler liefert Home Assistant nur die Rollen
# aus diesem Eintrag.
STATS_DISCOVERY = {
    "pv": "energy_node_pv_power",
    "grid_import": "energy_node_grid_import",
    "grid_export": "energy_node_grid_export",
    "battery_charge": "energy_node_battery_charge",
    "battery_discharge": "energy_node_battery_discharge",
    "load_total": "energy_node_house_load",
    "wallbox": "energy_node_wallbox",
    "heat_pump": "energy_node_heat_pump",
}
# Verbrauchskategorien melden ihren Sensor als energy_node_custom_<id>.
# Die Energiestatistik bietet jede solche Entitaet als Rolle custom:<id> an.
CUSTOM_UNIQUE_PREFIX = "energy_node_custom_"


def counter_key(role: str) -> str:
    """Optionsschluessel des Zaehlers einer Rolle."""
    return f"{COUNTER_PREFIX}{role}"
