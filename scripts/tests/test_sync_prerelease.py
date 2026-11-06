"""Tests for scripts/version/sync_prerelease.py.

Each test builds a throwaway repo: a copy of scripts/ with a small component
set in components.sh, a `main` branch and a `prerelease` branch checked out.
The script runs with `--main main` (local ref), so no remote is needed.
"""
import json
import os
import re
import shutil
import subprocess
from pathlib import Path

REPO = Path(__file__).resolve().parents[2]
REAL_SCRIPTS = REPO / "scripts"

SMALL_COMPONENTS = '''COMPONENTS=(
  "dashboard/:dashboard/VERSION"
  "services/shelly/:services/shelly/manifest.json"
)
'''

SMALL_TARGETS = '''CHANGELOG_TARGETS=(
  dashboard service:shelly
)
'''

BASE_CHANGELOG = "# Changelog\n\n## v0.4.5\n\n- base\n"


def _manifest(version, poll=10, timeout=5, kind="device"):
    data = {
        "service_id": "shelly",
        "version": version,
        "unit": "shelly-rpc.service",
        "poll_interval_s": poll,
        "diagnostic_poll_multiplier": 3,
        "http_timeout_s": timeout,
        "kind": kind,
    }
    return json.dumps(data, indent=2) + "\n"


def _git(repo, *args):
    return subprocess.run(
        ["git", *args], cwd=repo, capture_output=True, text=True, check=True,
    ).stdout.strip()


def _write(repo, files):
    for rel, text in files.items():
        path = repo / rel
        path.parent.mkdir(parents=True, exist_ok=True)
        # surrogateescape lets a test write a raw non-UTF-8 byte through a str.
        path.write_bytes(text.encode("utf-8", "surrogateescape"))


def _commit(repo, files, message):
    _write(repo, files)
    _git(repo, "add", "-A")
    _git(repo, "commit", "-q", "-m", message)


def _base_repo(tmp_path):
    repo = tmp_path / "repo"
    repo.mkdir()
    shutil.copytree(REAL_SCRIPTS, repo / "scripts",
                    ignore=shutil.ignore_patterns("__pycache__", "tests"))
    components = repo / "scripts/version/components.sh"
    text = components.read_text(encoding="utf-8")
    text = re.sub(r"^COMPONENTS=\(\n.*?^\)\n", SMALL_COMPONENTS, text, count=1, flags=re.M | re.S)
    text = re.sub(r"^CHANGELOG_TARGETS=\(\n.*?^\)\n", SMALL_TARGETS, text, count=1, flags=re.M | re.S)
    components.write_text(text, encoding="utf-8")

    _git(repo, "init", "-q")
    _git(repo, "symbolic-ref", "HEAD", "refs/heads/main")
    _git(repo, "config", "user.name", "Test Author")
    _git(repo, "config", "user.email", "author@example.invalid")
    _git(repo, "config", "commit.gpgsign", "false")
    _commit(repo, {
        "dashboard/VERSION": "v0.4.5\n",
        "dashboard/CHANGELOG.md": BASE_CHANGELOG,
        "services/shelly/manifest.json": _manifest("0.4.5"),
        "services/shelly/CHANGELOG.md": BASE_CHANGELOG,
        "services/shelly/driver.py": "STATE = 1\n",
    }, "base")
    return repo


def _setup(tmp_path, ours=None, theirs=None):
    """main gets `theirs`, prerelease gets `ours`; prerelease ends checked out."""
    repo = _base_repo(tmp_path)
    _git(repo, "checkout", "-q", "-b", "prerelease")
    if ours:
        _commit(repo, ours, "feat(dashboard): prerelease change")
    _git(repo, "checkout", "-q", "main")
    if theirs:
        _commit(repo, theirs, "fix(dashboard): main change")
    _git(repo, "checkout", "-q", "prerelease")
    return repo


def _run(repo, env=None):
    # Without this, a missing script shows up as exit 2 and passes the refusal tests.
    assert (repo / "scripts/version/sync_prerelease.py").is_file(), "sync_prerelease.py is missing"
    return subprocess.run(
        ["python3", "scripts/version/sync_prerelease.py", "--main", "main"],
        cwd=repo, capture_output=True, text=True, env=env,
    )


def _no_identity_env():
    """Environment in which `git commit` cannot find an identity for this call only."""
    env = dict(os.environ)
    env.update({
        "GIT_CONFIG_GLOBAL": "/dev/null",
        "GIT_AUTHOR_NAME": "",
        "GIT_COMMITTER_NAME": "",
    })
    return env


def _parents(repo):
    return _git(repo, "rev-list", "--parents", "-n1", "HEAD").split()[1:]


def _read(repo, rel):
    return (repo / rel).read_text(encoding="utf-8")


def _first_heading(text):
    return next(line for line in text.splitlines() if line.startswith("## "))


def _status(repo):
    return _git(repo, "status", "--porcelain")


