# shellcheck shell=bash
# shellcheck disable=SC2154,SC2034 # Globals kommen vom Aufrufer, FAILED wird dort gelesen
# Pruefgruppen fuer Updates: Angebot, Paketbezug (Redeploy-Vorschau), Versionsseite
# mit installiertem Zustand und Installation durch ein Konto mit install_updates.
# Jede Funktion setzt bei einem Fehler FAILED=1 und laeuft ohne Abbruch weiter.
# Globals wie BASE, WORK, COOKIES, CURL_INSECURE, SECURE_HEADER kommen vom Aufrufer.
# checks_update_install nutzt create_smoke_user aus checks_users (access.sh).

checks_update_offer() {
  # --simulate-update zeigt den Checker per ENERGY_NODE_UPDATES_API_BASE auf
  # fake_github_releases.py statt der echten API; main.go prueft einmal sofort
  # beim Start, wenn der Cache noch leer ist, also reicht kurzes Polling.
  echo "==> Update-Simulation: /api/v1/updates/status"
  update_available=0
  for _ in $(seq 1 10); do
    status="$(api "$BASE/api/v1/updates/status" || true)"
    if echo "$status" | grep -q '"available":true' && echo "$status" | grep -q '"latest":"9.9.9"'; then
      update_available=1
      break
    fi
    sleep 1
  done
  if [[ $update_available -eq 1 ]]; then
    echo "  OK   Update v9.9.9 wird als verfuegbar gemeldet"
  else
    echo "  FEHL Update-Status meldet kein verfuegbares v9.9.9: ${status:-<keine Antwort>}" >&2
    FAILED=1
  fi
}

