import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "src"))

from energy_node_common.settings import SlaveStatus


def test_new_status_fields_round_trip():
    status = SlaveStatus(poll_interval_s=10, diagnostic_poll_multiplier=2, actual_poll_interval_s=10,
                         runtime_status="rejected", error="kaputt", error_code="charge_source_required",
                         config_revision="abc", applied_revision="def")
    data = status.to_dict()
    assert data["error_code"] == "charge_source_required"
    assert data["config_revision"] == "abc"
    assert data["applied_revision"] == "def"
    assert SlaveStatus.from_dict(data) == status


def test_old_status_payloads_still_parse():
    status = SlaveStatus.from_dict({"poll_interval_s": 1, "runtime_status": "ok"})
    assert status.error_code == "" and status.config_revision == "" and status.applied_revision == ""
