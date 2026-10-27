# shellcheck shell=bash
# shellcheck disable=SC2154,SC2034 # Globals kommen vom Aufrufer, FAILED wird dort gelesen
# Pruefgruppen fuer Zugriff: Service-Logs (system-action-Helper) und
# Benutzerverwaltung mit Sichten je Konto. Jede Funktion setzt bei einem Fehler
# FAILED=1 und laeuft ohne Abbruch weiter. Die Auswahl der Gruppen trifft der Lauf.
# Globals wie BASE, COOKIES, CURL_INSECURE, SECURE_HEADER, WORK kommen vom Aufrufer.
# check_status kommt aus core.sh.

checks_journal() {
  # Service-Logs end-to-end, vom Endpunkt ueber sudo-Shim und Fake-Helper bis
  # zu den geparsten Zeilen.
  check "Service-Logs: Einheiten-Auswahl nennt elf Dienste" \
    "len(data['units']) == 11 and data['default_unit'] == 'dashboard'" \
    "$BASE/api/v1/system/logs/units"
  check "Service-Logs: Dashboard liefert Zeilen mit Prioritaet und Einheit" \
    "len(data['entries']) > 100 and data['cursor'].startswith('s=') and all('priority' in e and e['unit'] for e in data['entries'])" \
    "$BASE/api/v1/system/logs?unit=dashboard&since=60&lines=200"
  check "Service-Logs: Suchbegriff filtert, der Cursor rueckt trotzdem vor" \
    "all('health' in e['message'].lower() for e in data['entries']) and data['cursor'].startswith('s=')" \
    "$BASE/api/v1/system/logs?unit=dashboard&since=60&lines=200&q=HEALTH"
  check "Service-Logs: nicht installierter Dienst liefert eine leere Liste" \
    "data['entries'] == [] and data['cursor'] == ''" \
    "$BASE/api/v1/system/logs?unit=caddy&since=60&lines=200"
  code=$(curl -s "${CURL_INSECURE[@]}" -m 5 -o /dev/null -w '%{http_code}' -b "$COOKIES" -H "$SECURE_HEADER" \
    "$BASE/api/v1/system/logs?unit=sshd&since=60&lines=200")
  if [[ "$code" == "400" ]]; then
    echo "  OK   Service-Logs: Dienst ausserhalb der Allowlist abgelehnt (HTTP 400)"
  else
    echo "  FEHL Service-Logs: unbekannter Dienst kam mit HTTP $code durch"; FAILED=1
  fi
}

