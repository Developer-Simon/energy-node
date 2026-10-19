import struct
import subprocess
import sys
import zlib
from pathlib import Path

import pytest

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "build"))
import highlight_details as hd
import highlight_i18n as hi


def make_png(path, width, height):
    """Minimales PNG mit IHDR und IEND, reicht fuer png_size."""
    def chunk(kind, data):
        body = kind + data
        return struct.pack(">I", len(data)) + body + struct.pack(">I", zlib.crc32(body))
    ihdr = struct.pack(">IIBBBBB", width, height, 8, 2, 0, 0, 0)
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_bytes(b"\x89PNG\r\n\x1a\n" + chunk(b"IHDR", ihdr) + chunk(b"IEND", b""))


@pytest.fixture
def repo(tmp_path):
    make_png(tmp_path / "docs" / "images" / "shot.png", 40, 25)
    (tmp_path / "docs" / "dashboard").mkdir(parents=True)
    (tmp_path / "docs" / "dashboard" / "device-map.md").write_text("# Geraetekarte\n", encoding="utf-8")
    return tmp_path


FULL = {
    "en": "Switch channels",
    "de": "Schaltkanäle",
    "image": "shot.png",
    "crop": [0.5, 0.2, 0.5, 0.3],
    "doc": "dashboard/device-map.html#switches",
}


def test_resolve_builds_pinned_image_url_size_and_doc_link(repo):
    translations = {148: {"details": {"*": FULL}}}
    result = hd.resolve(repo, translations, 148, "service:shelly", image_ref="v0.10.0")
    assert result["text"] == {"en": "Switch channels", "de": "Schaltkanäle"}
    assert result["image"] == {
        "url": hd.RAW_BASE + "v0.10.0/docs/images/shot.png",
        "width": 40,
        "height": 25,
        "crop": [0.5, 0.2, 0.5, 0.3],
    }
    assert result["doc"] == hd.PAGES_BASE + "dashboard/device-map.html#switches"


def test_target_key_replaces_the_default_object(repo):
    details = {
        "*": FULL,
        "dashboard": {"en": "Dashboard only", "de": "Nur Dashboard"},
    }
    translations = {148: {"details": details}}
    dashboard = hd.resolve(repo, translations, 148, "dashboard", image_ref="v0.10.0")
    assert dashboard == {"text": {"en": "Dashboard only", "de": "Nur Dashboard"}}
    shelly = hd.resolve(repo, translations, 148, "service:shelly", image_ref="v0.10.0")
    assert shelly["text"] == {"en": "Switch channels", "de": "Schaltkanäle"}
    assert "image" in shelly


def test_resolve_without_details_is_none(repo):
    assert hd.resolve(repo, {148: {"de": {"*": "x"}}}, 148, "dashboard", image_ref="v0.10.0") is None
    assert hd.resolve(repo, {}, 148, "dashboard", image_ref="v0.10.0") is None


def test_text_only_details_have_no_image_and_no_doc(repo):
    translations = {148: {"details": {"*": {"en": "Text", "de": "Text"}}}}
    assert hd.resolve(repo, translations, 148, "dashboard", image_ref="v0.10.0") == {
        "text": {"en": "Text", "de": "Text"},
    }


