"""battery_soc_core darf nur die Standardbibliothek importieren - sonst kann
die Home-Assistant-Integration (Plan 2) es nicht ohne manifest-requirements
vendoren."""
import ast
import sys
from pathlib import Path

PKG = Path(__file__).resolve().parents[1] / "battery_soc_core"
STDLIB = set(sys.stdlib_module_names)
ALLOWED_LOCAL = {"battery_soc_core"}


def _imported_roots(path):
    tree = ast.parse(path.read_text())
    roots = set()
    for node in ast.walk(tree):
        if isinstance(node, ast.Import):
            roots.update(alias.name.split(".")[0] for alias in node.names)
        elif isinstance(node, ast.ImportFrom) and node.level == 0 and node.module:
            roots.add(node.module.split(".")[0])
    return roots


def test_core_modules_import_stdlib_only():
    offenders = {}
    for py in PKG.rglob("*.py"):
        bad = _imported_roots(py) - STDLIB - ALLOWED_LOCAL
        if bad:
            offenders[py.name] = sorted(bad)
    assert offenders == {}
