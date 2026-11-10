#!/usr/bin/env python3
"""Emits the data behind the "What's new" page of the documentation site.

Usage: whats-new.py [repo-root] > docs/_data/whats_new.json

The page (docs/whats-new.md) reads it as site.data.whats_new. Every highlight
of the bundle components since MIN_TAG (plus the installer components) is one
entry. An entry belongs to the first release tag on which its component already
had that version or a newer one. Highlights newer than the last tag form the
release "next". Entries of the same PR in several components are one entry.

build() is pure and takes the data as arguments. main() reads git and the
repository, and needs the full history and the tags.
"""
import json
import re
import subprocess
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "build"))
import highlight_details  # noqa: E402
import highlight_i18n  # noqa: E402
from changelog_history import compare_versions, versions_at  # noqa: E402
from make_changelog_json import build_document  # noqa: E402

MIN_TAG = "v0.8.0"
EXTRA_COMPONENTS = ("installer", "installer-webui")
NEXT = "next"
NEXT_LABEL = "Next release"
NEXT_DATE = "not released yet"
TAG = re.compile(r"^v\d+\.\d+\.\d+$")
PAGES_PREFIX = highlight_details.PAGES_BASE
DASHBOARD_PAGE = "dashboard/index.md"
INSTALLER_IDS = ("installer", "installer-webui", "bootstrap")
KIND_ORDER = {"image": 0, "text": 1, "tour": 2}

SCOPE_AREAS = {
    "automation": "Automations", "automations": "Automations",
    "auth": "Users and permissions", "devicemap": "Device map",
    "history": "History", "onboarding": "First-time setup",
    "diagnostics": "Diagnostics", "energy": "Energy", "energyflow": "Energy",
    "energy_flow": "Energy", "battery_soc": "Battery SoC", "battery-soc": "Battery SoC",
    "battery-card": "Overview cards", "settings": "Settings", "updates": "Updates",
    "changelog": "Updates", "installer": "Installer", "bootstrap": "Installer",
    "deploy": "Installer", "redeploy": "Installer", "shelly": "Device services",
    "tuya": "Device services", "trucki/dashboard": "Device services",
    "apsystems": "Device services", "services": "Device services",
    "discovery": "Devices", "devices": "Devices",
}


def _since(component_id, version, tags, tag_versions):
    """First tag whose version of the component is at least version, else NEXT."""
    for tag in tags:
        current = tag_versions.get(tag, {}).get(component_id)
        if current is not None and compare_versions(version, current) <= 0:
            return tag
    return NEXT


def _since_key(since):
    """Sort key: NEXT first, then tags by version."""
    if since == NEXT:
        return (1, (0, 0, 0))
    return (0, tuple(int(part) for part in since[1:].split(".")))


def _capitalised(text):
    text = text[:1].upper() + text[1:]
    return text[:-1] if text.endswith(".") else text


def _doc_page(doc_url):
    """Markdown source path under docs/ of a docs URL, e.g. "dashboard/device-map.md"."""
    rel = doc_url[len(PAGES_PREFIX):] if doc_url.startswith(PAGES_PREFIX) else doc_url
    page = rel.split("#", 1)[0]
    if page.endswith("/"):
        return page + "index.md"
    if page.endswith(".html"):
        return page[: -len(".html")] + ".md"
    return page


def _page_title(path):
    """title: aus dem Front Matter, sonst die erste "# "-Zeile, sonst None."""
    if not path.is_file():
        return None
    lines = path.read_text(encoding="utf-8").splitlines()
    start = 0
    if lines and lines[0].strip() == "---":
        for index in range(1, len(lines)):
            if lines[index].strip() == "---":
                for line in lines[1:index]:
                    match = re.match(r"^title:\s*(.*)$", line)
                    if match and match.group(1).strip().strip("\"'"):
                        return match.group(1).strip().strip("\"'")
                start = index + 1
                break
    for line in lines[start:]:
        if line.startswith("# "):
            return line[2:].strip()
    return None


def _doc(doc_url, docs_dir):
    rel = doc_url[len(PAGES_PREFIX):] if doc_url.startswith(PAGES_PREFIX) else doc_url
    page = _doc_page(doc_url)
    return {"url": rel, "title": _page_title(Path(docs_dir) / page) or page}


def _crop_style(crop, width, height):
    x, y, w, h = crop
    aspect = w * width / (h * height)
    px = 0 if w >= 1 else x / (1 - w) * 100
    py = 0 if h >= 1 else y / (1 - h) * 100
    return (f"aspect-ratio:{aspect:.3f};background-size:{100 / w:.2f}% auto;"
            f"background-position:{px:.2f}% {py:.2f}%")


def _image(image, image_sizes):
    name = image["url"].rsplit("/", 1)[-1]
    style = None
    if "crop" in image:
        style = _crop_style(image["crop"], *image_sizes[name])
    return {"name": name, "style": style}


