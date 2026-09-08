import sys
from pathlib import Path

import pytest

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "src"))

from energy_node_common.config import ReloadableConfig


def test_load_candidate_does_not_replace_active_value(tmp_path):
    path = tmp_path / "devices.json"
    path.write_text("alt", encoding="utf-8")
    store = ReloadableConfig(path, lambda p: Path(p).read_text(encoding="utf-8"))
    store.load()

    path.write_text("neu", encoding="utf-8")
    candidate = store.load_candidate()

    assert candidate == "neu"
    assert store.value == "alt"


def test_commit_replaces_active_value(tmp_path):
    path = tmp_path / "devices.json"
    path.write_text("alt", encoding="utf-8")
    store = ReloadableConfig(path, lambda p: Path(p).read_text(encoding="utf-8"))
    store.load()

    store.commit(store.load_candidate())
    assert store.value == "alt"

    path.write_text("neu", encoding="utf-8")
    store.commit(store.load_candidate())
    assert store.value == "neu"


def test_failing_loader_leaves_active_value_untouched(tmp_path):
    path = tmp_path / "devices.json"
    path.write_text("alt", encoding="utf-8")

    def loader(p):
        text = Path(p).read_text(encoding="utf-8")
        if text == "kaputt":
            raise ValueError("kaputt")
        return text

    store = ReloadableConfig(path, loader)
    store.load()
    path.write_text("kaputt", encoding="utf-8")

    with pytest.raises(ValueError):
        store.load_candidate()
    assert store.value == "alt"
