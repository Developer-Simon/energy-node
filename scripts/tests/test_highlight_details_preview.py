import struct
import sys
import zlib
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "dev"))
import highlight_details_preview as pv


def make_png(path, width, height):
    """Minimales PNG mit IHDR und IEND, reicht fuer png_size."""
    def chunk(kind, data):
        body = kind + data
        return struct.pack(">I", len(data)) + body + struct.pack(">I", zlib.crc32(body))
    ihdr = struct.pack(">IIBBBBB", width, height, 8, 2, 0, 0, 0)
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_bytes(b"\x89PNG\r\n\x1a\n" + chunk(b"IHDR", ihdr) + chunk(b"IEND", b""))


CHANGELOG = """# Changelog

## v0.9.9 (2026-10-18)

### Features

- **dashboard:** switches get a role (#146)
- **dashboard:** sub devices (#148)

## v0.9.8 (2026-10-12)

### Features

- **dashboard:** an older thing (#120)
"""


def test_preview_lists_highlights_and_marks_missing_details(tmp_path):
    repo = tmp_path
    make_png(repo / "docs" / "images" / "shot.png", 1046, 653)
    (repo / "dashboard").mkdir()
    (repo / "dashboard" / "CHANGELOG.md").write_text(CHANGELOG, encoding="utf-8")
    translations_dir = repo / "changelog" / "highlights"
    translations_dir.mkdir(parents=True)
    (translations_dir / "146.json").write_text(
        '{"de": {"*": "Schalter als Rolle"},'
        ' "details": {"*": {"en": "Switch role", "de": "Schalter sind eine Rolle.",'
        ' "image": "shot.png", "crop": [0.5, 0.2, 0.5, 0.3]}}}',
        encoding="utf-8",
    )
    (translations_dir / "148.json").write_text(
        '{"de": {"*": "Untergeraete"}}', encoding="utf-8",
    )
    out = repo / "drafts" / "highlight-preview" / "index.html"

    assert pv.main(["--repo", str(repo), "--since", "v0.9.8", "--out", str(out)]) == 0

    html = out.read_text(encoding="utf-8")
    assert "Schalter als Rolle" in html
    assert "Untergeraete" in html
    assert "an older thing" not in html  # v0.9.8 is the start, not part of the range
    assert html.count("ohne Erklärung") == 1
    assert 'src="../../docs/images/shot.png"' in html
    assert 'data-crop="0.5,0.2,0.5,0.3"' in html
