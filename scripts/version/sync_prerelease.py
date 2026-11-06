"""Merge main into the prerelease branch and set the prerelease versions.

Runs on the checked-out `prerelease` branch. Exit codes:
  0  already current, fast-forwarded, or merged and committed
  2  wrong branch, dirty tree, or an invalid version on one side
  3  code conflict: merge aborted, unresolved paths printed as `CONFLICT <path>`
"""
import argparse
import fnmatch
import json
import re
import subprocess
import sys
from pathlib import Path

HERE = Path(__file__).resolve().parent
sys.dont_write_bytecode = True
sys.path.insert(0, str(HERE))
import prerelease as pr  # noqa: E402

PRE_BRANCH = "prerelease"
MERGE_MESSAGE = "chore(prerelease): merge main into prerelease"
VERSION_LINE = re.compile(r'^\s*"version"\s*:')
MANIFEST_VERSION = re.compile(r'("version"\s*:\s*")[^"]*(")')


class Refused(Exception):
    """Stop before touching anything; exit code 2."""


def git(root, *args, check=True):
    return subprocess.run(["git", *args], cwd=root, capture_output=True, text=True, check=check)


def repo_root():
    return Path(git(Path.cwd(), "rev-parse", "--show-toplevel").stdout.strip())


def read_arrays(root):
    """COMPONENTS and CHANGELOG_TARGETS, read from the copy in this tree."""
    script = 'source "$1"; printf "%s\\n" "${COMPONENTS[@]}"; echo ---; printf "%s\\n" "${CHANGELOG_TARGETS[@]}"'
    out = subprocess.run(
        ["bash", "-c", script, "bash", str(HERE / "components.sh")],
        cwd=root, capture_output=True, text=True, check=True,
    ).stdout
    components_part, targets_part = out.split("---\n")
    components = [tuple(line.split(":", 1)) for line in components_part.splitlines() if line]
    targets = [line for line in targets_part.splitlines() if line]
    return components, targets


def is_manifest(path):
    return path.endswith(".json")


def parse_version(path, text):
    """Return 'vX.Y.Z[-pre.N]' from a VERSION file or a manifest, or None."""
    if text is None:
        return None
    if is_manifest(path):
        raw = json.loads(text).get("version")
        return None if raw is None else f"v{raw}"
    return text.strip()


def show(root, ref, path):
    # Lenient decode: planning must not fail on bytes that only matter after the merge starts.
    result = subprocess.run(["git", "show", f"{ref}:{path}"], cwd=root, capture_output=True)
    if result.returncode != 0:
        return None
    return result.stdout.decode("utf-8", "surrogateescape")


def version_key(version):
    base, pre = pr._parse(version)
    return base, pre is None, pre or 0


def target_version(ours, theirs):
    """ours with a pre suffix: renumber against theirs. Otherwise the larger one."""
    if ours is None:
        return theirs
    if theirs is None:
        return ours
    if pr.is_pre(ours):
        return pr.renumber(ours, theirs)
    return ours if version_key(ours) >= version_key(theirs) else theirs


def plan_targets(root, components, main_ref):
    targets = {}
    for _prefix, version_file in components:
        ours = parse_version(version_file, show(root, "HEAD", version_file))
        theirs = parse_version(version_file, show(root, main_ref, version_file))
        if ours is None and theirs is None:
            continue
        targets[version_file] = target_version(ours, theirs)
    return targets


def conflicted_paths(root):
    out = git(root, "diff", "--name-only", "--diff-filter=U").stdout
    return [line for line in out.splitlines() if line]


def parse_blocks(text):
    """Yield (ours_lines, theirs_lines, start, end) per conflict block, default or diff3 style."""
    lines = text.splitlines(keepends=True)
    blocks = []
    i = 0
    while i < len(lines):
        if not lines[i].startswith("<<<<<<<"):
            i += 1
            continue
        start = i
        ours, theirs, section = [], [], "ours"
        i += 1
        while i < len(lines) and not lines[i].startswith(">>>>>>>"):
            line = lines[i]
            if line.startswith("|||||||"):
                section = "base"
            elif line.startswith("======="):
                section = "theirs"
            elif section == "ours":
                ours.append(line)
            elif section == "theirs":
                theirs.append(line)
            i += 1
        if i >= len(lines):
            raise ValueError("unterminated conflict block")
        blocks.append((ours, theirs, start, i))
        i += 1
    return lines, blocks


def manifest_is_version_only(text):
    _lines, blocks = parse_blocks(text)
    if not blocks:
        return False
    for ours, theirs, _start, _end in blocks:
        for side in (ours, theirs):
            if not side or not all(VERSION_LINE.match(line) for line in side):
                return False
    return True


def resolve_manifest(root, path):
    """Keep the ours side of each (version-only) block; the target is written later."""
    file = root / path
    lines, blocks = parse_blocks(file.read_text(encoding="utf-8"))
    out = []
    cursor = 0
    for ours, _theirs, start, end in blocks:
        out.extend(lines[cursor:start])
        out.extend(ours)
        cursor = end + 1
    out.extend(lines[cursor:])
    file.write_text("".join(out), encoding="utf-8")


