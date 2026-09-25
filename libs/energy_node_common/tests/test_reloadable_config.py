import sys
from pathlib import Path

import pytest

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "src"))

import hashlib

from energy_node_common.config import ConfigRejected, ReloadableConfig


def _sha(text):
    return hashlib.sha256(text.encode("utf-8")).hexdigest()


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


def test_file_revision_is_the_sha256_of_the_bytes(tmp_path):
    path = tmp_path / "devices.json"
    path.write_text("[1]", encoding="utf-8")
    assert ConfigRejected.__module__  # Just to verify it's importable
    from energy_node_common.config import file_revision
    assert file_revision(path) == _sha("[1]")
    assert file_revision(tmp_path / "missing.json") == ""


def test_revisions_follow_attempts_and_commits(tmp_path):
    path = tmp_path / "devices.json"
    path.write_text("alt", encoding="utf-8")
    store = ReloadableConfig(path, lambda p: Path(p).read_text(encoding="utf-8"))
    store.load()
    assert store.attempted_revision == store.applied_revision == _sha("alt")

    path.write_text("neu", encoding="utf-8")
    store.load_candidate()
    assert store.attempted_revision == _sha("neu")
    assert store.applied_revision == _sha("alt"), "not committed yet"


def test_a_failed_candidate_still_records_the_attempt(tmp_path):
    path = tmp_path / "devices.json"
    path.write_text("kaputt", encoding="utf-8")

    def loader(p):
        raise ValueError("kaputt")

    store = ReloadableConfig(path, loader)
    with pytest.raises(ValueError):
        store.load_candidate()
    assert store.attempted_revision == _sha("kaputt")
    assert store.applied_revision == ""


def test_load_or_falls_back_and_remembers_the_error(tmp_path):
    path = tmp_path / "devices.json"
    path.write_text("kaputt", encoding="utf-8")

    def loader(p):
        raise ConfigRejected("keine Ladequelle", code="charge_source_required")

    store = ReloadableConfig(path, loader)
    value, error = store.load_or([])
    assert value == [] and store.value == []
    assert isinstance(error, ConfigRejected) and error.code == "charge_source_required"
    assert store.load_error is error
    assert store.attempted_revision == _sha("kaputt")
    assert store.applied_revision == ""


def test_load_or_returns_the_loaded_value_without_error(tmp_path):
    path = tmp_path / "devices.json"
    path.write_text("ok", encoding="utf-8")
    store = ReloadableConfig(path, lambda p: Path(p).read_text(encoding="utf-8"))
    assert store.load_or([]) == ("ok", None)
    assert store.load_error is None


def test_a_later_commit_clears_the_load_error(tmp_path):
    path = tmp_path / "devices.json"
    path.write_text("kaputt", encoding="utf-8")

    def loader(p):
        text = Path(p).read_text(encoding="utf-8")
        if text == "kaputt":
            raise ValueError("kaputt")
        return text

    store = ReloadableConfig(path, loader)
    store.load_or("")
    path.write_text("gut", encoding="utf-8")
    store.commit(store.load_candidate())
    assert store.load_error is None
    assert store.applied_revision == _sha("gut")


def test_config_rejected_is_a_value_error_with_a_code():
    exc = ConfigRejected("text", code="bank_a_voltage_required")
    assert isinstance(exc, ValueError)
    assert str(exc) == "text"
    assert exc.code == "bank_a_voltage_required"
