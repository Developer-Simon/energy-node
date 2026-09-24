import sys
from pathlib import Path

REPO = Path(__file__).resolve().parents[3]
sys.path.insert(0, str(REPO / "scripts"))
import sync_ha_descriptions


def test_shared_descriptions_match_the_service_schema():
    stale = sync_ha_descriptions.plan(REPO)
    assert not stale, (
        "HA descriptions differ from services/battery_soc/battery_soc_devices.schema.json. "
        "Edit the schema, then run `.venv/bin/python scripts/sync_ha_descriptions.py`. "
        f"stale={sorted(str(p.relative_to(REPO)) for p in stale)}")
