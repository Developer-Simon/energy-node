import sys
from pathlib import Path

REPO = Path(__file__).resolve().parents[3]
sys.path.insert(0, str(REPO / "scripts"))
import vendor_card


def test_vendored_card_core_matches_source():
    src = REPO / vendor_card.SRC_REL
    dst = REPO / vendor_card.DST_REL
    stale = vendor_card.plan(src, dst)
    assert stale == [], (
        "vendored card core is stale - run "
        "`.venv/bin/python scripts/vendor_card.py`"
    )
