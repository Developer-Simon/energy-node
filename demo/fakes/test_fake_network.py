"""Tests for the demo fake of the network helper verbs (fake_network.py)."""

import json
import re
import subprocess
import sys
import time
from pathlib import Path

import pytest

sys.path.insert(0, str(Path(__file__).resolve().parent))

import fake_network  # noqa: E402
from fake_network import SCENARIOS, SECTIONS, WERKSTATT_UUID  # noqa: E402

FAKES = Path(__file__).resolve().parent
TESTDATA = FAKES.parent.parent / "dashboard" / "internal" / "netmgr" / "testdata"
HELPER = str(FAKES / "fake_journal_helper.py")
NOW = 1_800_000_000.0  # fixed clock for the time based switch
GOOD_PASSWORD = "geheim123"
FALSCH = "falschesPasswort"


def sections_of(text):
    return re.findall(r"^##(\w+)$", text, flags=re.MULTILINE)


def body_of(text, name):
    parts = re.split(r"^##(\w+)\n", text, flags=re.MULTILINE)
    for i in range(1, len(parts), 2):
        if parts[i] == name:
            return parts[i + 1]
    raise AssertionError(f"no section {name}")


def wifi_rows(text):
    return {row.split(":")[1]: row for row in body_of(text, "wifi").splitlines() if row}


@pytest.fixture
def paths(tmp_path):
    (tmp_path / "data").mkdir()
    staged, progress, links = fake_network.paths_for(str(tmp_path / "fake-system.json"))
    return {"staged": staged, "progress": progress, "links": links}


def stage(paths, **request):
    body = {"ssid": "Gäste", "password": GOOD_PASSWORD, "profile_uuid": "", "hidden": False, "security": "WPA2"}
    body.update(request)
    paths["staged"].write_text(json.dumps(body), encoding="utf-8")


def run(verb, args, scenario, paths, now=NOW):
    return fake_network.run(verb, args, scenario, paths["staged"], paths["progress"], paths["links"], now=now)


def status(scenario, paths, now=NOW):
    return run("network-status", [], scenario, paths, now=now)[1]


def test_every_scenario_emits_all_sections_in_order():
    assert set(SCENARIOS) == {"lan", "both", "wifi", "error", "hotspot", "nowifi", "unmanaged"}
    for scenario in SCENARIOS:
        text = fake_network.status_text(scenario, None, NOW)
        assert sections_of(text) == list(SECTIONS), scenario


def test_nowifi_has_empty_wifidev():
    text = fake_network.status_text("nowifi", None, NOW)
    assert body_of(text, "wifidev") == ""
    assert body_of(text, "wifi") == ""


def test_unmanaged_reports_managed_no():
    text = fake_network.status_text("unmanaged", None, NOW)
    assert body_of(text, "managed").strip() == "no"
    assert body_of(text, "devices") == ""
    assert body_of(text, "profiles") == ""


def test_non_current_wifi_lines_start_with_space_and_current_with_star():
    text = fake_network.status_text("both", None, NOW)
    rows = [line for line in body_of(text, "wifi").splitlines() if line]
    assert rows[0].startswith("*:FritzBox Werkstatt:")
    assert all(row.startswith(" :") for row in rows[1:])
    assert any(row.startswith(" :Gäste:") for row in rows)


def test_wifi_list_uses_the_draft_values():
    rows = wifi_rows(fake_network.status_text("lan", None, NOW))
    assert rows["FritzBox Werkstatt"].split(":")[2:] == ["92", "5180 MHz", "36", "WPA2"]
    assert rows["Drucker-Setup"].endswith(":--")
    assert len(rows) == 6


def test_error_scenario_shows_static_auth_failure():
    switch = json.loads(body_of(fake_network.status_text("error", None, NOW), "switch"))
    assert switch["result"] == "fail"
    assert switch["error"] == "auth"
    assert switch["ssid"] == "FritzBox Werkstatt"
    assert switch["rolled_back"] is True


