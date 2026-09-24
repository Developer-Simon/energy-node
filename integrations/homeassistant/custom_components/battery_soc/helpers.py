"""Helper functions for battery_soc integration."""
from __future__ import annotations

from collections.abc import Mapping

from .battery_soc_core import SocParams, SourceConfig
from .const import (
    CONF_SYSTEM_TYPE, LAYOUT_PARALLEL, LAYOUT_SERIES, LAYOUT_SINGLE,
    SLOT_ENTITY_KEYS, SYSTEM_AC_COUPLED,
)


def params_from_config(cfg: Mapping) -> SocParams:
    """Create and validate SocParams from config dict."""
    p = SocParams.from_dict(dict(cfg))
    p.validate()
    return p


def source_config(cfg: Mapping, units: Mapping[str, str] | None = None) -> SourceConfig:
    """The core's view of which slots this entry fills."""
    return SourceConfig(
        configured=frozenset(slot for slot, key in SLOT_ENTITY_KEYS.items() if cfg.get(key)),
        units=dict(units or {}),
        topology=cfg.get("topology", "parallel"),
        bank_b_enabled=cfg.get("bank_b_enabled", True),
        system_type=cfg.get(CONF_SYSTEM_TYPE, SYSTEM_AC_COUPLED),
    )


def layout_from_config(cfg: Mapping) -> str:
    """Derive the form's bank layout from the stored fields."""
    if not cfg.get("bank_b_enabled", True):
        return LAYOUT_SINGLE
    return LAYOUT_SERIES if cfg.get("topology") == "series" else LAYOUT_PARALLEL
