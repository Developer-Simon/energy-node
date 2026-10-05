#!/usr/bin/env python3
"""Keeps the documentation colours in step with the dashboard.

Usage: check_tokens.py <docs site.css> <dashboard base.css>

The docs' light mode copies the dashboard's "tageslicht" scheme, the dark
mode copies "mint" (:root). Every token both files define must carry the
same value. Text tokens must reach a 4.5:1 contrast on --bg and --panel.
"""
from __future__ import annotations

import re
import sys
from pathlib import Path

CONTRAST_PAIRS = [(fg, bg) for fg in ("--text", "--text-muted", "--link") for bg in ("--bg", "--panel")]
MIN_CONTRAST = 4.5


def _block(css: str, selector_re: str) -> dict[str, str]:
    m = re.search(selector_re + r"\s*\{(.*?)\}", css, re.S)
    if not m:
        return {}
    return {k: v.strip().lower() for k, v in re.findall(r"(--[\w-]+)\s*:\s*([^;]+);", m.group(1))}


def _hex(v: str) -> tuple[float, float, float] | None:
    m = re.fullmatch(r"#([0-9a-f]{3}|[0-9a-f]{6})", v)
    if not m:
        return None
    h = m.group(1)
    if len(h) == 3:
        h = "".join(c * 2 for c in h)
    return tuple(int(h[i:i + 2], 16) / 255 for i in (0, 2, 4))


def _lum(rgb) -> float:
    lin = [c / 12.92 if c <= 0.03928 else ((c + 0.055) / 1.055) ** 2.4 for c in rgb]
    return 0.2126 * lin[0] + 0.7152 * lin[1] + 0.0722 * lin[2]


def _resolve(tokens: dict[str, str], name: str) -> str | None:
    v = tokens.get(name)
    for _ in range(5):
        m = re.fullmatch(r"var\((--[\w-]+)\)", v or "")
        if not m:
            break
        v = tokens.get(m.group(1))
    return v


def check(site_css: str, base_css: str) -> list[str]:
    findings: list[str] = []
    mint = _block(base_css, r":root(?!\[)")
    day = {**mint, **_block(base_css, r':root\[data-theme="tageslicht"\]')}
    light = _block(site_css, r":root(?![\[:])")
    dark = {**light, **_block(site_css, r':root\[data-theme="dark"\]')}
    for mode, docs, dash in (("light", light, day), ("dark", dark, mint)):
        for name, value in docs.items():
            if name in dash and not value.startswith("var(") and value != dash[name]:
                findings.append(f"{mode}: {name} is {value}, dashboard has {dash[name]}")
        for fg, bg in CONTRAST_PAIRS:
            a, b = _hex(_resolve(docs, fg) or ""), _hex(_resolve(docs, bg) or "")
            if a is None or b is None:
                continue
            la, lb = sorted((_lum(a), _lum(b)), reverse=True)
            ratio = (la + 0.05) / (lb + 0.05)
            if ratio < MIN_CONTRAST:
                findings.append(f"{mode}: contrast {fg} on {bg} is {ratio:.2f}:1, needs {MIN_CONTRAST}:1")
    return findings


def main(argv: list[str]) -> int:
    if len(argv) != 3:
        print(__doc__, file=sys.stderr)
        return 2
    findings = check(Path(argv[1]).read_text(), Path(argv[2]).read_text())
    for f in findings:
        print(f, file=sys.stderr)
    return 1 if findings else 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
