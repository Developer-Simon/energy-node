import sys
from pathlib import Path

import pytest

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

import recovery  # noqa: E402
import soc_config  # noqa: E402
from battery_soc_core.state import SocState  # noqa: E402
from state_store import StoredMeta  # noqa: E402


def parallel_state(coulomb_ah=50.0):
    cfg = soc_config.BatteryConfig(id="b", name="B", state_file=Path("/nonexistent/s.json"))
    params = cfg.soc_params()
    state = SocState(params, last_tick=0.0)
    state.units[0].coulomb_ah = coulomb_ah
    return params, state


def test_snapshot_round_trips_through_apply():
    _, before = parallel_state(40.0)
    baseline = recovery.counters(before)
    _, crashed = parallel_state(47.0)
    crashed.units[0].charged_ah = 7.0
    snap = recovery.snapshot(crashed, now=150.0)
    assert recovery.apply_snapshot(before, baseline, snap, saved_at=100.0, started_at=200.0)
    assert before.units[0].coulomb_ah == pytest.approx(47.0)
    assert before.units[0].charged_ah == pytest.approx(7.0)


def test_delta_keeps_integration_since_start():
    _, state = parallel_state(40.0)
    baseline = recovery.counters(state)
    state.units[0].coulomb_ah = 41.0            # erste Ticks nach dem Start
    _, crashed = parallel_state(45.0)
    snap = recovery.snapshot(crashed, now=150.0)
    assert recovery.apply_snapshot(state, baseline, snap, saved_at=100.0, started_at=200.0)
    assert state.units[0].coulomb_ah == pytest.approx(46.0)


@pytest.mark.parametrize("ts", [100.0, 90.0, 200.0, 250.0])
def test_snapshot_outside_the_window_is_rejected(ts):
    _, state = parallel_state(40.0)
    baseline = recovery.counters(state)
    snap = recovery.snapshot(parallel_state(45.0)[1], now=ts)
    assert not recovery.apply_snapshot(state, baseline, snap, saved_at=100.0, started_at=200.0)
    assert state.units[0].coulomb_ah == 40.0


def test_snapshot_with_other_units_is_rejected():
    _, state = parallel_state(40.0)
    baseline = recovery.counters(state)
    snap = {"ts": 150.0, "units": {"bank_a": {"coulomb_ah": 1.0, "charged_ah": 0.0,
                                              "discharged_ah": 0.0}}}
    assert not recovery.apply_snapshot(state, baseline, snap, saved_at=100.0, started_at=200.0)


@pytest.mark.parametrize("snap", [None, "x", {"ts": "1"}, {"ts": 150.0, "units": []},
                                  {"ts": 150.0, "units": {"pack": {"coulomb_ah": "x"}}}])
def test_malformed_snapshot_is_rejected(snap):
    _, state = parallel_state(40.0)
    assert not recovery.apply_snapshot(state, recovery.counters(state), snap,
                                       saved_at=100.0, started_at=200.0)


def test_apply_clamps_to_capacity():
    _, state = parallel_state(0.0)
    baseline = recovery.counters(state)
    crashed = parallel_state(0.0)[1]
    crashed.units[0].coulomb_ah = 10_000.0
    assert recovery.apply_snapshot(state, baseline, recovery.snapshot(crashed, 150.0),
                                   saved_at=100.0, started_at=200.0)
    assert state.units[0].coulomb_ah == state.units[0].capacity_ah


def test_extrapolation_uses_half_the_unsaved_window():
    params, state = parallel_state(50.0)
    name = state.units[0].name
    stored = StoredMeta(saved_at=0.0, clean=False, last_current_a={name: -36.0})
    hours = recovery.extrapolate(params, state, stored, now=10_000.0, interval_s=300.0)
    assert hours == pytest.approx(150.0 / 3600)
    assert state.units[0].coulomb_ah == pytest.approx(50.0 - 36.0 * 150.0 / 3600)


def test_extrapolation_shorter_gap_than_interval():
    params, state = parallel_state(50.0)
    name = state.units[0].name
    stored = StoredMeta(saved_at=0.0, clean=False, last_current_a={name: -36.0})
    assert recovery.extrapolate(params, state, stored, now=60.0, interval_s=300.0) \
        == pytest.approx(30.0 / 3600)


@pytest.mark.parametrize("stored", [
    None,
    StoredMeta(saved_at=0.0, clean=True, last_current_a={"pack": -36.0}),
    StoredMeta(saved_at=None, clean=False, last_current_a={"pack": -36.0}),
    StoredMeta(saved_at=500.0, clean=False, last_current_a={"pack": -36.0}),
])
def test_no_extrapolation_without_an_unclean_past_save(stored):
    params, state = parallel_state(50.0)
    assert recovery.extrapolate(params, state, stored, now=100.0, interval_s=300.0) == 0.0
    assert state.units[0].coulomb_ah == 50.0


def test_extrapolation_skips_units_without_current():
    params, state = parallel_state(50.0)
    stored = StoredMeta(saved_at=0.0, clean=False, last_current_a={})
    recovery.extrapolate(params, state, stored, now=600.0, interval_s=300.0)
    assert state.units[0].coulomb_ah == 50.0