def test_fixture_has_diverged_branches_and_small_component_set(tmp_path):
    repo = _setup(tmp_path, ours={"dashboard/feature.txt": "pre\n"},
                  theirs={"dashboard/other.txt": "main\n"})
    assert _git(repo, "rev-parse", "--abbrev-ref", "HEAD") == "prerelease"
    base = _git(repo, "merge-base", "main", "prerelease")
    assert base != _git(repo, "rev-parse", "main")
    assert base != _git(repo, "rev-parse", "prerelease")
    out = subprocess.run(
        ["bash", "-c", 'source scripts/version/components.sh; printf "%s\\n" "${COMPONENTS[@]}"; '
                       'printf "%s\\n" "${CHANGELOG_TARGETS[@]}"'],
        cwd=repo, capture_output=True, text=True, check=True,
    ).stdout.split()
    assert out == [
        "dashboard/:dashboard/VERSION", "services/shelly/:services/shelly/manifest.json",
        "dashboard", "service:shelly",
    ]


def test_fast_forward(tmp_path):
    repo = _setup(tmp_path, theirs={"dashboard/other.txt": "main\n"})
    main_sha = _git(repo, "rev-parse", "main")
    proc = _run(repo)
    assert proc.returncode == 0, proc.stdout + proc.stderr
    assert _git(repo, "rev-parse", "HEAD") == main_sha
    assert len(_parents(repo)) == 1


def test_already_current(tmp_path):
    repo = _setup(tmp_path, ours={"dashboard/feature.txt": "pre\n"})
    head_before = _git(repo, "rev-parse", "HEAD")
    proc = _run(repo)
    assert proc.returncode == 0, proc.stdout + proc.stderr
    assert _git(repo, "rev-parse", "HEAD") == head_before


def test_renumbers_collision(tmp_path):
    repo = _setup(
        tmp_path,
        ours={
            "dashboard/VERSION": "v0.4.6-pre.2\n",
            "dashboard/CHANGELOG.md": "# Changelog\n\n## v0.4.6-pre.2\n\n- pre\n",
            "dashboard/feature.txt": "pre\n",
            "services/shelly/manifest.json": _manifest("0.4.6-pre.2"),
        },
        theirs={
            "dashboard/VERSION": "v0.4.6\n",
            "dashboard/CHANGELOG.md": "# Changelog\n\n## v0.4.6\n\n- main\n",
            "services/shelly/manifest.json": _manifest("0.4.6"),
        },
    )
    main_sha = _git(repo, "rev-parse", "main")
    proc = _run(repo)
    assert proc.returncode == 0, proc.stdout + proc.stderr
    assert _read(repo, "dashboard/VERSION").strip() == "v0.4.7-pre.2"
    # The CHANGELOG conflict took ours; the generator ran afterwards and kept that entry.
    changelog = _read(repo, "dashboard/CHANGELOG.md")
    assert "<<<<<<<" not in changelog
    assert "- pre" in changelog
    assert "- main" not in changelog
    # The open (first) heading carries the renumbered version. The hand-written
    # "## v0.4.6-pre.2" block has no commit refs, so the generator keeps it below.
    assert _first_heading(changelog).startswith("## v0.4.7-pre.2")
    assert json.loads(_read(repo, "services/shelly/manifest.json"))["version"] == "0.4.7-pre.2"
    assert _first_heading(_read(repo, "services/shelly/CHANGELOG.md")).startswith("## v0.4.7-pre.2")
    assert main_sha in _parents(repo)
    assert _status(repo) == ""


def test_keeps_ahead_version(tmp_path):
    repo = _setup(
        tmp_path,
        ours={"dashboard/VERSION": "v0.4.8-pre.1\n"},
        theirs={"dashboard/VERSION": "v0.4.6\n"},
    )
    proc = _run(repo)
    assert proc.returncode == 0, proc.stdout + proc.stderr
    assert _read(repo, "dashboard/VERSION").strip() == "v0.4.8-pre.1"


def test_main_only_component(tmp_path):
    repo = _setup(
        tmp_path,
        ours={"dashboard/feature.txt": "pre\n"},
        theirs={"services/shelly/manifest.json": _manifest("0.4.6")},
    )
    proc = _run(repo)
    assert proc.returncode == 0, proc.stdout + proc.stderr
    assert json.loads(_read(repo, "services/shelly/manifest.json"))["version"] == "0.4.6"


def test_manifest_version_only_conflict(tmp_path):
    repo = _setup(
        tmp_path,
        ours={"services/shelly/manifest.json": _manifest("0.4.6-pre.1", timeout=7)},
        theirs={"services/shelly/manifest.json": _manifest("0.4.6", poll=12)},
    )
    proc = _run(repo)
    assert proc.returncode == 0, proc.stdout + proc.stderr
    data = json.loads(_read(repo, "services/shelly/manifest.json"))
    assert data["version"] == "0.4.7-pre.1"
    assert data["http_timeout_s"] == 7
    assert data["poll_interval_s"] == 12
    assert _status(repo) == ""


