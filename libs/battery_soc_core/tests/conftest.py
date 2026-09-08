"""Test fixtures for battery_soc_core."""
from battery_soc_core.params import SocParams


def make_params(**overrides):
    """SocParams with the defaults that most core tests want."""
    return SocParams(**overrides)
