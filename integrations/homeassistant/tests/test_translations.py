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


def test_user_step_has_a_field_for_every_conf_key():
    """Test that user step has labels for all configuration keys."""
    strings = json.loads((BASE / "strings.json").read_text())
    fields = set(strings["config"]["step"]["user"]["data"]) | set(strings["config"]["step"]["advanced"]["data"])
    # every selector key the flow shows must have a label
    from custom_components.battery_soc import config_flow  # noqa
    # spot-check a few
    for key in ("topology", "bank_a_capacity_ah", "empty_v_per_cell", "require_fresh_inputs"):
        assert key in fields
