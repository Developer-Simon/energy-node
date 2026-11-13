# shellcheck shell=bash
# shellcheck disable=SC2154,SC2034 # Globals kommen vom Aufrufer, FAILED wird dort gelesen
# Pruefgruppe fuer die Netzwerk-API: Status mit den zwei Verbindungen, Verbinden
# im Demo-Szenario lan bis zum Ergebnis ok. Das Passwort darf in keiner Antwort
# des Status stehen. Jede Funktion setzt bei einem Fehler FAILED=1.
# Globals wie BASE, COOKIES, CURL_INSECURE, SECURE_HEADER, WORK kommen vom Aufrufer.

checks_network() {
  echo "==> Netzwerk: Status mit zwei Verbindungen, Verbinden im Szenario lan"
  local csrf status_file code ssid body switch_result i
  csrf="$(api "$BASE/api/v1/auth/session" | python3 -c 'import json,sys; print(json.load(sys.stdin)["csrf_token"])')" || csrf=""
  if [[ -z "$csrf" ]]; then
    echo "  FEHL Sitzung des Admins ohne CSRF-Token"; FAILED=1
    return
  fi

  # 1. Status: genau zwei Verbindungen, lan und wifi.
  status_file="$WORK/network-status.json"
  code="$(curl -s "${CURL_INSECURE[@]}" -m 10 -o "$status_file" -w '%{http_code}' -b "$COOKIES" \
    -H "$SECURE_HEADER" "$BASE/api/v1/network/status")" || code=""
  if [[ "$code" == "200" ]] && python3 -c '
import json, sys
s = json.load(open(sys.argv[1]))
sys.exit(0 if [link["id"] for link in s.get("links", [])] == ["lan", "wifi"] else 1)
' "$status_file"; then
    echo "  OK   Status liefert die Verbindungen lan und wifi"
  else
    echo "  FEHL Status kam mit HTTP $code oder ohne die Verbindungen lan und wifi"; FAILED=1
    return
  fi

  # 2. Ein Netz aus dem Scan waehlen, ohne Passwort im Status.
  ssid="$(python3 -c '
import json, sys
nets = json.load(open(sys.argv[1])).get("networks", [])
print(nets[0]["ssid"] if nets else "")
' "$status_file")" || ssid=""
  if [[ -z "$ssid" ]]; then
    echo "  FEHL im Szenario lan kein WLAN-Netz gefunden"; FAILED=1
    return
  fi

  # 3. Verbinden: 202, dann bis zum Ergebnis pollen.
  body="$(python3 -c '
import json, sys
print(json.dumps({"ssid": sys.argv[1], "password": "smoke-wlan-1", "hidden": False, "profile_uuid": ""}))
' "$ssid")"
  code="$(curl -s "${CURL_INSECURE[@]}" -m 30 -o "$WORK/network-connect.json" -w '%{http_code}' -X POST \
    -b "$COOKIES" -H "$SECURE_HEADER" -H "X-CSRF-Token: $csrf" -H 'Content-Type: application/json' \
    -d "$body" "$BASE/api/v1/network/wifi/connect")" || code=""
  if [[ "$code" != "202" ]]; then
    echo "  FEHL Verbinden kam mit HTTP $code, erwartet 202"; FAILED=1
    return
  fi

  switch_result=""
  for i in $(seq 1 30); do
    curl -s "${CURL_INSECURE[@]}" -m 10 -o "$status_file" -b "$COOKIES" -H "$SECURE_HEADER" "$BASE/api/v1/network/status" || true
    switch_result="$(python3 -c '
import json, sys
sw = json.load(open(sys.argv[1])).get("switch") or {}
print(sw.get("result", ""))
' "$status_file")" || switch_result=""
    [[ -n "$switch_result" ]] && break
    sleep 1
  done
  if [[ "$switch_result" == "ok" ]]; then
    echo "  OK   Verbinden im Szenario lan endet mit ok"
  else
    echo "  FEHL Verbinden endete mit \"${switch_result:-keinem Ergebnis}\", erwartet ok"; FAILED=1
  fi
  if grep -q 'smoke-wlan-1' "$status_file"; then
    echo "  FEHL das Passwort steht im Status"; FAILED=1
  else
    echo "  OK   Passwort steht nicht im Status"
  fi

  # 4. Ersatzmodus umstellen: standby, im Status sichtbar, danach zurueck auf parallel.
  local mode put_code
  for mode in standby parallel; do
    put_code="$(curl -s "${CURL_INSECURE[@]}" -m 10 -o "$WORK/network-settings.json" -w '%{http_code}' -X PUT \
      -b "$COOKIES" -H "$SECURE_HEADER" -H "X-CSRF-Token: $csrf" -H 'Content-Type: application/json' \
      -d "{\"fallback\":\"$mode\",\"hotspot\":true}" "$BASE/api/v1/network/settings")" || put_code=""
    if [[ "$put_code" != "200" ]]; then
      echo "  FEHL Ersatzmodus $mode kam mit HTTP $put_code, erwartet 200"; FAILED=1
      return
    fi
    curl -s "${CURL_INSECURE[@]}" -m 10 -o "$status_file" -b "$COOKIES" -H "$SECURE_HEADER" "$BASE/api/v1/network/status" || true
    if python3 -c '
import json, sys
s = json.load(open(sys.argv[1]))
sys.exit(0 if (s.get("settings") or {}).get("fallback") == sys.argv[2] else 1)
' "$status_file" "$mode"; then
      echo "  OK   Ersatzmodus $mode steht im Status"
    else
      echo "  FEHL Status zeigt nicht den Ersatzmodus $mode"; FAILED=1
      return
    fi
  done

  # 5. Hotspot-Zugang: ssid und Passwort kommen, das Passwort steht nicht im Status.
  local secret_code password
  secret_code="$(curl -s "${CURL_INSECURE[@]}" -m 10 -o "$WORK/hotspot-secret.json" -w '%{http_code}' \
    -b "$COOKIES" -H "$SECURE_HEADER" "$BASE/api/v1/network/hotspot-secret")" || secret_code=""
  if [[ "$secret_code" == "200" ]] && python3 -c '
import json, sys
s = json.load(open(sys.argv[1]))
sys.exit(0 if s.get("ssid") and s.get("password") else 1)
' "$WORK/hotspot-secret.json"; then
    echo "  OK   Hotspot-Zugang liefert ssid und Passwort"
  else
    echo "  FEHL Hotspot-Zugang kam mit HTTP $secret_code oder ohne ssid und Passwort"; FAILED=1
    return
  fi
  password="$(python3 -c '
import json, sys
print(json.load(open(sys.argv[1]))["password"])
' "$WORK/hotspot-secret.json")" || password=""
  curl -s "${CURL_INSECURE[@]}" -m 10 -o "$status_file" -b "$COOKIES" -H "$SECURE_HEADER" "$BASE/api/v1/network/status" || true
  if [[ -n "$password" ]] && grep -qF -- "$password" "$status_file"; then
    echo "  FEHL das Hotspot-Passwort steht im Status"; FAILED=1
  else
    echo "  OK   Hotspot-Passwort steht nicht im Status"
  fi
}
