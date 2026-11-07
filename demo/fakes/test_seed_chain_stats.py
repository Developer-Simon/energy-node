"""Tests for the chain statistics seed: the Go file format, the profile and reruns."""

import json
import subprocess
import sys
from datetime import datetime, timezone
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "lib"))

import seed_chain_stats  # noqa: E402

SEED_SCRIPT = str(Path(__file__).resolve().parents[1] / "lib" / "seed_chain_stats.py")
DEMO_ENERGY = Path(__file__).resolve().parents[1] / "fixtures" / "seed" / "demo" / "energy.json"
NOW = "2026-11-07T12:00:00Z"

# Golden strings written by encodeChainMonth in dashboard/internal/energystats
# (chain_store_test.go and a temporary test with a standby entry, same input).
GO_PAIRS_OCT = (
    '{"version":1,"start":"2026-10-01T00:00:00Z","pairs":[{"source":"a","target":"b","hours":{"10":[1,0.9,3600]}},'
    '{"source":"b","target":"a","hours":{"10":[0.3,0.2,3600]}}],"standby":[]}'
)
GO_MIXED_NOV = (
    '{"version":1,"start":"2026-11-01T00:00:00Z","pairs":[{"source":"a","target":"b","hours":{"10":[1,0.9,3600],'
    '"2":[0.000001,0.123457,3600]}}],"standby":[{"entity":"speicher_plug_power","hours":{"100":[12.5,3600],'
    '"5":[0.005,3600]}}]}'
)


def run_seed(data_dir, now=NOW):
    return subprocess.run(
        [sys.executable, SEED_SCRIPT, str(data_dir), now], capture_output=True, text=True
    )


def make_data_dir(tmp_path):
    data = tmp_path / "data"
    data.mkdir()
    (data / "energy.json").write_text(DEMO_ENERGY.read_text(encoding="utf-8"), encoding="utf-8")
    return data


def chain_dir(data):
    return data / "energystats" / "chains"


def test_encoder_matches_go_october_fixture():
    got = seed_chain_stats.encode_month(
        datetime(2026, 10, 1, tzinfo=timezone.utc),
        {("a", "b"): {10: (1, 0.9, 3600)}, ("b", "a"): {10: (0.3, 0.2, 3600)}},
        {},
    )
    assert got.decode("utf-8") == GO_PAIRS_OCT


def test_encoder_matches_go_mixed_fixture():
    got = seed_chain_stats.encode_month(
        datetime(2026, 11, 1, tzinfo=timezone.utc),
        {("a", "b"): {10: (1, 0.9, 3600), 2: (0.000001, 0.123457, 3600)}},
        {"speicher_plug_power": {5: (0.005, 3600), 100: (12.5, 3600)}},
    )
    assert got.decode("utf-8") == GO_MIXED_NOV


def test_seed_writes_fourteen_days_of_both_months(tmp_path):
    data = make_data_dir(tmp_path)
    result = run_seed(data)
    assert result.returncode == 0, result.stderr
    files = sorted(p.name for p in chain_dir(data).iterdir())
    assert files == ["2026-10.json", "2026-11.json"]
    hours = 0
    for name in files:
        month = json.loads((chain_dir(data) / name).read_text(encoding="utf-8"))
        assert month["version"] == 1
        assert month["start"] == name.replace(".json", "") + "-01T00:00:00Z"
        for entry in month["pairs"]:
            assert set(entry) == {"source", "target", "hours"}
        standby_hours = {k for e in month["standby"] for k in e["hours"]}
        pair_hours = {k for e in month["pairs"] for k in e["hours"]}
        hours += len(standby_hours | pair_hours)
    assert hours == 14 * 24


def test_profile_charges_by_day_discharges_in_evening_and_standby_at_night(tmp_path):
    data = make_data_dir(tmp_path)
    assert run_seed(data).returncode == 0
    month = json.loads((chain_dir(data) / "2026-11.json").read_text(encoding="utf-8"))
    # Hour index 0 is 2026-11-01 00:00 UTC. 2026-11-06 is day 5, the window
    # ends at 2026-11-07 12:00 (exclusive), so day 5 is fully inside it.
    noon = str(5 * 24 + 12)
    evening = str(5 * 24 + 20)
    night = str(5 * 24 + 2)
    pairs = {(e["source"], e["target"]): e["hours"] for e in month["pairs"]}
    # Charging runs with the chain order, the battery is fed.
    assert pairs[("speicher_plug_power", "speicher_umrichter_power")][noon][0] > 0
    # Discharging runs against it, the battery is the source in the evening.
    assert pairs[("speicher_akku_power", "speicher_umrichter_power")][evening][0] > 0
    assert noon not in pairs.get(("speicher_akku_power", "speicher_umrichter_power"), {})
    # Standby at night: only the standby entry of the first point, no pairs.
    assert night not in pairs.get(("speicher_plug_power", "speicher_umrichter_power"), {})
    standby = {e["entity"]: e["hours"] for e in month["standby"]}
    assert night in standby["speicher_plug_power"]


def test_existing_month_file_is_untouched(tmp_path):
    data = make_data_dir(tmp_path)
    chain_dir(data).mkdir(parents=True)
    existing = chain_dir(data) / "2026-10.json"
    keep = b'{"keep":true}'
    existing.write_bytes(keep)
    assert run_seed(data).returncode == 0
    assert existing.read_bytes() == keep
    assert (chain_dir(data) / "2026-11.json").exists()


def test_second_run_changes_nothing(tmp_path):
    data = make_data_dir(tmp_path)
    assert run_seed(data).returncode == 0
    before = {p.name: p.read_bytes() for p in chain_dir(data).iterdir()}
    assert run_seed(data).returncode == 0
    after = {p.name: p.read_bytes() for p in chain_dir(data).iterdir()}
    assert after == before
