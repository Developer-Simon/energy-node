import json
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "build"))
import make_changelog_json as mcj

CHANGELOG = """# Changelog

## [Unreleased]

### Features

- not a release yet

## v0.7.0 (2026-09-16)

### Features

- **dashboard:** add a thing (#34) (e5fc9db)
- **⚠ Breaking — dashboard:** drop the legacy block (#14) (e7f4ba1)
- **⚠ Breaking:** plain breaking entry (abcdef1)
- an entry from the old repo with no reference

### Wibble

- **x:** a heading the generator does not know (0123456)

### Fixes

## v0.6.0 (2026-09-10)

### Fixes

- **installer:** older fix (#9) (1111111)

## Unversioniert (bis 2026-08-20)

### Other

- prehistoric note (deadbee)
"""


def test_parse_skips_unreleased_and_unversioned_sections():
    releases = mcj.parse_changelog(CHANGELOG)
    assert [r["version"] for r in releases] == ["v0.7.0", "v0.6.0"]
    assert releases[0]["date"] == "2026-09-16"


def test_parse_entries():
    entries = mcj.parse_changelog(CHANGELOG)[0]["groups"][0]["entries"]
    assert entries[0] == {"text": "add a thing", "breaking": False, "scope": "dashboard", "pr": 34, "hash": "e5fc9db"}
    assert entries[1] == {"text": "drop the legacy block", "breaking": True, "scope": "dashboard", "pr": 14, "hash": "e7f4ba1"}
    assert entries[2] == {"text": "plain breaking entry", "breaking": True, "hash": "abcdef1"}
    assert entries[3] == {"text": "an entry from the old repo with no reference", "breaking": False}


def test_unknown_group_label_becomes_other_and_empty_groups_vanish():
    groups = mcj.parse_changelog(CHANGELOG)[0]["groups"]
    assert [(g["type"], g["label"]) for g in groups] == [("feat", "Features"), ("other", "Wibble")]


def _repo(tmp_path):
    (tmp_path / "scripts/version").mkdir(parents=True)
    (tmp_path / "dashboard").mkdir()
    (tmp_path / "services/shelly").mkdir(parents=True)
    (tmp_path / "installer").mkdir()
    (tmp_path / "dashboard/VERSION").write_text("v0.7.5\n", encoding="utf-8")
    (tmp_path / "dashboard/CHANGELOG.md").write_text(CHANGELOG, encoding="utf-8")
    (tmp_path / "services/shelly/manifest.json").write_text('{"service_id": "shelly", "version": "0.4.1"}', encoding="utf-8")
    (tmp_path / "installer/VERSION").write_text("v0.1.5\n", encoding="utf-8")
    table = {"schema_version": 1, "components": [
        {"id": "dashboard", "label": "Dashboard", "kind": "app", "target": "dashboard",
         "version_file": "dashboard/VERSION", "changelog": "dashboard/CHANGELOG.md", "bundle": True, "ci": True},
        {"id": "service:shelly", "label": "Shelly", "kind": "service", "target": "service:shelly",
         "version_file": "services/shelly/manifest.json", "changelog": "services/shelly/CHANGELOG.md", "bundle": True, "ci": True},
        {"id": "installer", "label": "Installer", "kind": "app", "target": "installer",
         "version_file": "installer/VERSION", "changelog": "installer/CHANGELOG.md", "bundle": False, "ci": True},
    ]}
    (tmp_path / "scripts/version/components.json").write_text(json.dumps(table), encoding="utf-8")
    return tmp_path


def test_only_bundle_components_and_versions_come_from_the_version_file(tmp_path):
    repo = _repo(tmp_path)
    table = json.loads((repo / "scripts/version/components.json").read_text(encoding="utf-8"))
    doc = mcj.build_document(repo, table, bundle_version="v0.7.5", max_releases=20, generated_at="t")
    assert [c["id"] for c in doc["components"]] == ["dashboard", "service:shelly"]
    assert doc["components"][0]["version"] == "v0.7.5"
    assert doc["components"][1]["version"] == "v0.4.1", "manifest.json versions are bare semver and get the v prefix"
    assert doc["components"][1]["releases"] == [], "a missing CHANGELOG.md is an empty history, not an error"
    assert doc["schema_version"] == 1 and doc["bundle_version"] == "v0.7.5"


def test_max_releases_caps_each_component(tmp_path):
    repo = _repo(tmp_path)
    table = json.loads((repo / "scripts/version/components.json").read_text(encoding="utf-8"))
    doc = mcj.build_document(repo, table, bundle_version="v0.7.5", max_releases=1, generated_at="t")
    assert [r["version"] for r in doc["components"][0]["releases"]] == ["v0.7.0"]


def test_cli_writes_the_document(tmp_path):
    repo = _repo(tmp_path)
    out = tmp_path / "changelog.json"
    assert mcj.main(["--repo", str(repo), "--out", str(out), "--bundle-version", "v9.9.9"]) == 0
    doc = json.loads(out.read_text(encoding="utf-8"))
    assert doc["bundle_version"] == "v9.9.9"
    assert doc["generated_at"]


def _flags(release):
    return [(e["text"], e["highlight"]) for g in release["groups"] for e in g["entries"]]


def test_highlights_in_a_release_with_pr_entries_are_its_squash_entries_and_breaking_changes():
    release = mcj.mark_highlights(mcj.parse_changelog(CHANGELOG))[0]
    assert _flags(release) == [
        ("add a thing", True),
        ("drop the legacy block", True),
        ("plain breaking entry", True),
        ("an entry from the old repo with no reference", False),
        ("a heading the generator does not know", False),
    ]


def test_highlights_in_a_release_without_pr_entries_are_its_features_and_fixes():
    text = """## v0.3.0 (2026-07-01)

### Features

- **dashboard:** a feature from the old repo (1234567)

### Fixes

- a fix from the old repo

### Tests

- **dashboard:** a test (7654321)
"""
    release = mcj.mark_highlights(mcj.parse_changelog(text))[0]
    assert _flags(release) == [
        ("a feature from the old repo", True),
        ("a fix from the old repo", True),
        ("a test", False),
    ]


def test_a_pr_entry_that_is_not_feat_fix_or_perf_is_no_highlight():
    text = """## v0.3.1 (2026-07-02)

### Features

- **dashboard:** the feature (#3) (1234567)

### Tests

- **dashboard:** the tests of the feature (#4) (7654321)

### Performance

- **dashboard:** faster (#5) (abcdef0)
"""
    release = mcj.mark_highlights(mcj.parse_changelog(text))[0]
    assert _flags(release) == [
        ("the feature", True),
        ("the tests of the feature", False),
        ("faster", True),
    ]


def test_the_document_carries_the_flags(tmp_path):
    repo = _repo(tmp_path)
    table = json.loads((repo / "scripts/version/components.json").read_text(encoding="utf-8"))
    doc = mcj.build_document(repo, table, bundle_version="v0.7.5", max_releases=20, generated_at="t")
    entries = [e for r in doc["components"][0]["releases"] for g in r["groups"] for e in g["entries"]]
    assert all(isinstance(e["highlight"], bool) for e in entries)
    assert doc["schema_version"] == 1, "the flag is additive, readers ignore unknown keys"
