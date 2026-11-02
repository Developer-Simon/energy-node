#!/usr/bin/env python3
"""Welche CI-Bereiche laufen müssen, abhängig von den geänderten Dateien.

Liest Pfade zeilenweise von stdin (z. B. aus `git diff --name-only`) und
schreibt pro Bereich eine Zeile `bereich=true|false` auf stdout, in der
Reihenfolge von AREAS. Auf stderr steht pro Datei, welche Bereiche sie auslöst.

Erste passende Regel gewinnt. Eine neue Datei ohne Regel löst nichts aus,
darum gibt es Rückfallregeln je Verzeichnis (zuletzt `^dashboard/` und
`^scripts/`). Keine Pfade oder eine Datei aus FORCE_ALL lösen alle Bereiche aus.
"""
import re
import sys
from typing import Iterable

AREAS: tuple[str, ...] = (
    "dashboard_go",
    "dashboard_js",
    "dashboard_smoke",
    "installer_go",
    "installer_cross",
    "installer_webui",
    "python",
    "shell",
    "bootstrap",
    "vendored",
    "ha",
    "docs_scripts",
)

DASH_ALL = frozenset({"dashboard_go", "dashboard_js", "dashboard_smoke", "vendored"})

FORCE_ALL = re.compile(r"^\.github/workflows/(?!pages\.yml$)|^scripts/ci/")

