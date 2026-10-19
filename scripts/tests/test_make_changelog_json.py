import json
import struct
import subprocess
import sys
from pathlib import Path

import pytest

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


def test_attach_translations_sets_i18n_on_pr_entries_only():
    releases = mcj.mark_highlights(mcj.parse_changelog(CHANGELOG))
    translations = {34: {"de": {"*": "füge etwas hinzu"}}, 9: {"de": {"installer": "älterer Fix"}}}
    mcj.attach_translations(releases, translations, "dashboard")
    entries = releases[0]["groups"][0]["entries"]
    assert entries[0]["i18n"] == {"de": "füge etwas hinzu"}
    assert "i18n" not in entries[1]          # PR 14 has no file
    assert "i18n" not in entries[2]          # no PR number
    fixes = releases[1]["groups"][0]["entries"]
    assert "i18n" not in fixes[0]            # PR 9 only for target installer


def test_build_document_reads_translation_files(tmp_path):
    (tmp_path / "scripts/version").mkdir(parents=True)
    (tmp_path / "dashboard").mkdir()
    (tmp_path / "dashboard/VERSION").write_text("v0.7.0\n", encoding="utf-8")
    (tmp_path / "dashboard/CHANGELOG.md").write_text(CHANGELOG, encoding="utf-8")
    (tmp_path / "changelog/highlights").mkdir(parents=True)
    (tmp_path / "changelog/highlights/34.json").write_text('{"de": {"dashboard": "Ding"}}', encoding="utf-8")
    table = {"components": [{"id": "dashboard", "label": "Dashboard", "kind": "app", "target": "dashboard",
                             "version_file": "dashboard/VERSION", "changelog": "dashboard/CHANGELOG.md", "bundle": True}]}
    doc = mcj.build_document(tmp_path, table, bundle_version="v0.7.0", max_releases=5, generated_at="x")
    entry = doc["components"][0]["releases"][0]["groups"][0]["entries"][0]
    assert entry["i18n"] == {"de": "Ding"}
    assert doc["schema_version"] == 1


def test_parse_version_handles_version_file_and_manifest():
    assert mcj.parse_version("v1.2.3\n", "") == "v1.2.3"
    assert mcj.parse_version('{"version": "0.4.2"}', ".json") == "v0.4.2"


DETAILS_CHANGELOG = """# Changelog

## v0.10.0 (2026-10-19)

### Features

- **dashboard:** with details (#148) (abc1234)
- an entry without a PR number
- **dashboard:** without details (#149) (abc1235)
"""

PNG = b"\x89PNG\r\n\x1a\n" + b"\x00\x00\x00\x0dIHDR" + struct.pack(">II", 1200, 800) + b"\x08\x06\x00\x00\x00"


def _details_repo(tmp_path):
    repo = _repo(tmp_path)
    (repo / "dashboard/CHANGELOG.md").write_text(DETAILS_CHANGELOG, encoding="utf-8")
    (repo / "dashboard/VERSION").write_text("v0.10.0\n", encoding="utf-8")
    (repo / "docs/images").mkdir(parents=True)
    (repo / "docs/images/shot.png").write_bytes(PNG)
    (repo / "docs/dashboard").mkdir(parents=True)
    (repo / "docs/dashboard/device-map.md").write_text("# Device map\n", encoding="utf-8")
    (repo / "changelog/highlights").mkdir(parents=True)
    (repo / "changelog/highlights/148.json").write_text(json.dumps({
        "details": {"*": {"en": "A switch", "de": "Ein Schalter", "image": "shot.png",
                          "doc": "dashboard/device-map.html#switches"}},
    }), encoding="utf-8")
    return repo


def test_attach_details_sets_details_on_pr_entries_only(tmp_path):
    releases = mcj.mark_highlights(mcj.parse_changelog(DETAILS_CHANGELOG))
    translations = {148: {"details": {"*": {"en": "A switch", "de": "Ein Schalter"}}}, 149: {"de": {"*": "nur Text"}}}
    mcj.attach_details(releases, tmp_path, translations, "dashboard", "abc123")
    entries = releases[0]["groups"][0]["entries"]
    assert entries[0]["details"] == {"text": {"en": "A switch", "de": "Ein Schalter"}}
    assert "details" not in entries[1], "no PR number, no details"
    assert "details" not in entries[2], "PR 149 has no details key"


