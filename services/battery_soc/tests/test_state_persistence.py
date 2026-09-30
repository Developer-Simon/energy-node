"""Speicherintervall, Flush beim Beenden und Wiederherstellung nach Absturz."""
import json
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

import battery_soc_mqtt as battery_soc  # noqa: E402

SERVICE_DIR = Path(__file__).resolve().parents[1]


def test_schema_default_matches_the_service_default():
    schema = json.loads((SERVICE_DIR / "config.schema.json").read_text())
    prop = schema["properties"]["state_save_interval_s"]
    assert prop["default"] == battery_soc.DEFAULT_STATE_SAVE_INTERVAL_S
    assert "state_save_interval_s" not in schema["required"]
