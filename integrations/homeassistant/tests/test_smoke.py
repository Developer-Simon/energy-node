def test_core_is_vendored_and_imports():
    import sys
    import importlib
    from pathlib import Path

    # Ensure custom_components is importable by adding it to sys.path.
    # Clear cached modules first (pytest-homeassistant may have loaded them).
    for key in list(sys.modules.keys()):
        if key.startswith('custom_components'):
            del sys.modules[key]

    test_dir = Path(__file__).resolve().parent
    sys.path.insert(0, str(test_dir.parent))

    # Import via importlib to avoid caching issues with pytest plugins.
    core = importlib.import_module('custom_components.battery_soc.battery_soc_core')
    core.SocParams().validate()
    # Verify tick and entity_specs are available
    assert hasattr(core, 'tick')
    assert hasattr(core, 'entity_specs')


def test_manifest_has_no_requirements():
    import json
    from pathlib import Path
    mf = json.loads((Path(__file__).resolve().parents[1]
                     / "custom_components/battery_soc/manifest.json").read_text())
    assert mf["domain"] == "battery_soc"
    assert mf["requirements"] == []