def test_validate_reports_each_problem(repo):
    (repo / "docs" / "images" / "notes.png").write_text("kein png", encoding="utf-8")
    cases = {
        "missing de": ({"en": "x"}, "text for de is missing"),
        "empty en": ({"en": "  ", "de": "x"}, "text for en is empty"),
        "image not in docs/images": ({"en": "x", "de": "x", "image": "gone.png"}, "image gone.png not found in docs/images"),
        "image is not png": ({"en": "x", "de": "x", "image": "notes.png"}, "image notes.png is not a PNG"),
        "crop without image": ({"en": "x", "de": "x", "crop": [0, 0, 1, 1]}, "crop needs image"),
        "crop with three values": ({"en": "x", "de": "x", "image": "shot.png", "crop": [0, 0, 1]}, "crop must be four finite numbers"),
        "crop beyond right edge": ({"en": "x", "de": "x", "image": "shot.png", "crop": [0.5, 0, 0.6, 0.5]}, "crop must lie inside the image"),
        "crop width zero": ({"en": "x", "de": "x", "image": "shot.png", "crop": [0, 0, 0, 0.5]}, "crop width and height must be positive"),
        "doc page missing": ({"en": "x", "de": "x", "doc": "dashboard/nope.html"}, "doc page docs/dashboard/nope.md not found"),
    }
    for name, (entry, reason) in cases.items():
        messages = hd.validate(repo, {148: {"details": {"*": entry}}})
        assert len(messages) == 1, name
        assert messages[0].startswith("#148 *: "), name
        assert reason in messages[0], name


def test_validate_returns_nothing_for_valid_details(repo):
    assert hd.validate(repo, {148: {"details": {"*": FULL}}}) == []
    assert hd.validate(repo, {148: {"de": {"*": "x"}}}) == []


def test_resolve_raises_on_invalid_details(repo):
    translations = {148: {"details": {"*": {"en": "x"}}}}
    with pytest.raises(ValueError, match="#148"):
        hd.resolve(repo, translations, 148, "dashboard", image_ref="v0.10.0")


def git(repo, *args):
    return subprocess.run(
        ["git", "-c", "user.name=test", "-c", "user.email=test@example.com", *args],
        cwd=repo, check=True, capture_output=True, text=True,
    ).stdout.strip()


def test_image_ref_prefers_the_exact_tag(tmp_path):
    git(tmp_path, "init", "-q")
    git(tmp_path, "commit", "-q", "--allow-empty", "-m", "init")
    git(tmp_path, "tag", "v0.10.0")
    assert hd.image_ref(tmp_path) == "v0.10.0"


def test_image_ref_falls_back_to_the_commit(tmp_path):
    git(tmp_path, "init", "-q")
    git(tmp_path, "commit", "-q", "--allow-empty", "-m", "init")
    assert hd.image_ref(tmp_path) == git(tmp_path, "rev-parse", "HEAD")


@pytest.mark.parametrize("details", [["not", "a", "dict"], "text"])
def test_resolve_raises_on_non_dict_details(repo, details):
    with pytest.raises(ValueError, match="#148"):
        hd.resolve(repo, {148: {"details": details}}, 148, "dashboard", image_ref="v0.10.0")


@pytest.mark.parametrize("entry", [{}, None, "text"])
def test_resolve_raises_on_empty_or_null_entry(repo, entry):
    translations = {148: {"details": {"*": entry}}}
    with pytest.raises(ValueError, match="#148"):
        hd.resolve(repo, translations, 148, "dashboard", image_ref="v0.10.0")


def test_resolve_raises_on_null_target_entry(repo):
    translations = {148: {"details": {"dashboard": None, "*": FULL}}}
    with pytest.raises(ValueError, match="#148 dashboard"):
        hd.resolve(repo, translations, 148, "dashboard", image_ref="v0.10.0")


def test_validate_reports_language_from_highlight_i18n(repo, monkeypatch):
    monkeypatch.setattr(hi, "LANGUAGES", ("de", "fr"))
    messages = hd.validate(repo, {148: {"details": {"*": {"en": "x", "de": "x"}}}})
    assert messages == ["#148 *: text for fr is missing"]


def test_crop_rejects_nan_and_infinity(repo):
    for value in (float("nan"), float("inf")):
        entry = {"en": "x", "de": "x", "image": "shot.png", "crop": [0.5, 0.2, value, 0.3]}
        messages = hd.validate(repo, {148: {"details": {"*": entry}}})
        assert len(messages) == 1
        assert "crop must be four finite numbers" in messages[0]
