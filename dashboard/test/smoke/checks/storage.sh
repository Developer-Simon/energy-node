# shellcheck shell=bash
# shellcheck disable=SC2154,SC2034 # Globals kommen vom Aufrufer, FAILED wird dort gelesen
# Pruefgruppe Speicher: Gast ohne Recht bekommt 403, der Admin richtet das
# Demo-Laufwerk ein, bis der Zustand aktiv ist. Das Laufwerk ist der Fake aus
# demo/fixtures/storage-drives.json, der Helfer schreibt in ein Verzeichnis.
# Der Neustart am Ende der Einrichtung macht die Pruefung selbst: die Aktion
# Neustart ist im Fake ein No-op, die gestagte Konfiguration gilt erst beim
# naechsten Start. Jede Funktion setzt bei einem Fehler FAILED=1 und laeuft weiter.
# Globals wie BASE, COOKIES, CURL_INSECURE, SECURE_HEADER, WORK, BIN kommen vom Aufrufer.

# Wartet bis zu $2 Sekunden, bis der Ausdruck $1 ueber den Status wahr ist.
storage_wait_status() { # Python-Ausdruck mit s (Status), Sekunden
  local i
  for i in $(seq 1 "$2"); do
    if api "$BASE/api/v1/storage/status" | python3 -c '
import json, sys
s = json.load(sys.stdin)
sys.exit(0 if ('"$1"') else 1)
' 2>/dev/null; then
      return 0
    fi
    sleep 1
  done
  return 1
}

checks_storage() {
  echo "==> Speicher: Gast ohne Recht, Laufwerk einrichten bis aktiv"
  local csrf guest_jar code body

  # Gast: nur view_overview, also kein storage_manage.
  csrf="$(api "$BASE/api/v1/auth/session" | python3 -c 'import json,sys; print(json.load(sys.stdin)["csrf_token"])')" || { FAILED=1; return; }
  curl -sf "${CURL_INSECURE[@]}" -m 5 -o /dev/null -H "$SECURE_HEADER" -H "X-CSRF-Token: $csrf" \
    -H 'Content-Type: application/json' -X POST "$BASE/api/v1/users" -b "$COOKIES" \
    -d '{"username":"smoke-speicher","password":"smoke-speicher-1","roles":["view_overview"]}' || FAILED=1
  guest_jar="$WORK/smoke-speicher-cookies.txt"
  curl -sf "${CURL_INSECURE[@]}" -m 5 -c "$guest_jar" -H "$SECURE_HEADER" -H 'Content-Type: application/json' \
    -X POST "$BASE/api/v1/auth/login" -d '{"username":"smoke-speicher","password":"smoke-speicher-1"}' > /dev/null || FAILED=1
  code="$(curl -s "${CURL_INSECURE[@]}" -m 5 -o "$WORK/storage-guest.json" -w '%{http_code}' -b "$guest_jar" \
    -H "$SECURE_HEADER" "$BASE/api/v1/storage/status")"
  if [[ "$code" == "403" ]]; then
    echo "  OK   Gast ohne storage_manage bekommt 403 auf den Speicherstatus"
  else
    echo "  FEHL Gast kam auf den Speicherstatus mit HTTP $code"; FAILED=1
  fi

  # Admin: die Demo-Liste zeigt genau den Stick, der formatiert werden muss.
  body="$(api "$BASE/api/v1/storage/drives")" || { echo "  FEHL Laufwerksliste nicht abrufbar"; FAILED=1; return; }
  if printf '%s' "$body" | python3 -c '
import json, sys
drives = json.load(sys.stdin)
d = drives[0] if drives else {}
ok = [x["kname"] for x in drives] == ["sdb"] and drives[0]["needs_format"] \
    and d.get("disk") == "sdb" and d.get("whole") is False and d.get("label") == ""
sys.exit(0 if ok else 1)
'; then
    echo "  OK   Laufwerksliste zeigt das Demo-Laufwerk sdb als ganzen Stick zum Formatieren"
  else
    echo "  FEHL Laufwerksliste ist nicht das Demo-Laufwerk als ganzer Stick: $body"; FAILED=1; return
  fi

  # Einrichten: formatieren, Daten und Backups umziehen, Ausweichort Systemlaufwerk.
  code="$(curl -s "${CURL_INSECURE[@]}" -m 30 -o "$WORK/storage-setup.json" -w '%{http_code}' -b "$COOKIES" \
    -H "$SECURE_HEADER" -H "X-CSRF-Token: $ADMIN_CSRF" -H 'Content-Type: application/json' -X POST \
    "$BASE/api/v1/storage/setup" \
    -d '{"kname":"sdb","uuid":"","format":true,"use_data":true,"use_backups":true,"fallback":"system"}')"
  if [[ "$code" != "202" ]]; then
    echo "  FEHL Einrichten kam mit HTTP $code: $(cat "$WORK/storage-setup.json")"; FAILED=1; return
  fi
  if storage_wait_status "s['job']['done']" 120; then
    if api "$BASE/api/v1/storage/status" | python3 -c '
import json, sys
sys.exit(0 if json.load(sys.stdin)["job"]["err"] == "" else 1)
'; then
      echo "  OK   Einrichten ist ohne Fehler durchgelaufen"
    else
      echo "  FEHL Einrichten meldet einen Fehler"; FAILED=1; return
    fi
  else
    echo "  FEHL Einrichten ist nach 120 s nicht fertig"; FAILED=1; return
  fi

  # Neustart: die gestagte Konfiguration und der Laufwerkswaechter kommen mit dem neuen Prozess.
  kill "$DASHBOARD_PID" 2>/dev/null
  for _ in $(seq 1 50); do
    kill -0 "$DASHBOARD_PID" 2>/dev/null || break
    sleep 0.2
  done
  env_start_dashboard "$BIN"
  if ! env_wait_dashboard || ! env_login; then
    echo "  FEHL Dashboard startet nach dem Einrichten nicht neu"; FAILED=1; return
  fi
  if storage_wait_status "s['status']['mode'] == 'drive' and s['config']['uuid'] != ''" 60; then
    echo "  OK   Zustand ist aktiv, das Laufwerk trägt die laufenden Daten"
  else
    echo "  FEHL Zustand ist nach dem Neustart nicht aktiv"; FAILED=1
  fi
}
