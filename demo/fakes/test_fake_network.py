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
from fake_network import SECTIONS, WERKSTATT_UUID  # noqa: E402

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


def run(verb, args, env, paths, now=NOW):
    return fake_network.run(verb, args, env, paths["staged"], paths["progress"], paths["links"], now=now)


def status(env, paths, now=NOW):
    return run("network-status", [], env, paths, now=now)[1]


LAN = {"lan": True, "wifi": True, "password_ok": True, "werkstatt": True}
WIFI = dict(LAN, lan=False)  # no cable, the module is there
NOWIFI = dict(LAN, wifi=False)
NOTHING = dict(LAN, lan=False, wifi=False)
REJECT = dict(LAN, password_ok=False)
NO_WERKSTATT = dict(LAN, werkstatt=False)


def with_links(paths, connected, *profiles):
    """Stores a connected network and saved profiles, like the state after a connect."""
    paths["links"].write_text(json.dumps({"profiles": [list(p) for p in profiles], "connected": connected}),
                              encoding="utf-8")


def werkstatt(paths):
    with_links(paths, "FritzBox Werkstatt", (WERKSTATT_UUID, "yes", "FritzBox Werkstatt"))


def test_every_combination_emits_all_sections_in_order():
    for env in (LAN, WIFI, NOWIFI, NOTHING):
        assert sections_of(fake_network.status_text(env, None, NOW)) == list(SECTIONS), env


def test_env_of_falls_back_to_the_defaults_for_broken_values():
    assert fake_network.env_of({"lan": False, "wifi": True}) == WIFI  # older state files
    for broken in ("lan", None, {"lan": "yes", "wifi": True}, {"lan": True}):
        assert fake_network.env_of(broken) == LAN, broken


def test_module_switch_off_has_empty_wifidev_and_no_hotspot():
    text = fake_network.status_text(NOTHING, None, NOW)
    assert body_of(text, "wifidev") == ""
    assert body_of(text, "wifi") == ""
    assert body_of(text, "hotspot").splitlines()[0] == "active no"


def test_cable_switch_drives_the_ethernet_device():
    assert "eth0:ethernet:connected:Kabel" in body_of(fake_network.status_text(LAN, None, NOW), "devices")
    assert "eth0:ethernet:unavailable:--" in body_of(fake_network.status_text(WIFI, None, NOW), "devices")


def test_non_current_wifi_lines_start_with_space_and_current_with_star(paths):
    werkstatt(paths)
    text = status(LAN, paths)
    rows = [line for line in body_of(text, "wifi").splitlines() if line]
    assert rows[0].startswith("*:FritzBox Werkstatt:")
    assert all(row.startswith(" :") for row in rows[1:])
    assert any(row.startswith(" :Gäste:") for row in rows)


def test_wifi_list_uses_the_draft_values():
    rows = wifi_rows(fake_network.status_text(LAN, None, NOW))
    assert rows["FritzBox Werkstatt"].split(":")[2:] == ["92", "5180 MHz", "36", "WPA2"]
    assert rows["Drucker-Setup"].endswith(":--")
    assert len(rows) == 6


# ---- the emergency hotspot follows the switches ---------------------------------

def hotspot_active(env, paths, settings=None):
    if settings:
        run("network-settings", settings, env, paths)
    return body_of(status(env, paths), "hotspot").splitlines()[0] == "active yes"


def test_hotspot_opens_without_cable_and_connection(paths):
    assert hotspot_active(WIFI, paths)
    text = status(WIFI, paths)
    assert "wlan0:wifi:connected:energy-node-hotspot" in body_of(text, "devices")
    assert "10.42.0.1" in body_of(text, "addr")


def test_hotspot_stays_closed_with_cable_or_connection_or_switch_off(paths):
    assert not hotspot_active(LAN, paths)
    assert not hotspot_active(WIFI, paths, ["parallel", "off"])
    run("network-settings", ["parallel", "on"], WIFI, paths)
    werkstatt(paths)
    assert not hotspot_active(WIFI, paths)


# ---- password rule ---------------------------------------------------------

def test_password_rule_matches_the_real_helper(paths):
    stage(paths, password="geheim1")  # 7 characters
    assert run("wifi-connect", [], LAN, paths)[0] == 65
    stage(paths, password="geheim123")  # 8 characters
    assert run("wifi-connect", [], LAN, paths)[0] == 0


