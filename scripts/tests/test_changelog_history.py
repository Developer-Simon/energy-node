import json
import subprocess
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "build"))
import changelog_history as ch
import make_changelog_json as mcj

TABLE = {"components": [
    {"id": "dashboard", "label": "Dashboard", "kind": "app", "target": "dashboard",
     "version_file": "dashboard/VERSION", "changelog": "dashboard/CHANGELOG.md", "bundle": True},
    {"id": "service:shelly", "label": "Shelly", "kind": "service", "target": "service:shelly",
     "version_file": "shelly/manifest.json", "changelog": "shelly/CHANGELOG.md", "bundle": True},
]}


def git(repo, *args):
    subprocess.run(["git", "-C", str(repo), *args], check=True, capture_output=True)


def commit(repo, dashboard, shelly, tag=None):
    (repo / "dashboard").mkdir(exist_ok=True)
    (repo / "shelly").mkdir(exist_ok=True)
    (repo / "dashboard/VERSION").write_text(dashboard + "\n", encoding="utf-8")
    (repo / "shelly/manifest.json").write_text(json.dumps({"version": shelly.lstrip("v")}), encoding="utf-8")
    git(repo, "add", ".")
    git(repo, "commit", "-q", "-m", dashboard)
    if tag:
        git(repo, "tag", tag)


def make_repo(tmp_path):
    git(tmp_path, "init", "-q")
    git(tmp_path, "config", "user.email", "t@example.com")
    git(tmp_path, "config", "user.name", "t")
    commit(tmp_path, "v0.1.0", "v0.4.0", tag="v0.1.0")
    commit(tmp_path, "v0.2.0", "v0.4.0", tag="v0.2.0")
    commit(tmp_path, "v0.2.1", "v0.4.1", tag="v0.2.1-b1")    # pre-release, ignored
    commit(tmp_path, "v0.3.0", "v0.4.2")                     # working tree, no tag yet
    return tmp_path


def test_compare_and_range():
    assert ch.compare_versions("v0.10.0", "v0.9.9") == 1
    assert ch.compare_versions("0.4.2", "v0.4.2") == 0
    assert ch.in_range("v0.4.2", "v0.4.0", "v0.4.2")
    assert not ch.in_range("v0.4.0", "v0.4.0", "v0.4.2")
    assert ch.in_range("v0.4.2", None, "v0.4.2")
    assert not ch.in_range("v0.4.1", None, "v0.4.2")


def test_final_tags_skip_prereleases(tmp_path):
    assert ch.final_tags(make_repo(tmp_path)) == ["v0.2.0", "v0.1.0"]


def test_final_tags_without_git_is_empty(tmp_path):
    assert ch.final_tags(tmp_path) == []


def test_history_chains_versions_newest_first(tmp_path):
    repo = make_repo(tmp_path)
    steps = ch.history(repo, TABLE, "v0.3.0", 5)
    assert [(s["version"], s["previous"]) for s in steps] == [("v0.3.0", "v0.2.0"), ("v0.2.0", "v0.1.0"), ("v0.1.0", "")]
    assert steps[0]["versions"] == {"dashboard": "v0.3.0", "service:shelly": "v0.4.2"}
    assert steps[0]["previous_versions"] == {"dashboard": "v0.2.0", "service:shelly": "v0.4.0"}
    assert steps[2]["previous_versions"] == {}


def test_history_respects_limit_and_a_tagged_current(tmp_path):
    repo = make_repo(tmp_path)
    git(repo, "tag", "v0.3.0")
    steps = ch.history(repo, TABLE, "v0.3.0", 2)
    assert [(s["version"], s["previous"]) for s in steps] == [("v0.3.0", "v0.2.0"), ("v0.2.0", "v0.1.0")]


def test_version_document_cuts_the_releases():
    document = {"components": [
        {"id": "dashboard", "label": "Dashboard", "kind": "app", "version": "v0.3.0", "releases": [
            {"version": "v0.3.0", "groups": []}, {"version": "v0.2.0", "groups": []}]},
        {"id": "service:shelly", "label": "Shelly", "kind": "service", "version": "v0.4.2", "releases": [
            {"version": "v0.4.2", "groups": []}, {"version": "v0.4.1", "groups": []}, {"version": "v0.4.0", "groups": []}]},
        {"id": "new", "label": "New", "kind": "tool", "version": "v0.1.0", "releases": []},
    ]}
    step = {"version": "v0.3.0", "previous": "v0.2.0",
            "versions": {"dashboard": "v0.3.0", "service:shelly": "v0.4.2"},
            "previous_versions": {"dashboard": "v0.2.0", "service:shelly": "v0.4.0"}}
    out = ch.version_document(document, step, "t")
    assert out["bundle_version"] == "v0.3.0" and out["previous_bundle_version"] == "v0.2.0"
    assert out["schema_version"] == 1 and out["generated_at"] == "t"
    by_id = {c["id"]: [r["version"] for r in c["releases"]] for c in out["components"]}
    assert by_id == {"dashboard": ["v0.3.0"], "service:shelly": ["v0.4.2", "v0.4.1"]}
    assert out["components"][1]["version"] == "v0.4.2"


