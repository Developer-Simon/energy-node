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
