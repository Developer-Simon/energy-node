#!/usr/bin/env python3
"""Offline pre-publish sanity checks for the HACS mirror.

Run before scripts/publish_mirror.sh. No Home Assistant source, no network --
just the things hassfest / hacs/action would reject, checked locally. Prints
every failure and exits non-zero.

    .venv/bin/python scripts/check_mirror_manifest.py

Stdlib only.
"""
from __future__ import annotations

import ast
import json
import re
import subprocess
import sys
from pathlib import Path

SEMVER = re.compile(r"^\d+\.\d+\.\d+$")


def repo_root() -> Path:
    out = subprocess.check_output(["git", "rev-parse", "--show-toplevel"])
    return Path(out.decode().strip())


def _imported_roots(path: Path) -> set[str]:
    tree = ast.parse(path.read_text())
    roots: set[str] = set()
    for node in ast.walk(tree):
        if isinstance(node, ast.Import):
            roots.update(a.name.split(".")[0] for a in node.names)
        elif isinstance(node, ast.ImportFrom) and node.level == 0 and node.module:
            roots.add(node.module.split(".")[0])
    return roots


def _key_tree(obj, prefix: str = "") -> set[str]:
    out: set[str] = set()
    if isinstance(obj, dict):
        for key, value in obj.items():
            out.add(f"{prefix}/{key}")
            out |= _key_tree(value, f"{prefix}/{key}")
    return out


def check(root: Path) -> list[str]:
    cc = root / "integrations/homeassistant/custom_components/battery_soc"
    mirror = root / "integrations/homeassistant/mirror"
    fails: list[str] = []

    try:
        manifest = json.loads((cc / "manifest.json").read_text())
    except (OSError, ValueError) as exc:
        return [f"manifest.json unreadable: {exc}"]

    for key in ("domain", "name", "version", "documentation", "issue_tracker",
                "codeowners", "iot_class", "config_flow"):
        if key not in manifest:
            fails.append(f"manifest.json missing key: {key}")
    if manifest.get("requirements", "MISSING") != []:
        fails.append(
            f"manifest.json requirements must be [] (got {manifest.get('requirements')!r})"
        )
    version = str(manifest.get("version", ""))
    if not SEMVER.match(version):
        fails.append(f"manifest.json version must be a bare X.Y.Z, got {version!r}")

    # hassfest: keys must be domain, name, then the rest alphabetical.
    keys = list(manifest)
    if keys[:2] != ["domain", "name"]:
        fails.append(
            f"manifest.json must start with 'domain', 'name' (got {keys[:2]})"
        )
    rest = keys[2:]
    if rest != sorted(rest):
        fails.append(
            "manifest.json keys after domain/name must be alphabetical: "
            f"got {rest}, want {sorted(rest)}"
        )

    try:
        hacs = json.loads((mirror / "hacs.json").read_text())
        ha_min = hacs.get("homeassistant")
        if not isinstance(ha_min, str) or not re.match(r"^\d+\.\d+", ha_min):
            fails.append(
                f"hacs.json 'homeassistant' must be a version string, got {ha_min!r}"
            )
    except (OSError, ValueError) as exc:
        fails.append(f"hacs.json unreadable: {exc}")

    try:
        strings = json.loads((cc / "strings.json").read_text())
        en = json.loads((cc / "translations/en.json").read_text())
        if _key_tree(strings) != _key_tree(en):
            only_s = sorted(_key_tree(strings) - _key_tree(en))
            only_e = sorted(_key_tree(en) - _key_tree(strings))
            fails.append(
                "strings.json / translations/en.json key trees differ: "
                f"only-strings={only_s} only-en={only_e}"
            )
    except (OSError, ValueError) as exc:
        fails.append(f"strings.json / translations/en.json unreadable: {exc}")

    # Brand assets (HACS 'brands' check falls back to these) + README media.
    for asset in ("icon.png", "icon@2x.png"):
        if not (cc / "brand" / asset).is_file():
            fails.append(f"brand/{asset} is missing")
    shot = root / "integrations/homeassistant/docs/img/IntegrationDemo.png"
    if not shot.is_file():
        fails.append("docs/img/IntegrationDemo.png (README screenshot) is missing")
    try:
        readme = (mirror / "README.md").read_text()
        for needle in ("brand/icon.png", "IntegrationDemo.png",
                       "my.home-assistant.io/redirect/hacs_repository"):
            if needle not in readme:
                fails.append(f"mirror/README.md no longer references {needle!r}")
    except OSError as exc:
        fails.append(f"mirror/README.md unreadable: {exc}")

    vendored = cc / "battery_soc_core"
    if not vendored.is_dir():
        fails.append("vendored battery_soc_core/ is missing")
    else:
        if any(p.is_dir() for p in vendored.rglob("tests")):
            fails.append("vendored battery_soc_core/ still contains a tests/ dir")
        stdlib = set(sys.stdlib_module_names)
        allowed_local = {"battery_soc_core"}
        for py in sorted(vendored.rglob("*.py")):
            bad = _imported_roots(py) - stdlib - allowed_local
            if bad:
                fails.append(f"vendored {py.name} imports non-stdlib: {sorted(bad)}")

    return fails


def main() -> int:
    fails = check(repo_root())
    if fails:
        print("check_mirror_manifest: FAIL", file=sys.stderr)
        for failure in fails:
            print(f"  - {failure}", file=sys.stderr)
        return 1
    print("check_mirror_manifest: OK")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
