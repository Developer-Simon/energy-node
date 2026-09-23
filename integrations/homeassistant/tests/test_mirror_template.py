import json
import sys
from pathlib import Path

import pytest

sys.path.insert(0, str(Path(__file__).resolve().parents[3] / "scripts"))
import check_mirror_manifest


@pytest.mark.parametrize("component", ["battery_soc", "energy_node_icons"])
def test_hacs_json_is_valid_and_has_required_keys(component):
    mirror = Path(__file__).resolve().parents[1] / "mirror" / component
    data = json.loads((mirror / "hacs.json").read_text())
    assert data["name"]
    assert data["homeassistant"]


@pytest.mark.parametrize("component", ["battery_soc", "energy_node_icons"])
def test_overrides_cover_the_public_manifest_fields(component):
    mirror = Path(__file__).resolve().parents[1] / "mirror" / component
    manifest = Path(__file__).resolve().parents[1] / f"custom_components/{component}/manifest.json"
    overrides = json.loads((mirror / "manifest.overrides.json").read_text())
    mf = json.loads(manifest.read_text())
    for key in ("documentation", "issue_tracker", "codeowners", "version"):
        assert key in overrides
        assert key in mf  # the base manifest also carries a value (placeholder ok)


@pytest.mark.parametrize("component", ["battery_soc", "energy_node_icons"])
def test_license_matches_repo_license(component):
    mirror = Path(__file__).resolve().parents[1] / "mirror" / component
    repo_license = (Path(__file__).resolve().parents[3] / "LICENSE").read_text()
    assert (mirror / "LICENSE").read_text() == repo_license


@pytest.mark.parametrize("component", ["battery_soc", "energy_node_icons"])
def test_ai_disclaimer_names_service_and_contributor_rule(component):
    mirror = Path(__file__).resolve().parents[1] / "mirror" / component
    text = (mirror / "AI-DISCLAIMER.md").read_text()
    assert "Claude" in text
    assert "Claude Code" in text
    # contributors must disclose their AI use
    assert "pull request" in text.lower()
    assert "disclose" in text.lower()
    pr_template = (mirror / ".github/pull_request_template.md").read_text()
    assert "AI-use disclosure" in pr_template


@pytest.mark.parametrize("component", ["battery_soc", "energy_node_icons"])
def test_validate_workflow_runs_hacs_and_hassfest(component):
    mirror = Path(__file__).resolve().parents[1] / "mirror" / component
    wf = (mirror / ".github/workflows/validate.yml").read_text()
    assert "hacs/action@main" in wf
    assert "home-assistant/actions/hassfest" in wf


@pytest.mark.parametrize("component", ["battery_soc", "energy_node_icons"])
def test_check_mirror_manifest_passes(component):
    import subprocess
    result = subprocess.run(
        [sys.executable, "scripts/check_mirror_manifest.py", "--component", component],
        cwd=Path(__file__).resolve().parents[3],
        capture_output=True,
    )
    assert result.returncode == 0