def _nav_chain(nav, page):
    """Nav entries from the top level down to the entry for page, or []."""
    def walk(entries, trail):
        for entry in entries:
            if not isinstance(entry, dict):
                continue
            chain = trail + [entry]
            if entry.get("url") == page:
                return chain
            found = walk(entry.get("children", []), chain)
            if found:
                return found
        return []

    roots = []
    for section in nav:
        roots.extend(section.get("items", [section]))
    return walk(roots, [])


def _nav_label(nav, page):
    """Label for a docs page from the navigation, or None when it is not in it.

    Below "Dashboard" the entry directly under it names the area, otherwise the
    top entry of the section does.
    """
    chain = _nav_chain(nav, page)
    if not chain:
        return None
    for index, entry in enumerate(chain[:-1]):
        if entry.get("url") == DASHBOARD_PAGE:
            return chain[index + 1]["label"]
    return chain[0]["label"]


def _area(component_id, scope, page, nav, label):
    if component_id in INSTALLER_IDS:
        return "Installer"
    if page:
        found = _nav_label(nav, page)
        if found:
            return found
    if scope in SCOPE_AREAS:
        return SCOPE_AREAS[scope]
    return label


def _tour(entry):
    steps = (entry.get("tour") or {}).get("steps") or []
    return [{"title": step["title"]["en"], "text": step["text"]["en"]} for step in steps]


def _entry(component, source, since, target, translations, nav, docs_dir, image_sizes):
    pr = source.get("pr")
    details = source.get("details")
    text = image = doc = page = None
    if details:
        text = details["text"]["en"]
        if "image" in details:
            image = _image(details["image"], image_sizes)
        if "doc" in details:
            doc = _doc(details["doc"], docs_dir)
            page = _doc_page(details["doc"])
    tour = _tour(source)
    if image:
        kind = "image"
    elif details:
        kind = "text"
    elif tour:
        kind = "tour"
    else:
        kind = "title"
    # Der Text ist "en" aus changelog/highlights (build_document), sonst der
    # CHANGELOG-Text.
    title = _capitalised(source["text"])
    return {
        "pr": pr,
        "title": title,
        "components": [component["label"]],
        "area": _area(component["id"], source.get("scope"), page, nav, component["label"]),
        "kind": kind,
        "text": text,
        "image": image,
        "doc": doc,
        "tour": tour,
        "breaking": bool(source.get("breaking")),
        "since": since,
    }


def _release(tag, label, date, entries):
    cards = sorted((e for e in entries if e["kind"] != "title"),
                   key=lambda e: (KIND_ORDER[e["kind"]], -(e["pr"] or 0)))
    rest = sorted((e for e in entries if e["kind"] == "title"), key=lambda e: -(e["pr"] or 0))
    return {"tag": tag, "label": label, "date": date, "cards": cards, "rest": rest}


def build(document, tags, tag_versions, tag_dates, translations, nav, docs_dir, image_sizes):
    """The data for the What's new page, see the module docstring.

    tags: ascending "vX.Y.Z" tags, all of them (also those below MIN_TAG, they
    decide the release of an entry). document: build_document output over the
    selected components, each component may carry "target".
    """
    merged = {}
    ids = {}
    for component in document["components"]:
        component_id = component["id"]
        target = component.get("target", component_id)
        for release in component["releases"]:
            since = _since(component_id, release["version"], tags, tag_versions)
            if since != NEXT and compare_versions(since, MIN_TAG) < 0:
                continue
            for group in release["groups"]:
                for source in group["entries"]:
                    if not source.get("highlight"):
                        continue
                    pr = source.get("pr")
                    key = (since, pr) if pr is not None else object()
                    if key in merged:
                        if component["label"] not in merged[key]["components"]:
                            merged[key]["components"].append(component["label"])
                        ids[key].append(component_id)
                        continue
                    merged[key] = _entry(component, source, since, target, translations,
                                         nav, docs_dir, image_sizes)
                    ids[key] = [component_id]
    # An entry with any installer component is an Installer entry (P2 step 1),
    # even when its first component is not one of them.
    for key, entry in merged.items():
        if any(component_id in INSTALLER_IDS for component_id in ids[key]):
            entry["area"] = "Installer"
    entries = list(merged.values())

    releases = []
    by_since = {}
    for entry in entries:
        by_since.setdefault(entry["since"], []).append(entry)
    if NEXT in by_since:
        releases.append(_release(NEXT, NEXT_LABEL, NEXT_DATE, by_since[NEXT]))
    for tag in sorted((t for t in by_since if t != NEXT), key=_since_key, reverse=True):
        releases.append(_release(tag, tag, tag_dates.get(tag, ""), by_since[tag]))

    by_area = {}
    for entry in sorted(entries, key=lambda e: (_since_key(e["since"]), e["pr"] or 0), reverse=True):
        by_area.setdefault(entry["area"], []).append(entry)
    areas = [{"name": name, "entries": by_area[name]} for name in sorted(by_area)]
    return {"releases": releases, "areas": areas}


