#!/usr/bin/env python3
"""Vorschau der Highlights mit Details, zum Besprechen der Bilder.

    scripts/dev/highlight_details_preview.py --repo . --since v0.9.8 \\
        [--out drafts/highlight-preview/index.html]

Zeigt alle Highlights der Komponente dashboard aus den Changelog-Abschnitten,
die neuer sind als --since. Je Eintrag steht die Dialog-Ansicht (wie in den
Neuerungen) neben dem ganzen Bild. Ein Rahmen im Bild zeigt den Ausschnitt; er
laesst sich mit der Maus neu aufziehen, darunter steht die fertige crop-Zeile
zum Kopieren. Bilder kommen relativ aus docs/images, nicht von GitHub.

Highlights ohne Details sind als "ohne Erklaerung" markiert. Fehler aus
highlight_details.validate stehen oben auf der Seite.
"""
import argparse
import html
import os
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "build"))
import changelog_history
import highlight_details
import highlight_i18n
import make_changelog_json

COMPONENT = "dashboard"
CHANGELOG = Path("dashboard/CHANGELOG.md")
DEFAULT_OUT = Path("drafts/highlight-preview/index.html")
FULL_CROP = [0, 0, 1, 1]

CSS = """
:root { --bg: #111418; --panel: #171a20; --panel-alt: #1c2027; --sunken: #12151a;
  --border: #46515d; --border-soft: #333333; --text-strong: #f0f3f6; --text: #e6e6e6;
  --text-subtle: #b8c7d9; --text-muted: #8b949e; --accent: #9fd; --accent-soft: #9fe5d0;
  --accent-bg: #18321f; --accent-ink: #0f1216; --flag: #f5b54a; --radius-sm: 8px;
  --radius-md: 12px; --radius-lg: 16px; --layout-ease: cubic-bezier(.32, .72, 0, 1);
  color-scheme: dark; }
:root[data-theme="light"] { --bg: #f3f1ec; --panel: #ffffff; --panel-alt: #eceae4;
  --sunken: #e6e3dc; --border: #c3c9cf; --border-soft: #e0e3e6; --text-strong: #0d1116;
  --text: #1b2027; --text-subtle: #3f4750; --text-muted: #5b6570; --accent: #0f9e7a;
  --accent-soft: #0b6b53; --accent-bg: #e2f4ee; --accent-ink: #fff; --flag: #9a5b00;
  color-scheme: light; }
* { box-sizing: border-box; }
body { margin: 0; min-height: 100vh; background: var(--bg); color: var(--text); font: 15px/1.5 system-ui, sans-serif; }
button, input { font: inherit; }
.pv-page { max-width: 72rem; margin: 0 auto; padding: 1rem 16px 3rem; display: grid; gap: 1.5rem; }
.pv-head { display: flex; flex-wrap: wrap; gap: .75rem; align-items: baseline; justify-content: space-between; }
.pv-head h1 { margin: 0; font-size: 1.2rem; color: var(--text-strong); }
.pv-theme { padding: .35rem .8rem; border: 1px solid var(--border); border-radius: 999px; background: var(--panel); color: var(--text-subtle); cursor: pointer; font-size: .82rem; }
.pv-errors { padding: .8rem 1rem; border: 1px solid var(--flag); border-radius: var(--radius-md); background: var(--panel-alt); }
.pv-errors h2 { margin: 0 0 .4rem; font-size: .95rem; color: var(--flag); }
.pv-errors ul { margin: 0; padding-left: 1.2rem; font-size: .88rem; }
.pv-ok { color: var(--text-muted); font-size: .85rem; margin: 0; }
.pv-release { display: grid; gap: .6rem; }
.pv-release > h2 { margin: 0; font-size: 1rem; color: var(--text-strong); }
.pv-item { display: grid; grid-template-columns: minmax(0, 40rem) minmax(0, 1fr); gap: 1rem; align-items: start; }
@media (max-width: 760px) { .pv-item { grid-template-columns: minmax(0, 1fr); } }
.pv-dialog { padding: .8rem .9rem; border: 1px solid var(--border-soft); border-left: 3px solid var(--accent); border-radius: var(--radius-md); background: var(--panel); }
.pv-editor { display: grid; gap: .5rem; padding: .8rem .9rem; border: 1px solid var(--border-soft); border-radius: var(--radius-md); background: var(--panel-alt); }
.pv-editor-label { font-size: .72rem; font-weight: 600; letter-spacing: .07em; text-transform: uppercase; color: var(--text-muted); }
.pv-full { position: relative; display: block; user-select: none; touch-action: none; cursor: crosshair; }
.pv-full img { display: block; width: 100%; height: auto; border-radius: var(--radius-sm); }
.pv-frame { position: absolute; border: 2px solid var(--accent); background: color-mix(in srgb, var(--accent) 14%, transparent); box-shadow: 0 0 0 9999px color-mix(in srgb, #000 35%, transparent); pointer-events: none; }
.pv-crop-line { width: 100%; padding: .4rem .55rem; border: 1px solid var(--border); border-radius: var(--radius-sm); background: var(--sunken); color: var(--text); font-family: ui-monospace, monospace; font-size: .8rem; }
.pv-flag { display: inline-block; margin-left: .3rem; padding: .05rem .45rem; border-radius: 999px; border: 1px solid var(--flag); color: var(--flag); font-size: .72rem; white-space: nowrap; }
.wn-entries { margin: 0; padding: 0; list-style: none; display: grid; gap: .1rem; font-size: .88rem; line-height: 1.5; }
.wn-plain { position: relative; padding: .2rem .5rem .2rem 1.1rem; }
.wn-plain::before, .wn-summary::before { content: ""; position: absolute; left: .35rem; top: .78em; width: 5px; height: 5px; border-radius: 50%; background: var(--text-muted); }
.wn-pr { color: var(--text-muted); font-variant-numeric: tabular-nums; }
.wn-detail { border-radius: var(--radius-sm); }
.wn-detail[open] { background: color-mix(in srgb, var(--accent) 6%, transparent); }
.wn-summary { position: relative; display: grid; grid-template-columns: minmax(0, 1fr) auto; align-items: start; gap: .6rem; padding: .2rem .4rem .2rem 1.1rem; border-radius: var(--radius-sm); cursor: pointer; list-style: none; }
.wn-summary::-webkit-details-marker { display: none; }
.wn-summary::before { background: var(--accent); }
.wn-summary:hover { background: color-mix(in srgb, var(--text) 5%, transparent); }
.wn-summary:focus-visible { outline: 2px solid var(--accent); outline-offset: 1px; }
.wn-more { display: inline-flex; align-items: center; gap: .2rem; margin-top: .1rem; color: var(--accent-soft); font-size: .76rem; font-weight: 600; white-space: nowrap; }
.wn-chevron { width: 14px; height: 14px; fill: none; stroke: currentColor; stroke-width: 2; stroke-linecap: round; stroke-linejoin: round; transition: transform .32s var(--layout-ease); }
.wn-detail[open] .wn-chevron { transform: rotate(90deg); }
.wn-more-open { display: none; }
.wn-detail[open] .wn-more-open { display: inline; }
.wn-detail[open] .wn-more-closed { display: none; }
.wn-body { display: grid; gap: .6rem; padding: .25rem .6rem .7rem 1.1rem; }
.wn-text { margin: 0; color: var(--text-subtle); font-size: .86rem; line-height: 1.55; }
.wn-shot { display: block; position: relative; aspect-ratio: 16 / 10; border: 1px solid var(--border-soft); border-radius: var(--radius-sm); background: var(--sunken); overflow: hidden; }
.wn-shot img { position: absolute; display: block; max-width: none; }
.wn-figure { display: grid; gap: .35rem; margin: 0; }
.wn-crop-toggle { justify-self: end; display: inline-flex; align-items: center; gap: .3rem; padding: .2rem .55rem; border: 1px solid var(--border-soft); border-radius: 999px; background: var(--panel); color: var(--text-muted); font-size: .74rem; cursor: pointer; }
.wn-crop-toggle:hover { color: var(--text); }
.wn-crop-toggle svg { width: 12px; height: 12px; fill: none; stroke: currentColor; stroke-width: 2; stroke-linecap: round; stroke-linejoin: round; }
.wn-crop-part, .wn-crop-toggle[aria-pressed="true"] .wn-crop-full { display: none; }
.wn-crop-toggle[aria-pressed="true"] .wn-crop-part { display: inline; }
.wn-link { justify-self: start; color: var(--accent-soft); font-size: .82rem; font-weight: 500; text-decoration: none; }
.wn-link:hover { color: var(--accent); text-decoration: underline; }
"""

