"""The bundled device templates and automation rules ship empty.

A fresh install starts without example devices or rules. The example
content is supplied by the onboarding flow, not by the bundle.
"""

import json
from pathlib import Path

import pytest

REPO = Path(__file__).resolve().parents[2]
SERVICES = REPO / "services"
DEVICE_TEMPLATES = sorted(SERVICES.glob("*/*_devices.json"))
RULES_FILE = SERVICES / "automation" / "automation_rules.json"


@pytest.mark.parametrize("path", DEVICE_TEMPLATES, ids=lambda p: p.parent.name)
def test_bundled_device_templates_are_empty(path):
    with path.open(encoding="utf-8") as fh:
        assert json.load(fh) == [], f"{path.relative_to(REPO)} is not empty"


def test_bundled_rules_are_empty():
    with RULES_FILE.open(encoding="utf-8") as fh:
        data = json.load(fh)
    assert data["rules"] == []
    assert data.get("scenarios", []) == []
