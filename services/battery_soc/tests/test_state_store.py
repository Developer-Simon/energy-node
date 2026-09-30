import json, sys
from pathlib import Path
sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
import soc_config, state_store
from battery_soc_core.state import SocState


def test_save_then_load_roundtrips_the_counter(tmp_path):
    cfg = soc_config.BatteryConfig(id="b", name="B", state_file=tmp_path / "s.json")
    s = SocState(cfg.soc_params(), last_tick=0.0)
    s.units[0].coulomb_ah = 88.0
    state_store.save_state(cfg, s)
    s2 = SocState(cfg.soc_params(), last_tick=0.0)
    state_store.load_state(cfg, s2)
    assert s2.units[0].coulomb_ah == 88.0


def test_load_migrates_legacy_parallel_format(tmp_path):
    cfg = soc_config.BatteryConfig(id="b", name="B", state_file=tmp_path / "s.json")
    (tmp_path / "s.json").write_text(json.dumps({"bank_a_coulomb_ah": 20.0, "bank_b_coulomb_ah": 10.0}))
    s = SocState(cfg.soc_params(), last_tick=0.0)
    state_store.load_state(cfg, s)
    assert s.units[0].coulomb_ah == 30.0


def test_missing_file_is_silent(tmp_path):
    cfg = soc_config.BatteryConfig(id="b", name="B", state_file=tmp_path / "nope.json")
    s = SocState(cfg.soc_params(), last_tick=0.0)
    state_store.load_state(cfg, s)  # no raise


def _cfg(tmp_path):
    return soc_config.BatteryConfig(id="b", name="B", state_file=tmp_path / "s.json")


def test_save_writes_metadata_and_load_returns_it(tmp_path):
    cfg = _cfg(tmp_path)
    s = SocState(cfg.soc_params(), last_tick=0.0)
    assert state_store.save_state(cfg, s, now=1000.0, clean=False,
                                  last_current_a={"pack": -12.5})
    meta = state_store.load_state(cfg, SocState(cfg.soc_params(), last_tick=0.0))
    assert meta == state_store.StoredMeta(saved_at=1000.0, clean=False,
                                          last_current_a={"pack": -12.5})


def test_save_leaves_no_temp_file(tmp_path):
    cfg = _cfg(tmp_path)
    state_store.save_state(cfg, SocState(cfg.soc_params(), last_tick=0.0), now=1.0)
    assert sorted(p.name for p in tmp_path.iterdir()) == ["s.json"]


def test_legacy_file_counts_as_clean(tmp_path):
    cfg = _cfg(tmp_path)
    (tmp_path / "s.json").write_text(json.dumps({"units": {"pack": {"coulomb_ah": 5.0}}}))
    meta = state_store.load_state(cfg, SocState(cfg.soc_params(), last_tick=0.0))
    assert meta == state_store.StoredMeta(saved_at=None, clean=True, last_current_a={})


def test_broken_file_returns_none(tmp_path):
    cfg = _cfg(tmp_path)
    (tmp_path / "s.json").write_text("{kaputt")
    assert state_store.load_state(cfg, SocState(cfg.soc_params(), last_tick=0.0)) is None


def test_non_numeric_currents_are_dropped(tmp_path):
    cfg = _cfg(tmp_path)
    (tmp_path / "s.json").write_text(json.dumps(
        {"units": {}, "saved_at": 5.0, "clean": False,
         "last_current_a": {"pack": "x", "bank_a": None, "bank_b": 2}}))
    meta = state_store.load_state(cfg, SocState(cfg.soc_params(), last_tick=0.0))
    assert meta.last_current_a == {"bank_b": 2.0}


def test_save_reports_failure_instead_of_raising(tmp_path):
    blocker = tmp_path / "datei"
    blocker.write_text("")
    cfg = soc_config.BatteryConfig(id="b", name="B", state_file=blocker / "s.json")
    assert state_store.save_state(cfg, SocState(cfg.soc_params(), last_tick=0.0)) is False