# --- navigation (docs/_data/nav.yml): a parser for the form used there ---

FLOW_PAIR = re.compile(r"(\w+):\s*(\"[^\"]*\"|'[^']*'|[^,}]+)")


def _scalar(text):
    text = text.strip()
    if len(text) >= 2 and text[0] == text[-1] and text[0] in "\"'":
        return text[1:-1]
    return text


def _flow(text):
    """{ label: "X", url: "Y" } as a dict."""
    return {key: _scalar(value) for key, value in FLOW_PAIR.findall(text.strip()[1:-1])}


def _lines(text):
    out = []
    for raw in text.splitlines():
        stripped = raw.strip()
        if stripped and not stripped.startswith("#"):
            out.append([len(raw) - len(raw.lstrip(" ")), stripped])
    return out


def _node(lines, pos, indent):
    if lines[pos][1].startswith("-"):
        return _list(lines, pos, indent)
    return _mapping(lines, pos, indent)


def _list(lines, pos, indent):
    items = []
    while pos < len(lines) and lines[pos][0] == indent and lines[pos][1].startswith("-"):
        rest = lines[pos][1][1:].strip()
        if rest.startswith("{"):
            items.append(_flow(rest))
            pos += 1
        elif re.match(r"^[\w ]+:", rest):
            lines[pos] = [indent + 2, rest]
            value, pos = _mapping(lines, pos, indent + 2)
            items.append(value)
        else:
            raise ValueError(f"nav.yml: unsupported list item {lines[pos][1]!r}")
    return items, pos


def _mapping(lines, pos, indent):
    result = {}
    while pos < len(lines) and lines[pos][0] == indent and not lines[pos][1].startswith("-"):
        key, _, rest = lines[pos][1].partition(":")
        rest = rest.strip()
        pos += 1
        if rest.startswith("{"):
            result[key.strip()] = _flow(rest)
        elif rest:
            result[key.strip()] = _scalar(rest)
        elif pos < len(lines) and lines[pos][0] > indent:
            result[key.strip()], pos = _node(lines, pos, lines[pos][0])
        else:
            result[key.strip()] = None
    return result, pos


def parse_nav(text):
    """docs/_data/nav.yml as a list of sections, each with "title" and "items"."""
    lines = _lines(text)
    if not lines:
        return []
    value, _ = _node(lines, 0, lines[0][0])
    return value


# --- command line: reads git and the repository ---

def _git(repo, *args):
    return subprocess.run(["git", "-C", str(repo), *args], capture_output=True, text=True)


def _release_tags(repo):
    found = [tag for tag in _git(repo, "tag", "-l", "v*").stdout.split() if TAG.match(tag)]
    return sorted(found, key=lambda tag: tuple(int(part) for part in tag[1:].split(".")))


def _tag_date(repo, tag):
    return _git(repo, "log", "-1", "--format=%ad", "--date=format:%-d %b %Y", tag).stdout.strip()


def _image_names(document):
    names = set()
    for component in document["components"]:
        for release in component["releases"]:
            for group in release["groups"]:
                for entry in group["entries"]:
                    details = entry.get("details") or {}
                    if "image" in details:
                        names.add(details["image"]["url"].rsplit("/", 1)[-1])
    return sorted(names)


def main(argv=None):
    argv = sys.argv[1:] if argv is None else argv
    repo = Path(argv[0] if argv else ".")
    table = json.loads((repo / "scripts/version/components.json").read_text(encoding="utf-8"))
    rows = [dict(row, bundle=True) for row in table["components"]
            if row.get("bundle") or row["id"] in EXTRA_COMPONENTS]
    selection = {"components": rows}
    translations = highlight_i18n.load_translations(repo / highlight_i18n.TRANSLATIONS_DIR)
    document = build_document(repo, selection, bundle_version="", max_releases=10_000,
                              generated_at="", image_ref="local")
    targets = {row["id"]: row.get("target", row["id"]) for row in rows}
    for component in document["components"]:
        component["target"] = targets[component["id"]]

    tags = _release_tags(repo)
    tag_versions = {tag: versions_at(repo, tag, selection) for tag in tags}
    tag_dates = {tag: _tag_date(repo, tag) for tag in tags}
    image_sizes = {}
    for name in _image_names(document):
        image_sizes[name] = highlight_details.png_size(repo / highlight_details.IMAGES_DIR / name)
    nav = parse_nav((repo / "docs/_data/nav.yml").read_text(encoding="utf-8"))
    result = build(document, tags, tag_versions, tag_dates, translations, nav,
                   repo / highlight_details.DOCS_DIR, image_sizes)
    print(json.dumps(result, indent=1, ensure_ascii=False))
    return 0


if __name__ == "__main__":
    sys.exit(main())