def test_cli_archive_translation_reaches_the_older_version_file(tmp_path):
    # Archiv-Anpassung: die Uebersetzung steht in archive-v0.json, der Eintrag (#34)
    # gehoert zu einem aelteren Release als dem neuesten Bundle. Die Datei dieser
    # aelteren Bundle-Version muss sie tragen.
    repo = tmp_path / "repo"
    (repo / "dashboard").mkdir(parents=True)
    (repo / "scripts/version").mkdir(parents=True)
    (repo / "changelog/highlights").mkdir(parents=True)
    git(repo, "init", "-q")
    git(repo, "config", "user.email", "t@example.com")
    git(repo, "config", "user.name", "t")
    table = {"components": [
        {"id": "dashboard", "label": "Dashboard", "kind": "app", "target": "dashboard",
         "version_file": "dashboard/VERSION", "changelog": "dashboard/CHANGELOG.md", "bundle": True}]}
    (repo / "scripts/version/components.json").write_text(json.dumps(table), encoding="utf-8")
    (repo / "changelog/highlights/archive-v0.json").write_text(
        json.dumps({"34": {"de": {"*": "Ding"}}}), encoding="utf-8")
    old = ("## v0.6.0 (2026-09-01)\n\n### Fixes\n\n- **dashboard:** older fix (#9) (1111111)\n")
    new = old + ("\n## v0.7.0 (2026-09-16)\n\n### Features\n\n- **dashboard:** add a thing (#34) (e5fc9db)\n")

    def release(version, changelog):
        (repo / "dashboard/VERSION").write_text(version + "\n", encoding="utf-8")
        (repo / "dashboard/CHANGELOG.md").write_text(changelog, encoding="utf-8")
        git(repo, "add", ".")
        git(repo, "commit", "-q", "-m", version)

    release("v0.6.0", old)
    git(repo, "tag", "v0.6.0")
    release("v0.7.0", new)
    git(repo, "tag", "v0.7.0")
    release("v0.7.1", new)                                    # working tree, no tag yet

    out = tmp_path / "changelog.json"
    out_dir = tmp_path / "cl"
    assert mcj.main(["--repo", str(repo), "--out", str(out), "--out-dir", str(out_dir)]) == 0

    newest = json.loads(out.read_text(encoding="utf-8"))
    assert newest["bundle_version"] == "v0.7.1"
    older = json.loads((out_dir / "v0.7.0.json").read_text(encoding="utf-8"))
    assert older["previous_bundle_version"] == "v0.6.0"
    entry = older["components"][0]["releases"][0]["groups"][0]["entries"][0]
    assert entry["pr"] == 34
    assert entry["i18n"] == {"de": "Ding"}


def test_a_manifest_without_version_at_the_previous_tag_is_unknown_not_fatal(tmp_path):
    # Service-Manifeste aelterer Tags haben noch kein "version": die Komponente ist dort
    # unbekannt und bekommt nur ihr aktuelles Release, statt die Historie abzubrechen.
    repo = tmp_path
    git(repo, "init", "-q")
    git(repo, "config", "user.email", "t@example.com")
    git(repo, "config", "user.name", "t")
    (repo / "dashboard").mkdir()
    (repo / "shelly").mkdir()
    (repo / "dashboard/VERSION").write_text("v0.1.0\n", encoding="utf-8")
    (repo / "shelly/manifest.json").write_text('{"service_id": "shelly"}', encoding="utf-8")
    git(repo, "add", ".")
    git(repo, "commit", "-q", "-m", "old")
    git(repo, "tag", "v0.1.0")
    (repo / "dashboard/VERSION").write_text("v0.2.0\n", encoding="utf-8")
    (repo / "shelly/manifest.json").write_text('{"service_id": "shelly", "version": "0.4.2"}', encoding="utf-8")
    git(repo, "add", ".")
    git(repo, "commit", "-q", "-m", "new")

    steps = ch.history(repo, TABLE, "v0.2.0", 5)
    assert steps[0]["previous"] == "v0.1.0"
    assert steps[0]["previous_versions"] == {"dashboard": "v0.1.0"}
    assert steps[0]["versions"] == {"dashboard": "v0.2.0", "service:shelly": "v0.4.2"}

    document = {"components": [
        {"id": "service:shelly", "label": "Shelly", "kind": "service", "version": "v0.4.2", "releases": [
            {"version": "v0.4.2", "groups": []}, {"version": "v0.4.1", "groups": []}]},
    ]}
    out = ch.version_document(document, steps[0], "t")
    assert [r["version"] for r in out["components"][0]["releases"]] == ["v0.4.2"]
