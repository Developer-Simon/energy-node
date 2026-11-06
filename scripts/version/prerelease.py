"""Version rules for the prerelease line (vX.Y.Z-pre.N).

Only two shapes are valid: vX.Y.Z and vX.Y.Z-pre.N. Anything else raises
ValueError. The CLI prints the result and exits 2 on invalid input.
"""
import re
import sys

_RE = re.compile(r"^v(\d+)\.(\d+)\.(\d+)(?:-pre\.(\d+))?$")


def _parse(v: str):
    m = _RE.match(v)
    if not m:
        raise ValueError(f"invalid version: {v!r}")
    major, minor, patch = (int(m.group(i)) for i in (1, 2, 3))
    pre = int(m.group(4)) if m.group(4) is not None else None
    return (major, minor, patch), pre


def _fmt(base, pre=None):
    text = f"v{base[0]}.{base[1]}.{base[2]}"
    return text if pre is None else f"{text}-pre.{pre}"


def is_pre(v: str) -> bool:
    _, pre = _parse(v)
    return pre is not None


def next_pre(v: str) -> str:
    base, pre = _parse(v)
    if pre is not None:
        return _fmt(base, pre + 1)
    return _fmt((base[0], base[1], base[2] + 1), 1)


def renumber(pre: str, main_final: str) -> str:
    base, n = _parse(pre)
    if n is None:
        raise ValueError(f"not a prerelease version: {pre!r}")
    main_base, main_pre = _parse(main_final)
    if main_pre is not None:
        raise ValueError(f"main version must be final: {main_final!r}")
    if base <= main_base:
        base = (main_base[0], main_base[1], main_base[2] + 1)
    return _fmt(base, n)


def strip(v: str) -> str:
    base, _ = _parse(v)
    return _fmt(base)


def main(argv: list[str]) -> int:
    commands = {"next": next_pre, "strip": strip}
    if len(argv) != 3 or argv[1] not in commands:
        print("usage: prerelease.py <next|strip> <version>", file=sys.stderr)
        return 2
    try:
        print(commands[argv[1]](argv[2]))
    except ValueError as exc:
        print(f"error: {exc}", file=sys.stderr)
        return 2
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
