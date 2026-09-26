import datetime

import pytest

import sun

# Referenzwerte aus astral 3.x (unabhaengige Implementierung, Zenit 90.833 Grad),
# in UTC. Toleranz 2 Minuten - genug fuer eine Minuten-Automation.
REFERENCE = [
    ("berlin", 52.52, 13.405, "2024-06-21", "02:43:34", "19:32:59"),
    ("berlin", 52.52, 13.405, "2024-12-21", "07:15:32", "14:53:49"),
    ("berlin", 52.52, 13.405, "2024-03-31", "04:42:30", "17:39:20"),
    ("berlin", 52.52, 13.405, "2024-10-27", "05:54:27", "15:45:04"),
    ("muenchen", 48.137, 11.575, "2024-06-21", "03:13:52", "19:17:20"),
    ("muenchen", 48.137, 11.575, "2024-12-21", "07:01:48", "15:22:12"),
    ("muenchen", 48.137, 11.575, "2024-03-31", "04:53:34", "17:42:45"),
    ("muenchen", 48.137, 11.575, "2024-10-27", "05:52:00", "16:02:16"),
]


def _utc(day: str, clock: str) -> float:
    return datetime.datetime.fromisoformat(f"{day}T{clock}+00:00").timestamp()


@pytest.mark.parametrize("name,lat,lon,day,rise,set_", REFERENCE)
def test_sun_times_matches_reference(name, lat, lon, day, rise, set_):
    result = sun.sun_times(datetime.date.fromisoformat(day), lat, lon)
    assert result is not None
    sunrise_ts, sunset_ts = result
    assert abs(sunrise_ts - _utc(day, rise)) < 120
    assert abs(sunset_ts - _utc(day, set_)) < 120


def test_sun_times_polar_day_returns_none():
    assert sun.sun_times(datetime.date(2024, 6, 21), 78.22, 15.65) is None


def test_sun_times_polar_night_returns_none():
    assert sun.sun_times(datetime.date(2024, 12, 21), 78.22, 15.65) is None
