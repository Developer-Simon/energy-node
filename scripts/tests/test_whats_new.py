import importlib.util
from pathlib import Path

import pytest

ROOT = Path(__file__).resolve().parents[2]
_spec = importlib.util.spec_from_file_location("whats_new", ROOT / "scripts/docs/whats-new.py")
whats_new = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(whats_new)

PAGES = whats_new.PAGES_PREFIX
IMG = "https://raw.githubusercontent.com/Developer-Simon/energy-node/local/docs/images/shot.png"
TAGS = ["v0.7.0", "v0.8.0", "v0.9.0"]
TAG_VERSIONS = {
    "v0.7.0": {"dashboard": "v0.7.0"},
    "v0.8.0": {"dashboard": "v0.8.0"},
    "v0.9.0": {"dashboard": "v0.9.0", "installer-webui": "v0.2.0"},
}
TAG_DATES = {"v0.7.0": "1 Jan 2026", "v0.8.0": "2 Feb 2026", "v0.9.0": "3 Mar 2026"}
NAV = [{"title": "Using", "items": [
    {"label": "Dashboard", "url": "dashboard/index.md", "children": [
        {"label": "Automations", "url": "dashboard/automations.md", "children": [
            {"label": "Scenarios", "url": "dashboard/automations-scenarios.md"},
        ]},
    ]},
    {"label": "Device services", "url": "services/index.md", "children": [
        {"label": "Shelly", "url": "services/shelly.md"},
    ]},
]}]


def e(pr, text, *, scope=None, details=None, tour=None, breaking=False):
    entry = {"text": text, "pr": pr, "highlight": True, "breaking": breaking}
    if scope:
        entry["scope"] = scope
    if details:
        entry["details"] = details
    if tour:
        entry["tour"] = tour
    return entry


def rel(version, *entries):
    return {"version": version, "date": "",
            "groups": [{"type": "feat", "label": "Features", "entries": list(entries)}]}


def component(component_id, label, version, releases):
    return {"id": component_id, "label": label, "kind": "app", "version": version, "releases": releases}


def sample_document():
    dashboard = component("dashboard", "Dashboard", "v0.9.1", [
        rel("v0.9.1", e(30, "Fresh thing.")),
        rel("v0.9.0",
            e(20, "image thing", details={
                "text": {"en": "Shows an image."},
                "image": {"url": IMG, "width": 1000, "height": 500},
                "doc": PAGES + "dashboard/automations-scenarios.html#scenarios"}),
            e(21, "text thing", details={"text": {"en": "Explained."}}),
            e(22, "tour thing", tour={"steps": [{"title": {"en": "Step one"}, "text": {"en": "Tap it."}}]}),
            e(23, "title only"),
            e(40, "shared thing"),
            e(60, "login thing", scope="auth")),
        rel("v0.8.5", e(15, "mobile view for the statistics page.")),
        rel("v0.7.0", e(10, "old thing")),
    ])
    installer = component("installer-webui", "Installer web UI", "v0.2.0", [
        rel("v0.2.0", e(40, "shared thing"), e(41, "installer only")),
    ])
    return {"components": [dashboard, installer]}


@pytest.fixture
def docs(tmp_path):
    (tmp_path / "dashboard").mkdir()
    (tmp_path / "dashboard" / "automations-scenarios.md").write_text(
        '---\ntitle: "Automations: Scenarios"\n---\n\n# Automations: Scenarios\n', encoding="utf-8")
    (tmp_path / "dashboard" / "heading.md").write_text("# Heading only\n", encoding="utf-8")
    return tmp_path


def run(docs, document=None, translations=None, nav=None, image_sizes=None):
    return whats_new.build(document or sample_document(), TAGS, TAG_VERSIONS, TAG_DATES,
                           translations or {}, nav or [], docs,
                           image_sizes or {"shot.png": (1000, 500)})


def find(result, pr):
    for release in result["releases"]:
        for entry in release["cards"] + release["rest"]:
            if entry["pr"] == pr:
                return entry
    raise AssertionError(f"PR {pr} not in releases")


def test_release_assignment_and_min_tag(docs):
    result = run(docs)
    assert all(entry["pr"] != 10 for release in result["releases"] for entry in release["cards"] + release["rest"])
    assert find(result, 15)["since"] == "v0.9.0"
    assert result["releases"][0]["tag"] == "next"
    assert find(result, 30)["since"] == "next"


def test_component_missing_at_old_tag(docs):
    result = run(docs)
    assert find(result, 41)["since"] == "v0.9.0"


def test_cards_order_and_rest(docs):
    result = run(docs)
    release = next(r for r in result["releases"] if r["tag"] == "v0.9.0")
    assert [e["kind"] for e in release["cards"]][:3] == ["image", "text", "tour"]
    assert [e["pr"] for e in release["rest"] if e["pr"] == 23] == [23]
    assert all(e["kind"] == "title" for e in release["rest"])