def resolve_policy(root, conflicts):
    """Apply the conflict policy in place. Return the paths that need a merge abort."""
    unresolved = []
    for path in conflicts:
        if fnmatch.fnmatch(path, "*/CHANGELOG.md") or path == "CHANGELOG.md":
            resolve_ours(root, path, unresolved)
        elif path.endswith("VERSION") and not is_manifest(path):
            resolve_ours(root, path, unresolved)
        elif is_manifest(path) and manifest_is_version_only((root / path).read_text(encoding="utf-8")):
            resolve_manifest(root, path)
            git(root, "add", "--", path)
        else:
            unresolved.append(path)
    return unresolved


def resolve_ours(root, path, unresolved):
    result = git(root, "checkout", "--ours", "--", path, check=False)
    if result.returncode != 0:
        unresolved.append(path)
        return
    git(root, "add", "--", path)


def write_version(root, version_file, version):
    file = root / version_file
    if not file.exists():
        return
    if is_manifest(version_file):
        text = file.read_text(encoding="utf-8")
        raw = version[1:]
        new = MANIFEST_VERSION.sub(lambda m: f"{m.group(1)}{raw}{m.group(2)}", text, count=1)
        if new != text:
            file.write_text(new, encoding="utf-8")
    else:
        if file.read_text(encoding="utf-8").strip() != version:
            file.write_text(version + "\n", encoding="utf-8")


def sync(root, main_ref, push):
    if git(root, "branch", "--show-current").stdout.strip() != PRE_BRANCH:
        raise Refused(f"not on branch {PRE_BRANCH}")
    if git(root, "status", "--porcelain", "--untracked-files=no").stdout.strip():
        raise Refused("working tree is dirty")

    if git(root, "merge-base", "--is-ancestor", main_ref, "HEAD", check=False).returncode == 0:
        print("prerelease already contains main")
        return 0
    if git(root, "merge-base", "--is-ancestor", "HEAD", main_ref, check=False).returncode == 0:
        ff = git(root, "merge", "--ff-only", main_ref, check=False)
        if ff.returncode != 0:
            print(ff.stderr.strip() or "fast-forward failed", file=sys.stderr)
            return 1
        print("fast-forwarded prerelease to main")
        return 0

    components, changelog_targets = read_arrays(root)
    targets = plan_targets(root, components, main_ref)

    before = git(root, "rev-parse", "HEAD").stdout.strip()
    main_sha = git(root, "rev-parse", main_ref).stdout.strip()
    merge = git(root, "merge", "--no-commit", "--no-ff", main_ref, check=False)
    conflicts = conflicted_paths(root)
    if merge.returncode != 0 and not conflicts:
        git(root, "merge", "--abort", check=False)
        raise Refused(merge.stderr.strip() or "merge failed")

    # From here on the merge is in progress. Any failure must leave HEAD and the worktree as they were.
    try:
        unresolved = resolve_policy(root, conflicts) if conflicts else []
        if unresolved:
            abort_merge(root)
            for path in unresolved:
                print(f"CONFLICT {path}")
            return 3

        for version_file, version in targets.items():
            write_version(root, version_file, version)
        git(root, "add", "-u")
        git(root, "commit", "-q", "-m", MERGE_MESSAGE)

        # The open changelog heading comes from the committed versions, so the generator
        # runs after the merge commit and its output is amended into that same commit.
        for target in changelog_targets:
            subprocess.run([str(HERE.parent / "generate_changelog.sh"), target], cwd=root, check=True,
                           capture_output=True, text=True)
        git(root, "add", "-u")
        if git(root, "diff", "--cached", "--quiet", check=False).returncode != 0:
            git(root, "commit", "-q", "--amend", "--no-edit")
    except Exception as exc:
        if is_own_merge(root, before, main_sha):
            # Our own unpushed merge commit: go back to the recorded HEAD.
            git(root, "reset", "--hard", before)
            outcome = "merge commit reset"
        else:
            abort_merge(root)
            outcome = "merge aborted"
        print(f"error: prerelease sync failed, {outcome}: {describe(exc)}", file=sys.stderr)
        return 1

    print(f"merged {main_ref} into {PRE_BRANCH}")
    if push:
        pushed = git(root, "push", "origin", PRE_BRANCH, check=False)
        if pushed.returncode != 0:
            print(f"error: push failed, commit kept: {pushed.stderr.strip()}", file=sys.stderr)
            return 1
    return 0


def is_own_merge(root, before, main_sha):
    """True when HEAD is the merge commit this run created (parents: our old HEAD, then main)."""
    parents = git(root, "rev-list", "--parents", "-n1", "HEAD").stdout.split()[1:]
    return parents == [before, main_sha]


def abort_merge(root):
    """Abort only while a merge is in progress; a finished commit is never reset."""
    if git(root, "rev-parse", "-q", "--verify", "MERGE_HEAD", check=False).returncode == 0:
        git(root, "merge", "--abort", check=False)


def describe(exc):
    if isinstance(exc, subprocess.CalledProcessError):
        return (exc.stderr or "").strip() or f"{exc.cmd[0]} exited {exc.returncode}"
    return f"{type(exc).__name__}: {exc}"


def main(argv):
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument("--main", default="origin/main", dest="main_ref")
    parser.add_argument("--push", action="store_true")
    args = parser.parse_args(argv)
    try:
        return sync(repo_root(), args.main_ref, args.push)
    except Refused as exc:
        print(f"refused: {exc}", file=sys.stderr)
        return 2
    except ValueError as exc:
        # Planning errors (invalid versions) happen before the merge starts.
        print(f"error: {exc}", file=sys.stderr)
        return 2
    except Exception as exc:
        print(f"error: {describe(exc)}", file=sys.stderr)
        return 1


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
