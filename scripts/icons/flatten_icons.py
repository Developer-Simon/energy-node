#!/usr/bin/env python3
"""Erzeugt das Home-Assistant-Icon-Modul aus dem Geraete-Icon-Katalog.

Die Dashboard-Icons sind Strichzeichnungen (stroke, kein fill). Home Assistant
zeichnet ein Icon dagegen als EINEN gefuellten Pfad. Dieses Skript wickelt
deshalb jeden Strich zu seinem Umriss ab (Puffer um die halbe Strichbreite,
runde Enden und Ecken wie im Dashboard) und vereinigt alles zu einem Pfad.

Quelle:  integrations/homeassistant/icons.source.json
         (erzeugt von: cd dashboard && go run ./cmd/deviceicons)
Ziel:    integrations/homeassistant/custom_components/energy_node_icons/www/energy-node-icons.js

Befehle:
    .venv/bin/python scripts/icons/flatten_icons.py           # Modul neu schreiben
    .venv/bin/python scripts/icons/flatten_icons.py --check   # Drift pruefen, Exit 1 bei Abweichung

Das Erzeugen braucht zwei Bibliotheken, die nur hier gebraucht werden:
    .venv/bin/pip install "svgelements==1.9.6" "shapely>=2.1"

--check kommt ohne sie aus (nur Standardbibliothek): es vergleicht die
Quell-Hashes, Labels und die Strichbreite im Modul mit icons.source.json. Deshalb
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
    target.write_text(render_module(read_source()), encoding="utf-8")
    print(f"geschrieben: {target.relative_to(repo_root())}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
