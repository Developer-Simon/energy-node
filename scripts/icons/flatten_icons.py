#!/usr/bin/env python3
"""Erzeugt das Home-Assistant-Icon-Modul aus dem Geraete-Icon-Katalog.

Die Dashboard-Icons sind Strichzeichnungen (stroke, kein fill). Home Assistant
zeichnet ein Icon dagegen als EINEN gefuellten Pfad. Dieses Skript wickelt
deshalb jeden Strich zu seinem Umriss ab (Puffer um die halbe Strichbreite,
runde Enden und Ecken wie im Dashboard) und vereinigt alles zu einem Pfad.

Quelle:  integrations/homeassistant/icons.source.json
         (erzeugt von: cd dashboard && go run ./cmd/deviceicons)
Ziel:    integrations/homeassistant/custom_components/energy_node_icons/www/energy-node-icons.js
         docs/images/device-icons/<name>.svg  (je Icon, fuer die Doku-Tabellen)
         die Icon-Tabellen in DOC_TABLES (zwischen den icon-table-Markierungen)

Befehle:
    .venv/bin/python scripts/icons/flatten_icons.py           # Modul neu schreiben
    .venv/bin/python scripts/icons/flatten_icons.py --check   # Drift pruefen, Exit 1 bei Abweichung

Das Erzeugen braucht zwei Bibliotheken, die nur hier gebraucht werden:
    .venv/bin/pip install "svgelements==1.9.6" "shapely>=2.1"

--check kommt ohne sie aus (nur Standardbibliothek): es vergleicht die
Quell-Hashes, Labels und die Strichbreite im Modul mit icons.source.json und
prueft, dass SVG-Dateien und Doku-Tabellen zu den Pfaden im Modul passen. Deshalb
werden svgelements und shapely erst in outline() importiert - der Test in
integrations/homeassistant/tests laeuft in .venv-ha, wo sie fehlen.
"""
from __future__ import annotations

import argparse
import io
import json
import math
import re
import subprocess
import sys
from pathlib import Path

VIEW_BOX = "0 0 24 24"

# 0.15 Einheiten bei 24x24: der Umriss weicht bei den Groessen, in denen Home
# Assistant Icons zeichnet, um weniger als ein Zehntel Pixel ab.
SAMPLE_STEP = 0.15

HEADER = """\
// AUTOMATISCH ERZEUGT - nicht von Hand aendern.
// Quelle: dashboard/internal/webui/deviceicons.go
// Erneuern mit: .venv/bin/python scripts/icons/flatten_icons.py
"""

FOOTER = """\
const VIEW_BOX = "0 0 24 24";

// Home Assistant zeichnet ein Icon als einen gefuellten Pfad. Die Striche der
// Dashboard-Zeichnung sind deshalb schon zu Umrissen abgewickelt.
const getIcon = async (name) => {
  const icon = ICONS[name];
  if (icon) {
    return { path: icon.path, viewBox: VIEW_BOX };
  }
  // Unbekannter Name (Tippfehler, umbenanntes Icon): ha-icon liest .path ohne
  // Pruefung - also das Standard-Symbol statt undefined.
  return { path: ICONS["chip-outline"].path, viewBox: VIEW_BOX };
};

window.customIconsets = window.customIconsets || {};
window.customIconsets["energy-node"] = getIcon;

// Die neuere Schnittstelle: nur ueber getIconList taucht das Set auch in der
// Icon-Auswahl auf, statt nur beim direkten Eintippen zu funktionieren.
window.customIcons = window.customIcons || {};
window.customIcons["energy-node"] = {
  getIcon,
  getIconList: async () =>
    Object.entries(ICONS).map(([name, icon]) => ({ name, keywords: [icon.label] })),
};
"""

_ENTRY_RE = re.compile(r'^  "([^"]+)": \{\n    label: (.*),\n    sourceHash: "([0-9a-f]{64})",$', re.M)
_STROKE_RE = re.compile(r'^const SOURCE_STROKE_WIDTH = "([^"]+)";$', re.M)
_PATH_RE = re.compile(r'^  "([^"]+)": \{\n(?:    .*\n)*?    path: "([^"]+)",$', re.M)

