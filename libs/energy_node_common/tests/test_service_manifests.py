"""Jeder Geräte-Dienst trägt ein wohlgeformtes Manifest und Schema-Fragment.

Ausfuehren mit `.venv/bin/pytest libs/energy_node_common/tests/test_service_manifests.py`.
"""

import json
from pathlib import Path

import pytest

REPO_ROOT = Path(__file__).resolve().parents[3]
SERVICES_DIR = REPO_ROOT / "services"

# Verzeichnisname -> erwartete service_id (Bindestrich-/Suffix-Abweichungen).
EXPECTED = {
    "apsystems_ez1": "apsystems",
    "shelly": "shelly",
    "trucki": "trucki",
    "tuya_mqtt": "tuya",
    "battery_soc": "battery_soc",
    "automation": "automation",
}


@pytest.mark.parametrize("dir_name, service_id", sorted(EXPECTED.items()))
def test_manifest_and_fragment_are_wellformed(dir_name, service_id):
    service_dir = SERVICES_DIR / dir_name
    manifest_path = service_dir / "manifest.json"
    manifest = json.loads(manifest_path.read_text(encoding="utf-8"))

    assert set(manifest) == {"service_id", "unit", "schema", "required"}
    assert manifest["service_id"] == service_id
    assert manifest["schema"] == "config.schema.json"
    assert (service_dir / manifest["unit"]).is_file()
    assert isinstance(manifest["required"], list) and manifest["required"]
    assert all(isinstance(x, str) and x for x in manifest["required"])
    assert manifest["required"][0] == "service_id"

    fragment = json.loads((service_dir / manifest["schema"]).read_text(encoding="utf-8"))
    assert fragment["type"] == "object"
    assert fragment["additionalProperties"] is False
    assert fragment["required"] == manifest["required"]
    assert set(fragment["properties"]) >= set(fragment["required"])
    assert fragment["properties"]["service_id"]["title"] == "Dienst-ID"


def test_no_unexpected_service_dir_has_a_manifest():
    with_manifest = {p.parent.name for p in SERVICES_DIR.glob("*/manifest.json")}
    assert with_manifest == set(EXPECTED)


def test_manifest_set_matches_config_template_services():
    """Der Manifest-Satz und der services-Block der ausgelieferten
    config.json-Vorlage muessen deckungsgleich sein: energy_node_common
    prueft auf dem Node beide Richtungen (fehlendes Manifest wie
    ueberzaehliges Manifest), also faellt der Bridge-Start sonst hart.
    """
    template = json.loads(
        (REPO_ROOT / "services" / "energy-node.config.json").read_text(encoding="utf-8")
    )
    config_services = set(template["services"])
    manifest_ids = {
        json.loads(
            (SERVICES_DIR / dir_name / "manifest.json").read_text(encoding="utf-8")
        )["service_id"]
        for dir_name in EXPECTED
    }
    assert manifest_ids == config_services