# ---- password rule ---------------------------------------------------------

def test_password_rule_matches_the_real_helper(paths):
    stage(paths, password="geheim1")  # 7 characters
    assert run("wifi-connect", [], "lan", paths)[0] == 65
    stage(paths, password="geheim123")  # 8 characters
    assert run("wifi-connect", [], "lan", paths)[0] == 0


def test_password_rule_accepts_64_hex_digits(paths):
    stage(paths, password="ab" * 32)
    assert run("wifi-connect", [], "lan", paths)[0] == 0


def test_short_falsch_is_rejected_as_invalid_request(paths):
    stage(paths, password="falsch")
    assert run("wifi-connect", [], "lan", paths)[0] == 65


def test_connect_rejects_non_ascii_password(paths):
    stage(paths, password="Passwört123")
    assert run("wifi-connect", [], "lan", paths)[0] == 65


# ---- connect ---------------------------------------------------------------

def test_connect_with_falsch_prefix_fails_with_rollback_and_hides_password(paths):
    stage(paths, password=FALSCH)
    assert run("wifi-connect", [], "lan", paths, now=NOW)[:2] == (0, "")
    assert not paths["staged"].exists()
    assert FALSCH not in paths["progress"].read_text(encoding="utf-8")

    later = status("lan", paths, now=NOW + 5)
    switch = json.loads(body_of(later, "switch"))
    assert switch["result"] == "fail"
    assert switch["rolled_back"] is True
    assert switch["error"] == "auth"
    assert FALSCH not in later


def test_connect_succeeds_after_three_steps(paths):
    stage(paths, ssid="Gäste", password=GOOD_PASSWORD)
    assert run("wifi-connect", [], "lan", paths, now=NOW)[0] == 0

    midway = json.loads(body_of(status("lan", paths, now=NOW + 2), "switch"))
    assert midway["result"] == ""
    assert [s["state"] for s in midway["steps"]] == ["done", "run", "wait"]

    done = json.loads(body_of(status("lan", paths, now=NOW + 4), "switch"))
    assert done["result"] == "ok"
    assert done["rolled_back"] is False
    assert done["internet"] == "ok"
    assert done["address"] == "192.168.178.52"
    assert [s["state"] for s in done["steps"]] == ["done", "done", "done"]


def test_connect_error_scenario_fails_even_with_good_password(paths):
    stage(paths, ssid="FritzBox Werkstatt", password=GOOD_PASSWORD)
    assert run("wifi-connect", [], "error", paths, now=NOW)[0] == 0
    switch = json.loads(body_of(status("error", paths, now=NOW + 5), "switch"))
    assert switch["result"] == "fail"


def test_connect_while_running_exits_75(paths):
    stage(paths)
    assert run("wifi-connect", [], "lan", paths, now=NOW)[0] == 0
    stage(paths)
    assert run("wifi-connect", [], "lan", paths, now=NOW + 1)[0] == 75


def test_connect_without_staged_file_exits_65(paths):
    assert run("wifi-connect", [], "lan", paths, now=NOW)[0] == 65


def test_unmanaged_scan_and_connect_exit_69(paths):
    stage(paths)
    assert run("wifi-scan", [], "unmanaged", paths)[0] == 69
    assert run("wifi-connect", [], "unmanaged", paths)[0] == 69


# ---- success follows into the network state ----------------------------------