SCRIPT = """
(() => {
  const clamp = (v) => Math.min(1, Math.max(0, v));
  const round = (v) => Math.round(v * 1000) / 1000;
  // Crop x, y, width, height as fractions of the image; the frame gets the exact aspect ratio
  const applyFrame = (shot, useCrop) => {
    const [W, H] = (shot.dataset.size || '16,10').split(',').map(Number);
    const crop = useCrop && shot.dataset.crop ? shot.dataset.crop.split(',').map(Number) : [0, 0, 1, 1];
    const [x, y, w, h] = crop;
    shot.style.aspectRatio = `${w * W} / ${h * H}`;
    Object.assign(shot.querySelector('img').style, {width: `${100 / w}%`, height: `${100 / h}%`, left: `${-x / w * 100}%`, top: `${-y / h * 100}%`});
  };
  document.querySelectorAll('.wn-shot').forEach((s) => applyFrame(s, true));
  document.querySelectorAll('.wn-crop-toggle').forEach((btn) => btn.addEventListener('click', () => {
    const full = btn.getAttribute('aria-pressed') !== 'true';
    btn.setAttribute('aria-pressed', String(full));
    applyFrame(btn.closest('.pv-item').querySelector('.wn-shot'), !full);
  }));
  document.getElementById('pv-theme').addEventListener('click', (e) => {
    const light = document.documentElement.dataset.theme !== 'light';
    document.documentElement.dataset.theme = light ? 'light' : 'dark';
    e.currentTarget.textContent = light ? 'Dunkel' : 'Hell';
  });
  // Drawing a frame: press, drag, release. The dialog view follows the frame.
  document.querySelectorAll('.pv-editor').forEach((editor) => {
    const item = editor.closest('.pv-item');
    const shot = item.querySelector('.wn-shot');
    const full = editor.querySelector('.pv-full');
    const frame = editor.querySelector('.pv-frame');
    const line = editor.querySelector('.pv-crop-line');
    let crop = shot.dataset.crop.split(',').map(Number);
    let start = null;
    const show = () => {
      const [x, y, w, h] = crop;
      Object.assign(frame.style, {left: `${x * 100}%`, top: `${y * 100}%`, width: `${w * 100}%`, height: `${h * 100}%`});
      const rounded = crop.map(round);
      line.value = `"crop": [${rounded.join(', ')}]`;
      shot.dataset.crop = rounded.join(',');
      applyFrame(shot, true);
    };
    const point = (e) => {
      const r = full.getBoundingClientRect();
      return [clamp((e.clientX - r.left) / r.width), clamp((e.clientY - r.top) / r.height)];
    };
    full.addEventListener('pointerdown', (e) => {
      if (e.button !== 0) return;
      full.setPointerCapture(e.pointerId);
      start = point(e);
      e.preventDefault();
    });
    full.addEventListener('pointermove', (e) => {
      if (!start) return;
      const [px, py] = point(e);
      const [sx, sy] = start;
      const next = [Math.min(sx, px), Math.min(sy, py), Math.abs(px - sx), Math.abs(py - sy)];
      // A frame under 1 percent in either direction is no crop, the last one stays
      if (next[2] < 0.01 || next[3] < 0.01) return;
      crop = next;
      show();
    });
    const stop = () => { start = null; };
    full.addEventListener('pointerup', stop);
    full.addEventListener('pointercancel', stop);
    line.addEventListener('focus', () => line.select());
    show();
  });
})();
"""

