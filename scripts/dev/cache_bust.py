"""Cache-bust-Marken (?v=) der Dashboard-Assets pruefen.

Die Web-UI laedt JS und CSS aus dem Dashboard mit `/static/<pfad>?v=N`. Wird
eine Datei geaendert, muss ihr N steigen, sonst holt sich der Browser die alte
Version aus dem Cache. Dieses Skript vergleicht die Marken im Basis-Stand mit
dem Arbeitsstand und sagt, welche Assets ein `?v=` brauchen.

Unterbefehl:
  report [--base REF] [--root PATH]
      Gibt eine Zeile pro betroffenem Asset aus (Basis -> Stand, erwartet,
      Status) und am Ende die Anzahl der Assets mit Status `bump`. Beendet
      sich immer mit 0. Ohne --base gilt `git merge-base origin/main HEAD`.

  check [--base REF] [--root PATH] [--lenient]
      Meldet jede Abweichung mit einer Zeile pro Asset und endet mit 1, wenn
      es welche gibt. Warnungen (nicht referenzierte Assets) zaehlen nicht.
      Mit --lenient reicht jede Erhoehung von ?v= bei geaenderten Dateien.
      Zusaetzlich wird die Verlaufs-CSV geprueft (fehlende Zeilen, und im
      strikten Modus falsche Version oder Zeilen ohne Marke).

  history [--base REF] [--root PATH]
      Schreibt dashboard/internal/webui/cache-bust-history.csv neu. Jede Marke mit
      Status ok, too-high oder too-low bekommt eine Zeile mit der erwarteten Version
      und dem heutigen Datum. Branch-Zeilen, deren Marke sich erledigt hat, fliegen
      raus und werden mit ihrer Notiz ausgegeben. Passt die Version auf den Branch
      an, bleiben Datum und Notiz erhalten. Die Basis-Zeilen bleiben unberuehrt.

Referenzen stehen in dashboard/internal/webui/templates/*.html und in
webui.go. Marken, die keine ganze Zahl sind (etwa `{{.Foo}}`), zaehlen nicht.
"""
import argparse
import csv
import glob
import io
import re
import subprocess
import sys
from dataclasses import dataclass, replace
from datetime import date
from pathlib import Path

WEBUI = "dashboard/internal/webui"
ASSET_DIRS = ("js", "js-deps", "css")
REF_RE = re.compile(r"/static/((?:js|js-deps|css)/[A-Za-z0-9._-]+)(?:\?v=([A-Za-z0-9{}._-]+))?")
HISTORY = "dashboard/internal/webui/cache-bust-history.csv"
VERSION_FILE = "dashboard/VERSION"
HEADER = ["version", "date", "asset", "v", "note"]
VERSION_RE = re.compile(r"^v(\d+)\.(\d+)\.(\d+)(?:-pre\.(\d+))?$")


@dataclass(frozen=True)
class Ref:
    file: str
    asset: str
    v: int | None


@dataclass
class Row:
    asset: str
    base: int | None
    head: set
    changed: bool
    expected: int | None
    status: str


def git(root: Path, *args: str) -> str:
    result = subprocess.run(
        ["git", "-C", str(root), *args],
        check=True,
        capture_output=True,
        text=True,
    )
    return result.stdout


def read_text(root: Path, rev: str | None, path: str) -> str | None:
    if rev is None:
        target = root / path
        if not target.is_file():
            return None
        return target.read_text(encoding="utf-8")
    try:
        return git(root, "show", f"{rev}:{path}")
    except subprocess.CalledProcessError:
        return None


def ref_files(root: Path, rev: str | None) -> list[str]:
    templates = f"{WEBUI}/templates"
    if rev is None:
        files = [
            f"{templates}/{Path(p).name}"
            for p in glob.glob(str(root / templates / "*.html"))
            if not p.endswith("_test.html")
        ]
        if (root / WEBUI / "webui.go").is_file():
            files.append(f"{WEBUI}/webui.go")
        return sorted(files)
    listing = git(root, "ls-tree", "-r", "--name-only", rev, templates, f"{WEBUI}/webui.go")
    files = []
    for path in listing.splitlines():
        if path == f"{WEBUI}/webui.go":
            files.append(path)
        elif path.startswith(f"{templates}/") and path.endswith(".html") and not path.endswith("_test.html"):
            if "/" not in path[len(templates) + 1:]:
                files.append(path)
    return sorted(files)