def test_successful_connect_connects_and_saves_the_network(paths):
    stage(paths, ssid="Gäste", password=GOOD_PASSWORD)
    run("wifi-connect", [], "lan", paths, now=NOW)

    during = status("lan", paths, now=NOW + 2)
    assert "wlan0:wifi:disconnected:--" in body_of(during, "devices")
    assert not paths["links"].exists()

    after = status("lan", paths, now=NOW + 4)
    rows = wifi_rows(after)
    assert rows["Gäste"].startswith("*:Gäste:")
    assert all(row.startswith(" :") for name, row in rows.items() if name != "Gäste")
    assert "wlan0:wifi:connected:Gäste" in body_of(after, "devices")
    profiles = body_of(after, "profiles").splitlines()
    assert any(line.endswith("\tyes\tGäste") for line in profiles)
    assert "eth0:ethernet:connected:Kabel" in body_of(after, "devices")
    assert '"metric":600' in body_of(after, "route")

    again = status("lan", paths, now=NOW + 9)
    assert wifi_rows(again)["Gäste"].startswith("*:Gäste:")
    assert len([line for line in body_of(again, "profiles").splitlines() if "Gäste" in line]) == 1


def test_connect_on_the_wifi_scenario_keeps_the_switch_result_ok(paths):
    stage(paths, ssid="Gäste", password=GOOD_PASSWORD)
    run("wifi-connect", [], "wifi", paths, now=NOW)
    after = status("wifi", paths, now=NOW + 4)
    assert json.loads(body_of(after, "switch"))["result"] == "ok"
    assert wifi_rows(after)["Gäste"].startswith("*:Gäste:")


# ---- forget ----------------------------------------------------------------

def test_forget_drops_the_profile_from_the_state(paths):
    assert "FritzBox Werkstatt" in wifi_rows(status("both", paths))
    assert run("wifi-forget", [WERKSTATT_UUID], "both", paths)[:2] == (0, "")

    after = status("both", paths)
    assert WERKSTATT_UUID not in body_of(after, "profiles")
    assert "*:" not in body_of(after, "wifi")  # the forgotten network was the connected one
    assert "wlan0:wifi:disconnected:--" in body_of(after, "devices")


def test_forget_twice_exits_65(paths):
    assert run("wifi-forget", [WERKSTATT_UUID], "both", paths)[0] == 0
    assert run("wifi-forget", [WERKSTATT_UUID], "both", paths)[0] == 65


def test_forget_checks_uuid_format_and_known_profile(paths):
    assert run("wifi-forget", ["nicht-eine-uuid"], "both", paths)[0] == 65
    assert run("wifi-forget", ["00000000-0000-4000-8000-000000000000"], "both", paths)[0] == 65


def test_forget_then_connect_again_saves_the_network_again(paths):
    run("wifi-forget", [WERKSTATT_UUID], "both", paths)
    stage(paths, ssid="FritzBox Werkstatt", password=GOOD_PASSWORD)
    run("wifi-connect", [], "both", paths, now=NOW)
    after = status("both", paths, now=NOW + 4)
    assert "FritzBox Werkstatt" in body_of(after, "profiles")
    assert wifi_rows(after)["FritzBox Werkstatt"].startswith("*:")


def test_wrong_argument_count_exits_64(paths):
    assert run("network-status", ["x"], "lan", paths)[0] == 64
    assert run("wifi-forget", [], "both", paths)[0] == 64
    assert run("wifi-connect", ["x"], "lan", paths)[0] == 64


def test_fake_output_matches_go_testdata():
    expected = {"fake_lan": "lan", "fake_wifi": "wifi", "fake_error": "error"}
    for name, scenario in expected.items():
        text = fake_network.status_text(scenario, None, NOW)
        assert (TESTDATA / f"{name}.txt").read_text(encoding="utf-8") == text, name


def test_helper_passes_network_verbs_to_the_fake(tmp_path):
    state = tmp_path / "fake-system.json"
    state.write_text(json.dumps({"network": "wifi"}), encoding="utf-8")
    env = {"PATH": "", "FAKE_SYSTEM_STATE": str(state)}
    result = subprocess.run([sys.executable, HELPER, "network-status"], env=env, capture_output=True, text=True)
    assert result.returncode == 0
    assert sections_of(result.stdout) == list(SECTIONS)
    assert body_of(result.stdout, "managed").strip() == "yes"
    assert "wlan0:wifi:connected" in body_of(result.stdout, "devices")