PAGE = """<!doctype html>
<html lang="de" data-theme="dark">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Highlight-Vorschau</title>
<style>
@@CSS@@
</style>
</head>
<body>
<main class="pv-page">
<header class="pv-head">
  <h1>Highlights mit Details: @@COMPONENT@@ seit @@SINCE@@</h1>
  <button type="button" class="pv-theme" id="pv-theme">Hell</button>
</header>
@@ERRORS@@
@@BODY@@
</main>
<script>
@@SCRIPT@@
</script>
</body>
</html>
"""

CROP_ICON = ('<svg viewBox="0 0 24 24"><path d="M4 9V4h5M20 9V4h-5M4 15v5h5M20 15v5h-5"/></svg>')
LINK_ICON = ('<svg viewBox="0 0 24 24"><path d="M7 17 17 7M8 7h9v9"/></svg>')
CHEVRON = ('<svg class="wn-chevron" viewBox="0 0 24 24"><path d="m9 6 6 6-6 6"/></svg>')


def _round(value):
    return round(float(value), 3)


def _crop_text(crop):
    """Ausschnitt als x,y,Breite,Höhe für data-crop, auf 3 Nachkommastellen."""
    return ",".join(f"{_round(v):g}" for v in crop)


def _crop_line(crop):
    """Die fertige Zeile für die Highlight-Datei."""
    return '"crop": [' + ", ".join(f"{_round(v):g}" for v in crop) + "]"


