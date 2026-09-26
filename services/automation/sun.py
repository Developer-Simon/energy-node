"""Kalendarischer Sonnenauf- und -untergang ohne Abhaengigkeit und ohne Netz.

NOAA-Algorithmus (Solar Calculator, Meeus-Naeherung) mit dem ueblichen Zenit
von 90.833 Grad: Sonnenscheibe voll unter dem Horizont inklusive Refraktion.
Genauigkeit rund eine Minute in mittleren Breiten - genug fuer Regeln, die
im Minutentakt entscheiden."""

from __future__ import annotations

import datetime
import math
from typing import Optional

_ZENITH_DEG = 90.833


def _julian_day(moment: datetime.datetime) -> float:
    return moment.timestamp() / 86400.0 + 2440587.5


def sun_times(day: datetime.date, latitude: float, longitude: float) -> Optional[tuple[float, float]]:
    """Liefert (sunrise_ts, sunset_ts) als Unix-Zeitstempel fuer ``day`` am
    Standort, oder None, wenn die Sonne an dem Tag nicht auf- oder untergeht
    (Polartag/-nacht).

    ``day`` ist der Kalendertag, dessen Sonnenmittag gemeint ist. Fuer alle
    Zeitzonen, deren Mittag nicht an der Datumsgrenze liegt, ist das derselbe
    Tag wie das lokale Datum."""
    noon_guess = datetime.datetime(day.year, day.month, day.day, 12, tzinfo=datetime.timezone.utc) \
        - datetime.timedelta(hours=longitude / 15.0)
    t = (_julian_day(noon_guess) - 2451545.0) / 36525.0

    mean_long = math.radians((280.46646 + t * (36000.76983 + t * 0.0003032)) % 360)
    mean_anom = math.radians(357.52911 + t * (35999.05029 - 0.0001537 * t))
    ecc = 0.016708634 - t * (0.000042037 + 0.0000001267 * t)
    center = (math.sin(mean_anom) * (1.914602 - t * (0.004817 + 0.000014 * t))
              + math.sin(2 * mean_anom) * (0.019993 - 0.000101 * t)
              + math.sin(3 * mean_anom) * 0.000289)
    omega = math.radians(125.04 - 1934.136 * t)
    app_long = math.radians(math.degrees(mean_long) + center - 0.00569 - 0.00478 * math.sin(omega))
    obliq0 = 23 + (26 + (21.448 - t * (46.815 + t * (0.00059 - t * 0.001813))) / 60) / 60
    obliq = math.radians(obliq0 + 0.00256 * math.cos(omega))
    decl = math.asin(math.sin(obliq) * math.sin(app_long))

    y = math.tan(obliq / 2) ** 2
    eq_time_min = 4 * math.degrees(
        y * math.sin(2 * mean_long)
        - 2 * ecc * math.sin(mean_anom)
        + 4 * ecc * y * math.sin(mean_anom) * math.cos(2 * mean_long)
        - 0.5 * y * y * math.sin(4 * mean_long)
        - 1.25 * ecc * ecc * math.sin(2 * mean_anom))

    lat = math.radians(latitude)
    cos_hour_angle = (math.cos(math.radians(_ZENITH_DEG)) / (math.cos(lat) * math.cos(decl))
                      - math.tan(lat) * math.tan(decl))
    if not -1.0 <= cos_hour_angle <= 1.0:
        return None
    hour_angle_deg = math.degrees(math.acos(cos_hour_angle))

    noon_min = 720 - 4 * longitude - eq_time_min
    midnight = datetime.datetime(day.year, day.month, day.day, tzinfo=datetime.timezone.utc).timestamp()
    return (midnight + (noon_min - 4 * hour_angle_deg) * 60,
            midnight + (noon_min + 4 * hour_angle_deg) * 60)