def test_console_scenario_choice_is_stored_and_drops_old_switch_and_links(tmp_path):
    from console_system import SystemTab
    from console_system_ui import system_html
    from fake_console import Log
    from fake_journal_helper import load_state

    state = tmp_path / "fake-system.json"
    epoch = tmp_path / "epoch"
    epoch.write_text(str(time.time()))
    tab = SystemTab(Log(), state, tmp_path / "events.jsonl", epoch)
    _, progress, links = fake_network.paths_for(str(state))
    progress.write_text('{"started_at": "2026-11-12T13:02:40Z", "_fake": {"fail": true}}')
    links.write_text('{"profiles": [], "connected": null}')

    result = tab.act("network", {"scenario": "wifi"})

    assert load_state(state)["network"] == "wifi"
    assert result["network"] == "wifi"
    assert not progress.exists()
    assert not links.exists()
    with pytest.raises(ValueError):
        tab.act("network", {"scenario": "quatsch"})
    html = system_html({})
    assert 'data-network' in html
    assert '<option value="unmanaged">Ohne NetworkManager</option>' in html


# ---- scenario feedback: a successful connect and a forget switch the scenario --

def scenario_after_connect(scenario, paths, ssid="Gäste", password=GOOD_PASSWORD):
    stage(paths, ssid=ssid, password=password)
    run("wifi-connect", [], scenario, paths, now=NOW)
    return run("network-status", [], scenario, paths, now=NOW + 4)[2]


def test_connect_ok_in_lan_switches_the_scenario_to_both(paths):
    assert scenario_after_connect("lan", paths) == "both"


def test_connect_ok_in_hotspot_switches_the_scenario_to_wifi(paths):
    assert scenario_after_connect("hotspot", paths, ssid="Gäste") == "wifi"


def test_connect_ok_keeps_other_scenarios(paths):
    assert scenario_after_connect("wifi", paths) == "wifi"
    assert scenario_after_connect("both", paths) == "both"


def test_failing_connect_keeps_the_scenario(paths):
    stage(paths, ssid="Gäste", password=FALSCH)
    run("wifi-connect", [], "lan", paths, now=NOW)
    code, out, scenario = run("network-status", [], "lan", paths, now=NOW + 4)
    assert json.loads(body_of(out, "switch"))["result"] == "fail"
    assert scenario == "lan"


def test_scenario_switch_keeps_the_links_overlay(paths):
    scenario_after_connect("lan", paths)
    doc = json.loads(paths["links"].read_text(encoding="utf-8"))
    assert doc["connected"] == "Gäste"
    assert any(p[2] == "Gäste" for p in doc["profiles"])


def test_switch_is_reported_once(paths):
    scenario_after_connect("lan", paths)
    assert run("network-status", [], "both", paths, now=NOW + 9)[2] == "both"


def test_forget_of_the_connected_network_in_both_switches_to_lan(paths):
    code, out, scenario = run("wifi-forget", [WERKSTATT_UUID], "both", paths)
    assert (code, out, scenario) == (0, "", "lan")


def test_forget_of_another_network_in_both_keeps_both(paths):
    stage(paths, ssid="Gäste", password=GOOD_PASSWORD)
    run("wifi-connect", [], "both", paths, now=NOW)
    assert run("network-status", [], "both", paths, now=NOW + 4)[2] == "both"
    code, _, scenario = run("wifi-forget", [WERKSTATT_UUID], "both", paths)
    assert (code, scenario) == (0, "both")