def test_title_is_the_capitalised_highlight_text(docs):
    assert find(run(docs), 15)["title"] == "Mobile view for the statistics page"


def test_same_pr_in_two_components_is_one_entry(docs):
    result = run(docs)
    shared = [e for release in result["releases"] for e in release["cards"] + release["rest"] if e["pr"] == 40]
    assert len(shared) == 1
    assert shared[0]["components"] == ["Dashboard", "Installer web UI"]


def test_crop_style_full_width(docs):
    document = {"components": [component("dashboard", "Dashboard", "v0.9.0", [
        rel("v0.9.0", e(70, "cropped", details={
            "text": {"en": "c"},
            "image": {"url": IMG, "width": 1000, "height": 500, "crop": [0, 0.5, 1, 0.25]}})),
    ])]}
    entry = find(run(docs, document=document), 70)
    assert entry["image"] == {
        "name": "shot.png",
        "style": "aspect-ratio:8.000;background-size:100.00% auto;background-position:0.00% 66.67%",
    }


def test_image_without_crop_has_no_style(docs):
    assert find(run(docs), 20)["image"] == {"name": "shot.png", "style": None}


def test_doc_title_fallbacks(docs):
    document = {"components": [component("dashboard", "Dashboard", "v0.9.0", [
        rel("v0.9.0",
            e(50, "front matter", details={"text": {"en": "x"}, "doc": PAGES + "dashboard/automations-scenarios.html"}),
            e(51, "heading", details={"text": {"en": "x"}, "doc": PAGES + "dashboard/heading.html#top"}),
            e(52, "missing", details={"text": {"en": "x"}, "doc": PAGES + "dashboard/missing.html"})),
    ])]}
    result = run(docs, document=document)
    assert find(result, 50)["doc"] == {"url": "dashboard/automations-scenarios.html",
                                       "title": "Automations: Scenarios"}
    assert find(result, 51)["doc"] == {"url": "dashboard/heading.html#top", "title": "Heading only"}
    assert find(result, 52)["doc"]["title"] == "dashboard/missing.md"


def test_installer_component_beats_nav(docs):
    nav = [{"title": "Getting started", "items": [
        {"label": "Deploying with the installer", "url": "installer.md"},
    ]}]
    details = {"text": {"en": "Installer step."}, "doc": PAGES + "installer.html#4-dashboard"}
    document = {"components": [
        component("dashboard", "Dashboard", "v0.9.0", [rel("v0.9.0", e(80, "step", details=details))]),
        component("installer-webui", "Installer web UI", "v0.2.0",
                  [rel("v0.2.0", e(80, "step", details=details))]),
    ]}
    result = run(docs, document=document, nav=nav)
    entry = find(result, 80)
    assert entry["components"] == ["Dashboard", "Installer web UI"]
    assert entry["area"] == "Installer"


def test_area_from_nav_scope_component(docs):
    result = run(docs, nav=NAV)
    assert find(result, 20)["area"] == "Automations"
    assert find(result, 60)["area"] == "Users and permissions"
    assert find(result, 41)["area"] == "Installer"
    assert find(result, 15)["area"] == "Dashboard"
    names = [area["name"] for area in result["areas"]]
    assert names == sorted(names)


def test_parse_nav_reads_sections_and_children():
    text = (
        "# comment\n"
        "- title: Using\n"
        "  items:\n"
        "    - { label: \"Shelly\", url: \"services/shelly.md\" }\n"
        "    - label: \"Dashboard\"\n"
        "      url: \"dashboard/index.md\"\n"
        "      children:\n"
        "        - { label: \"History\", url: \"dashboard/history.md\" }\n"
    )
    nav = whats_new.parse_nav(text)
    assert nav == [{"title": "Using", "items": [
        {"label": "Shelly", "url": "services/shelly.md"},
        {"label": "Dashboard", "url": "dashboard/index.md",
         "children": [{"label": "History", "url": "dashboard/history.md"}]},
    ]}]


def test_parse_real_nav_file():
    nav = whats_new.parse_nav((ROOT / "docs/_data/nav.yml").read_text(encoding="utf-8"))
    assert [section["title"] for section in nav][:2] == ["Getting started", "Using"]
    assert whats_new._nav_label(nav, "dashboard/automations-scenarios.md") == "Automations"
    assert whats_new._nav_label(nav, "services/shelly.md") == "Device services"


def test_kinds_and_tours(docs):
    result = run(docs)
    assert find(result, 22)["tour"] == [{"title": "Step one", "text": "Tap it."}]
    assert find(result, 22)["kind"] == "tour"
    assert find(result, 23)["kind"] == "title"