# Die Doku zeigt jedes Icon so, wie Home Assistant es zeichnet: der gefuellte
# Umriss aus dem Modul, in einem Mittelblau, das auf hellem und dunklem
# Grund lesbar ist (ein <img> erbt keine currentColor).
ICON_SVG_DIR = "docs/images/device-icons"
ICON_SVG_FILL = "#1488c9"

# Englische Geraetebezeichnung je Icon fuer die (englische) Doku. Die Labels
# im Katalog sind deutsch; fehlt hier ein Icon, meldet --check Drift.
DESCRIPTIONS = {
    "chip-outline": "Generic device (fallback)",
    "solar-panel": "Solar panel",
    "current-ac": "Inverter",
    "power-plug": "Smart plug",
    "meter-electric": "3-phase energy meter",
    "pipe-valve": "Heating-pipe valve",
    "raspberry-pi": "Raspberry Pi",
    "home-battery": "Battery",
    "battery-charging": "Charger",
    "thermometer": "Thermometer",
    "power-socket-de": "Flush-mounted socket",
    "gas-burner": "Heating (oil/gas)",
    "water-boiler": "Water boiler",
    "ev-station": "Wallbox (EV charger)",
    "transmission-tower": "Power grid",
    "sitemap": "Automations",
    "home": "Building",
    "heat-pump": "Heat pump",
    "flash-circle": "Consumption",
}

# Dateien mit einer generierten Icon-Tabelle und wie sie die SVGs erreichen:
# die Doku-Seite relativ, der HACS-Mirror ueber seine eigene raw-URL
# (publish_mirror.sh kopiert ICON_SVG_DIR dorthin nach docs/icons/).
MIRROR_ICON_URL = "https://raw.githubusercontent.com/Developer-Simon/ha-energy-node-icons/main/docs/icons/{}.svg"
DOC_TABLES = {
    "docs/integration/energy-node-icons.md": "../images/device-icons/{}.svg",
    "integrations/homeassistant/mirror/energy_node_icons/README.md": MIRROR_ICON_URL,
    "integrations/homeassistant/mirror/energy_node_icons/docs/integration.md": MIRROR_ICON_URL,
}
TABLE_START = "<!-- icon-table:start (generated by scripts/icons/flatten_icons.py) -->"
TABLE_END = "<!-- icon-table:end -->"


def repo_root() -> Path:
    out = subprocess.check_output(["git", "rev-parse", "--show-toplevel"], cwd=Path(__file__).parent)
    return Path(out.decode().strip())


def source_path() -> Path:
    return repo_root() / "integrations/homeassistant/icons.source.json"


def module_path() -> Path:
    return repo_root() / "integrations/homeassistant/custom_components/energy_node_icons/www/energy-node-icons.js"


def read_source() -> dict:
    return json.loads(source_path().read_text(encoding="utf-8"))


