"""Table test for scripts/bootstrap/lib/restart_rule.py and a drift guard for
its library map against what the services really import."""
import json
import pathlib
import re
import subprocess
import sys

REPO = pathlib.Path(__file__).resolve().parents[2]
LIB = REPO / "scripts" / "bootstrap" / "lib"
CASES = json.loads((REPO / "scripts" / "bootstrap" / "testdata" / "restart_cases.json").read_text(encoding="utf-8"))

sys.path.insert(0, str(LIB))
import restart_rule  # noqa: E402


def test_every_case_in_the_table():
    for case in CASES:
        got = restart_rule.restart_reason(case["candidate"], case["installed"], case["step_id"], case["restart_all"])
        assert got == case["want"], case["name"]


def test_cli_reads_manifests_and_env(tmp_path):
    bundle, state = tmp_path / "bundle", tmp_path / "state"
    bundle.mkdir()
    state.mkdir()
    (bundle / "manifest.json").write_text(json.dumps(
        {"steps": [{"id": "83", "dir": "shelly", "version": "v0.4.3"}], "components": {}}))
    (state / "installed-manifest.json").write_text(json.dumps(
        {"steps": [{"id": "83", "dir": "shelly", "version": "v0.4.2"}], "components": {}}))

    def run(env=None):
        out = subprocess.run([sys.executable, str(LIB / "restart_rule.py"), str(bundle), str(state), "83"],
                             capture_output=True, text=True, check=True, env=env)
        return out.stdout.strip()

    assert run({"PATH": "/usr/bin"}) == "version"
    assert run({"PATH": "/usr/bin", "EN_RESTART": "all"}) == "all"
    (state / "installed-manifest.json").unlink()
    assert run({"PATH": "/usr/bin"}) == "first"
    (bundle / "manifest.json").write_text("not json")
    assert run({"PATH": "/usr/bin"}) == "unknown"


def test_library_map_matches_the_services_imports():
    """battery_soc_core is imported by battery_soc only; every service imports
    energy_node_common. A new importer must be added to LIBRARY_USERS."""
    importers = {"battery_soc_core": set(), "energy_node_common": set()}
    for path in (REPO / "services").glob("*/*.py"):
        text = path.read_text(encoding="utf-8")
        for lib in importers:
            if re.search(r"^\s*(from|import)\s+%s\b" % lib, text, re.M):
                importers[lib].add(path.parent.name)
    assert importers["battery_soc_core"] == restart_rule.LIBRARY_USERS["battery_soc_core"]
    assert restart_rule.LIBRARY_USERS["energy_node_common"] is None, "every service restarts for it"
    assert importers["energy_node_common"], "no service imports energy_node_common any more?"


def test_service_steps_rerun_exactly_for_their_libraries():
    """A service step's fingerprint names the library versions the restart rule
    looks at, and not wheels/: a new third-party wheel (certifi) must not run
    every service step again when none of them would restart."""
    services = {}
    for script in (REPO / "scripts" / "bootstrap").glob("8*-*.sh"):
        text = script.read_text(encoding="utf-8")
        inputs = " ".join(line.split(":", 1)[1] for line in text.splitlines() if line.startswith("# step-inputs:")).split()
        directory = next(item.split("/")[1] for item in inputs if item.startswith("services/"))
        assert "wheels/" not in inputs, script.name
        services[directory] = {item.rsplit(".", 1)[1] for item in inputs if item.startswith("manifest:components.")}
    for directory, named in services.items():
        wanted = {lib for lib, users in restart_rule.LIBRARY_USERS.items() if users is None or directory in users}
        assert named == wanted, directory