# Begründungen (eine Zeile je Regel):
# 5, 6, 9: Kopien prüft nur ihr Drift-Check, die Quelle löst die Verbraucher aus.
# 13, 14: Kataloge und Templates prüfen die Go-Webui-Tests.
# 29, 30: Das Dashboard bindet installer/webui per replace ein.
# 29, 32: Der Installer bindet ./webui ein, daher Cross-Compile bei Go-Code in webui.
# 36, 38: bootstrap_container.sh baut das Bundle über scripts/build/make_bundle.sh.
# 21: Die bisherige Dashboard-Breite ist ein sicherer Rückfall für Dateien wie Caddyfile und Service-Units.
# 19: energy-node-updater.sh löste bisher den Shell-Job nicht aus, obwohl dieser shellcheck und test_updater*.sh darauf laufen lässt.
RULES: list[tuple[re.Pattern, frozenset[str]]] = [
    # 1
    (re.compile(r"\.md$|(^|/)VERSION$|^changelog/"), frozenset()),
    # 2
    (re.compile(r"^docs/images/(device-icons|ha/icons)/"), frozenset({"vendored"})),
    # 3
    (re.compile(r"^docs/_data/nav\.yml$|^scripts/docs/|^scripts/tests/test_docs_pages\.sh$"), frozenset({"docs_scripts"})),
    # 4
    (re.compile(r"^docs/"), frozenset()),
    # 5
    (re.compile(
        r"^integrations/homeassistant/custom_components/battery_soc/battery_soc_core/"
        r"|^integrations/homeassistant/custom_components/battery_soc/energy_node_messages\.py$"
        r"|^integrations/homeassistant/custom_components/battery_soc/www/battery-card-core\.js$"
        r"|^integrations/homeassistant/custom_components/energy_node_icons/www/energy-node-icons\.js$"
    ), frozenset({"vendored"})),
    # 6
    (re.compile(r"^integrations/homeassistant/icons\.source\.json$"), frozenset({"dashboard_go", "vendored"})),
    # 7
    (re.compile(r"^integrations/homeassistant/tests/"), frozenset({"ha"})),
    # 8
    (re.compile(r"^integrations/homeassistant/"), frozenset({"ha", "vendored"})),
    # 9
    (re.compile(r"^dashboard/internal/appconfig/config\.schema\.json$"), frozenset({"dashboard_go"})),
    # 10
    (re.compile(r"^dashboard/internal/webui/static/js-deps/"), frozenset({"dashboard_js", "dashboard_smoke"})),
    # 11
    (re.compile(r"^dashboard/internal/webui/static/js/battery-card-core\.js$"),
     frozenset({"dashboard_js", "dashboard_smoke", "vendored"})),
    # 12
    (re.compile(r"^dashboard/internal/webui/static/"), frozenset({"dashboard_js", "dashboard_smoke"})),
    # 13
    (re.compile(r"^dashboard/internal/schemaloc/catalogs/"),
     frozenset({"dashboard_go", "dashboard_js", "dashboard_smoke", "vendored", "ha"})),
    # 14
    (re.compile(r"^dashboard/internal/webui/(templates|catalogs)/"),
     frozenset({"dashboard_go", "dashboard_js", "dashboard_smoke"})),
    # 15
    (re.compile(r"^dashboard/.*_test\.go$"), frozenset({"dashboard_go"})),
    # 16
    (re.compile(r"^dashboard/test/smoke/|^demo/"), frozenset({"dashboard_smoke"})),
    # 17
    (re.compile(r"^dashboard/test/browser/"), frozenset()),
    # 18
    (re.compile(r"^dashboard/test/|^dashboard/package(-lock)?\.json$"), frozenset({"dashboard_js"})),
    # 19
    (re.compile(r"^dashboard/energy-node-(updater\.sh|dashboard-system-action(\.sudoers)?)$"),
     DASH_ALL | frozenset({"shell", "bootstrap"})),
    # 20
    (re.compile(r"^dashboard/.*\.go$|^dashboard/go\.(mod|sum)$"),
     frozenset({"dashboard_go", "dashboard_smoke"})),
    # 21
    (re.compile(r"^dashboard/"), DASH_ALL),
    # 22
    (re.compile(r"^services/battery_soc/battery_soc_devices\.schema\.json$"),
     frozenset({"dashboard_go", "dashboard_js", "dashboard_smoke", "python", "vendored"})),
    # 23
    (re.compile(r"^services/[^/]+/[^/]+\.schema\.json$"),
     frozenset({"dashboard_go", "dashboard_js", "dashboard_smoke", "python"})),
    # 24
    (re.compile(r"^(services|libs)/[^/]+/tests/|^requirements-dev\.txt$"), frozenset({"python"})),
    # 25
    (re.compile(r"^libs/battery_soc_core/"), frozenset({"python", "vendored", "ha"})),
    # 26
    (re.compile(r"^libs/energy_node_common/src/energy_node_common/messages\.py$"),
     frozenset({"python", "vendored", "ha"})),
    # 27
    (re.compile(r"^(services|libs)/"), frozenset({"python"})),
    # 28
    (re.compile(r"^installer/webui/test/|^installer/webui/.*_test\.go$|^installer/webui/package(-lock)?\.json$"
                r"|^installer/webui/static/js-deps/"),
     frozenset({"installer_webui"})),
    # 29
    (re.compile(r"^installer/webui/.*\.go$|^installer/webui/go\.(mod|sum)$"),
     frozenset({"installer_webui", "installer_go", "installer_cross", "dashboard_go"})),
    # 30
    (re.compile(r"^installer/webui/"), frozenset({"installer_webui", "installer_go", "dashboard_go"})),
    # 31
    (re.compile(r"^installer/.*_test\.go$"), frozenset({"installer_go"})),
    # 32
    (re.compile(r"^installer/.*\.go$|^installer/go\.(mod|sum)$"), frozenset({"installer_go", "installer_cross"})),
    # 33
    (re.compile(r"^installer/"), frozenset({"installer_go"})),
    # 34
    (re.compile(r"^scripts/bootstrap/"), frozenset({"installer_go", "shell", "bootstrap"})),
    # 35
    (re.compile(r"^scripts/tests/test_[^/]*\.py$"), frozenset({"python"})),
    # 36
    (re.compile(r"^scripts/tests/(bootstrap_container\.sh$|lib/)"), frozenset({"shell", "bootstrap"})),
    # 37
    (re.compile(r"^scripts/build/(make_changelog_json|highlight_i18n|highlight_translations|highlight_details)\.py$"
                r"|^scripts/release/release_highlights\.py$"),
     frozenset({"python"})),
    # 38
    (re.compile(r"^scripts/build/"), frozenset({"shell", "bootstrap"})),
    # 39
    (re.compile(r"^scripts/version/|^scripts/generate_changelog\.sh$"), frozenset({"python", "shell"})),
    # 40
    (re.compile(r"^scripts/(vendor_(core|card)|render_ha_descriptions)\.py$|^scripts/icons/"),
     frozenset({"vendored"})),
    # 41
    (re.compile(r"^scripts/"), frozenset({"shell"})),
]


def areas_for(path: str) -> frozenset[str]:
    for pattern, areas in RULES:
        if pattern.search(path):
            return areas
    return frozenset()


def detect(paths: Iterable[str]) -> dict[str, bool]:
    cleaned = [p.strip() for p in paths]
    cleaned = [p for p in cleaned if p]
    if not cleaned or any(FORCE_ALL.search(p) for p in cleaned):
        return {area: True for area in AREAS}
    hit: set[str] = set()
    for p in cleaned:
        hit |= areas_for(p)
    return {area: area in hit for area in AREAS}


def main() -> None:
    paths = [p.strip() for p in sys.stdin.read().splitlines()]
    paths = [p for p in paths if p]
    for p in paths:
        areas = [a for a in AREAS if a in areas_for(p)]
        text = ",".join(areas) if areas else "(nichts)"
        if FORCE_ALL.search(p):
            text += " (alle, FORCE_ALL)"
        print(f"{p} -> {text}", file=sys.stderr)
    result = detect(paths)
    for area in AREAS:
        print(f"{area}={'true' if result[area] else 'false'}")


if __name__ == "__main__":
    main()