def check() -> int:
    """Vergleicht das Modul mit der Quelle; 0 = aktuell, 1 = veraltet."""
    doc = read_source()
    try:
        module = module_path().read_text(encoding="utf-8")
    except FileNotFoundError:
        print(f"{module_path()} fehlt - flatten_icons.py laufen lassen", file=sys.stderr)
        return 1

    problems = []
    stroke = _STROKE_RE.search(module)
    if not stroke or stroke.group(1) != doc["stroke_width"]:
        problems.append(
            f"Strichbreite: Modul {stroke.group(1) if stroke else 'fehlt'}, Quelle {doc['stroke_width']}"
        )
    entries = _ENTRY_RE.findall(module)
    have = {name: digest for name, _label, digest in entries}
    have_labels = {name: json.loads(label) for name, label, _digest in entries}
    want = {icon["ha_name"]: icon["sha256"] for icon in doc["icons"]}
    want_labels = {icon["ha_name"]: icon["label"] for icon in doc["icons"]}
    for name in sorted(want.keys() - have.keys()):
        problems.append(f"{name}: fehlt im Modul")
    for name in sorted(have.keys() - want.keys()):
        problems.append(f"{name}: nicht mehr im Katalog")
    for name in sorted(want.keys() & have.keys()):
        if want[name] != have[name]:
            problems.append(f"{name}: Zeichnung geaendert")
        # Das Label ist das Suchwort in der Icon-Auswahl - eine reine
        # Umbenennung muss deshalb genauso auffallen wie eine neue Zeichnung.
        if want_labels[name] != have_labels[name]:
            problems.append(f"{name}: Label geaendert ({have_labels[name]!r} -> {want_labels[name]!r})")
    for name in sorted(want.keys() - DESCRIPTIONS.keys()):
        problems.append(f"{name}: keine englische Beschreibung in DESCRIPTIONS")
    paths = dict(_PATH_RE.findall(module))
    if not problems:
        for rel, expected in doc_artifacts(doc, paths).items():
            target = repo_root() / rel
            if not target.is_file() or target.read_text(encoding="utf-8") != expected:
                problems.append(f"{rel}: passt nicht zum Modul")
        svg_dir = repo_root() / ICON_SVG_DIR
        extra = {f.stem for f in svg_dir.glob("*.svg")} - want.keys() if svg_dir.is_dir() else set()
        for name in sorted(extra):
            problems.append(f"{ICON_SVG_DIR}/{name}.svg: nicht mehr im Katalog")
    if list(have) != [n for n in want if n in have]:
        problems.append("Reihenfolge weicht vom Katalog ab")

    for problem in problems:
        print(problem, file=sys.stderr)
    if problems:
        print("energy-node-icons.js ist veraltet - flatten_icons.py neu laufen lassen", file=sys.stderr)
        return 1
    return 0


def outline(markup: str, stroke_width: float) -> str:
    """Ein gefuellter Pfad, der alles abdeckt, was die Strichzeichnung malt."""
    from shapely.geometry import LineString
    from shapely.ops import unary_union
    from svgelements import SVG, Path as SvgPath, Shape

    document = (
        f'<svg xmlns="http://www.w3.org/2000/svg" viewBox="{VIEW_BOX}" fill="none" '
        f'stroke="#000" stroke-width="{stroke_width}" stroke-linecap="round" '
        f'stroke-linejoin="round">{markup}</svg>'
    )
    # reify=True rechnet jede Transformation ein (mdi:power-plug ist eine
    # gedrehte Gruppe) und macht aus rect/circle/ellipse Pfadgeometrie - die
    # Schleife unten sieht also nur noch Kurven.
    svg = SVG.parse(io.StringIO(document), reify=True)
    pieces = []
    for element in svg.elements():
        if not isinstance(element, Shape):
            continue
        for subpath in SvgPath(element).as_subpaths():
            points = sample(SvgPath(subpath))
            if not points:
                continue
            if len(points) == 1:
                points = points * 2  # die "h.01"-Punkte: ein rundes Ende auf einem Punkt
            pieces.append(
                LineString(points).buffer(stroke_width / 2, cap_style="round", join_style="round", quad_segs=12)
            )
    return to_path_data(unary_union(pieces))


def sample(path) -> list[tuple[float, float]]:
    """Tastet jedes Segment ausser Move in Schritten von hoechstens SAMPLE_STEP ab."""
    from svgelements import Move

    points: list[tuple[float, float]] = []

    def add(point) -> None:
        xy = (float(point.x), float(point.y))
        if not points or math.dist(points[-1], xy) > 1e-9:
            points.append(xy)

    for segment in path.segments():
        if isinstance(segment, Move):
            if segment.end is not None:
                add(segment.end)
            continue
        if segment.start is None or segment.end is None:
            continue
        steps = max(2, math.ceil(segment.length() / SAMPLE_STEP))
        for i in range(steps + 1):
            add(segment.point(i / steps))
    return points


