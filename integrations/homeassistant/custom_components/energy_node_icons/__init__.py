"""energy-node Icons: meldet das Icon-Set im Frontend an, mehr nicht."""
from pathlib import Path

from homeassistant.components.frontend import add_extra_js_url
from homeassistant.components.http import StaticPathConfig
from homeassistant.config_entries import ConfigEntry
from homeassistant.core import HomeAssistant

from .const import DOMAIN, FRONTEND_FILES, FRONTEND_URL_BASE


async def _async_register_frontend(hass: HomeAssistant) -> None:
    """Modul ausliefern und im Frontend laden - einmal pro HA-Start."""
    if hass.data.get(f"{DOMAIN}_frontend_registered"):
        return
    http = getattr(hass, "http", None)
    if http is None:
        # Ohne http-Komponente (Testinstanzen, minimale Setups) bleibt das
        # Icon-Set einfach unregistriert, statt den Config-Entry zu kippen.
        return

    www = Path(__file__).parent / "www"
    await http.async_register_static_paths(
        [StaticPathConfig(FRONTEND_URL_BASE, str(www), False)]
    )
    for name in FRONTEND_FILES:
        add_extra_js_url(hass, f"{FRONTEND_URL_BASE}/{name}")
    hass.data[f"{DOMAIN}_frontend_registered"] = True


async def async_setup_entry(hass: HomeAssistant, entry: ConfigEntry) -> bool:
    await _async_register_frontend(hass)
    return True


async def async_unload_entry(hass: HomeAssistant, entry: ConfigEntry) -> bool:
    # Ein registrierter statischer Pfad und eine extra_js_url lassen sich zur
    # Laufzeit nicht zuruecknehmen. Das Modul bleibt deshalb bis zum Neustart
    # geladen; entfernt ist die Integration trotzdem, und ein Icon-Set, das
    # niemand mehr referenziert, tut nichts.
    return True
