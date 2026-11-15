import pytest

import energy_node_common.scheduler as scheduler_module
from energy_node_common.scheduler import Scheduler


def test_scheduler_reports_round_duration(monkeypatch):
    rounds = []
    clock = iter([100.0, 100.4, 200.0])
    monkeypatch.setattr(scheduler_module, "_monotonic", lambda: next(clock))
    s = Scheduler(poll_core=lambda: None, get_interval_s=lambda: 10, on_round=rounds.append)
    s._run_once()
    assert rounds == [pytest.approx(0.4)]