def _releases(repo, since):
    text = (Path(repo) / CHANGELOG).read_text(encoding="utf-8")
    releases = make_changelog_json.mark_highlights(make_changelog_json.parse_changelog(text))
    return [r for r in releases if changelog_history.compare_versions(r["version"], since) > 0]


def _image(repo, out_dir, details):
    """Bild samt Größe und relativem Pfad ab out_dir, None ohne (gültiges) Bild."""
    name = details.get("image")
    if not name:
        return None
    path = Path(repo) / highlight_details.IMAGES_DIR / name
    try:
        width, height = highlight_details.png_size(path)
    except (OSError, ValueError):
        return None
    return {
        "src": os.path.relpath(path, out_dir).replace(os.sep, "/"),
        "name": name,
        "width": width,
        "height": height,
        "crop": details.get("crop") or FULL_CROP,
    }


def _highlight(repo, out_dir, translations, entry):
    pr = entry.get("pr")
    details = highlight_details.lookup(translations, pr, COMPONENT)
    title = highlight_i18n.lookup(translations, pr, COMPONENT, "de") or entry["text"]
    item = {"pr": pr, "title": title, "details": details, "image": None}
    if details is not None:
        item["image"] = _image(repo, out_dir, details)
    return item


def _figure(image):
    alt = html.escape(image["name"])
    return (
        '<figure class="wn-figure">'
        f'<span class="wn-shot" data-size="{image["width"]},{image["height"]}" '
        f'data-crop="{_crop_text(image["crop"])}"><img src="{html.escape(image["src"])}" alt="{alt}"></span>'
        '<button type="button" class="wn-crop-toggle" aria-pressed="false">'
        f'{CROP_ICON}<span class="wn-crop-full">Ganzes Bild</span><span class="wn-crop-part">Ausschnitt</span>'
        '</button></figure>'
    )


def _editor(image):
    return (
        '<div class="pv-editor"><span class="pv-editor-label">Ganzes Bild, Rahmen ziehen</span>'
        f'<div class="pv-full"><img src="{html.escape(image["src"])}" alt="">'
        '<div class="pv-frame"></div></div>'
        f'<input class="pv-crop-line" readonly value="{html.escape(_crop_line(image["crop"]))}">'
        '</div>'
    )


