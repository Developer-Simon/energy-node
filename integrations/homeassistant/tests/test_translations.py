"""Tests for translations."""
import json
from pathlib import Path

BASE = Path(__file__).resolve().parents[1] / "custom_components/battery_soc"


def test_translation_keys_match_strings():
    """Test that all translation files have the same key structure as strings.json."""
    strings = json.loads((BASE / "strings.json").read_text())
    en = json.loads((BASE / "translations/en.json").read_text())
    de = json.loads((BASE / "translations/de.json").read_text())

    def keys(d, prefix=""):
        out = set()
        for k, v in d.items():
            p = f"{prefix}.{k}" if prefix else k
            out.add(p)
            if isinstance(v, dict):
                out |= keys(v, p)
        return out

    assert keys(strings) == keys(en)
    assert keys(strings) == keys(de)


def _load(name):
    return json.loads((BASE / name).read_text())


def test_every_config_field_has_a_label():
    from custom_components.battery_soc import config_flow as cf

    CONFIG_FORMS = {
        "user": lambda: cf._user_schema(),
        "sources_ac": lambda: cf._sources_schema("ac_coupled", {}),
        "sources_dc": lambda: cf._sources_schema("dc_only", {}),
        "bank_b": lambda: cf._bank_b_schema("series", {}),
        "advanced": lambda: cf._advanced_schema_dict({}, "ac_coupled"),
    }
    strings = _load("strings.json")
    for step, build in CONFIG_FORMS.items():
        keys = {str(k) for k in build()}
        labels = set(strings["config"]["step"][step]["data"])
        assert keys <= labels, (step, keys - labels)


def test_described_steps_describe_every_field_except_the_name():
    from custom_components.battery_soc import config_flow as cf

    CONFIG_FORMS = {
        "user": lambda: cf._user_schema(),
        "sources_ac": lambda: cf._sources_schema("ac_coupled", {}),
        "sources_dc": lambda: cf._sources_schema("dc_only", {}),
        "bank_b": lambda: cf._bank_b_schema("series", {}),
        "advanced": lambda: cf._advanced_schema_dict({}, "ac_coupled"),
    }
    DESCRIBED = {"user", "sources_ac", "sources_dc", "bank_b"}
    for name in ("strings.json", "translations/en.json", "translations/de.json"):
        strings = _load(name)
        for step in DESCRIBED:
            keys = {str(k) for k in CONFIG_FORMS[step]()} - {"name"}
            described = set(strings["config"]["step"][step].get("data_description", {}))
            assert keys <= described, (name, step, keys - described)


def test_promised_descriptions_carry_the_spec_texts():
    en = _load("strings.json")["config"]["step"]
    assert "capacities add up" in en["sources_ac"]["data_description"]["bank_layout"]
    assert "A 5S pack is 5" in en["sources_ac"]["data_description"]["bank_a_cell_count"]
    assert "five 3.6 Ah cells in series are 3.6 Ah" in \
        en["sources_ac"]["data_description"]["bank_a_capacity_ah"]
    assert "Leave at 1.0" in en["sources_ac"]["data_description"]["bank_a_voltage_scale"]
    de = _load("translations/de.json")["config"]["step"]
    for step, fields in (("sources_ac", ("bank_layout", "bank_a_cell_count", "bank_a_capacity_ah", "bank_a_voltage_scale")),
                         ("sources_dc", ("bank_layout", "bank_a_cell_count", "bank_a_capacity_ah", "bank_a_voltage_scale")),
                         ("bank_b", ("bank_b_cell_count", "bank_b_capacity_ah", "bank_b_voltage_scale"))):
        for field in fields:
            assert de[step]["data_description"][field] != \
                en[step]["data_description"][field], (step, field)


def test_error_codes_are_translated():
    codes = {"bank_a_voltage_required", "bank_b_voltage_required", "charge_source_required",
             "discharge_source_required", "current_only_on_dc", "ac_source_in_dc_system"}
    for name in ("strings.json", "translations/en.json", "translations/de.json"):
        assert codes <= set(_load(name)["config"]["error"]), name
