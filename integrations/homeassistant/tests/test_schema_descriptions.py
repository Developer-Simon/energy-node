"""Shared field descriptions come from the battery_soc service schema."""
import json
import shutil
import sys
from pathlib import Path

REPO = Path(__file__).resolve().parents[3]
sys.path.insert(0, str(REPO / "scripts"))
import render_ha_descriptions as rhd  # noqa: E402

COMPONENT = REPO / rhd.HA_REL


def test_strings_use_schema_placeholders():
    assert rhd.lint(REPO) == []


def test_render_leaves_no_placeholder_and_uses_the_schema_text(tmp_path):
    target = tmp_path / "battery_soc"
    shutil.copytree(COMPONENT, target, ignore=shutil.ignore_patterns("__pycache__"))
    rendered = rhd.render_tree(target, REPO / rhd.SCHEMA_REL)
    assert {p.name for p in rendered} == {"strings.json", "en.json"}

    for path in [target / "strings.json", *(target / "translations").glob("*.json")]:
        assert not rhd.PLACEHOLDER.search(path.read_text()), path

    schema = rhd.schema_descriptions(REPO / rhd.SCHEMA_REL)
    steps = json.loads((target / "translations/en.json").read_text())["config"]["step"]
    assert steps["sources_ac"]["data_description"]["bank_a_capacity_ah"] == \
        schema["bank_a_capacity_ah"]
    # HA-only text after the placeholder survives the rendering.
    assert steps["sources_dc"]["data_description"]["charger_dc_power_entity"] == \
        schema["charger_dc_power_topic"] + \
        " Current sensors (A/mA) are accepted and converted with the pack voltage."


def test_render_rejects_an_unknown_placeholder(tmp_path):
    target = tmp_path / "battery_soc"
    (target / "translations").mkdir(parents=True)
    (target / "strings.json").write_text('{"x": "[%schema:no_such_field%]"}')
    try:
        rhd.render_tree(target, REPO / rhd.SCHEMA_REL)
    except KeyError as err:
        assert "no_such_field" in str(err)
    else:
        raise AssertionError("an unknown placeholder must not render silently")