def scan_refs(root: Path, rev: str | None) -> list[Ref]:
    refs = []
    for file in ref_files(root, rev):
        text = read_text(root, rev, file)
        if text is None:
            continue
        for match in REF_RE.finditer(text):
            raw = match.group(2)
            if raw is None:
                value = None
            elif raw.isdigit():
                value = int(raw)
            else:
                continue
            refs.append(Ref(file=file, asset=match.group(1), v=value))
    return refs


def versions(refs: list[Ref]) -> dict[str, set[int | None]]:
    result: dict[str, set[int | None]] = {}
    for ref in refs:
        result.setdefault(ref.asset, set()).add(ref.v)
    return result


def changed_assets(root: Path, base: str) -> set[str]:
    dirs = [f"{WEBUI}/static/{d}" for d in ASSET_DIRS]
    listing = git(root, "diff", "--name-only", "--diff-filter=d", base, "--", *dirs)
    prefix = f"{WEBUI}/static/"
    assets = set()
    for path in listing.splitlines():
        if path.startswith(prefix) and path[len(prefix):].split("/", 1)[0] in ASSET_DIRS:
            assets.add(path[len(prefix):])
    return assets


def _base_value(values: set) -> int | None:
    numbers = [v for v in values if v is not None]
    return max(numbers) if numbers else None


def _classify(base: int | None, head: set, changed: bool, lenient: bool) -> tuple[int | None, str]:
    if changed and not head:
        return None, "unreferenced"
    if len(head) > 1:
        b = base or 0
        return (b + 1 if changed else base), "inconsistent"
    (h,) = head
    hn, bn = h or 0, base or 0
    if changed:
        if hn == bn:
            return bn + 1, "bump"
        if hn == bn + 1 or (lenient and hn > bn):
            return h, "ok"
        if hn > bn + 1:
            return bn + 1, "too-high"
        return bn + 1, "too-low"
    if hn == bn:
        return h, "ok"
    if hn > bn:
        return base, "no-change"
    return base, "too-low"


def compare(root: Path, base: str, lenient: bool = False) -> list[Row]:
    base_v = versions(scan_refs(root, base))
    head_v = versions(scan_refs(root, None))
    changed = changed_assets(root, base)
    rows = []
    for asset in sorted(changed | set(base_v) | set(head_v)):
        b_set = base_v.get(asset, set())
        h_set = head_v.get(asset, set())
        is_changed = asset in changed
        base_int = _base_value(b_set)
        if not is_changed and (not h_set or h_set == {base_int}):
            continue
        expected, status = _classify(base_int, h_set, is_changed, lenient)
        rows.append(Row(
            asset=asset,
            base=base_int,
            head=h_set,
            changed=is_changed,
            expected=expected,
            status=status,
        ))
    return rows


def default_base(root: Path) -> str:
    return git(root, "merge-base", "origin/main", "HEAD").strip()


def _fmt(value) -> str:
    return "-" if value is None else str(value)


def _fmt_head(head: set) -> str:
    if len(head) == 1:
        return _fmt(next(iter(head)))
    return "{" + ",".join(sorted(_fmt(v) for v in head)) + "}"


FAILING = {"bump", "inconsistent", "too-high", "no-change", "too-low"}


def _sort_key(value):
    return -1 if value is None else value


def _error_message(r: Row) -> str:
    single = next(iter(r.head)) if len(r.head) == 1 else None
    if r.status == "bump":
        return f"{r.asset}: content changed, ?v= is {_fmt(single)}, expected {_fmt(r.expected)}"
    if r.status == "inconsistent":
        values = ", ".join(_fmt(v) for v in sorted(r.head, key=_sort_key))
        return f"{r.asset}: references disagree ({values}), expected {_fmt(r.expected)} everywhere"
    if r.status == "too-high":
        return (
            f"{r.asset}: ?v= went from {_fmt(r.base)} to {_fmt(single)}, "
            f"a branch raises it by exactly 1 (expected {_fmt(r.expected)})"
        )
    if r.status == "no-change":
        return f"{r.asset}: ?v= changed from {_fmt(r.base)} to {_fmt(single)} but the file did not"
    return (
        f"{r.asset}: ?v= went down from {_fmt(r.base)} to {_fmt(single)}, "
        f"expected {_fmt(r.expected)}"
    )


def check_code(rows: list[Row]) -> list[str]:
    return [_error_message(r) for r in rows if r.status in FAILING]


def read_version(root: Path, rev: str | None) -> str:
    text = read_text(root, rev, VERSION_FILE)
    if text is None:
        raise ValueError(f"{VERSION_FILE} is missing at {rev or 'the working tree'}")
    return text.strip()


