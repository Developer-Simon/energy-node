import json
import sys
from pathlib import Path

MIRROR = Path(__file__).resolve().parents[1] / "mirror"
MANIFEST = Path(__file__).resolve().parents[1] / "custom_components/battery_soc/manifest.json"

sys.path.insert(0, str(Path(__file__).resolve().parents[3] / "scripts"))
import check_mirror_manifest


def test_hacs_json_is_valid_and_has_required_keys():
    data = json.loads((MIRROR / "hacs.json").read_text())
    assert data["name"]
    assert data["homeassistant"]


def test_overrides_cover_the_public_manifest_fields():
    overrides = json.loads((MIRROR / "manifest.overrides.json").read_text())
    manifest = json.loads(MANIFEST.read_text())
    for key in ("documentation", "issue_tracker", "codeowners", "version"):
        assert key in overrides
        assert key in manifest  # the base manifest also carries a value (placeholder ok)


def test_license_matches_repo_license():
    repo_license = (Path(__file__).resolve().parents[3] / "LICENSE").read_text()
    assert (MIRROR / "LICENSE").read_text() == repo_license


def test_ai_disclaimer_names_service_and_contributor_rule():
    text = (MIRROR / "AI-DISCLAIMER.md").read_text()
    assert "Claude" in text
    assert "Claude Code" in text
    # contributors must disclose their AI use
    assert "pull request" in text.lower()
    assert "disclose" in text.lower()
    pr_template = (MIRROR / ".github/pull_request_template.md").read_text()
    assert "AI-use disclosure" in pr_template


def test_validate_workflow_runs_hacs_and_hassfest():
    wf = (MIRROR / ".github/workflows/validate.yml").read_text()
    assert "hacs/action@main" in wf
    assert "home-assistant/actions/hassfest" in wf


def test_check_mirror_manifest_passes():
    assert check_mirror_manifest.main() == 0
