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
    assert {p.name for p in rendered} == {"strings.json", "en.json", "de.json"}

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

    german = rhd.catalog_descriptions(REPO / rhd.CATALOG_REL)
    steps_de = json.loads((target / "translations/de.json").read_text())["config"]["step"]
    assert steps_de["sources_ac"]["data_description"]["bank_a_capacity_ah"] == \
        german["bank_a_capacity_ah"]
    assert steps_de["sources_dc"]["data_description"]["charger_dc_power_entity"] == \
        german["charger_dc_power_topic"] + \
        " Stromsensoren (A/mA) werden akzeptiert und mit der Packspannung umgerechnet."


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


def test_catalog_descriptions_reads_the_battery_schema_keys(tmp_path):
    catalog = tmp_path / "de.json"
    catalog.write_text(json.dumps({
        "schema.battery_soc_devices.items.bank_a_capacity_ah.description": "Kapazität von Bank A",
        "schema.battery_soc_devices.items.bank_a_capacity_ah.title": "ignoriert",
        "schema.system.mqtt.host.title": "ignoriert",
    }, ensure_ascii=False))
    assert rhd.catalog_descriptions(catalog) == {"bank_a_capacity_ah": "Kapazität von Bank A"}


def test_render_tree_fills_german_from_the_catalog(tmp_path):
    component = tmp_path / "battery_soc"
    (component / "translations").mkdir(parents=True)
    body = {"config": {"step": {"user": {"data_description": {"bank_a_capacity_ah": "[%schema:bank_a_capacity_ah%] Nur in HA."}}}}}
    for name in ("strings.json", "translations/en.json", "translations/de.json"):
        (component / name).write_text(json.dumps(body))
    schema = tmp_path / "schema.json"
    schema.write_text(json.dumps({"items": {"properties": {"bank_a_capacity_ah": {"description": "Capacity of bank A."}}}}))
    catalog = tmp_path / "de.json"
    catalog.write_text(json.dumps({"schema.battery_soc_devices.items.bank_a_capacity_ah.description": "Kapazität von Bank A."}, ensure_ascii=False))

    rhd.render_tree(component, schema, catalog)

    text = lambda name: json.loads((component / name).read_text())["config"]["step"]["user"]["data_description"]["bank_a_capacity_ah"]
    assert text("translations/en.json") == "Capacity of bank A. Nur in HA."
    assert text("translations/de.json") == "Kapazität von Bank A. Nur in HA."


def test_lint_requires_placeholders_in_german_too():
    problems = rhd.lint()
    assert not any("only allowed in" in p for p in problems)
