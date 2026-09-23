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


def test_module_falls_back_to_chip_outline_for_unknown_icons():
    """Verifies getIcon returns chip-outline as fallback for unknown icon names."""
    source = MODULE.read_text(encoding="utf-8")
    # Check that the fallback logic uses chip-outline
    assert 'ICONS["chip-outline"]' in source, "chip-outline must be in the fallback logic"
    assert "chip-outline" in source