def _dialog(item):
    pr = f' <span class="wn-pr">(#{item["pr"]})</span>' if item["pr"] is not None else ""
    title = html.escape(item["title"])
    details = item["details"]
    if details is None:
        return (
            '<div class="pv-dialog"><ul class="wn-entries"><li class="wn-plain">'
            f'{title}{pr} <span class="pv-flag">ohne Erklärung</span></li></ul></div>'
        )
    body = [f'<p class="wn-text">{html.escape(details.get("de", ""))}</p>']
    if item["image"] is not None:
        body.append(_figure(item["image"]))
    if "doc" in details:
        body.append(
            f'<a class="wn-link" href="{html.escape(highlight_details.PAGES_BASE + details["doc"])}" '
            f'target="_blank" rel="noopener">In der Anleitung ansehen {LINK_ICON}</a>'
        )
    return (
        '<div class="pv-dialog"><ul class="wn-entries"><li>'
        '<details class="wn-detail" name="wn">'
        f'<summary class="wn-summary"><span>{title}{pr}</span>'
        '<span class="wn-more"><span class="wn-more-closed">Mehr</span><span class="wn-more-open">Weniger</span>'
        f'{CHEVRON}</span></summary>'
        f'<div class="wn-body">{"".join(body)}</div>'
        '</details></li></ul></div>'
    )


def _item(repo, out_dir, translations, entry):
    item = _highlight(repo, out_dir, translations, entry)
    editor = _editor(item["image"]) if item["image"] is not None else ""
    return f'<div class="pv-item">{_dialog(item)}{editor}</div>'


def _body(repo, out_dir, translations, releases, since):
    if not releases:
        return f'<p class="pv-ok">Keine Abschnitte neuer als {html.escape(since)}.</p>'
    sections = []
    for release in releases:
        items = [
            _item(repo, out_dir, translations, entry)
            for group in release["groups"]
            for entry in group["entries"]
            if entry.get("highlight")
        ]
        if not items:
            continue
        sections.append(
            f'<section class="pv-release"><h2>{html.escape(COMPONENT)} {html.escape(release["version"])}</h2>'
            + "".join(items) + "</section>"
        )
    return "".join(sections) or f'<p class="pv-ok">Keine Highlights seit {html.escape(since)}.</p>'


def _errors(problems):
    if not problems:
        return '<p class="pv-ok">Keine Fehler in den Details.</p>'
    rows = "".join(f"<li>{html.escape(p)}</li>" for p in problems)
    return f'<section class="pv-errors"><h2>Fehler aus validate</h2><ul>{rows}</ul></section>'


def render(repo, out_dir, since):
    repo = Path(repo)
    translations = highlight_i18n.load_translations(repo / highlight_i18n.TRANSLATIONS_DIR)
    problems = highlight_details.validate(repo, translations)
    releases = _releases(repo, since)
    page = PAGE
    for marker, value in (
        ("@@CSS@@", CSS),
        ("@@COMPONENT@@", html.escape(COMPONENT)),
        ("@@SINCE@@", html.escape(since)),
        ("@@ERRORS@@", _errors(problems)),
        ("@@BODY@@", _body(repo, out_dir, translations, releases, since)),
        ("@@SCRIPT@@", SCRIPT),
    ):
        page = page.replace(marker, value)
    return page


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__.split("\n")[0])
    parser.add_argument("--repo", required=True)
    parser.add_argument("--since", required=True, help="Tag, ab dem die Abschnitte gezeigt werden, z. B. v0.9.8")
    parser.add_argument("--out", help="Ziel der HTML-Seite (Standard: drafts/highlight-preview/index.html im Repo)")
    args = parser.parse_args(argv)
    repo = Path(args.repo)
    out = Path(args.out) if args.out else repo / DEFAULT_OUT
    out_dir = out.resolve().parent
    out_dir.mkdir(parents=True, exist_ok=True)
    out.write_text(render(repo, out_dir, args.since), encoding="utf-8")
    print(f"highlight preview: {out}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