def test_helper_writes_the_switched_scenario_into_the_state_and_console_shows_it(tmp_path, monkeypatch, capsys):
    import fake_journal_helper
    from console_system import SystemTab
    from fake_console import Log

    state = tmp_path / "fake-system.json"
    state.write_text(json.dumps({"network": "lan"}), encoding="utf-8")
    epoch = tmp_path / "epoch"
    epoch.write_text(str(time.time()))
    (tmp_path / "data").mkdir()
    staged, progress, links = fake_network.paths_for(str(state))
    progress.write_text(json.dumps({
        "ssid": "Gäste", "started_at": "2020-01-01T00:00:00Z",
        "steps": [], "internet": "unknown", "result": "", "error": "", "address": "",
        "rolled_back": False, "finished_at": "", "_fake": {"fail": False, "applied": False}}),
        encoding="utf-8")
    monkeypatch.setenv("FAKE_SYSTEM_STATE", str(state))
    monkeypatch.setenv("FAKE_JOURNAL_EPOCH", str(epoch))

    assert fake_journal_helper.main(["network-status"]) == 0

    assert json.loads(state.read_text(encoding="utf-8"))["network"] == "both"
    assert json.loads(links.read_text(encoding="utf-8"))["connected"] == "Gäste"
    tab = SystemTab(Log(), state, tmp_path / "events.jsonl", epoch)
    assert tab.state()["network"] == "both"
    assert "wlan0:wifi:connected:Gäste" in body_of(capsys.readouterr().out, "devices")


# ---- network-settings and hotspot-secret ------------------------------------

def settings_of(text):
    return json.loads(body_of(text, "settings"))


def test_default_settings_are_parallel_and_hotspot_on(paths):
    assert settings_of(status("lan", paths)) == {"fallback": "parallel", "hotspot": True}


def test_network_settings_standby_off_shows_in_status(paths):
    assert run("network-settings", ["standby", "off"], "lan", paths)[:2] == (0, "")
    assert settings_of(status("lan", paths)) == {"fallback": "standby", "hotspot": False}
    stored = json.loads(paths["links"].with_name("network-settings.json").read_text(encoding="utf-8"))
    assert stored == {"fallback": "standby", "hotspot": False}


def test_network_settings_keeps_the_scenario(paths):
    assert run("network-settings", ["parallel", "on"], "both", paths)[2] == "both"


def test_network_settings_rejects_unknown_values_and_keeps_the_stored_ones(paths):
    run("network-settings", ["standby", "off"], "lan", paths)
    for args in (["fast", "on"], ["parallel", "maybe"], ["Standby", "off"], ["parallel"],
                 ["parallel", "on", "extra"], []):
        assert run("network-settings", args, "lan", paths)[0] == 64, args
    assert settings_of(status("lan", paths)) == {"fallback": "standby", "hotspot": False}


def test_network_settings_without_state_file_exits_1(paths):
    assert run_without_state("network-settings", ["parallel", "on"]) == 1


def test_hotspot_secret_prints_ssid_and_password_on_two_lines(paths):
    code, out, scenario = run("hotspot-secret", [], "lan", paths)
    assert (code, scenario) == (0, "lan")
    assert out == "EnergyNode-4F2A\nNotfall2026Demo\n"


def test_hotspot_secret_with_arguments_exits_64(paths):
    assert run("hotspot-secret", ["x"], "lan", paths)[0] == 64


def test_hotspot_secret_without_state_file_exits_66():
    assert run_without_state("hotspot-secret", []) == 66


def test_status_never_contains_the_hotspot_password(paths):
    run("hotspot-secret", [], "hotspot", paths)
    for scenario in SCENARIOS:
        assert "Notfall2026Demo" not in status(scenario, paths)


def test_hotspot_scenario_reports_an_active_hotspot():
    assert body_of(fake_network.status_text("hotspot", None, NOW), "hotspot").splitlines()[0] == "active yes"


def run_without_state(verb, args):
    return fake_network.run(verb, args, "lan", None, None, None, now=NOW)[0]


def test_helper_answers_hotspot_secret_with_66_without_state(tmp_path):
    env = {"PATH": ""}
    result = subprocess.run([sys.executable, HELPER, "hotspot-secret"], env=env, capture_output=True, text=True)
    assert result.returncode == 66
    assert result.stdout == ""