def test_build_document_resolves_details_with_the_image_ref(tmp_path):
    repo = _details_repo(tmp_path)
    table = json.loads((repo / "scripts/version/components.json").read_text(encoding="utf-8"))
    doc = mcj.build_document(repo, table, bundle_version="v0.10.0", max_releases=5, generated_at="t", image_ref="v0.10.0")
    entry = doc["components"][0]["releases"][0]["groups"][0]["entries"][0]
    assert entry["details"]["text"] == {"en": "A switch", "de": "Ein Schalter"}
    assert entry["details"]["image"]["url"].endswith("/v0.10.0/docs/images/shot.png")
    assert (entry["details"]["image"]["width"], entry["details"]["image"]["height"]) == (1200, 800)
    assert entry["details"]["doc"] == "https://developer-simon.github.io/energy-node/dashboard/device-map.html#switches"


def test_cli_passes_the_image_ref(tmp_path):
    repo = _details_repo(tmp_path)
    out = tmp_path / "changelog.json"
    assert mcj.main(["--repo", str(repo), "--out", str(out), "--bundle-version", "v0.10.0", "--image-ref", "abc123"]) == 0
    doc = json.loads(out.read_text(encoding="utf-8"))
    entry = doc["components"][0]["releases"][0]["groups"][0]["entries"][0]
    assert "/abc123/" in entry["details"]["image"]["url"]


@pytest.mark.parametrize("details", [{"*": None}, "x"])
def test_attach_details_raises_on_malformed_details(tmp_path, details):
    releases = mcj.mark_highlights(mcj.parse_changelog(DETAILS_CHANGELOG))
    with pytest.raises(ValueError, match="#148"):
        mcj.attach_details(releases, tmp_path, {148: {"details": details}}, "dashboard", "abc123")


@pytest.mark.parametrize("details", [{"*": None}, "x"])
def test_build_document_fails_on_malformed_details(tmp_path, details):
    repo = _details_repo(tmp_path)
    (repo / "changelog/highlights/148.json").write_text(json.dumps({"details": details}), encoding="utf-8")
    table = json.loads((repo / "scripts/version/components.json").read_text(encoding="utf-8"))
    with pytest.raises(ValueError, match="#148"):
        mcj.build_document(repo, table, bundle_version="v0.10.0", max_releases=5, generated_at="t", image_ref="v0.10.0")


def test_attach_details_skips_targets_without_details(tmp_path):
    releases = mcj.mark_highlights(mcj.parse_changelog(DETAILS_CHANGELOG))
    translations = {148: {"details": {"installer": {"en": "x", "de": "x"}}}}
    mcj.attach_details(releases, tmp_path, translations, "dashboard", None)
    assert "details" not in releases[0]["groups"][0]["entries"][0]


def test_attach_details_pins_the_image_of_a_target_entry(tmp_path):
    repo = _details_repo(tmp_path)
    (repo / "changelog/highlights/148.json").write_text(json.dumps({
        "details": {"dashboard": {"en": "A switch", "de": "Ein Schalter", "image": "shot.png"}},
    }), encoding="utf-8")
    subprocess.run(["git", "init", "-q"], cwd=repo, check=True)
    subprocess.run(["git", "add", "-A"], cwd=repo, check=True)
    subprocess.run(["git", "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "-m", "init"], cwd=repo, check=True)
    head = subprocess.run(["git", "rev-parse", "HEAD"], cwd=repo, check=True, capture_output=True, text=True).stdout.strip()
    translations = json.loads((repo / "changelog/highlights/148.json").read_text(encoding="utf-8"))
    translations = {148: translations}
    releases = mcj.mark_highlights(mcj.parse_changelog((repo / "dashboard/CHANGELOG.md").read_text(encoding="utf-8")))
    mcj.attach_details(releases, repo, translations, "dashboard", None)
    url = releases[0]["groups"][0]["entries"][0]["details"]["image"]["url"]
    assert f"/{head}/docs/images/shot.png" in url
