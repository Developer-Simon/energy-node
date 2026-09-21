"""Haelt scripts/version/components.json gegen die drei Bash-Listen.

Die Tabelle traegt, was keine der Listen kennt (Label, Art, "im Bundle"). Damit
sie nicht still von COMPONENTS (components.sh), den Generator-Zielen und
CHANGELOG_TARGETS (version-bump.yml) wegdriftet, prueft dieser Test alle vier
gegeneinander.
"""
import json
import re
import subprocess
from pathlib import Path

REPO = Path(__file__).resolve().parents[2]
TABLE = json.loads((REPO / "scripts/version/components.json").read_text(encoding="utf-8"))
ROWS = TABLE["components"]
KINDS = {"app", "shared", "service", "library", "tool", "integration"}


def _bash_components():
    out = subprocess.run(
        ["bash", "-c", f'source "{REPO}/scripts/version/components.sh"; printf "%s\\n" "${{COMPONENTS[@]}}"'],
        capture_output=True, text=True, check=True,
    ).stdout
    return [line.split(":", 1) for line in out.splitlines() if line]


def _generator_targets():
    # usage() schreibt nach stderr und endet mit "|all]".
    proc = subprocess.run(
        ["bash", str(REPO / "scripts/generate_changelog.sh"), "--help"],
        capture_output=True, text=True,
    )
    match = re.search(r"\] \[([^\]]*)\|all\]", proc.stderr)
    assert match, proc.stderr
    return set(match.group(1).split("|"))


def _workflow_targets():
    lines = (REPO / ".github/workflows/version-bump.yml").read_text(encoding="utf-8").splitlines()
    for index, line in enumerate(lines):
        if line.strip().startswith("CHANGELOG_TARGETS:"):
            rest = line.split(":", 1)[1].strip()
            if rest not in (">-", ">", "|", "|-"):
                return set(rest.split())
            indent = len(line) - len(line.lstrip())
            tokens = []
            for follow in lines[index + 1:]:
                if not follow.strip() or len(follow) - len(follow.lstrip()) <= indent:
                    break
                tokens += follow.split()
            return set(tokens)
    raise AssertionError("CHANGELOG_TARGETS fehlt in version-bump.yml")


def test_ids_are_unique_and_kinds_known():
    ids = [row["id"] for row in ROWS]
    assert len(ids) == len(set(ids))
    assert {row["kind"] for row in ROWS} <= KINDS


def test_every_table_target_exists_in_the_generator_and_vice_versa():
    assert {row["target"] for row in ROWS} == _generator_targets()


def test_ci_flag_matches_the_workflow():
    assert {row["target"] for row in ROWS if row["ci"]} == _workflow_targets()


def test_version_files_match_components_sh():
    pairs = {version_file for _prefix, version_file in _bash_components()}
    assert {row["version_file"] for row in ROWS} == pairs


def test_files_exist():
    for row in ROWS:
        assert (REPO / row["version_file"]).is_file(), row["version_file"]
        if row["ci"]:
            assert (REPO / row["changelog"]).is_file(), row["changelog"]