checks_package() {
  # Derselbe Weg wie im Browser. Die Oberflaeche ruft
  # POST /redeploy/api/run mit mode "prepare", das Dashboard laedt das Bundle vom
  # Fake-GitHub in redeploy-candidate/, danach liest die Vorschau dessen Schritte
  # (GET /redeploy/api/plan). Der zweite Lauf muss den Download ueberspringen.
  # Am Ende wird das Verzeichnis wieder geleert: bei --keep soll der erste Klick
  # auf "Aktualisieren" im Browser das Herunterladen zeigen, nicht ein fertiges Paket.
  CANDIDATE="$WORK/data/redeploy-candidate"
  # Wie der Browser: den Token aus der ausgelieferten Seite nehmen (data-token)
  # und als X-Installer-Token schicken. Ein Token aus /api/v1/auth/session zu
  # holen haette verdeckt, dass die Seite ihn nicht bekam (403 csrf_failed).
  page_token="$(api "$BASE/redeploy/" | sed -n 's/.*data-token="\([^"]*\)".*/\1/p' | head -n 1)"
  if [[ -n "$page_token" ]]; then
    echo "  OK   die Redeploy-Seite traegt den Sitzungs-Token fuer ihre Aufrufe"
  else
    echo "  FEHL die Redeploy-Seite hat keinen Token (data-token ist leer)"; FAILED=1
  fi
  echo "==> Redeploy folgt der Dashboard-Sprache"
  redeploy_lang() { api "$@" "$BASE/redeploy/api/bootstrap" | python3 -c 'import json,sys; print(json.load(sys.stdin)["language"])'; }
  if [[ "$(redeploy_lang)" == "de" && "$(redeploy_lang -H 'Accept-Language: en-GB,en;q=0.9')" == "en" ]]; then
    echo "  OK   Redeploy-Bildschirm ohne Wahl deutsch, mit englischem Browser englisch"
  else
    echo "  FEHL Redeploy-Bildschirm folgt der Sprache nicht"; FAILED=1
  fi
  start_prepare() {
    api -H "X-Installer-Token: $page_token" -H 'Content-Type: application/json' -X POST \
      "$BASE/redeploy/api/run" -d '{"mode":"prepare"}' > /dev/null
  }
  echo "==> Paketbezug: Vorbereiten laedt das Bundle"
  if start_prepare; then
    for _ in $(seq 1 30); do
      [[ -f "$CANDIDATE/manifest.json" ]] && break
      sleep 1
    done
  fi
  if [[ -f "$CANDIDATE/manifest.json" && -f "$CANDIDATE/manifest.json.sig" ]]; then
    echo "  OK   Bundle liegt in redeploy-candidate/"
  else
    echo "  FEHL Bundle wurde nicht heruntergeladen (siehe $WORK/dashboard.log)"; FAILED=1
  fi
  # Der Ereignisstrom bleibt offen, curl bricht nach zwei Sekunden ab.
  events="$(api -m 2 "$BASE/redeploy/api/events?since=0" || true)"
  if grep -q 'package.log.ready' <<<"$events"; then
    echo "  OK   Download-Protokoll kommt als Katalogschluessel"
  else
    echo "  FEHL Download-Protokoll ohne package.log-Schluessel"; FAILED=1
  fi
  check "Vorschau nennt Version und alle Schritte des heruntergeladenen Bundles" \
    "data['bundle_version'] == 'v9.9.9' and [s['id'] for s in data['steps']] == ['10','20','30','40','50','60','65','70','81','82']" \
    "$BASE/redeploy/api/plan"
  check "Auswahl des Nodes gilt (optionaler Schritt 70 abgewaehlt)" \
    "any(s['id'] == '70' and s['state'] == 'deselected' for s in data['steps'])" \
    "$BASE/redeploy/api/plan"
  check "geaenderter Dienst (battery_soc) steht mit restart=version in der Vorschau" \
    "next(s for s in data['steps'] if s['id'] == '82')['restart'] == 'version'" \
    "$BASE/redeploy/api/plan"
  check "unveraenderter Dienst (apsystems) hat keinen Neustartgrund" \
    "not next(s for s in data['steps'] if s['id'] == '81').get('restart')" \
    "$BASE/redeploy/api/plan"
  check "Was ist neu: Changelog des Pakets neben den installierten Versionen" \
    "data['bundle_version'] == 'v9.9.9' and data['installed']['dashboard'] == 'v0.7.5' and data['installed']['service:battery_soc'] == 'v0.4.0' and len(data['document']['components']) == 3" \
    "$BASE/redeploy/api/changelog"
  check "Was ist neu: Highlight-Markierung kommt unveraendert an" \
    "sorted({e['highlight'] for c in data['document']['components'] for r in c['releases'] for g in r['groups'] for e in g['entries']}) == [False, True]" \
    "$BASE/redeploy/api/changelog"
  check "Die Oberflaeche kennt jetzt das bereitliegende Paket" \
    "data['auto_prepare'] is True and data['bundle_version'] == 'v9.9.9'" \
    "$BASE/redeploy/api/bootstrap"
  # Zweiter Lauf: gleiche Version, also kein zweiter Download.
  before="$(stat -c %Y "$CANDIDATE/manifest.json")"
  sleep 1
  start_prepare || true
  sleep 2
  if [[ "$(stat -c %Y "$CANDIDATE/manifest.json")" == "$before" ]]; then
    echo "  OK   gleiche Version wird nicht erneut heruntergeladen"
  else
    echo "  FEHL das Bundle wurde trotz gleicher Version neu geschrieben"; FAILED=1
  fi
  rm -rf "$CANDIDATE"
}

checks_installed() {
  # Die Versionsseite liest genau die Dateien, die Installer und Updater
  # ablegen. Shelly ist in selection.json abgewaehlt (Schritt 83).
  check "Versionen: Paket und laufendes Dashboard" \
    "data['bundle']['version'] == 'v0.7.5' and data['bundle']['arch'] == 'armv6' and data['running']['dashboard'].startswith('v')" \
    "$BASE/api/v1/versions"
  check "Versionen: Komponenten mit Art, Reihenfolge wie im Changelog" \
    "[c['id'] for c in data['components']] == ['dashboard', 'service:battery_soc', 'service:shelly', 'energy_node_common'] and data['has_changelog'] is True" \
    "$BASE/api/v1/versions"
  check "Versionen: abgewaehlter Dienst gilt als nicht installiert" \
    "{c['id']: c['installed'] for c in data['components']} == {'dashboard': True, 'service:battery_soc': True, 'service:shelly': False, 'energy_node_common': True}" \
    "$BASE/api/v1/versions"
  check "Changelog: nach Komponente gefiltert" \
    "[c['id'] for c in data['components']] == ['service:battery_soc']" \
    "$BASE/api/v1/changelog?component=service:battery_soc"
  check "Changelog: Breaking-Eintrag bleibt als solcher erhalten" \
    "any(e['breaking'] for r in data['components'][0]['releases'] for g in r['groups'] for e in g['entries'])" \
    "$BASE/api/v1/changelog?component=dashboard"
  check "Changelog: Highlights und Details markiert" \
    "[e['highlight'] for g in data['components'][0]['releases'][0]['groups'] for e in g['entries']] == [True, False, True, False]" \
    "$BASE/api/v1/changelog?component=dashboard"
  check "Versionen: Vorgaenger gemeldet" \
    "data['previous'] is not None" \
    "$BASE/api/v1/versions"
  check "Neuerungen: Bereich seit dem Vorgaenger" \
    "data['since_previous'] is True and data['from'] != data['to'] and len(data['components']) > 0" \
    "$BASE/api/v1/changelog/highlights"

  # Oberflaeche: die Skripte der Neuerungen und der Update-Leiste werden
  # ausgeliefert, und die Basisseite bindet sie ein (der Neuerungen-Dialog
  # liegt in der Basisseite).
  for asset in js/update-bar.js js/whatsnew.page.js; do
    if api "$BASE/static/$asset" > /dev/null; then
      echo "  OK   Asset $asset wird ausgeliefert"
    else
      echo "  FEHL Asset $asset wird nicht ausgeliefert"; FAILED=1
    fi
  done
  # In eine Variable lesen: grep -q beendet sich beim ersten Treffer und kann
  # curl per SIGPIPE abbrechen (Exit 23), was unter pipefail als Fehler zaehlt.
  base_page="$(api "$BASE/" || true)"
  if grep -q 'whatsnew.page.js' <<<"$base_page" && grep -q 'update-whatsnew-open' <<<"$base_page"; then
    echo "  OK   Basisseite bindet das Neuerungen-Skript und den Dialog ein"
  else
    echo "  FEHL Basisseite bindet whatsnew.page.js oder den Neuerungen-Dialog nicht ein"; FAILED=1
  fi
  settings_js="$(api "$BASE/static/js/settings.page.js" || true)"
  if grep -q "get('page')" <<<"$settings_js"; then
    echo "  OK   settings.page.js wertet den Tiefenlink ?page= aus"
  else
    echo "  FEHL settings.page.js enthaelt die Auswertung von ?page= nicht"; FAILED=1
  fi

  # Fortschritt: ein laufender Auftrag, dann sein Ende. Das Job-Verzeichnis
  # gehoert im Smoke-Test dem Testbenutzer, wie auf dem Pi dem Dashboard-Konto.
  STATE="$WORK/installer-state"
  job_dir="$STATE/job"
  mkdir -p "$job_dir"
  check "Fortschritt: ohne Auftrag idle" "data['state'] == 'idle'" "$BASE/api/v1/updates/progress"
  printf '{"bundle_version":"v9.9.9","mode":"redeploy","steps":["10","60"],"run_id":"smoke-1"}\n' > "$job_dir/current.json"
  printf '%s ##STEP 10 ok\n%s ##STEP 60 begin\n' "$(date +%s%3N)" "$(date +%s%3N)" > "$job_dir/log"
  check "Fortschritt: laeuft mit Schrittnamen" \
    "data['state'] == 'running' and data['done'] == 1 and data['step']['id'] == '60' and data['step']['title'] != ''" \
    "$BASE/api/v1/updates/progress"
  printf '{"result":"ok"}\n' > "$job_dir/status.json"
  check "Fortschritt: abgeschlossen" \
    "data['state'] == 'ok' and data['done'] == 2 and data['finished_at']" \
    "$BASE/api/v1/updates/progress"
  rm -rf "$job_dir"
}

checks_update_install() {
  # Update installieren: ein Konto mit nur install_updates startet das angebotene
  # Update per POST /api/v1/updates/install. Das braucht ein angebotenes Update
  # (fake_github_releases.py, also --simulate-package) und das Zustandsverzeichnis
  # des Smoke-Tests. Der Updater laeuft hier nicht, der Auftrag bleibt pending.
  echo "==> Update installieren: Konto mit install_updates"
  UPDATER_COOKIES="$WORK/smoke-updater-cookies.txt"
  create_smoke_user "smoke-updater" '["install_updates"]' "$UPDATER_COOKIES"
  UPDATER_CSRF="$(curl -sf "${CURL_INSECURE[@]}" -m 5 -b "$UPDATER_COOKIES" -H "$SECURE_HEADER" "$BASE/api/v1/auth/session" | python3 -c 'import json,sys; print(json.load(sys.stdin)["csrf_token"])')"
  curl -sf "${CURL_INSECURE[@]}" -m 5 -o /dev/null -b "$UPDATER_COOKIES" -H "$SECURE_HEADER" "$BASE/api/v1/updates/check" || FAILED=1
  code=$(curl -s "${CURL_INSECURE[@]}" -m 5 -o /dev/null -w '%{http_code}' -b "$UPDATER_COOKIES" -H "$SECURE_HEADER" \
    -H "X-CSRF-Token: $UPDATER_CSRF" -H 'Content-Type: application/json' -X POST "$BASE/api/v1/updates/install")
  if [[ "$code" == "202" ]]; then
    echo "  OK   smoke-updater startet das angebotene Update (HTTP 202)"
  else
    echo "  FEHL Update-Installation fuer smoke-updater kam mit HTTP $code"; FAILED=1
  fi
  check_status "Installer-Bildschirm fuer smoke-updater gesperrt" 403 "$UPDATER_COOKIES" "$BASE/redeploy/"
  update_state() { api "$BASE/api/v1/updates/progress" | python3 -c 'import json,sys; print(json.load(sys.stdin).get("state", ""))' 2>/dev/null || true; }
  for _ in $(seq 1 30); do
    [[ "$(update_state)" == "pending" ]] && break
    sleep 1
  done
  if [[ "$(update_state)" == "pending" ]]; then
    echo "  OK   Update-Fortschritt meldet den wartenden Auftrag (pending)"
  else
    echo "  FEHL Update-Fortschritt meldet nicht pending: $(update_state)"; FAILED=1
  fi
  # Den Auftrag entfernen, damit spaetere Pruefungen nicht an ihm haengen.
  rm -rf "$WORK/installer-state/job"
}