def to_path_data(geometry) -> str:
    from shapely.geometry import MultiPolygon, Polygon
    from shapely.geometry.polygon import orient

    if isinstance(geometry, Polygon):
        polygons = [geometry]
    elif isinstance(geometry, MultiPolygon):
        polygons = list(geometry.geoms)
    else:
        raise ValueError(f"unerwartete Geometrie {geometry.geom_type}")

    def ring(coords) -> str:
        pts = [f"{x:.2f} {y:.2f}" for x, y in list(coords)[:-1]]
        return "M" + pts[0] + "L" + "L".join(pts[1:]) + "Z"

    parts = []
    for polygon in polygons:
        # orient() windet Loecher gegen ihren Aussenrand - die nonzero-Fuellregel
        # des Browsers stanzt sie dann aus, statt sie mitzufuellen.
        polygon = orient(polygon.simplify(0.01, preserve_topology=True), sign=1.0)
        parts.append(ring(polygon.exterior.coords))
        parts.extend(ring(interior.coords) for interior in polygon.interiors)
    return "".join(parts)


def icon_svg(path_data: str) -> str:
    return (
        f'<svg xmlns="http://www.w3.org/2000/svg" viewBox="{VIEW_BOX}" width="48" height="48">'
        f'<path fill="{ICON_SVG_FILL}" d="{path_data}"/></svg>\n'
    )


def icon_table(doc: dict, url: str) -> str:
    rows = ["| Icon | Name | Device type |", "|:---:|---|---|"]
    for icon in doc["icons"]:
        name = icon["ha_name"]
        rows.append(
            f'| <img src="{url.format(name)}" width="32" height="32" alt="{name}"> '
            f"| `energy-node:{name}` | {DESCRIPTIONS[name]} |"
        )
    return "\n".join(rows)


def splice_table(text: str, table: str, rel: str) -> str:
    start, end = text.find(TABLE_START), text.find(TABLE_END)
    if start < 0 or end < start:
        raise SystemExit(f"{rel}: Markierungen {TABLE_START!r} / {TABLE_END!r} fehlen")
    return text[: start + len(TABLE_START)] + "\n" + table + "\n" + text[end:]


def doc_artifacts(doc: dict, paths: dict[str, str]) -> dict[str, str]:
    """Alle Doku-Dateien, die aus den Modul-Pfaden folgen: SVGs und Tabellen."""
    out = {f"{ICON_SVG_DIR}/{name}.svg": icon_svg(d) for name, d in paths.items()}
    for rel, url in DOC_TABLES.items():
        current = (repo_root() / rel).read_text(encoding="utf-8")
        out[rel] = splice_table(current, icon_table(doc, url), rel)
    return out


def render_module(doc: dict) -> str:
    stroke_width = float(doc["stroke_width"])
    lines = [HEADER, f'const SOURCE_STROKE_WIDTH = {json.dumps(doc["stroke_width"])};', "", "const ICONS = {"]
    for icon in doc["icons"]:
        lines += [
            f'  {json.dumps(icon["ha_name"])}: {{',
            f'    label: {json.dumps(icon["label"], ensure_ascii=False)},',
            f'    sourceHash: "{icon["sha256"]}",',
            f'    path: "{outline(icon["markup"], stroke_width)}",',
            "  },",
        ]
    lines += ["};", "", FOOTER]
    return "\n".join(lines)


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument("--check", action="store_true", help="nur Drift pruefen, nichts schreiben")
    args = parser.parse_args()
    if args.check:
        return check()
    target = module_path()
    target.parent.mkdir(parents=True, exist_ok=True)
    doc = read_source()
    module = render_module(doc)
    target.write_text(module, encoding="utf-8")
    print(f"geschrieben: {target.relative_to(repo_root())}")

    svg_dir = repo_root() / ICON_SVG_DIR
    svg_dir.mkdir(parents=True, exist_ok=True)
    for stale in svg_dir.glob("*.svg"):
        stale.unlink()
    for rel, content in doc_artifacts(doc, dict(_PATH_RE.findall(module))).items():
        (repo_root() / rel).write_text(content, encoding="utf-8")
    print(f"geschrieben: {ICON_SVG_DIR}/*.svg und {len(DOC_TABLES)} Icon-Tabellen")
    return 0


if __name__ == "__main__":
    sys.exit(main())
