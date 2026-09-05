from __future__ import annotations

# ---------------------------------------------------------------------------
# Lastkompensation (IR-Ausgleich), grobe Startwerte in mV/Zelle je C-Rate-Bin
# C-Rate = |Strom| / Nennkapazitaet_Ah. Bin-Grenzen sind die oberen Enden.
# Aequivalent zur "Spannungskurvenschar" aus einer einzigen Ruhespannungskurve.
LOAD_OFFSET_TABLE_MV = [
    (0.05, 5),    # bis 0.05C: quasi Ruhespannung, kaum Korrektur
    (0.2, 25),
    (0.5, 60),
    (1.0, 120),
    (float("inf"), 200),
]

# --- Normierte LiFePO4-Ruhespannungskurven ---
# Stuetzstellen sind (SoC-Anteil, Anteil im Intervall
# [empty_v_per_cell, full_v_per_cell]). Bewusst relativ statt in absoluten
# Volt: so bleibt die Kurve auch bei geaenderten Schwellen monoton zwischen
# ihnen. Die absoluten Spannungen einer Kurve haengen damit KOMPLETT an den
# beiden Schwellen - stehen die falsch, verschiebt sich die ganze Kurve mit,
# und der flache Mittelteil landet an der falschen Spannung. Beim Umstellen
# der Kurve also immer beide Schwellen mit pruefen.

# Generischer LiFePO4-Verlauf, historischer Standard dieses Dienstes. Keine
# Herstellerquelle - eine Lehrbuchform mit steilem unterem Ast und langem
# flachen Mittelteil. Passt, solange kein Datenblatt der verbauten Zellen
# vorliegt.
GENERIC_LIFEPO4_CURVE = [
    (0.00, 0.00),
    (0.05, 0.48),
    (0.15, 0.67),
    (0.85, 0.76),
    (0.95, 0.86),
    (1.00, 1.00),
]

# Dyness AR2.5-24V, Benutzerhandbuch Tabelle 2-1 (Ruhespannung nach >= 3 h
# ohne Strom). Mit den Schwellen 2,70 / 3,50 V je Zelle trifft diese Kurve
# jede Zeile der Tabelle exakt:
#   21,6 V = 0 % | 25,8 V = 20 % | 26,0 V = 30 %
#   26,4 V = 70 % | 26,6 V = 95 % | 28,0 V = 100 %   (8 Zellen in Reihe)
# Die beiden Stuetzstellen bei 5 % und 10 % stehen NICHT im Datenblatt. Sie
# ersetzen dessen grobes 1-20-%-Band (das ueber 4,2 V linear interpoliert
# und den Ladezustand dort deutlich ueberschaetzt) durch den realen steilen
# Anstieg am unteren Ende.
DYNESS_AR25_CURVE = [
    (0.00, 0.00000),   # 21,6 V - Datenblatt 0 %
    (0.05, 0.42500),   # 24,3 V - interpoliert, nicht aus dem Datenblatt
    (0.10, 0.53750),   # 25,0 V - interpoliert, nicht aus dem Datenblatt
    (0.20, 0.65625),   # 25,8 V - Datenblatt 20 %
    (0.30, 0.68750),   # 26,0 V - Datenblatt 30 %
    (0.70, 0.75000),   # 26,4 V - Datenblatt 70 %
    (0.95, 0.78125),   # 26,6 V - Datenblatt 95 %
    (1.00, 1.00000),   # 28,0 V - Datenblatt 100 %
]

SOC_CURVES = {
    "generic_lifepo4": GENERIC_LIFEPO4_CURVE,
    "dyness_ar2.5": DYNESS_AR25_CURVE,
}

# --- Toleranzfenster der Kalibrierung (stromabhaengig) ---
# Das Problem: die Schwellen empty/full_v_per_cell sind RUHESPANNUNGEN. Der
# Ladestrom-Regler erreicht die Vollspannung womoeglich nie (die CV-Schwelle
# des Ladegeraets liegt tiefer als die 100-%-Ruhespannung), und der
# Wechselrichter schaltet vor der Leerspannung ab. Dann feuert die
# Kalibrierung NIE und der Coulomb-Zaehler treibt ohne Ankerpunkt.
#
# Die Loesung nutzt aus, dass der Strom an genau diesen Punkten aussagekraeftig
# wird. Ein LiFePO4-Paket in der CV-Phase nimmt immer weniger Strom auf, je
# voller es wird; umgekehrt ist eine tiefe Spannung bei kleinem Entladestrom
# nicht Lastdurchhang, sondern ein wirklich leeres Paket.
#
# Formal ist das ein VERTRAUENSFENSTER auf die lastkorrigierte Spannung: die
# IR-Kompensation (LOAD_OFFSET_TABLE_MV) zieht bei 1C bis zu 200 mV/Zelle ab
# und ist dort selbst grob geschaetzt - die korrigierte Spannung darf also
# nicht knapp danebenliegen duerfen. Bei kleinem Strom betraegt die Korrektur
# 5 mV, die korrigierte Spannung ist praktisch die Ruhespannung, und ein Wert
# der die Schwelle knapp verfehlt darf trotzdem kalibrieren.
#
# Daraus folgt die Bandform: volle Toleranz unterhalb TAPER (inklusive 0 A -
# das Paket ruht, die Messung ist dann am vertrauenswuerdigsten), linear
# fallend bis BULK, darueber keine. Die Toleranz ist bewusst richtungsblind:
# sie haengt am Betrag der C-Rate, nicht am Vorzeichen, und weicht beide
# Schwellen gleich weit auf.
CALIBRATION_TAPER_C_RATE = 0.02   # darunter: volle Toleranz
CALIBRATION_BULK_C_RATE = 0.10    # darueber: keine Toleranz

# Obergrenze fuer einen einzelnen Integrationsschritt. Schuetzt den Coulomb-
# Zaehler gegen Zeitspruenge (NTP-Korrektur, Suspend, haengender Scheduler).
MAX_TICK_HOURS = 1.0
# Unterhalb dieser Netto-Leistung ist eine Zeitschaetzung sinnlos (der Wert
# liefe gegen unendlich) - keine eigene Einstellung, den Knopf ist es nicht wert.
MIN_TIME_ESTIMATE_W = 10.0


def load_offset_mv(c_rate):
    for upper_bound, offset in LOAD_OFFSET_TABLE_MV:
        if c_rate <= upper_bound:
            return offset
    return LOAD_OFFSET_TABLE_MV[-1][1]


def soc_curve_for(curve_key):
    """Stuetzstellen der benannten Kurve. Ein unbekannter Schluessel faellt auf
    die generische Kurve zurueck statt zu stoppen - eine vertippte Kurve darf
    die SoC-Schaetzung verschlechtern, aber nicht die Ueberwachung abschalten."""
    curve = SOC_CURVES.get(curve_key)
    if curve is None:
        print(f"WARNUNG: unbekannte SoC-Kurve '{curve_key}', benutze 'generic_lifepo4'")
        return GENERIC_LIFEPO4_CURVE
    return curve
