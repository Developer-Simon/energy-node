# shellcheck shell=bash
# shellcheck disable=SC2154,SC2034 # Globals kommen vom Aufrufer, FAILED wird dort gelesen
# Pruefgruppe fuer das Monitoring: Meldungen als Admin, Quittung und Rücknahme,
# und Quittieren ohne das Recht Einrichten. Jede Funktion setzt bei einem Fehler
# FAILED=1 und laeuft ohne Abbruch weiter. Die Auswahl der Gruppen trifft der Lauf.
# Globals wie BASE, COOKIES, CURL_INSECURE, SECURE_HEADER, WORK kommen vom Aufrufer.
# check und check_status kommen aus core.sh.

checks_monitoring() {
  echo "==> Monitoring: Meldungen und Quittung"
  local csrf first_id guest_jar guest_csrf code

  check "Monitoring: Meldungen sind eine Liste, Lernwerte stehen bereit" \
    "isinstance(data['notices'], list) and isinstance(data['learned'], dict)" \
    "$BASE/api/v1/monitoring"

  csrf="$(api "$BASE/api/v1/auth/session" | python3 -c 'import json,sys; print(json.load(sys.stdin)["csrf_token"])')" || { FAILED=1; return; }
  first_id="$(api "$BASE/api/v1/monitoring" | python3 -c 'import json,sys; notices = json.load(sys.stdin)["notices"]; print(notices[0]["id"] if notices else "")')" || first_id=""

  # Quittung und Rücknahme an der ersten Meldung. Die Antwort kommt in die Datei.
  post_ack() { # Cookie-Datei, CSRF, ID, true|false -> HTTP-Code
    curl -s "${CURL_INSECURE[@]}" -m 5 -o "$WORK/monitoring-ack.json" -w '%{http_code}' -b "$1" \
      -H "$SECURE_HEADER" -H "X-CSRF-Token: $2" -H 'Content-Type: application/json' \
      -X POST "$BASE/api/v1/monitoring/ack" -d "{\"id\":\"$3\",\"ack\":$4}"
  }
  if [[ -z "$first_id" ]]; then
    echo "  OK   Monitoring: keine offene Meldung, Quittung nicht geprueft"
  else
    code="$(post_ack "$COOKIES" "$csrf" "$first_id" true)"
    if [[ "$code" == "200" ]] && python3 -c '
import json, sys
data = json.load(open(sys.argv[1]))
sys.exit(0 if any(n["id"] == sys.argv[2] and n["ack"] is not None for n in data["notices"]) else 1)
' "$WORK/monitoring-ack.json" "$first_id"; then
      echo "  OK   Quittung an $first_id gesetzt (HTTP 200)"
    else
      echo "  FEHL Quittung an $first_id (HTTP $code)"; FAILED=1
    fi
    code="$(post_ack "$COOKIES" "$csrf" "$first_id" false)"
    if [[ "$code" == "200" ]] && python3 -c '
import json, sys
data = json.load(open(sys.argv[1]))
sys.exit(0 if any(n["id"] == sys.argv[2] and n["ack"] is None for n in data["notices"]) else 1)
' "$WORK/monitoring-ack.json" "$first_id"; then
      echo "  OK   Quittung an $first_id zurueckgenommen (HTTP 200)"
    else
      echo "  FEHL Ruecknahme der Quittung an $first_id (HTTP $code)"; FAILED=1
    fi
  fi

  # Konto mit nur view_monitoring: sehen ja, quittieren nein (Recht Einrichten fehlt).
  curl -sf "${CURL_INSECURE[@]}" -m 5 -o /dev/null -H "$SECURE_HEADER" -H "X-CSRF-Token: $csrf" \
    -H 'Content-Type: application/json' -X POST "$BASE/api/v1/users" -b "$COOKIES" \
    -d '{"username":"smoke-monitoring","password":"smoke-monitoring-1","roles":["view_monitoring"]}' || FAILED=1
  guest_jar="$WORK/smoke-monitoring-cookies.txt"
  curl -sf "${CURL_INSECURE[@]}" -m 5 -c "$guest_jar" -H "$SECURE_HEADER" -H 'Content-Type: application/json' \
    -X POST "$BASE/api/v1/auth/login" -d '{"username":"smoke-monitoring","password":"smoke-monitoring-1"}' > /dev/null || FAILED=1
  guest_csrf="$(curl -sf "${CURL_INSECURE[@]}" -m 5 -b "$guest_jar" -H "$SECURE_HEADER" "$BASE/api/v1/auth/session" \
    | python3 -c 'import json,sys; print(json.load(sys.stdin)["csrf_token"])')" || guest_csrf=""
  check_status "Monitoring fuer das Konto mit view_monitoring erlaubt" "!403" "$guest_jar" "$BASE/api/v1/monitoring"
  code="$(post_ack "$guest_jar" "$guest_csrf" "${first_id:-energy:smoke-probe}" true)"
  if [[ "$code" == "403" ]]; then
    echo "  OK   Quittieren ohne Recht Einrichten abgelehnt (HTTP 403)"
  else
    echo "  FEHL Quittieren ohne Recht Einrichten kam mit HTTP $code durch"; FAILED=1
  fi
}
