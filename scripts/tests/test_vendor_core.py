import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
import vendor_core


def _mk(src: Path, dst: Path):
    (src / "pkg").mkdir(parents=True)
    (src / "pkg" / "__init__.py").write_text("x = 1\n")
    (src / "pkg" / "curves.py").write_text("A = 1\n")
    (src / "pkg" / "tests").mkdir()
    (src / "pkg" / "tests" / "test_x.py").write_text("assert True\n")
    dst.mkdir(parents=True)
    (dst / "_VENDORED.md").write_text("do not edit\n")


def test_plan_lists_new_and_stale_files(tmp_path):
    src, dst = tmp_path / "src", tmp_path / "dst"
    _mk(src, dst)
    src_pkg = src / "pkg"
    to_write, to_delete = vendor_core.plan(src_pkg, dst)
    assert Path("curves.py") in to_write and Path("__init__.py") in to_write
    assert all("tests" not in p.parts for p in to_write)
    assert to_delete == []


def test_apply_then_check_is_clean(tmp_path):
    src, dst = tmp_path / "src", tmp_path / "dst"
    _mk(src, dst)
    src_pkg = src / "pkg"
    vendor_core.apply(src_pkg, dst, vendor_core.plan(src_pkg, dst))
    assert (dst / "curves.py").read_text() == "A = 1\n"
    assert (dst / "_VENDORED.md").exists()
    assert vendor_core.plan(src_pkg, dst) == ([], [])


def test_stale_file_is_flagged_for_deletion(tmp_path):
    src, dst = tmp_path / "src", tmp_path / "dst"
    _mk(src, dst)
    src_pkg = src / "pkg"
    vendor_core.apply(src_pkg, dst, vendor_core.plan(src_pkg, dst))
    (dst / "old.py").write_text("gone\n")
    _tw, to_delete = vendor_core.plan(src_pkg, dst)
    assert Path("old.py") in to_delete
