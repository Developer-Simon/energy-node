# shellcheck shell=bash
# shellcheck disable=SC2154,SC2034 # Globals kommen vom Aufrufer, FAILED wird dort gelesen
# Pruefgruppe fuer die Backup-API end to end: Archiv anlegen und laden, ein
# beschaedigtes und ein unbeschaedigtes Archiv als Vorschau einlesen, Passwort,
# Rechte und Loeschen. Eingespielt wird nie, sonst startet das Dashboard neu.
# Jede Funktion setzt bei einem Fehler FAILED=1 und laeuft ohne Abbruch weiter.
# Globals wie BASE, COOKIES, CURL_INSECURE, SECURE_HEADER, WORK kommen vom Aufrufer.

# Ein Aufruf der Backup-API mit der Sitzung des Aufrufers. Gibt den HTTP-Code aus,
# der Antworttext landet in $4.
backup_call() { # Methode, Pfad, Cookie-Datei, Ausgabedatei, weitere curl-Argumente
  local method="$1" path="$2" jar="$3" out="$4"
  shift 4
  curl -s "${CURL_INSECURE[@]}" -m 30 -o "$out" -w '%{http_code}' -X "$method" \
    -b "$jar" -H "$SECURE_HEADER" "$@" "$BASE$path"
}

# Wartet bis zu 30 s auf das Ende des Jobs. Gibt die Ergebnis-ID aus, sonst
# Rueckgabe 1 mit der Fehlermeldung des Jobs auf stderr.
backup_wait_job() {
  local i state
  for i in $(seq 1 30); do
    state="$(api "$BASE/api/v1/backup/job" | python3 -c '
import json, sys
s = json.load(sys.stdin)
if s["running"]:
    print("laeuft")
elif s["error"]:
    print("fehler " + s["error"])
else:
    print("fertig " + s["result"])
')" || return 1
    case "$state" in
      laeuft) sleep 1 ;;
      fertig\ *) echo "${state#fertig }"; return 0 ;;
      fehler\ *) echo "  Job ${state}" >&2; return 1 ;;
      *) return 1 ;;
    esac
  done
  echo "  Job lief nach 30 s noch" >&2
  return 1
}

# Legt ein Archiv im Ablageort an. Gibt die ID des neuen Archivs aus.
backup_create() { # Passwort (leer = ohne Passwort)
  local password="$1" body code result
  body="$(python3 -c '
import json, sys
print(json.dumps({
    "areas": {"settings": True, "devices": True, "users": True, "stats": True},
    "secrets": False,
    "password": sys.argv[1],
    "target": "store",
}))
' "$password")"
  code="$(backup_call POST /api/v1/backup/archives "$COOKIES" "$WORK/backup-start.json" \
    -H "X-CSRF-Token: $BACKUP_CSRF" -H 'Content-Type: application/json' -d "$body")" || code=""
  if [[ "$code" != "202" ]]; then
    echo "  FEHL Backup anlegen kam mit HTTP $code"; FAILED=1
    return 1
  fi
  result="$(backup_wait_job)" || { FAILED=1; return 1; }
  if [[ -z "$result" ]]; then
    echo "  FEHL Backup-Job meldet kein Ergebnis"; FAILED=1
    return 1
  fi
  echo "$result"
}

# Liste der Archiv-IDs aus dem Status, eine je Zeile.
backup_archive_ids() {
  api "$BASE/api/v1/backup/status" | python3 -c '
import json, sys
for archive in json.load(sys.stdin)["archives"]:
    print(archive["id"])
'
}