def test_manifest_with_real_conflict(tmp_path):
    repo = _setup(
        tmp_path,
        ours={"services/shelly/manifest.json": _manifest("0.4.5", kind="sensor")},
        theirs={"services/shelly/manifest.json": _manifest("0.4.5", kind="meter")},
    )
    proc = _run(repo)
    assert proc.returncode == 3, proc.stdout + proc.stderr
    assert "CONFLICT services/shelly/manifest.json" in proc.stdout.splitlines()


def test_code_conflict(tmp_path):
    repo = _setup(
        tmp_path,
        ours={"services/shelly/driver.py": "STATE = 2\n"},
        theirs={"services/shelly/driver.py": "STATE = 3\n"},
    )
    proc = _run(repo)
    assert proc.returncode == 3, proc.stdout + proc.stderr
    assert "CONFLICT services/shelly/driver.py" in proc.stdout.splitlines()


def test_conflict_leaves_branch_untouched(tmp_path):
    repo = _setup(
        tmp_path,
        ours={"services/shelly/driver.py": "STATE = 2\n"},
        theirs={"services/shelly/driver.py": "STATE = 3\n"},
    )
    head_before = _git(repo, "rev-parse", "HEAD")
    proc = _run(repo)
    assert proc.returncode == 3, proc.stdout + proc.stderr
    assert _git(repo, "rev-parse", "HEAD") == head_before
    assert _status(repo) == ""


def test_refuses_wrong_branch(tmp_path):
    repo = _setup(
        tmp_path,
        ours={"dashboard/feature.txt": "pre\n"},
        theirs={"dashboard/other.txt": "main\n"},
    )
    _git(repo, "checkout", "-q", "-b", "feature")
    proc = _run(repo)
    assert proc.returncode == 2, proc.stdout + proc.stderr


def test_refuses_dirty_tree(tmp_path):
    repo = _setup(
        tmp_path,
        ours={"dashboard/feature.txt": "pre\n"},
        theirs={"dashboard/other.txt": "main\n"},
    )
    _write(repo, {"dashboard/VERSION": "v9.9.9\n"})
    proc = _run(repo)
    assert proc.returncode == 2, proc.stdout + proc.stderr


def _assert_untouched(repo, head_before):
    assert _git(repo, "rev-parse", "HEAD") == head_before
    assert _status(repo) == ""
    assert not (repo / ".git" / "MERGE_HEAD").exists()


def test_missing_identity_refuses_before_merge(tmp_path):
    # git merge needs an identity already at merge start, so this is a clean refusal.
    repo = _setup(
        tmp_path,
        ours={"dashboard/VERSION": "v0.4.6-pre.2\n"},
        theirs={"dashboard/VERSION": "v0.4.6\n"},
    )
    head_before = _git(repo, "rev-parse", "HEAD")
    proc = _run(repo, env=_no_identity_env())
    assert proc.returncode == 2, proc.stdout + proc.stderr
    _assert_untouched(repo, head_before)


def test_commit_failure_aborts_merge(tmp_path):
    # A failing pre-commit hook fails only the final commit, after the versions were written.
    repo = _setup(
        tmp_path,
        ours={"dashboard/VERSION": "v0.4.6-pre.2\n"},
        theirs={"dashboard/VERSION": "v0.4.6\n"},
    )
    hook = repo / ".git" / "hooks" / "pre-commit"
    hook.write_text("#!/bin/sh\necho hook refused >&2\nexit 1\n", encoding="utf-8")
    hook.chmod(0o755)
    head_before = _git(repo, "rev-parse", "HEAD")
    proc = _run(repo)
    assert proc.returncode == 1, proc.stdout + proc.stderr
    assert "merge aborted" in proc.stderr
    _assert_untouched(repo, head_before)


def test_unreadable_manifest_aborts_merge(tmp_path):
    # Both sides change the unit line identically (clean merge), so the raw byte
    # sits in the merged file. The version line conflicts, so the policy reads the file.
    raw_unit = "shelly\udcff-rpc.service"
    repo = _setup(
        tmp_path,
        ours={"services/shelly/manifest.json": _manifest("0.4.6-pre.1").replace("shelly-rpc.service", raw_unit)},
        theirs={"services/shelly/manifest.json": _manifest("0.4.6").replace("shelly-rpc.service", raw_unit)},
    )
    head_before = _git(repo, "rev-parse", "HEAD")
    proc = _run(repo)
    assert proc.returncode == 1, proc.stdout + proc.stderr
    assert "merge aborted" in proc.stderr
    _assert_untouched(repo, head_before)


def test_generator_failure_after_commit_resets_own_merge(tmp_path):
    # The prerelease side ships a broken generator, so it only fails after the merge commit exists.
    repo = _setup(
        tmp_path,
        ours={"scripts/generate_changelog.sh": "#!/usr/bin/env bash\necho generator broke >&2\nexit 1\n"},
        theirs={"dashboard/other.txt": "main\n"},
    )
    head_before = _git(repo, "rev-parse", "HEAD")
    proc = _run(repo)
    assert proc.returncode == 1, proc.stdout + proc.stderr
    assert "merge commit reset" in proc.stderr
    _assert_untouched(repo, head_before)
