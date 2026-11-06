import subprocess
import sys
from pathlib import Path

import pytest

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "version"))
import prerelease as pr

SCRIPT = Path(__file__).resolve().parents[1] / "version" / "prerelease.py"


def test_next_from_final():
    assert pr.next_pre("v0.4.5") == "v0.4.6-pre.1"


def test_next_from_pre():
    assert pr.next_pre("v0.4.6-pre.2") == "v0.4.6-pre.3"


def test_renumber_when_main_caught_up():
    assert pr.renumber("v0.4.6-pre.2", "v0.4.6") == "v0.4.7-pre.2"


def test_renumber_keeps_when_ahead():
    assert pr.renumber("v0.4.7-pre.2", "v0.4.6") == "v0.4.7-pre.2"


def test_renumber_main_far_ahead():
    assert pr.renumber("v0.4.6-pre.2", "v0.5.1") == "v0.5.2-pre.2"


def test_strip():
    assert pr.strip("v0.4.7-pre.2") == "v0.4.7"


def test_strip_final_unchanged():
    assert pr.strip("v0.4.7") == "v0.4.7"


def test_is_pre():
    assert pr.is_pre("v0.4.6-pre.1")
    assert not pr.is_pre("v0.4.6")


@pytest.mark.parametrize("bad", ["0.4.5", "v0.4", "v0.4.5-rc1"])
def test_invalid_raises(bad):
    with pytest.raises(ValueError):
        pr.next_pre(bad)


def test_cli_prints_result():
    out = subprocess.run([sys.executable, str(SCRIPT), "next", "v0.4.5"],
                         capture_output=True, text=True, check=True)
    assert out.stdout.strip() == "v0.4.6-pre.1"


def test_cli_strip_prints_result():
    out = subprocess.run([sys.executable, str(SCRIPT), "strip", "v0.4.7-pre.2"],
                         capture_output=True, text=True, check=True)
    assert out.stdout.strip() == "v0.4.7"


def test_cli_invalid_exits_2():
    res = subprocess.run([sys.executable, str(SCRIPT), "next", "0.4.5"],
                         capture_output=True, text=True)
    assert res.returncode == 2
