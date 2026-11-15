# shellcheck shell=bash
# shellcheck disable=SC2154,SC2034 # Globals kommen vom Aufrufer, FAILED wird dort gelesen
# Pruefgruppe Leistung: die API liefert Budget und Messwerte, die Seite liefert
# die beiden Tour-Ziele. Der Lauf haupt setzt ENERGY_NODE_PERFORMANCE_FAKE=1, damit
# die Werte nicht vom Prozessverbrauch des Testrechners abhaengen. Jede Funktion
# setzt bei einem Fehler FAILED=1 und laeuft weiter.
# Globals wie BASE, COOKIES, CURL_INSECURE, SECURE_HEADER kommen vom Aufrufer.

checks_performance() {
  echo "==> Leistung: API mit Budget und Messwerten, Seite mit Tour-Zielen"

  check_status "Leistungs-API liefert HTTP 200" 200 "$COOKIES" "$BASE/api/v1/performance"
  check "Leistungs-API hat Zeilen, Einheiten, Ziel und Messdauer" \
    "all(k in data for k in ('rows', 'units', 'target_pct', 'measured_s'))" \
    "$BASE/api/v1/performance"

  check_status "Leistungs-Unterseite liefert HTTP 200" 200 "$COOKIES" \
    "$BASE/?fragment=settings-page&page=performance"
  html_has "Leistungs-Unterseite traegt das Tour-Ziel Budget" \
    'data-tour="performance-budget"' "$BASE/?fragment=settings-page&page=performance"
  html_has "Leistungs-Unterseite traegt das Tour-Ziel Messwerte" \
    'data-tour="performance-measure"' "$BASE/?fragment=settings-page&page=performance"
  html_has "Leistungs-Unterseite traegt das Tour-Ziel Abfrageraten" \
    'data-tour="performance-rates"' "$BASE/?fragment=settings-page&page=performance"
}
