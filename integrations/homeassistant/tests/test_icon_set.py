"""Das erzeugte Icon-Modul muss zur Quelle passen und sich anmelden."""
import json
import sys
from pathlib import Path

HA = Path(__file__).resolve().parents[1]
MODULE = HA / "custom_components/energy_node_icons/www/energy-node-icons.js"
SOURCE = HA / "icons.source.json"

sys.path.insert(0, str(HA.parents[1] / "scripts/icons"))
import flatten_icons


def test_generated_module_matches_the_catalogue():
    assert flatten_icons.check() == 0, "energy-node-icons.js ist veraltet - flatten_icons.py neu laufen lassen"


def test_module_registers_both_icon_apis():
    source = MODULE.read_text(encoding="utf-8")
    assert 'window.customIconsets["energy-node"]' in source
    assert 'window.customIcons["energy-node"]' in source
    assert "getIconList" in source


def test_every_catalogue_icon_reaches_the_module():
    names = {icon["ha_name"] for icon in json.loads(SOURCE.read_text())["icons"]}
    source = MODULE.read_text(encoding="utf-8")
    for name in names:
        assert f'"{name}"' in source
    assert "mdi:" not in source


def test_module_falls_back_to_chip_for_unknown_icons():
    source = MODULE.read_text(encoding="utf-8")
    assert 'ICONS["chip"]' in source
    assert 'ICONS["chip-outline"]' not in source


def test_aliases_resolve_but_stay_out_of_the_icon_list():
    doc = json.loads(SOURCE.read_text())
    names = {icon["ha_name"] for icon in doc["icons"]}
    source = MODULE.read_text(encoding="utf-8")
    assert "const ALIASES = {" in source
    assert len(doc["aliases"]) == 11
    for alias in doc["aliases"]:
        assert alias["to"] in names
        assert alias["from"] not in names
        assert f'  "{alias["from"]}": "{alias["to"]}",' in source
    assert "ICONS[ALIASES[name]]" in source
    assert "Object.entries(ICONS)" in source
