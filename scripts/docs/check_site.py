#!/usr/bin/env python3
"""Checks a built documentation site.

Usage: check_site.py <site-dir> <docs-dir>

Reports broken internal links and anchors, navigation entries without a
source file, published pages missing from the navigation, a navigation
deeper than three levels, and anchor_moves entries pointing nowhere.
Exit 1 with one finding per line on stderr, exit 0 when clean.
"""
from __future__ import annotations

import json
import re
import sys
from html.parser import HTMLParser
from pathlib import Path
from urllib.parse import unquote, urlsplit

import yaml

BASEURL = "/energy-node"
MAX_DEPTH = 3
SKIP_DIRS = ("_", "redirects/", "superpowers/")


class _Page(HTMLParser):
    def __init__(self) -> None:
        super().__init__()
        self.links: list[str] = []
        self.ids: set[str] = set()
        self.moves: dict[str, str] = {}
        self._in_moves = False

    def handle_starttag(self, tag, attrs):
        a = dict(attrs)
        if a.get("id"):
            self.ids.add(a["id"])
        if tag == "a" and a.get("href"):
            self.links.append(a["href"])
        if tag in ("img", "script", "link") and (a.get("src") or (tag == "link" and a.get("href"))):
            if tag == "link" and a.get("rel") not in ("stylesheet", "icon", "apple-touch-icon"):
                return
            self.links.append(a.get("src") or a["href"])
        self._in_moves = tag == "script" and a.get("id") == "anchor-moves"

    def handle_data(self, data):
        if self._in_moves and data.strip():
            self.moves = json.loads(data)

    def handle_endtag(self, tag):
        if tag == "script":
            self._in_moves = False


def _parse(path: Path) -> _Page:
    p = _Page()
    p.feed(path.read_text(encoding="utf-8", errors="replace"))
    return p


def _resolve(site: Path, page: Path, href: str) -> tuple[Path | None, str]:
    """Maps a link on `page` to a file in `site` and its fragment.
    Returns (None, "") for links that leave the site."""
    parts = urlsplit(href)
    if parts.scheme or parts.netloc or href.startswith(("mailto:", "tel:", "javascript:")):
        return None, ""
    path = unquote(parts.path)
    if not path:
        return page, parts.fragment
    if path.startswith("/"):
        if not path.startswith(BASEURL + "/") and path != BASEURL:
            return None, ""
        target = site / path[len(BASEURL):].lstrip("/")
    else:
        target = (page.parent / path)
    if path.endswith("/") or target.is_dir():
        target = target / "index.html"
    return target.resolve(), parts.fragment


def _nav_urls(items, depth: int, out: list[tuple[str, int]]) -> None:
    for item in items or []:
        if "url" in item:
            out.append((item["url"], depth))
        _nav_urls(item.get("children"), depth + 1, out)


def _front_matter(path: Path) -> str:
    text = path.read_text(encoding="utf-8", errors="replace")
    m = re.match(r"---\n(.*?)\n---", text, re.S)
    return m.group(1) if m else ""


def check(site: Path, docs: Path) -> list[str]:
    site, docs = site.resolve(), docs.resolve()
    findings: list[str] = []
    pages = {p.resolve(): _parse(p) for p in site.rglob("*.html")
             if p.relative_to(site).parts[0] != "pagefind"}

    for page, parsed in pages.items():
        rel = page.relative_to(site)
        for href in parsed.links:
            target, frag = _resolve(site, page, href)
            if target is None:
                continue
            if not target.exists():
                findings.append(f"{rel}: broken link {href}")
                continue
            if frag and target.suffix == ".html":
                tp = pages.get(target) or _parse(target)
                if frag not in tp.ids and frag not in tp.moves:
                    findings.append(f"{rel}: missing anchor {href} (#{frag})")
        for old, dest in parsed.moves.items():
            target, frag = _resolve(site, page, dest)
            if target is None or not target.exists():
                findings.append(f"{rel}: anchor_moves #{old} -> {dest} does not exist")
            elif frag and frag not in (pages.get(target) or _parse(target)).ids:
                findings.append(f"{rel}: anchor_moves #{old} -> {dest} has no #{frag}")

    nav = yaml.safe_load((docs / "_data" / "nav.yml").read_text()) or []
    urls: list[tuple[str, int]] = []
    for group in nav:
        _nav_urls(group.get("items"), 1, urls)
    listed = set()
    for url, depth in urls:
        listed.add(url)
        if not (docs / url).exists():
            findings.append(f"nav.yml: {url} has no source file")
        if depth > MAX_DEPTH:
            findings.append(f"nav.yml: {url} is deeper than {MAX_DEPTH} levels")

    for md in docs.rglob("*.md"):
        rel = md.relative_to(docs).as_posix()
        if rel.startswith(SKIP_DIRS) or "/_" in rel:
            continue
        if re.search(r"^redirect_to:", _front_matter(md), re.M):
            continue
        if rel not in listed:
            findings.append(f"{rel}: published but not in nav.yml")
    return findings


def main(argv: list[str]) -> int:
    if len(argv) != 3:
        print(__doc__, file=sys.stderr)
        return 2
    findings = check(Path(argv[1]), Path(argv[2]))
    for f in findings:
        print(f, file=sys.stderr)
    return 1 if findings else 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
