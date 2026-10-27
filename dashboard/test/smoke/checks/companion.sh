# shellcheck shell=bash
# shellcheck disable=SC2154,SC2034 # Globals kommen vom Aufrufer, FAILED wird dort gelesen
# Pruefgruppen fuer den Home-Assistant-Companion: Kopplung, Anmeldung eines
# HA-Benutzers und Sitzung am Companion-Port, dazu der Verlauf-Austausch zwischen
# zwei Peers. Jede Funktion setzt bei einem Fehler FAILED=1 und laeuft ohne Abbruch weiter.
# Globals wie BASE, COOKIES, ADMIN_CSRF, FAKES, WORK, HTTPS, COMPANION_PORT kommen vom Aufrufer.
# ADMIN_CSRF kommt aus checks_users (access.sh).

checks_companion() {
  # Kopplung am Companion-Port, Anmeldung eines HA-Benutzers ueber den
  # Pruefstrom. fake_ha_companion.py spielt die Integration.
  echo "==> Companion: Kopplung und Home-Assistant-Anmeldung"
  COMPANION_BASE="https://127.0.0.1:$COMPANION_PORT"
  HA_USER="maria"
  HA_PASSWORD="maria-smoke-pass-1"
  HA_USER_ID="ha-smoke-maria"
  HA_TOKEN_FILE="$WORK/companion-token"
  HA_COOKIES="$WORK/smoke-ha-cookies.txt"
  pairing_code="$(api -X POST -H "X-CSRF-Token: $ADMIN_CSRF" -H 'Content-Type: application/json' \
    "$BASE/api/v1/users/companion/pairing-code" | python3 -c 'import json,sys; print(json.load(sys.stdin)["code"])')" || pairing_code=""
  if [[ -z "$pairing_code" ]]; then
    echo "  FEHL Kopplungscode konnte nicht angelegt werden"; FAILED=1
  else
    # Der Companion-Port liegt im Dashboard-Prozess, er kann kurz nach dem Start
    # noch nicht antworten. GET ohne Token ergibt 401 - jede Antwort zaehlt.
    for _ in $(seq 1 20); do
      [[ "$(curl -sk -m 2 -o /dev/null -w '%{http_code}' "$COMPANION_BASE/api/v1/companion/pair")" != "000" ]] && break
      sleep 1
    done
    python3 "$FAKES/fake_ha_companion.py" --companion "$COMPANION_BASE" --code "$pairing_code" \
      --user "$HA_USER" --password "$HA_PASSWORD" --ha-user-id "$HA_USER_ID" \
      --token-file "$HA_TOKEN_FILE" > "$WORK/fake-ha-companion.log" 2>&1 &
    FAKE_HA_PID=$!
    env_track "$FAKE_HA_PID"
    for _ in $(seq 1 10); do
      [[ -s "$HA_TOKEN_FILE" ]] && break
      sleep 1
    done
    if [[ ! -s "$HA_TOKEN_FILE" ]]; then
      echo "  FEHL Fake-Companion hat kein Token erhalten (Log: $WORK/fake-ha-companion.log)"; FAILED=1
    else
      HA_TOKEN="$(cat "$HA_TOKEN_FILE")"

      login_code="$(curl -s "${CURL_INSECURE[@]}" -m 20 -c "$HA_COOKIES" -H "$SECURE_HEADER" -H 'Content-Type: application/json' \
        -o "$WORK/ha-login.json" -w '%{http_code}' -X POST "$BASE/api/v1/auth/login" \
        -d "{\"username\":\"$HA_USER\",\"password\":\"$HA_PASSWORD\"}")"
      if [[ "$login_code" == "200" ]] && python3 -c '
import json, sys
sys.exit(0 if json.load(open(sys.argv[1])).get("source") == "homeassistant" else 1)
' "$WORK/ha-login.json"; then
        echo "  OK   Home-Assistant-Benutzer $HA_USER meldet sich ueber HTTPS an (source homeassistant)"
      else
        echo "  FEHL $HA_USER meldet sich nicht ueber HTTPS an (HTTP $login_code)"; FAILED=1
      fi

      # Unter echtem TLS ist jede Anfrage sicher, das Fehlen des Kennzeichens prueft dann nichts.
      if [[ $HTTPS -eq 1 ]]; then
        echo "  --   $HA_USER ohne HTTPS-Kennzeichen: uebersprungen (echtes TLS)"
      else
        code="$(curl -s "${CURL_INSECURE[@]}" -m 20 -o /dev/null -w '%{http_code}' -H 'Content-Type: application/json' \
          -X POST "$BASE/api/v1/auth/login" -d "{\"username\":\"$HA_USER\",\"password\":\"$HA_PASSWORD\"}")"
        if [[ "$code" == "401" ]]; then
          echo "  OK   $HA_USER ohne HTTPS-Kennzeichen wird abgewiesen (HTTP 401)"
        else
          echo "  FEHL $HA_USER ohne HTTPS-Kennzeichen kam mit HTTP $code durch"; FAILED=1
        fi
      fi

      session_code="$(curl -sk -m 15 -o "$WORK/companion-session.json" -w '%{http_code}' -X POST \
        "$COMPANION_BASE/api/v1/companion/session" -H "Authorization: Bearer $HA_TOKEN" -H "$SECURE_HEADER" \
        -H 'Content-Type: application/json' \
        -d "{\"ha_user_id\":\"$HA_USER_ID\",\"username\":\"$HA_USER\",\"name\":\"Maria\",\"is_admin\":false}")"
      CS_TOKEN="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["token"])' "$WORK/companion-session.json" 2>/dev/null)" || CS_TOKEN=""
      if [[ "$session_code" == "200" && -n "$CS_TOKEN" ]]; then
        echo "  OK   Companion-Sitzung fuer $HA_USER am Companion-Port ausgestellt (HTTP 200)"
      else
        echo "  FEHL Companion-Sitzung kam mit HTTP $session_code ohne Token"; FAILED=1
      fi

      if [[ -n "$CS_TOKEN" ]]; then
        auth_code="$(curl -sk -m 15 -o "$WORK/companion-authsession.json" -w '%{http_code}' \
          -H "Authorization: Bearer $HA_TOKEN" -H "Cookie: energy_node_session=$CS_TOKEN" -H "$SECURE_HEADER" \
          "$COMPANION_BASE/api/v1/auth/session")"
        if [[ "$auth_code" == "200" ]] && python3 -c '
import json, sys
sys.exit(0 if json.load(open(sys.argv[1])).get("via") == "companion" else 1)
' "$WORK/companion-authsession.json"; then
          echo "  OK   Sitzung am Companion-Port meldet via companion (HTTP 200)"
        else
          echo "  FEHL Sitzung am Companion-Port kam mit HTTP $auth_code ohne via companion"; FAILED=1
        fi
        code="$(curl -s "${CURL_INSECURE[@]}" -m 15 -o /dev/null -w '%{http_code}' \
          -H "Cookie: energy_node_session=$CS_TOKEN" -H "$SECURE_HEADER" "$BASE/api/v1/auth/session")"
        if [[ "$code" == "401" ]]; then
          echo "  OK   Companion-Sitzung am Hauptport abgewiesen (HTTP 401)"
        else
          echo "  FEHL Companion-Sitzung kam am Hauptport mit HTTP $code durch"; FAILED=1
        fi
      fi

      code="$(curl -s "${CURL_INSECURE[@]}" -m 10 -o /dev/null -w '%{http_code}' -b "$COOKIES" -H "$SECURE_HEADER" \
        -H "X-CSRF-Token: $ADMIN_CSRF" -X DELETE "$BASE/api/v1/users/companion")"
      if [[ "$code" == "200" ]]; then
        echo "  OK   Kopplung durch Admin aufgehoben (HTTP 200)"
      else
        echo "  FEHL Kopplung aufheben kam mit HTTP $code"; FAILED=1
      fi
      code="$(curl -sk -m 15 -o /dev/null -w '%{http_code}' -H "Authorization: Bearer $HA_TOKEN" \
        -H "$SECURE_HEADER" "$COMPANION_BASE/api/v1/auth/session")"
      if [[ "$code" == "401" ]]; then
        echo "  OK   altes Companion-Token nach Entkoppeln abgewiesen (HTTP 401)"
      else
        echo "  FEHL altes Companion-Token kam nach Entkoppeln mit HTTP $code durch"; FAILED=1
      fi
    fi
  fi
}

