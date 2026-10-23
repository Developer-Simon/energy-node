"""Gegenprobe: jedes Dokument des Vorlagen-Fixtures besteht die Dienstvalidierung."""

import json
from pathlib import Path

import automation_mqtt as automation

FIXTURE = (
    Path(__file__).resolve().parents[3]
    / "dashboard" / "test" / "fixtures" / "automation-templates-built.json"
)


def test_template_fixture_passes_service_validation(tmp_path):
    documents = json.loads(FIXTURE.read_text(encoding="utf-8"))["documents"]
    assert documents, "fixture has no documents"
    for index, document in enumerate(documents):
        path = tmp_path / f"document_{index}.json"
        path.write_text(json.dumps(document), encoding="utf-8")
        automation.load_and_validate(str(path))
