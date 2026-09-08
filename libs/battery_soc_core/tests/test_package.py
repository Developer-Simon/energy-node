import pytest


def test_package_imports():
    import battery_soc_core  # noqa: F401


def test_conftest_helper_exists():
    from tests.conftest import make_params  # noqa: F401