check_history_exchange() {
  echo "==> Verlauf-Austausch: Ankuendigung"
  local announced
  announced="$(curl -sf "${CURL_INSECURE[@]}" -b "$COOKIES" -H "$SECURE_HEADER" "${BASE}/api/v1/history/exchange")" || {
    echo "FEHLER: Ankuendigung nicht erreichbar" >&2
    return 1
  }
  echo "${announced}" | grep -q '"protocol":1' || {
    echo "FEHLER: Ankuendigung nennt nicht Protokoll 1: ${announced}" >&2
    return 1
  }
  echo "${announced}" | grep -q '"tiers":\["1m","5m","1h"\]' || {
    echo "FEHLER: Ankuendigung nennt nicht genau die Stufen 1m, 5m und 1h" >&2
    return 1
  }

  echo "==> Verlauf-Austausch: zwei gleichzeitige Peers"
  local first_out second_out
  first_out="$(mktemp)"
  second_out="$(mktemp)"
  curl -sN "${CURL_INSECURE[@]}" --max-time 8 -b "$COOKIES" -H "$SECURE_HEADER" "${BASE}/api/v1/history/exchange/stream" >"${first_out}" &
  local first_pid=$!
  curl -sN "${CURL_INSECURE[@]}" --max-time 8 -b "$COOKIES" -H "$SECURE_HEADER" "${BASE}/api/v1/history/exchange/stream" >"${second_out}" &
  local second_pid=$!
  sleep 2

  local first_peer
  first_peer="$(grep -m1 '"peer_id"' "${first_out}" | sed 's/.*"peer_id":"\([^"]*\)".*/\1/')"
  if [ -z "${first_peer}" ]; then
    echo "FEHLER: erster Peer hat kein hello erhalten" >&2
    kill "${first_pid}" "${second_pid}" 2>/dev/null
    return 1
  fi

  curl -sf "${CURL_INSECURE[@]}" -X POST -b "$COOKIES" -H "$SECURE_HEADER" "${BASE}/api/v1/history/exchange/offer" \
    -H 'Content-Type: application/json' \
    -d "{\"peer\":\"${first_peer}\",\"coverage\":{\"1m\":{\"role:pv\":{\"from\":0,\"step\":3600000,\"n\":[60]}}}}" \
    -o /dev/null || {
    echo "FEHLER: Angebot wurde abgelehnt" >&2
    kill "${first_pid}" "${second_pid}" 2>/dev/null
    return 1
  }
  sleep 2
  kill "${first_pid}" "${second_pid}" 2>/dev/null
  wait "${first_pid}" "${second_pid}" 2>/dev/null

  grep -q "\"peer\":\"${first_peer}\"" "${second_out}" || {
    echo "FEHLER: das Angebot kam beim zweiten Peer nicht an" >&2
    cat "${second_out}" >&2
    return 1
  }
  grep -q "\"peer\":\"${first_peer}\"" "${first_out}" && {
    echo "FEHLER: der Absender hat sein eigenes Angebot zurueckerhalten" >&2
    return 1
  }
  rm -f "${first_out}" "${second_out}"
  echo "    ok: Angebot erreichte genau den anderen Peer"
}

checks_history_exchange() {
  check_history_exchange || FAILED=1
}