checks_users() {
  # Benutzerverwaltung: ein Konto mit nur view_overview meldet sich ueber HTTPS an.
  # Der Admin entzieht die Berechtigung, dieselbe Sitzung sieht sie ohne
  # Neustart nicht mehr.
  echo "==> Benutzerverwaltung: eingeschraenktes Konto"
  ADMIN_CSRF="$(api "$BASE/api/v1/auth/session" | python3 -c 'import json,sys; print(json.load(sys.stdin)["csrf_token"])')"
  SMOKE_USER="smoke-user"
  SMOKE_PASSWORD="smoke-user-pass-1"
  USER_COOKIES="$WORK/smoke-user-cookies.txt"
  curl -sf "${CURL_INSECURE[@]}" -m 5 -o /dev/null -H "$SECURE_HEADER" -H "X-CSRF-Token: $ADMIN_CSRF" \
    -H 'Content-Type: application/json' -X POST "$BASE/api/v1/users" \
    -b "$COOKIES" -d "{\"username\":\"$SMOKE_USER\",\"password\":\"$SMOKE_PASSWORD\",\"roles\":[\"view_overview\"]}" || FAILED=1
  curl -sf "${CURL_INSECURE[@]}" -m 5 -c "$USER_COOKIES" -H "$SECURE_HEADER" -H 'Content-Type: application/json' \
    -X POST "$BASE/api/v1/auth/login" -d "{\"username\":\"$SMOKE_USER\",\"password\":\"$SMOKE_PASSWORD\"}" > /dev/null || FAILED=1
  user_api() { curl -sf "${CURL_INSECURE[@]}" -m 5 -b "$USER_COOKIES" -H "$SECURE_HEADER" "$@"; }
  if user_api "$BASE/api/v1/auth/session" | python3 -c '
import json, sys
sys.exit(0 if json.load(sys.stdin)["roles"] == ["view_overview"] else 1)
'; then
    echo "  OK   smoke-user meldet sich mit view_overview an"
  else
    echo "  FEHL smoke-user hat nach der Anmeldung nicht genau view_overview"; FAILED=1
  fi
  code=$(curl -s "${CURL_INSECURE[@]}" -m 5 -o /dev/null -w '%{http_code}' -b "$USER_COOKIES" -H "$SECURE_HEADER" \
    "$BASE/api/v1/system/logs?unit=dashboard&since=60&lines=10")
  if [[ "$code" == "403" ]]; then
    echo "  OK   Service-Logs fuer smoke-user abgelehnt (HTTP 403)"
  else
    echo "  FEHL Service-Logs fuer smoke-user kam mit HTTP $code durch"; FAILED=1
  fi
  code=$(curl -s "${CURL_INSECURE[@]}" -m 5 -o /dev/null -w '%{http_code}' -b "$COOKIES" -H "$SECURE_HEADER" \
    -H "X-CSRF-Token: $ADMIN_CSRF" -H 'Content-Type: application/json' -X PUT "$BASE/api/v1/users/$SMOKE_USER" \
    -d '{"roles":[],"disabled":false}')
  if [[ "$code" == "200" ]]; then
    echo "  OK   view_overview fuer smoke-user entzogen"
  else
    echo "  FEHL Entziehen der Berechtigung kam mit HTTP $code"; FAILED=1
  fi
  if user_api "$BASE/api/v1/auth/session" | python3 -c '
import json, sys
sys.exit(0 if json.load(sys.stdin)["roles"] == [] else 1)
'; then
    echo "  OK   dieselbe Sitzung hat ohne Neustart keine Berechtigung mehr"
  else
    echo "  FEHL dieselbe Sitzung behaelt die entzogene Berechtigung"; FAILED=1
  fi

  # Ansehen auf Seitenebene: ein Konto mit nur view_stats sieht nur seine Seite.
  # Der Admin ergaenzt view_automations, dieselbe Sitzung darf sofort mehr.
  echo "==> Ansehen: eingeschraenktes Konto mit view_stats"
  create_smoke_user() { # Name, Rollen (JSON), Cookie-Datei
    curl -sf "${CURL_INSECURE[@]}" -m 5 -o /dev/null -H "$SECURE_HEADER" -H "X-CSRF-Token: $ADMIN_CSRF" \
      -H 'Content-Type: application/json' -X POST "$BASE/api/v1/users" \
      -b "$COOKIES" -d "{\"username\":\"$1\",\"password\":\"$SMOKE_PASSWORD\",\"roles\":$2}" || FAILED=1
    curl -sf "${CURL_INSECURE[@]}" -m 5 -c "$3" -H "$SECURE_HEADER" -H 'Content-Type: application/json' \
      -X POST "$BASE/api/v1/auth/login" -d "{\"username\":\"$1\",\"password\":\"$SMOKE_PASSWORD\"}" > /dev/null || FAILED=1
  }
  VIEWER_COOKIES="$WORK/smoke-viewer-cookies.txt"
  create_smoke_user "smoke-viewer" '["view_stats"]' "$VIEWER_COOKIES"
  check_status "Verlaufs-Entitaeten (anyView) fuer smoke-viewer erlaubt" "!403" "$VIEWER_COOKIES" "$BASE/api/v1/history/entities"
  check_status "Diagnose fuer smoke-viewer gesperrt" 403 "$VIEWER_COOKIES" "$BASE/api/v1/diagnostics"
  if curl -s "${CURL_INSECURE[@]}" -m 5 -b "$VIEWER_COOKIES" -H "$SECURE_HEADER" "$BASE/api/v1/diagnostics" | grep -q '"view_forbidden"'; then
    echo "  OK   Diagnose-Sperre nennt view_forbidden"
  else
    echo "  FEHL Diagnose-Sperre nennt nicht view_forbidden"; FAILED=1
  fi
  check_status "Verlaufs-Panel fuer smoke-viewer gesperrt" 403 "$VIEWER_COOKIES" "$BASE/?fragment=panel&panel=history"
  viewer_page="$(curl -sf "${CURL_INSECURE[@]}" -m 5 -b "$VIEWER_COOKIES" -H "$SECURE_HEADER" "$BASE/")"
  if grep -q 'id="tab-stats"' <<<"$viewer_page" && ! grep -q 'id="tab-history"' <<<"$viewer_page"; then
    echo "  OK   Startseite zeigt Statistik-Tab, aber keinen Verlaufs-Tab"
  else
    echo "  FEHL Startseite von smoke-viewer hat falsche Tabs"; FAILED=1
  fi
  code=$(curl -s "${CURL_INSECURE[@]}" -m 5 -o /dev/null -w '%{http_code}' -b "$COOKIES" -H "$SECURE_HEADER" \
    -H "X-CSRF-Token: $ADMIN_CSRF" -H 'Content-Type: application/json' -X PUT "$BASE/api/v1/users/smoke-viewer" \
    -d '{"roles":["view_stats","view_automations"],"disabled":false}')
  if [[ "$code" == "200" ]]; then
    echo "  OK   view_automations fuer smoke-viewer vergeben"
  else
    echo "  FEHL Vergeben von view_automations kam mit HTTP $code"; FAILED=1
  fi
  check_status "Automations-Zustand ohne Neustart fuer dieselbe Sitzung erlaubt" "!403" "$VIEWER_COOKIES" "$BASE/api/v1/automation/state"

  # Verlauf speichern: ein Konto mit nur view_history darf history_views ueber
  # PUT /api/v1/settings schreiben, eine Theme-Aenderung bleibt verboten.
  echo "==> Ansehen: Konto mit view_history speichert Verlaufs-Sichten"
  HISTORY_COOKIES="$WORK/smoke-history-cookies.txt"
  create_smoke_user "smoke-history" '["view_history"]' "$HISTORY_COOKIES"
  HISTORY_CSRF="$(curl -sf "${CURL_INSECURE[@]}" -m 5 -b "$HISTORY_COOKIES" -H "$SECURE_HEADER" "$BASE/api/v1/auth/session" | python3 -c 'import json,sys; print(json.load(sys.stdin)["csrf_token"])')"
  history_settings="$WORK/smoke-history-settings.json"
  curl -sf "${CURL_INSECURE[@]}" -m 5 -b "$HISTORY_COOKIES" -H "$SECURE_HEADER" "$BASE/api/v1/settings" \
    | python3 -c '
import json, sys
value = json.load(sys.stdin)
value["history_views"] = value.get("history_views", []) + [{"id": "smoke-history-view", "name": "Smoke Verlauf", "series": [], "range_hours": 24, "aggregate": "avg"}]
json.dump(value, sys.stdout)
' > "$history_settings" || FAILED=1
  code=$(curl -s "${CURL_INSECURE[@]}" -m 5 -o /dev/null -w '%{http_code}' -b "$HISTORY_COOKIES" -H "$SECURE_HEADER" \
    -H "X-CSRF-Token: $HISTORY_CSRF" -H 'Content-Type: application/json' -X PUT "$BASE/api/v1/settings" \
    --data-binary "@$history_settings")
  if [[ "$code" == "200" ]]; then
    echo "  OK   smoke-history speichert history_views (HTTP 200)"
  else
    echo "  FEHL smoke-history kann history_views nicht speichern (HTTP $code)"; FAILED=1
  fi
  python3 -c '
import json, sys
value = json.load(open(sys.argv[1]))
value["theme"] = "stromblau" if value["theme"] != "stromblau" else "mint"
json.dump(value, open(sys.argv[1], "w"))
' "$history_settings"
  code=$(curl -s "${CURL_INSECURE[@]}" -m 5 -o /dev/null -w '%{http_code}' -b "$HISTORY_COOKIES" -H "$SECURE_HEADER" \
    -H "X-CSRF-Token: $HISTORY_CSRF" -H 'Content-Type: application/json' -X PUT "$BASE/api/v1/settings" \
    --data-binary "@$history_settings")
  if [[ "$code" == "403" ]]; then
    echo "  OK   Theme-Aenderung fuer smoke-history abgelehnt (HTTP 403)"
  else
    echo "  FEHL Theme-Aenderung fuer smoke-history kam mit HTTP $code durch"; FAILED=1
  fi
}