def history_errors(root: Path, base: str, rows: list[Row], lenient: bool) -> list[str]:
    try:
        base_entries = parse_history(read_text(root, base, HISTORY))
        head_entries = parse_history(read_text(root, None, HISTORY))
        version = expected_version(read_version(root, base), read_version(root, None))
    except ValueError as exc:
        return [str(exc)]
    return check_history(base_entries, head_entries, bumps(rows), version, lenient)


def cmd_history(root: Path, base: str | None, today: date) -> int:
    base = base or default_base(root)
    try:
        base_entries = parse_history(read_text(root, base, HISTORY))
        head_entries = parse_history(read_text(root, None, HISTORY))
        version = expected_version(read_version(root, base), read_version(root, None))
    except ValueError as exc:
        print(exc)
        return 1
    new_entries, removed = update_history(
        base_entries, head_entries, bumps(compare(root, base)), version, today
    )
    head_set = set(head_entries)
    head_versions: dict[tuple[str, int], str] = {}
    for e in head_entries:
        head_versions.setdefault((e.asset, e.v), e.version)
    for e in new_entries:
        if e in head_set:
            continue
        old = head_versions.get((e.asset, e.v))
        if old is None:
            print(f"added {e.version} {e.asset} ?v={e.v}")
        else:
            print(f"updated {e.asset} ?v={e.v}, {old} to {e.version}")
    for e in removed:
        print(f"removed {e.version} {e.asset} ?v={e.v}, note: {e.note or '(empty)'}")
    target = root / HISTORY
    target.parent.mkdir(parents=True, exist_ok=True)
    target.write_text(format_history(new_entries), encoding="utf-8")
    return 0


def cmd_check(root: Path, base: str | None, lenient: bool) -> int:
    base = base or default_base(root)
    rows = compare(root, base, lenient)
    for r in rows:
        if r.status == "unreferenced":
            print(f"warning: {r.asset}: changed but not referenced by any template or webui.go")
    errors = check_code(rows) + history_errors(root, base, rows, lenient)
    for line in errors:
        print(line)
    if errors:
        print("Run scripts/dev/cache_bust.py report for the full list.")
        return 1
    return 0


def cmd_report(root: Path, base: str | None) -> int:
    base = base or default_base(root)
    rows = compare(root, base)
    width = max((len(r.asset) for r in rows), default=0)
    for r in rows:
        print(
            f"{r.asset:<{width}}  {_fmt(r.base)} -> {_fmt_head(r.head)}  "
            f"{_fmt(r.expected)}  {r.status}"
        )
    bumps = sum(1 for r in rows if r.status == "bump")
    print(f"{bumps} assets to bump" if bumps else "nothing to bump")
    return 0


@dataclass(frozen=True)
class Entry:
    version: str
    date: str
    asset: str
    v: int
    note: str


def version_key(version: str) -> tuple:
    match = VERSION_RE.match(version)
    if not match:
        raise ValueError(f"not a version: {version!r}")
    major, minor, patch, pre = match.groups()
    if pre is None:
        return (int(major), int(minor), int(patch), 1, 0)
    return (int(major), int(minor), int(patch), 0, int(pre))


def parse_history(text: str | None) -> list[Entry]:
    if not text:
        return []
    reader = csv.reader(io.StringIO(text))
    entries: list[Entry] = []
    header_seen = False
    for row in reader:
        if not row:
            continue
        where = f"{HISTORY} line {reader.line_num}"
        if not header_seen:
            if row != HEADER:
                raise ValueError(f"{where}: header must be {','.join(HEADER)}")
            header_seen = True
            continue
        if len(row) != len(HEADER):
            raise ValueError(f"{where}: expected {len(HEADER)} fields, got {len(row)}")
        if any("\n" in field or "\r" in field for field in row):
            raise ValueError(f"{where}: fields must not contain a newline")
        version, day, asset, v, note = row
        try:
            version_key(version)
        except ValueError:
            raise ValueError(f"{where}: not a version: {version!r}") from None
        try:
            number = int(v)
        except ValueError:
            raise ValueError(f"{where}: v must be an integer, got {v!r}") from None
        entries.append(Entry(version, day, asset, number, note))
    return list(dict.fromkeys(entries))


