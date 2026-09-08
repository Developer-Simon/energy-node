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
