# shellcheck shell=bash
# shellcheck disable=SC2154,SC2034 # Globals kommen vom Aufrufer, FAILED wird dort gelesen
# Gemeinsame Pruefhelfer fuer die Smoke-Gruppen: check (JSON-Probe),
# check_status (HTTP-Code) und html_lacks (Praesenz im HTML).
# Die Gruppen setzen bei einem Fehler FAILED=1 und brechen nicht ab.
# Globals wie BASE, COOKIES, CURL_INSECURE, SECURE_HEADER kommen vom Aufrufer.

check() { # name, bedingung-als-python-ausdruck ueber `data`
  local name="$1" expression="$2" source="$3"
  if api "$source" | python3 -c "
import json, sys
data = json.load(sys.stdin)
sys.exit(0 if ($expression) else 1)
"; then
    echo "  OK   $name"
  else
    echo "  FEHL $name"
    FAILED=1
  fi
}

# Erwarteter HTTP-Code: eine Zahl oder "!403" fuer "nicht verboten".
check_status() { # Name, erwarteter Code, Cookie-Datei, URL
  local code
  code=$(curl -s "${CURL_INSECURE[@]}" -m 5 -o /dev/null -w '%{http_code}' -b "$3" -H "$SECURE_HEADER" "$4")
  if [[ "$2" == "!403" && "$code" != "403" ]] || [[ "$2" != "!403" && "$code" == "$2" ]]; then
    echo "  OK   $1 (HTTP $code)"
  else
    echo "  FEHL $1 (HTTP $code, erwartet $2)"; FAILED=1
  fi
}

html_lacks() { # name, verbotener-string, url
  local name="$1" verboten="$2" url="$3"
  if api "$url" | grep -qF -- "$verboten"; then
    echo "  FEHL $name (\"$verboten\" steht trotzdem im HTML)"; FAILED=1
  else
    echo "  OK   $name"
  fi
}
