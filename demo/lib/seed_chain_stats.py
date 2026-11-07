"""Seeds the measurement chain statistics of the demo with 14 days of hours.

Usage: seed_chain_stats.py <data_dir> <now_iso>

The chains are read from <data_dir>/energy.json. The hours are written to
<data_dir>/energystats/chains/<jjjj-mm>.json in the format of the Go ChainStore
(dashboard/internal/energystats/chain_store.go, chainMonthFile). Charging runs
in the day, discharging in the evening, standby at night. The random numbers
come from a fixed seed. A month file that exists is left untouched, so a
second run with the same arguments changes nothing.

The learned loss of every segment and direction goes to
<data_dir>/energystats/chains/learned.json in the format of the Go chainwatch
table (dashboard/internal/chainwatch/table.go, tableFile). It holds classes 1
to 12 with 1200 samples each and a curve that peaks in the middle. first_seen
is 14 days back. An existing file is left untouched.
"""

import json
import math
import os
import random
import sys
from datetime import datetime, timedelta, timezone
from decimal import Decimal

SEED = 20261107
WINDOW_DAYS = 14
FORMAT_VERSION = 1
HOUR_SECONDS = 3600
LEARNED_FILE = "learned.json"
LEARNED_CLASSES = range(1, 13)
LEARNED_SAMPLES = 1200
LEARNED_CLASS_WIDTH_W = 50


def parse_now(text):
    """Parses an ISO 8601 time, naive values count as UTC."""
    value = datetime.fromisoformat(text.replace("Z", "+00:00"))
    if value.tzinfo is None:
        value = value.replace(tzinfo=timezone.utc)
    return value.astimezone(timezone.utc)


def month_start(t):
    return t.replace(day=1, hour=0, minute=0, second=0, microsecond=0)


def month_key(start):
    return start.strftime("%Y-%m")


def go_round(v):
    """Rounds half away from zero, as Go's math.Round does."""
    return math.copysign(math.floor(abs(v) + 0.5), v)


def round_kwh(v):
    """Rounds to 1 mWh, as roundKWh in the Go store does."""
    return go_round(v * 1e6) / 1e6


def go_number(v):
    """Formats a float the way Go's encoding/json does."""
    if v == int(v) and abs(v) < 1e21:
        return str(int(v))
    return format(Decimal(repr(v)), "f")


def encode_month(start, pairs, standby):
    """Encodes one month like encodeChainMonth in the Go store.

    pairs maps (source, target) to {hour index: (source kWh, target kWh, seconds)},
    standby maps entity to {hour index: (kWh, seconds)}. Keys are sorted as Go
    sorts them, the hour keys as strings.
    """
    pair_parts = []
    for source, target in sorted(pairs):
        cells = pairs[(source, target)]
        if not cells:
            continue
        hours = ",".join(
            f"{json.dumps(str(i))}:[{go_number(round_kwh(s))},{go_number(round_kwh(t))},{go_number(float(sec))}]"
            for i, (s, t, sec) in sorted(cells.items(), key=lambda kv: str(kv[0]))
        )
        pair_parts.append(
            f'{{"source":{json.dumps(source, ensure_ascii=False)},"target":{json.dumps(target, ensure_ascii=False)},"hours":{{{hours}}}}}'
        )
    standby_parts = []
    for entity in sorted(standby):
        cells = standby[entity]
        if not cells:
            continue
        hours = ",".join(
            f"{json.dumps(str(i))}:[{go_number(round_kwh(kwh))},{go_number(float(sec))}]"
            for i, (kwh, sec) in sorted(cells.items(), key=lambda kv: str(kv[0]))
        )
        standby_parts.append(
            f'{{"entity":{json.dumps(entity, ensure_ascii=False)},"hours":{{{hours}}}}}'
        )
    return (
        f'{{"version":{FORMAT_VERSION},"start":"{start.strftime("%Y-%m-%dT%H:%M:%SZ")}",'
        f'"pairs":[{",".join(pair_parts)}],"standby":[{",".join(standby_parts)}]}}'
    ).encode("utf-8")


def hour_plan(hour_of_day, rng):
    """Returns the mode of an hour and its power in W, drawn in a fixed order."""
    if 6 <= hour_of_day <= 16:
        frac = math.sin(math.pi * (hour_of_day - 5.5) / 11)
        return "charge", 2200 * frac * (0.85 + 0.3 * rng.random())
    if 17 <= hour_of_day <= 22:
        return "discharge", 600 + 400 * rng.random()
    return "standby", 4 + 4 * rng.random()


