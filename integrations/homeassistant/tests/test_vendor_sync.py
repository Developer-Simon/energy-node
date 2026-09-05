import sys
from pathlib import Path

REPO = Path(__file__).resolve().parents[3]
sys.path.insert(0, str(REPO / "scripts"))
import vendor_core


def test_vendored_core_matches_source():
    src = REPO / "src/battery_soc_core/battery_soc_core"
    dst = REPO / "integrations/homeassistant/custom_components/battery_soc/battery_soc_core"
    to_write, to_delete = vendor_core.plan(src, dst)
    assert (to_write, to_delete) == ([], []), (
        f"vendored core is stale — run `.venv/bin/python scripts/vendor_core.py`. "
        f"write={to_write} delete={to_delete}")
