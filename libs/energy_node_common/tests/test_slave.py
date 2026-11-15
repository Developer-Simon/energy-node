from energy_node_common.settings import SlaveStatus
from energy_node_common.slave import Slave


def make_slave():
    return Slave("service", lambda: None)


def test_slave_status_carries_median_round():
    slave = make_slave()
    for d in [0.3, 0.5, 0.4]:
        slave.record_round(d)
    assert slave._status().to_dict()["poll_round_s"] == 0.4


def test_status_without_rounds_omits_field():
    assert "poll_round_s" not in SlaveStatus().to_dict()