def test_password_rule_accepts_64_hex_digits(paths):
    stage(paths, password="ab" * 32)
    assert run("wifi-connect", [], LAN, paths)[0] == 0


def test_short_falsch_is_rejected_as_invalid_request(paths):
    stage(paths, password="falsch")
    assert run("wifi-connect", [], LAN, paths)[0] == 65


def test_connect_rejects_non_ascii_password(paths):
    stage(paths, password="Passwört123")
    assert run("wifi-connect", [], LAN, paths)[0] == 65


# ---- connect ---------------------------------------------------------------

def test_connect_with_falsch_prefix_fails_with_rollback_and_hides_password(paths):
    stage(paths, password=FALSCH)
    assert run("wifi-connect", [], LAN, paths, now=NOW)[:2] == (0, "")
    assert not paths["staged"].exists()
    assert FALSCH not in paths["progress"].read_text(encoding="utf-8")

    later = status(LAN, paths, now=NOW + 5)
    switch = json.loads(body_of(later, "switch"))
    assert switch["result"] == "fail"
    assert switch["rolled_back"] is True
    assert switch["error"] == "auth"
    assert FALSCH not in later


def test_connect_succeeds_after_three_steps(paths):
    stage(paths, ssid="Gäste", password=GOOD_PASSWORD)
    assert run("wifi-connect", [], LAN, paths, now=NOW)[0] == 0

    midway = json.loads(body_of(status(LAN, paths, now=NOW + 2), "switch"))
    assert midway["result"] == ""
    assert [s["state"] for s in midway["steps"]] == ["done", "run", "wait"]

    done = json.loads(body_of(status(LAN, paths, now=NOW + 4), "switch"))
    assert done["result"] == "ok"
    assert done["rolled_back"] is False
    assert done["internet"] == "ok"
    assert done["address"] == "192.168.178.52"
    assert [s["state"] for s in done["steps"]] == ["done", "done", "done"]


def test_connect_while_running_exits_75(paths):
    stage(paths)
    assert run("wifi-connect", [], LAN, paths, now=NOW)[0] == 0
    stage(paths)
    assert run("wifi-connect", [], LAN, paths, now=NOW + 1)[0] == 75


def test_connect_without_staged_file_exits_65(paths):
    assert run("wifi-connect", [], LAN, paths, now=NOW)[0] == 65


def test_without_wifi_module_scan_and_connect_exit_1(paths):
    stage(paths)
    assert run("wifi-scan", [], NOWIFI, paths)[0] == 1
    assert run("wifi-connect", [], NOWIFI, paths)[0] == 1


# ---- success follows into the network state ----------------------------------

def test_successful_connect_connects_and_saves_the_network(paths):
    stage(paths, ssid="Gäste", password=GOOD_PASSWORD)
    run("wifi-connect", [], LAN, paths, now=NOW)

    during = status(LAN, paths, now=NOW + 2)
    assert "wlan0:wifi:disconnected:--" in body_of(during, "devices")
    assert not paths["links"].exists()

    after = status(LAN, paths, now=NOW + 4)
    rows = wifi_rows(after)
    assert rows["Gäste"].startswith("*:Gäste:")
    assert all(row.startswith(" :") for name, row in rows.items() if name != "Gäste")
    assert "wlan0:wifi:connected:Gäste" in body_of(after, "devices")
    profiles = body_of(after, "profiles").splitlines()
    assert any(line.endswith("\tyes\tGäste") for line in profiles)
    assert "eth0:ethernet:connected:Kabel" in body_of(after, "devices")
    assert '"metric":600' in body_of(after, "route")

    again = status(LAN, paths, now=NOW + 9)
    assert wifi_rows(again)["Gäste"].startswith("*:Gäste:")
    assert len([line for line in body_of(again, "profiles").splitlines() if "Gäste" in line]) == 1


def test_connect_without_cable_closes_the_hotspot(paths):
    assert hotspot_active(WIFI, paths)
    stage(paths, ssid="Gäste", password=GOOD_PASSWORD)
    run("wifi-connect", [], WIFI, paths, now=NOW)
    after = status(WIFI, paths, now=NOW + 4)
    assert json.loads(body_of(after, "switch"))["result"] == "ok"
    assert wifi_rows(after)["Gäste"].startswith("*:Gäste:")
    assert body_of(after, "hotspot").splitlines()[0] == "active no"