def format_history(entries: list[Entry]) -> str:
    unique = sorted(set(entries), key=lambda e: (e.asset, e.v, e.date, e.note))
    unique.sort(key=lambda e: version_key(e.version), reverse=True)
    out = io.StringIO()
    writer = csv.writer(out, lineterminator="\n", quoting=csv.QUOTE_MINIMAL)
    writer.writerow(HEADER)
    for e in unique:
        writer.writerow([e.version, e.date, e.asset, e.v, e.note])
    return out.getvalue()


def expected_version(base_version: str, head_version: str) -> str:
    if head_version != base_version:
        return head_version
    match = VERSION_RE.match(head_version)
    if not match:
        raise ValueError(f"not a version: {head_version!r}")
    major, minor, patch, pre = match.groups()
    if pre is not None:
        return f"v{major}.{minor}.{patch}-pre.{int(pre) + 1}"
    return f"v{major}.{minor}.{int(patch) + 1}"


def bumps(rows: list[Row]) -> dict[str, int]:
    result: dict[str, int] = {}
    for row in rows:
        if row.status not in ("ok", "too-high", "too-low") or len(row.head) != 1:
            continue
        (value,) = row.head
        if isinstance(value, int) and value != row.base:
            result[row.asset] = value
    return result


def update_history(
    base_entries: list[Entry],
    head_entries: list[Entry],
    bump_map: dict[str, int],
    version: str,
    today: date,
) -> tuple[list[Entry], list[Entry]]:
    base_set = set(base_entries)
    new_entries = [e for e in head_entries if e in base_set]
    removed: list[Entry] = []
    for e in head_entries:
        if e in base_set:
            continue
        if bump_map.get(e.asset) == e.v:
            new_entries.append(replace(e, version=version))
        else:
            removed.append(e)
    present = {(e.asset, e.v) for e in head_entries}
    for asset, v in sorted(bump_map.items()):
        if (asset, v) not in present:
            new_entries.append(Entry(version, today.isoformat(), asset, v, ""))
    return new_entries, removed


def check_history(
    base_entries: list[Entry],
    head_entries: list[Entry],
    bump_map: dict[str, int],
    version: str,
    lenient: bool,
) -> list[str]:
    name = Path(HISTORY).name
    present = {(e.asset, e.v) for e in head_entries}
    errors = []
    for asset, v in sorted(bump_map.items()):
        if (asset, v) not in present:
            errors.append(f"{name} has no row for {asset} ?v={v}, run scripts/dev/cache_bust.py history")
    if lenient:
        return errors
    base_set = set(base_entries)
    for e in head_entries:
        if e in base_set:
            continue
        if bump_map.get(e.asset) != e.v:
            errors.append(
                f"{name} row {e.asset} ?v={e.v} has no matching bump, "
                "run scripts/dev/cache_bust.py history"
            )
        elif e.version != version:
            errors.append(f"{name} row {e.asset} ?v={e.v} says {e.version}, expected {version}")
    return errors


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description="Cache-bust-Marken der Dashboard-Assets pruefen")
    sub = parser.add_subparsers(dest="command", required=True)
    report = sub.add_parser("report", help="Abweichungen zwischen Basis und Arbeitsstand anzeigen")
    report.add_argument("--base", default=None, help="Basis-Ref (Standard: merge-base mit origin/main)")
    report.add_argument("--root", default=None, help="Repo-Wurzel (Standard: dieses Repo)")
    check = sub.add_parser("check", help="Fehlende oder falsche ?v=-Marken melden (Exit 1)")
    check.add_argument("--base", default=None, help="Basis-Ref (Standard: merge-base mit origin/main)")
    check.add_argument("--root", default=None, help="Repo-Wurzel (Standard: dieses Repo)")
    check.add_argument(
        "--lenient",
        action="store_true",
        help="Jede Erhoehung von ?v= bei geaenderten Dateien akzeptieren",
    )
    history = sub.add_parser("history", help="Verlaufs-CSV mit den geaenderten ?v=-Marken fortschreiben")
    history.add_argument("--base", default=None, help="Basis-Ref (Standard: merge-base mit origin/main)")
    history.add_argument("--root", default=None, help="Repo-Wurzel (Standard: dieses Repo)")
    history.add_argument("--today", type=date.fromisoformat, default=None, help=argparse.SUPPRESS)
    args = parser.parse_args(argv)
    root = Path(args.root) if args.root else Path(__file__).resolve().parents[2]
    root = root.resolve()
    if args.command == "report":
        return cmd_report(root, args.base)
    if args.command == "check":
        return cmd_check(root, args.base, args.lenient)
    if args.command == "history":
        return cmd_history(root, args.base, args.today or date.today())
    return 0


if __name__ == "__main__":
    sys.exit(main())
