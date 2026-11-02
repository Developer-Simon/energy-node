"""Tests for scripts/ci/detect_changes.py (Regeltabelle der feineren CI-Erkennung)."""
import importlib.util
import subprocess
import sys
from pathlib import Path

import pytest

ROOT = Path(__file__).resolve().parents[2]
SCRIPT = ROOT / "scripts" / "ci" / "detect_changes.py"
spec = importlib.util.spec_from_file_location("detect_changes", SCRIPT)
detect_changes = importlib.util.module_from_spec(spec)
spec.loader.exec_module(detect_changes)

AREAS = detect_changes.AREAS
detect = detect_changes.detect
areas_for = detect_changes.areas_for

CASES = [
    ("README.md", set()),
    ("dashboard/VERSION", set()),
    ("changelog/highlights/196.json", set()),
    ("docs/images/device-icons/battery.svg", {"vendored"}),
    ("docs/_data/nav.yml", {"docs_scripts"}),
    ("scripts/docs/build.py", {"docs_scripts"}),
    ("docs/images/ha/screenshot.png", set()),
    ("integrations/homeassistant/custom_components/battery_soc/battery_soc_core/tuning.py", {"vendored"}),
    ("integrations/homeassistant/custom_components/battery_soc/energy_node_messages.py", {"vendored"}),
    ("integrations/homeassistant/custom_components/battery_soc/www/battery-card-core.js", {"vendored"}),
    ("integrations/homeassistant/custom_components/energy_node_icons/www/energy-node-icons.js", {"vendored"}),
    ("integrations/homeassistant/icons.source.json", {"dashboard_go", "vendored"}),
    ("integrations/homeassistant/tests/test_sensor.py", {"ha"}),
    ("integrations/homeassistant/custom_components/battery_soc/sensor.py", {"ha", "vendored"}),
    ("dashboard/internal/appconfig/config.schema.json", {"dashboard_go"}),
    ("dashboard/internal/webui/static/js-deps/alpine.min.js", {"dashboard_js", "dashboard_smoke"}),
    ("dashboard/internal/webui/static/js/battery-card-core.js", {"dashboard_js", "dashboard_smoke", "vendored"}),
    ("dashboard/internal/webui/static/js/config.page.js", {"dashboard_js", "dashboard_smoke"}),
    ("dashboard/internal/webui/static/css/manager.css", {"dashboard_js", "dashboard_smoke"}),
    ("dashboard/internal/schemaloc/catalogs/de.json", {"dashboard_go", "dashboard_js", "dashboard_smoke", "vendored", "ha"}),
    ("dashboard/internal/webui/templates/base.html", {"dashboard_go", "dashboard_js", "dashboard_smoke"}),
    ("dashboard/internal/webui/catalogs/en.json", {"dashboard_go", "dashboard_js", "dashboard_smoke"}),
    ("dashboard/internal/webui/webui_test.go", {"dashboard_go"}),
    ("dashboard/test/smoke/smoke.sh", {"dashboard_smoke"}),
    ("demo/fakes/fake_shelly.py", {"dashboard_smoke"}),
    ("dashboard/test/browser/screenshot.mjs", set()),
    ("dashboard/test/config.page.test.mjs", {"dashboard_js"}),
    ("dashboard/package-lock.json", {"dashboard_js"}),
    ("dashboard/energy-node-updater.sh", {"dashboard_go", "dashboard_js", "dashboard_smoke", "vendored", "shell", "bootstrap"}),
    ("dashboard/internal/httpapi/server.go", {"dashboard_go", "dashboard_smoke"}),
    ("dashboard/go.sum", {"dashboard_go", "dashboard_smoke"}),
    ("dashboard/Caddyfile", {"dashboard_go", "dashboard_js", "dashboard_smoke", "vendored"}),
    ("services/battery_soc/battery_soc_devices.schema.json", {"dashboard_go", "dashboard_js", "dashboard_smoke", "python", "vendored"}),
    ("services/shelly/shelly.schema.json", {"dashboard_go", "dashboard_js", "dashboard_smoke", "python"}),
    ("libs/battery_soc_core/tests/test_tuning.py", {"python"}),
    ("services/shelly/tests/test_bridge.py", {"python"}),
    ("requirements-dev.txt", {"python"}),
    ("libs/battery_soc_core/battery_soc_core/tuning.py", {"python", "vendored", "ha"}),
    ("libs/energy_node_common/src/energy_node_common/messages.py", {"python", "vendored", "ha"}),
    ("services/shelly/shelly_bridge.py", {"python"}),
    ("installer/webui/test/api.test.mjs", {"installer_webui"}),
    ("installer/webui/webui_test.go", {"installer_webui"}),
    ("installer/webui/static/js-deps/alpine.min.js", {"installer_webui"}),
    ("installer/webui/hostapi/backend.go", {"installer_webui", "installer_go", "installer_cross", "dashboard_go"}),
    ("installer/webui/templates/steps.html", {"installer_webui", "installer_go", "dashboard_go"}),
    ("installer/internal/shell/open_test.go", {"installer_go"}),
    ("installer/internal/shell/shell.go", {"installer_go", "installer_cross"}),
    ("installer/go.mod", {"installer_go", "installer_cross"}),
    ("installer/internal/assets/logo.png", {"installer_go"}),
    ("scripts/bootstrap/steps/10_apt.sh", {"installer_go", "shell", "bootstrap"}),
    ("scripts/tests/test_highlight_i18n.py", {"python"}),
    ("scripts/tests/bootstrap_container.sh", {"shell", "bootstrap"}),
    ("scripts/tests/lib/fake_system_commands.sh", {"shell", "bootstrap"}),
    ("scripts/build/highlight_details.py", {"python"}),
    ("scripts/release/release_highlights.py", {"python"}),
    ("scripts/build/make_bundle.sh", {"shell", "bootstrap"}),
    ("scripts/version/bump.py", {"python", "shell"}),
    ("scripts/generate_changelog.sh", {"python", "shell"}),
    ("scripts/vendor_core.py", {"vendored"}),
    ("scripts/icons/flatten_icons.py", {"vendored"}),
    ("scripts/dev/create-pr.sh", {"shell"}),
    ("scripts/tests/test_updater.sh", {"shell"}),
    (".gitignore", set()),
]