# ---- forget ----------------------------------------------------------------

def test_forget_drops_the_profile_from_the_state(paths):
    werkstatt(paths)
    assert "FritzBox Werkstatt" in wifi_rows(status(LAN, paths))
    assert run("wifi-forget", [WERKSTATT_UUID], LAN, paths)[:2] == (0, "")

    after = status(LAN, paths)
    assert WERKSTATT_UUID not in body_of(after, "profiles")
    assert "*:" not in body_of(after, "wifi")  # the forgotten network was the connected one
    assert "wlan0:wifi:disconnected:--" in body_of(after, "devices")


def test_forget_twice_exits_65(paths):
    werkstatt(paths)
    assert run("wifi-forget", [WERKSTATT_UUID], LAN, paths)[0] == 0
    assert run("wifi-forget", [WERKSTATT_UUID], LAN, paths)[0] == 65


def test_forget_checks_uuid_format_and_known_profile(paths):
    werkstatt(paths)
    assert run("wifi-forget", ["nicht-eine-uuid"], LAN, paths)[0] == 65
    assert run("wifi-forget", ["00000000-0000-4000-8000-000000000000"], LAN, paths)[0] == 65


def test_forget_then_connect_again_saves_the_network_again(paths):
    werkstatt(paths)
    run("wifi-forget", [WERKSTATT_UUID], LAN, paths)
    stage(paths, ssid="FritzBox Werkstatt", password=GOOD_PASSWORD)
    run("wifi-connect", [], LAN, paths, now=NOW)
    after = status(LAN, paths, now=NOW + 4)
    assert "FritzBox Werkstatt" in body_of(after, "profiles")
    assert wifi_rows(after)["FritzBox Werkstatt"].startswith("*:")


def test_wrong_argument_count_exits_64(paths):
    assert run("network-status", ["x"], LAN, paths)[0] == 64
    assert run("wifi-forget", [], LAN, paths)[0] == 64
    assert run("wifi-connect", ["x"], LAN, paths)[0] == 64


def test_fake_output_matches_go_testdata(paths):
    assert (TESTDATA / "fake_lan.txt").read_text(encoding="utf-8") == fake_network.status_text(LAN, None, NOW)
    werkstatt(paths)
    text = fake_network.status_text(WIFI, None, NOW, fake_network.load_links(paths["links"]))
    assert (TESTDATA / "fake_wifi.txt").read_text(encoding="utf-8") == text


def test_helper_passes_network_verbs_to_the_fake(tmp_path):
    state = tmp_path / "fake-system.json"
    state.write_text(json.dumps({"network": WIFI}), encoding="utf-8")
    env = {"PATH": "", "FAKE_SYSTEM_STATE": str(state)}
    result = subprocess.run([sys.executable, HELPER, "network-status"], env=env, capture_output=True, text=True)
    assert result.returncode == 0
    assert sections_of(result.stdout) == list(SECTIONS)
    assert body_of(result.stdout, "managed").strip() == "yes"
    assert "eth0:ethernet:unavailable:--" in body_of(result.stdout, "devices")
    assert "wlan0:wifi:connected" in body_of(result.stdout, "devices")


def make_tab(tmp_path):
    from console_system import SystemTab
    from fake_console import Log

    epoch = tmp_path / "epoch"
    epoch.write_text(str(time.time()))
    return SystemTab(Log(), tmp_path / "fake-system.json", tmp_path / "events.jsonl", epoch)


def test_console_switches_are_stored_one_at_a_time(tmp_path):
    from console_system_ui import system_html
    from fake_journal_helper import load_state

    tab = make_tab(tmp_path)
    assert tab.state()["network"] == LAN
    assert tab.act("network", {"lan": False})["network"] == WIFI
    assert load_state(tmp_path / "fake-system.json")["network"] == WIFI
    assert tab.act("network", {"wifi": False})["network"] == NOTHING
    assert tab.act("network", {"lan": True, "wifi": True})["network"] == LAN
    html = system_html({})
    assert 'data-network-lan' in html and 'data-network-wifi' in html


