"""Helper functions for battery_soc integration."""
from __future__ import annotations

from collections.abc import Mapping

from .battery_soc_core import SocParams


def params_from_config(cfg: Mapping) -> SocParams:
    """Create and validate SocParams from config dict."""
    p = SocParams.from_dict(dict(cfg))
    p.validate()
    return p
