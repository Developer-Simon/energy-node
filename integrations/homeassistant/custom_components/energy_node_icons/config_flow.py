"""Ein Klick-Dialog: die Integration hat nichts zu konfigurieren."""
from homeassistant.config_entries import ConfigFlow

from .const import DOMAIN


class EnergyNodeIconsConfigFlow(ConfigFlow, domain=DOMAIN):
    """Genau eine Instanz - ein zweites Icon-Set waere dasselbe Set."""

    VERSION = 1

    async def async_step_user(self, user_input=None):
        # Check if we already have an entry (single-instance check)
        if self._async_current_entries():
            return self.async_abort(reason="single_instance_allowed")

        await self.async_set_unique_id(DOMAIN)
        self._abort_if_unique_id_configured()

        if user_input is None:
            return self.async_show_form(step_id="user")
        return self.async_create_entry(title="energy-node Icons", data={})