def test_console_rejects_invalid_network_bodies(tmp_path):
    tab = make_tab(tmp_path)
    for body in ({}, {"lan": "ja"}, {"wifi": 1}, {"scenario": "wifi"}):
        with pytest.raises(ValueError):
            tab.act("network", body)
    assert tab.state()["network"] == LAN


def test_console_switch_keeps_saved_networks_and_the_last_switch(tmp_path):
    tab = make_tab(tmp_path)
    _, progress, links = fake_network.paths_for(str(tmp_path / "fake-system.json"))
    progress.write_text('{"started_at": "2026-11-12T13:02:40Z", "_fake": {"fail": true}}')
    links.write_text('{"profiles": [], "connected": null}')
    tab.act("network", {"lan": False})
    assert progress.exists() and links.exists()


def test_helper_follows_the_console_switches(tmp_path, monkeypatch, capsys):
    import fake_journal_helper

    tab = make_tab(tmp_path)
    monkeypatch.setenv("FAKE_SYSTEM_STATE", str(tmp_path / "fake-system.json"))
    tab.act("network", {"lan": False})
    assert fake_journal_helper.main(["network-status"]) == 0
    out = capsys.readouterr().out
    assert body_of(out, "hotspot").splitlines()[0] == "active yes"
    tab.act("network", {"lan": True})
    assert fake_journal_helper.main(["network-status"]) == 0
    assert body_of(capsys.readouterr().out, "hotspot").splitlines()[0] == "active no"


# ---- network-settings and hotspot-secret ------------------------------------

def settings_of(text):
    return json.loads(body_of(text, "settings"))


def test_default_settings_are_parallel_and_hotspot_on(paths):
    assert settings_of(status(LAN, paths)) == {"fallback": "parallel", "hotspot": True}


def test_network_settings_standby_off_shows_in_status(paths):
    assert run("network-settings", ["standby", "off"], LAN, paths)[:2] == (0, "")
    assert settings_of(status(LAN, paths)) == {"fallback": "standby", "hotspot": False}
    stored = json.loads(paths["links"].with_name("network-settings.json").read_text(encoding="utf-8"))
    assert stored == {"fallback": "standby", "hotspot": False}


def test_network_settings_rejects_unknown_values_and_keeps_the_stored_ones(paths):
    run("network-settings", ["standby", "off"], LAN, paths)
    for args in (["fast", "on"], ["parallel", "maybe"], ["Standby", "off"], ["parallel"],
                 ["parallel", "on", "extra"], []):
        assert run("network-settings", args, LAN, paths)[0] == 64, args
    assert settings_of(status(LAN, paths)) == {"fallback": "standby", "hotspot": False}


def test_network_settings_without_state_file_exits_1(paths):
    assert run_without_state("network-settings", ["parallel", "on"]) == 1


def test_hotspot_secret_prints_ssid_and_password_on_two_lines(paths):
    code, out = run("hotspot-secret", [], LAN, paths)
    assert code == 0
    assert out == "EnergyNode-4F2A\nNotfall2026Demo\n"


def test_hotspot_secret_with_arguments_exits_64(paths):
    assert run("hotspot-secret", ["x"], LAN, paths)[0] == 64


def test_hotspot_secret_without_state_file_exits_66():
    assert run_without_state("hotspot-secret", []) == 66


def test_status_never_contains_the_hotspot_password(paths):
    run("hotspot-secret", [], WIFI, paths)
    for env in (LAN, WIFI, NOWIFI, NOTHING):
        assert "Notfall2026Demo" not in status(env, paths)


def test_hotspot_secret_is_answered_whatever_the_switches_say(paths):
    run("network-settings", ["parallel", "off"], WIFI, paths)
    for env in (LAN, WIFI, NOTHING):
        assert run("hotspot-secret", [], env, paths)[1] == "EnergyNode-4F2A\nNotfall2026Demo\n"


def run_without_state(verb, args):
    return fake_network.run(verb, args, LAN, None, None, None, now=NOW)[0]


def test_helper_answers_hotspot_secret_with_66_without_state(tmp_path):
    env = {"PATH": ""}
    result = subprocess.run([sys.executable, HELPER, "hotspot-secret"], env=env, capture_output=True, text=True)
    assert result.returncode == 66
    assert result.stdout == ""