def chain_pairs(points, charging, power_w, rng):
    """Splits the power of a chain over its segments, losses per segment.

    Charging follows the point order, discharging runs against it.
    """
    order = list(points) if charging else list(reversed(points))
    pairs = {}
    flow = power_w
    for source, target in zip(order, order[1:]):
        efficiency = rng.uniform(0.9, 0.97)
        pairs[(source, target)] = (flow / 1000, flow * efficiency / 1000, HOUR_SECONDS)
        flow *= efficiency
    return pairs


def build_hours(chains, end):
    """Returns {month key: {"start", "pairs", "standby"}} for the window before end."""
    rng = random.Random(SEED)
    months = {}
    hour = end - timedelta(days=WINDOW_DAYS)
    while hour < end:
        mode, power = hour_plan(hour.hour, rng)
        start = month_start(hour)
        key = month_key(start)
        month = months.setdefault(key, {"start": start, "pairs": {}, "standby": {}})
        index = int((hour - start).total_seconds()) // HOUR_SECONDS
        for chain_id in sorted(chains):
            points = chains[chain_id].get("points") or []
            if not points:
                continue
            if mode == "standby":
                cells = month["standby"].setdefault(points[0], {})
                cells[index] = (power / 1000, HOUR_SECONDS)
                continue
            if len(points) < 2:
                continue
            for pair, value in chain_pairs(points, mode == "charge", power, rng).items():
                cells = month["pairs"].setdefault(pair, {})
                cells[index] = value
        hour += timedelta(hours=1)
    return months


def learned_loss(cls, forward):
    """Returns the loss fraction of a segment in a load class.

    The loss is smallest around class 6 (300 to 350 W), so the efficiency
    curve peaks in the middle. Segments against the chain order lose a bit more.
    """
    base = 0.02 if forward else 0.03
    return base + 0.004 * (cls - 6.2) ** 2


def learned_table(chains, end):
    """Returns the learned.json content of the chain segments in both directions.

    Every class holds [samples, source W, target W, deviation W], the values
    are rounded to 3 decimals as in the Go table.
    """
    forward = set()
    for chain_id in sorted(chains):
        points = chains[chain_id].get("points") or []
        forward.update(zip(points, points[1:]))
    both = set(forward) | {(t, s) for s, t in forward}
    first_seen = (end - timedelta(days=WINDOW_DAYS)).strftime("%Y-%m-%dT%H:%M:%SZ")
    entries = []
    for source, target in sorted(both):
        classes = []
        for cls in sorted(LEARNED_CLASSES, key=str):
            power = cls * LEARNED_CLASS_WIDTH_W + LEARNED_CLASS_WIDTH_W / 2
            target_w = power * (1 - learned_loss(cls, (source, target) in forward))
            dev_w = max(0.5, 0.003 * power)
            classes.append(
                f'"{cls}":[{LEARNED_SAMPLES},{go_number(round3(power))},'
                f"{go_number(round3(target_w))},{go_number(round3(dev_w))}]"
            )
        entries.append(
            f'{{"source":{json.dumps(source, ensure_ascii=False)},'
            f'"target":{json.dumps(target, ensure_ascii=False)},'
            f'"first_seen":"{first_seen}","scale_source":1,"scale_target":1,'
            f'"invert_source":false,"invert_target":false,"agree":{LEARNED_SAMPLES},'
            f'"opposite":0,"classes":{{{",".join(classes)}}}}}'
        )
    return f'{{"version":1,"pairs":[{",".join(entries)}]}}'.encode("utf-8")


def round3(v):
    """Rounds to 3 decimals, half away from zero, as the Go table does."""
    return go_round(v * 1000) / 1000


def write_month(path, data):
    tmp = path + ".tmp"
    with open(tmp, "wb") as f:
        f.write(data)
    os.replace(tmp, path)


def seed(data_dir, now):
    with open(os.path.join(data_dir, "energy.json"), encoding="utf-8") as f:
        chains = json.load(f).get("chains") or {}
    end = now.replace(minute=0, second=0, microsecond=0)
    target = os.path.join(data_dir, "energystats", "chains")
    os.makedirs(target, exist_ok=True)
    for key, month in sorted(build_hours(chains, end).items()):
        path = os.path.join(target, key + ".json")
        if os.path.exists(path):
            continue
        write_month(path, encode_month(month["start"], month["pairs"], month["standby"]))
    learned = os.path.join(target, LEARNED_FILE)
    if not os.path.exists(learned):
        write_month(learned, learned_table(chains, end))


def main(argv):
    if len(argv) != 3:
        print("Aufruf: seed_chain_stats.py <data_dir> <now_iso>", file=sys.stderr)
        return 2
    seed(argv[1], parse_now(argv[2]))
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