checks_backup() {
  echo "==> Backup: Archiv anlegen, laden, einlesen, Passwort, Rechte, Loeschen"
  local id_plain id_locked code
  BACKUP_CSRF="$(api "$BASE/api/v1/auth/session" | python3 -c 'import json,sys; print(json.load(sys.stdin)["csrf_token"])')" || BACKUP_CSRF=""
  if [[ -z "$BACKUP_CSRF" ]]; then
    echo "  FEHL Sitzung des Admins ohne CSRF-Token"; FAILED=1
    return
  fi

  # 1. Anlegen ohne Passwort: 202, Job laeuft durch, das Archiv steht im Status.
  id_plain="$(backup_create "")" || return
  echo "  OK   Backup angelegt, Job ohne Fehler beendet ($id_plain)"
  if backup_archive_ids | grep -qx "$id_plain"; then
    echo "  OK   Status listet das neue Archiv"
  else
    echo "  FEHL Status listet das neue Archiv nicht"; FAILED=1
  fi

  # 2. Laden: gzip-Kennung und Anhang.
  plain="$WORK/backup-plain.bin"
  headers="$WORK/backup-plain-headers.txt"
  code="$(backup_call GET "/api/v1/backup/archives/$id_plain" "$COOKIES" "$plain" -D "$headers")" || code=""
  magic="$(head -c 2 "$plain" | od -An -tx1 | tr -d ' \n')"
  if [[ "$code" == "200" && "$magic" == "1f8b" ]] && grep -qi '^content-disposition: attachment' "$headers"; then
    echo "  OK   Archiv laedt als gzip mit Content-Disposition attachment"
  else
    echo "  FEHL Archiv laden: HTTP $code, Kennung $magic"; FAILED=1
  fi

  # 3. Beschaedigt: ein Byte in der Mitte kippen, der Import meldet backup_corrupt
  # und der Status bleibt unveraendert.
  before="$(backup_archive_ids | sort)"
  corrupt="$WORK/backup-corrupt.bin"
  python3 -c '
import sys
data = bytearray(open(sys.argv[1], "rb").read())
data[len(data) // 2] ^= 0xFF
open(sys.argv[2], "wb").write(data)
' "$plain" "$corrupt"
  code="$(backup_call POST /api/v1/backup/import "$COOKIES" "$WORK/import-corrupt.json" \
    -H "X-CSRF-Token: $BACKUP_CSRF" -H 'Content-Type: application/octet-stream' --data-binary "@$corrupt")" || code=""
  if [[ "$code" == "422" ]] && python3 -c '
import json, sys
sys.exit(0 if json.load(open(sys.argv[1]))["code"] == "backup_corrupt" else 1)
' "$WORK/import-corrupt.json"; then
    echo "  OK   beschaedigtes Archiv wird mit backup_corrupt abgewiesen (HTTP 422)"
  else
    echo "  FEHL beschaedigtes Archiv: HTTP $code, erwartet 422 backup_corrupt"; FAILED=1
  fi
  after="$(backup_archive_ids | sort)"
  if [[ "$before" == "$after" ]]; then
    echo "  OK   Status unveraendert nach dem beschaedigten Import"
  else
    echo "  FEHL Status hat sich nach dem beschaedigten Import geaendert"; FAILED=1
  fi

  # 4. Unbeschaedigt: nur die Vorschau, kein Einspielen.
  code="$(backup_call POST /api/v1/backup/import "$COOKIES" "$WORK/import-good.json" \
    -H "X-CSRF-Token: $BACKUP_CSRF" -H 'Content-Type: application/octet-stream' --data-binary "@$plain")" || code=""
  if [[ "$code" == "200" ]] && python3 -c '
import json, sys
data = json.load(open(sys.argv[1]))
sys.exit(0 if "manifest" in data and any(a["area"] == "settings" for a in data["areas"]) else 1)
' "$WORK/import-good.json"; then
    echo "  OK   unbeschaedigtes Archiv gibt eine Vorschau mit Bereich settings"
  else
    echo "  FEHL unbeschaedigtes Archiv: HTTP $code, keine Vorschau mit settings"; FAILED=1
  fi

  # 5. Passwort: ohne Passwort 409, falsches 422, richtiges 200 als Vorschau.
  id_locked="$(backup_create "smoketest-pw-1")" || return
  echo "  OK   verschluesseltes Backup angelegt ($id_locked)"
  locked="$WORK/backup-locked.bin"
  code="$(backup_call GET "/api/v1/backup/archives/$id_locked" "$COOKIES" "$locked")" || code=""
  if [[ "$code" != "200" ]]; then
    echo "  FEHL verschluesseltes Archiv laden kam mit HTTP $code"; FAILED=1
  fi
  code="$(backup_call POST /api/v1/backup/import "$COOKIES" "$WORK/import-locked-none.json" \
    -H "X-CSRF-Token: $BACKUP_CSRF" -H 'Content-Type: application/octet-stream' --data-binary "@$locked")" || code=""
  if [[ "$code" == "409" ]] && grep -q '"password_required"' "$WORK/import-locked-none.json"; then
    echo "  OK   ohne Passwort: password_required (HTTP 409)"
  else
    echo "  FEHL ohne Passwort kam mit HTTP $code, erwartet 409 password_required"; FAILED=1
  fi
  code="$(backup_call POST /api/v1/backup/import/unlock "$COOKIES" "$WORK/unlock-wrong.json" \
    -H "X-CSRF-Token: $BACKUP_CSRF" -H 'Content-Type: application/json' -d '{"password":"falsch-smoke-1"}')" || code=""
  if [[ "$code" == "422" ]] && grep -q '"wrong_password"' "$WORK/unlock-wrong.json"; then
    echo "  OK   falsches Passwort: wrong_password (HTTP 422)"
  else
    echo "  FEHL falsches Passwort kam mit HTTP $code, erwartet 422 wrong_password"; FAILED=1
  fi
  code="$(backup_call POST /api/v1/backup/import/unlock "$COOKIES" "$WORK/unlock-right.json" \
    -H "X-CSRF-Token: $BACKUP_CSRF" -H 'Content-Type: application/json' -d '{"password":"smoketest-pw-1"}')" || code=""
  if [[ "$code" == "200" ]] && python3 -c '
import json, sys
sys.exit(0 if "manifest" in json.load(open(sys.argv[1])) else 1)
' "$WORK/unlock-right.json"; then
    echo "  OK   richtiges Passwort: Vorschau (HTTP 200)"
  else
    echo "  FEHL richtiges Passwort kam mit HTTP $code, erwartet 200 mit Vorschau"; FAILED=1
  fi

  # 6. Rechte: ein Konto mit nur view_overview bekommt 403, ohne Sitzung 401.
  curl -sf "${CURL_INSECURE[@]}" -m 5 -o /dev/null -H "$SECURE_HEADER" \
    -H "X-CSRF-Token: $BACKUP_CSRF" -H 'Content-Type: application/json' -X POST "$BASE/api/v1/users" \
    -b "$COOKIES" -d '{"username":"smoke-backup-view","password":"smoke-user-pass-1","roles":["view_overview"]}' \
    || FAILED=1
  VIEW_COOKIES="$WORK/smoke-backup-view-cookies.txt"
  curl -sf "${CURL_INSECURE[@]}" -m 5 -c "$VIEW_COOKIES" -H "$SECURE_HEADER" -H 'Content-Type: application/json' \
    -X POST "$BASE/api/v1/auth/login" -d '{"username":"smoke-backup-view","password":"smoke-user-pass-1"}' > /dev/null \
    || FAILED=1
  check_status "Backup-Status fuer Konto ohne Backup-Recht gesperrt" 403 "$VIEW_COOKIES" "$BASE/api/v1/backup/status"
  NO_SESSION_COOKIES="$WORK/backup-no-session-cookies.txt"
  : > "$NO_SESSION_COOKIES"
  check_status "Backup-Status ohne Sitzung abgewiesen" 401 "$NO_SESSION_COOKIES" "$BASE/api/v1/backup/status"

  # 7. Loeschen: beide Archive verschwinden aus dem Status.
  for id in "$id_plain" "$id_locked"; do
    code="$(backup_call DELETE "/api/v1/backup/archives/$id" "$COOKIES" "$WORK/backup-delete.json" \
      -H "X-CSRF-Token: $BACKUP_CSRF")" || code=""
    if [[ "$code" != "200" ]]; then
      echo "  FEHL Archiv $id loeschen kam mit HTTP $code"; FAILED=1
    fi
  done
  remaining="$(backup_archive_ids)"
  if grep -qx "$id_plain" <<<"$remaining" || grep -qx "$id_locked" <<<"$remaining"; then
    echo "  FEHL Status listet geloeschte Archive noch"; FAILED=1
  else
    echo "  OK   beide Archive sind nach dem Loeschen aus dem Status verschwunden"
  fi
}