# ---- the wifi login rejects, a network vanishes ---------------------------------

def test_rejecting_login_fails_even_with_a_good_password(paths):
    stage(paths, ssid="Gäste", password=GOOD_PASSWORD)
    assert run("wifi-connect", [], REJECT, paths, now=NOW)[0] == 0
    switch = json.loads(body_of(status(REJECT, paths, now=NOW + 5), "switch"))
    assert (switch["result"], switch["error"], switch["rolled_back"]) == ("fail", "auth", True)
    assert switch["steps"][0]["detail"] == "Passwort abgelehnt"
    assert "*:" not in body_of(status(REJECT, paths, now=NOW + 9), "wifi")


def test_login_accepts_again_after_switching_back(paths):
    stage(paths)
    run("wifi-connect", [], REJECT, paths, now=NOW)
    stage(paths)
    assert run("wifi-connect", [], LAN, paths, now=NOW + 6)[0] == 0
    assert json.loads(body_of(status(LAN, paths, now=NOW + 12), "switch"))["result"] == "ok"


def test_unreachable_werkstatt_leaves_the_scan_list(paths):
    assert "FritzBox Werkstatt" not in wifi_rows(status(NO_WERKSTATT, paths))
    assert "FritzBox Werkstatt 2.4" in wifi_rows(status(NO_WERKSTATT, paths))
    assert len(wifi_rows(status(NO_WERKSTATT, paths))) == 5


def test_unreachable_werkstatt_drops_the_connection_but_keeps_the_profile(paths):
    werkstatt(paths)
    text = status(NO_WERKSTATT, paths)
    assert "wlan0:wifi:disconnected:--" in body_of(text, "devices")
    assert WERKSTATT_UUID in body_of(text, "profiles")
    assert "wlan0:wifi:connected:FritzBox Werkstatt" in body_of(status(LAN, paths), "devices")


def test_unreachable_werkstatt_lets_the_hotspot_open_without_cable(paths):
    werkstatt(paths)
    assert not hotspot_active(WIFI, paths)
    assert hotspot_active(dict(WIFI, werkstatt=False), paths)


def test_connect_to_the_vanished_werkstatt_fails_with_not_found(paths):
    stage(paths, ssid="FritzBox Werkstatt", password=GOOD_PASSWORD)
    run("wifi-connect", [], NO_WERKSTATT, paths, now=NOW)
    switch = json.loads(body_of(status(NO_WERKSTATT, paths, now=NOW + 5), "switch"))
    assert (switch["result"], switch["error"]) == ("fail", "not_found")
    assert switch["steps"][0]["detail"] == "Netz nicht gefunden"


def test_console_stores_login_and_werkstatt_switches(tmp_path):
    tab = make_tab(tmp_path)
    assert tab.act("network", {"password_ok": False})["network"] == REJECT
    assert tab.act("network", {"password_ok": True, "werkstatt": False})["network"] == NO_WERKSTATT


# ---- the fallback mode decides whether Wi-Fi stays connected beside the cable ----

def test_parallel_keeps_wifi_connected_beside_the_cable(paths):
    werkstatt(paths)
    text = status(LAN, paths)
    assert "wlan0:wifi:connected:FritzBox Werkstatt" in body_of(text, "devices")
    assert "192.168.178.52" in body_of(text, "addr")
    assert wifi_rows(text)["FritzBox Werkstatt"].startswith("*:")


def test_standby_leaves_wifi_unconnected_while_the_cable_is_up(paths):
    werkstatt(paths)
    run("network-settings", ["standby", "on"], LAN, paths)
    text = status(LAN, paths)
    assert "wlan0:wifi:disconnected:--" in body_of(text, "devices")
    assert "192.168.178.52" not in body_of(text, "addr")
    assert '"dev":"wlan0"' not in body_of(text, "route")
    assert "*:" not in body_of(text, "wifi")
    assert WERKSTATT_UUID in body_of(text, "profiles")


def test_standby_connects_the_saved_network_when_the_cable_is_gone(paths):
    werkstatt(paths)
    run("network-settings", ["standby", "on"], WIFI, paths)
    text = status(WIFI, paths)
    assert "wlan0:wifi:connected:FritzBox Werkstatt" in body_of(text, "devices")
    assert body_of(text, "hotspot").splitlines()[0] == "active no"
