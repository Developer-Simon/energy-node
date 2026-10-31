# shellcheck shell=bash
# shellcheck disable=SC2154,SC2034 # Globals kommen vom Aufrufer, FAILED wird dort gelesen
# Pruefgruppe fuer die Einrichtungsfuehrung: Rolle und Fortschritt des Admins,
# der eigene Fortschritt (gesehen, Warteschlange) ueber zwei Anfragen und die
# Schritte der Installation, die nur Admins aendern duerfen. Die Gruppe setzt
# bei einem Fehler FAILED=1 und laeuft ohne Abbruch weiter.
# Globals wie BASE, COOKIES, CURL_INSECURE, SECURE_HEADER, WORK, ADMIN_CSRF kommen
# vom Aufrufer. ADMIN_CSRF kommt aus env_login, check aus core.sh.

checks_onboarding() {
  echo "==> Einrichtungsfuehrung: Fortschritt des Admins"
  check "Einrichtung: Admin hat Rolle admin, Installation ist noch nicht eingerichtet" \
    "data['role'] == 'admin' and data['setup']['done'] is False" \
    "$BASE/api/v1/onboarding"

  # Eigener Fortschritt: gesehen und Warteschlange, die zweite Anfrage liest sie zurueck.
  code=$(curl -s "${CURL_INSECURE[@]}" -m 5 -o /dev/null -w '%{http_code}' -b "$COOKIES" -H "$SECURE_HEADER" \
    -H "X-CSRF-Token: $ADMIN_CSRF" -H 'Content-Type: application/json' -X PUT "$BASE/api/v1/onboarding/me" \
    -d '{"seen_add":["h999"],"queue":["explore"]}')
  if [[ "$code" == "200" ]]; then
    echo "  OK   Fortschritt des Admins gespeichert (HTTP 200)"
  else
    echo "  FEHL Fortschritt des Admins kam mit HTTP $code"; FAILED=1
  fi
  check "Einrichtung: gesehen und Warteschlange bleiben bei der naechsten Anfrage erhalten" \
    "'h999' in data['user']['seen'] and data['user']['queue'] == ['explore']" \
    "$BASE/api/v1/onboarding"

  # Schritte der Installation: der Admin setzt einen Schritt auf spaeter.
  code=$(curl -s "${CURL_INSECURE[@]}" -m 5 -o /dev/null -w '%{http_code}' -b "$COOKIES" -H "$SECURE_HEADER" \
    -H "X-CSRF-Token: $ADMIN_CSRF" -H 'Content-Type: application/json' -X PUT "$BASE/api/v1/onboarding/step" \
    -d '{"id":"theme","state":"later"}')
  if [[ "$code" == "200" ]]; then
    echo "  OK   Schritt theme vom Admin auf spaeter gesetzt (HTTP 200)"
  else
    echo "  FEHL Schritt theme kam mit HTTP $code"; FAILED=1
  fi
  check "Einrichtung: Schritt theme steht auf later" \
    "data['setup']['steps'].get('theme') == 'later'" \
    "$BASE/api/v1/onboarding"

  # Eingeschraenktes Konto mit nur view_overview: Rolle user, Schritte bleiben verboten.
  echo "==> Einrichtungsfuehrung: eingeschraenktes Konto"
  ONBOARDING_USER="smoke-onboarding"
  ONBOARDING_PASSWORD="smoke-onboarding-pass-1"
  ONBOARDING_COOKIES="$WORK/smoke-onboarding-cookies.txt"
  curl -sf "${CURL_INSECURE[@]}" -m 5 -o /dev/null -H "$SECURE_HEADER" -H "X-CSRF-Token: $ADMIN_CSRF" \
    -H 'Content-Type: application/json' -X POST "$BASE/api/v1/users" \
    -b "$COOKIES" -d "{\"username\":\"$ONBOARDING_USER\",\"password\":\"$ONBOARDING_PASSWORD\",\"roles\":[\"view_overview\"]}" || FAILED=1
  curl -sf "${CURL_INSECURE[@]}" -m 5 -c "$ONBOARDING_COOKIES" -H "$SECURE_HEADER" -H 'Content-Type: application/json' \
    -X POST "$BASE/api/v1/auth/login" -d "{\"username\":\"$ONBOARDING_USER\",\"password\":\"$ONBOARDING_PASSWORD\"}" > /dev/null || FAILED=1
  ONBOARDING_CSRF="$(curl -sf "${CURL_INSECURE[@]}" -m 5 -b "$ONBOARDING_COOKIES" -H "$SECURE_HEADER" \
    "$BASE/api/v1/auth/session" | python3 -c 'import json,sys; print(json.load(sys.stdin)["csrf_token"])')" || ONBOARDING_CSRF=""
  # check nutzt das Admin-Cookie, deshalb hier die Anfrage mit dem Konto selbst.
  if curl -sf "${CURL_INSECURE[@]}" -m 5 -b "$ONBOARDING_COOKIES" -H "$SECURE_HEADER" "$BASE/api/v1/onboarding" \
    | python3 -c '
import json, sys
sys.exit(0 if json.load(sys.stdin)["role"] == "user" else 1)
'; then
    echo "  OK   eingeschraenktes Konto hat Rolle user"
  else
    echo "  FEHL eingeschraenktes Konto hat nicht die Rolle user"; FAILED=1
  fi
  code=$(curl -s "${CURL_INSECURE[@]}" -m 5 -o /dev/null -w '%{http_code}' -b "$ONBOARDING_COOKIES" -H "$SECURE_HEADER" \
    -H "X-CSRF-Token: $ONBOARDING_CSRF" -H 'Content-Type: application/json' -X PUT "$BASE/api/v1/onboarding/step" \
    -d '{"id":"theme","state":"done"}')
  if [[ "$code" == "403" ]]; then
    echo "  OK   Schritt theme fuer eingeschraenktes Konto abgelehnt (HTTP 403)"
  else
    echo "  FEHL Schritt theme fuer eingeschraenktes Konto kam mit HTTP $code durch"; FAILED=1
  fi
}

# Lauf seed: die Vorgaben aus onboarding-seed.json sind beim Start uebernommen
# (Schritte als installer, spaeter-Schritt als later) und die Datei ist weg.
checks_onboarding_seed() {
  echo "==> Einrichtungsfuehrung: Seed-Datei des Installers"
  check "Seed: Schritte theme und users stehen auf installer" \
    "data['setup']['steps'].get('theme') == 'installer' and data['setup']['steps'].get('users') == 'installer'" \
    "$BASE/api/v1/onboarding"
  check "Seed: Schritt guest steht auf later" \
    "data['setup']['steps'].get('guest') == 'later'" \
    "$BASE/api/v1/onboarding"
  if [[ -e "$WORK/onboarding-seed.json" ]]; then
    echo "  FEHL Seed-Datei ist nach dem Start noch vorhanden"; FAILED=1
  else
    echo "  OK   Seed-Datei nach dem Start geloescht"
  fi
}