@pytest.mark.parametrize("path, expected", CASES)
def test_rule_table(path, expected):
    assert areas_for(path) == frozenset(expected)


def test_empty_input_means_all():
    assert detect([]) == {area: True for area in AREAS}


def test_force_all():
    everything = {area: True for area in AREAS}
    nothing = {area: False for area in AREAS}
    assert detect([".github/workflows/ci.yml"]) == everything
    assert detect(["scripts/ci/detect_changes.py"]) == everything
    assert detect([".github/workflows/pages.yml"]) == nothing
    assert detect([".github/workflows/pages.yml", ".github/workflows/ci.yml"]) == everything


def test_union_over_files():
    result = detect(["dashboard/test/config.page.test.mjs", "libs/battery_soc_core/tests/test_tuning.py"])
    assert {area for area, on in result.items() if on} == {"dashboard_js", "python"}


def test_source_plus_copy_still_runs_consumers():
    result = detect([
        "libs/battery_soc_core/battery_soc_core/tuning.py",
        "integrations/homeassistant/custom_components/battery_soc/battery_soc_core/tuning.py",
    ])
    assert {area for area, on in result.items() if on} == {"python", "vendored", "ha"}


def test_blank_and_whitespace_lines_ignored():
    proc = subprocess.run(
        [sys.executable, str(SCRIPT)],
        input="\n  \nREADME.md\r\n\n",
        capture_output=True,
        text=True,
    )
    assert proc.returncode == 0
    lines = proc.stdout.splitlines()
    assert lines
    assert all(line.endswith("=false") for line in lines)


def test_blank_only_input_means_all():
    proc = subprocess.run(
        [sys.executable, str(SCRIPT)],
        input="",
        capture_output=True,
        text=True,
    )
    assert proc.returncode == 0
    lines = proc.stdout.splitlines()
    assert lines
    assert all(line.endswith("=true") for line in lines)


def test_cli_output_format():
    proc = subprocess.run(
        [sys.executable, str(SCRIPT)],
        input="dashboard/test/x.test.mjs\n",
        capture_output=True,
        text=True,
    )
    assert proc.returncode == 0
    assert proc.stdout.splitlines() == [
        f"{a}={'true' if a == 'dashboard_js' else 'false'}" for a in AREAS
    ]


def test_workflow_wires_every_area():
    import re

    text = (ROOT / ".github" / "workflows" / "ci.yml").read_text(encoding="utf-8")
    for a in AREAS:
        assert f"{a}: ${{{{ steps.filter.outputs.{a} }}}}" in text, a
        assert f"needs.changes.outputs.{a} == 'true'" in text, a
    assert set(re.findall(r"needs\.changes\.outputs\.(\w+)", text)) == set(AREAS)
