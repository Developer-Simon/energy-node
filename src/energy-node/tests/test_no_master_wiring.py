import inspect
from pathlib import Path

import sys

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

import energy_node_mqtt as node


def test_module_has_no_master_symbols():
    assert not hasattr(node, "Master")
    src = Path(node.__file__).read_text(encoding="utf-8")
    assert "register_slave" not in src
    assert "master.handle_message" not in src
    assert "service_last_updates" not in src


def test_publish_discovery_dropped_bridges_param():
    params = list(inspect.signature(node.publish_discovery).parameters)
    assert params == ["client", "node_config", "topics"]


def test_publish_slow_diagnostics_dropped_master_param():
    params = list(inspect.signature(node.publish_slow_diagnostics).parameters)
    assert params == ["client", "topics"]
