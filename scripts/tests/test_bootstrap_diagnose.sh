#!/usr/bin/env bash
# Test for scripts/bootstrap/diagnose.sh
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
script="$here/../bootstrap/diagnose.sh"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
fail() { echo "FAIL: $1"; [ -n "${2:-}" ] && printf '%s\n' "$2"; exit 1; }

mkdir -p "$tmp/bin"
# ACTIVE listet die Units, die als aktiv gelten sollen.
cat > "$tmp/bin/systemctl" <<'SH'
#!/usr/bin/env bash
[ "$1" = is-active ] || exit 1
case " ${ACTIVE:-} " in *" $2 "*) echo active; exit 0 ;; esac
echo inactive; exit 3
SH
cat > "$tmp/bin/ss" <<'SH'
#!/usr/bin/env bash
printf 'State Recv-Q Send-Q Local-Address:Port\n'
for p in ${LISTENING:-}; do printf 'LISTEN 0 128 0.0.0.0:%s 0.0.0.0:*\n' "$p"; done
SH
cat > "$tmp/bin/tailscale" <<'SH'
#!/usr/bin/env bash
exit "${TS_STATUS_RC:-0}"
SH
chmod +x "$tmp/bin/systemctl" "$tmp/bin/ss" "$tmp/bin/tailscale"
export PATH="$tmp/bin:$PATH"

bundle="$tmp/bundle"
mkdir -p "$bundle"
cat > "$bundle/manifest.json" <<'JSON'
{
  "version": "v0.2.0",
  "steps": [
    { "id": "10", "optional": false },
    { "id": "83", "optional": true, "service_id": "shelly", "dir": "shelly",
      "unit": "shelly-rpc.service" },
    { "id": "85", "optional": true, "service_id": "tuya", "dir": "tuya_mqtt",
      "unit": "tuya.service" }
  ]
}
JSON

export EN_STATE_DIR="$tmp/state" EN_ROOT="$tmp/root" EN_BUNDLE_DIR="$bundle"
export EN_BUNDLE_VERSION=v0.2.0 EN_SUDO=""

mkdir -p "$EN_STATE_DIR/steps" "$tmp/root/etc/energy-node/manifests"
printf 'bundle=v0.2.0\n' > "$EN_STATE_DIR/steps/10"
printf 'bundle=v0.1.0\n' > "$EN_STATE_DIR/steps/83"
printf '{}\n' > "$tmp/root/etc/energy-node/config.json"
printf '{"service_id":"shelly"}\n' > "$tmp/root/etc/energy-node/manifests/shelly.json"
printf '{"service_id":"tuya"}\n'   > "$tmp/root/etc/energy-node/manifests/tuya.json"

out="$(ACTIVE="mosquitto.service shelly-rpc.service" LISTENING="1883" \
       TS_STATUS_RC=0 bash "$script")"

get() { python3 -c 'import json,sys; d=json.load(sys.stdin); print(eval(sys.argv[1], {"d": d}))' "$1" <<<"$out"; }

[ "$(get 'd["bundle_version"]')" = v0.2.0 ] || fail "bundle_version falsch" "$out"
[ "$(get 'd["steps"]["10"]')" = v0.2.0 ] || fail "Stempel 10 falsch" "$out"
[ "$(get 'd["steps"]["83"]')" = v0.1.0 ] || fail "alter Stempel nicht gemeldet" "$out"
[ "$(get 'd["units"]["mosquitto.service"]')" = active ] || fail "mosquitto nicht aktiv" "$out"
[ "$(get 'd["units"]["caddy.service"]')" = inactive ] || fail "caddy nicht inaktiv" "$out"
# Die Unit aus der Schrittliste muss ohne Codeaenderung auftauchen.
[ "$(get 'd["units"]["shelly-rpc.service"]')" = active ] || fail "Dienst-Unit fehlt" "$out"
# Ohne Unit-Datei und nicht aktiv: nie installiert. Eine feste Unit bleibt
# "inactive", auch ohne Datei unter /etc/systemd/system.
[ "$(get 'd["units"]["tuya.service"]')" = not-installed ] || fail "nicht installierter Dienst nicht erkannt" "$out"
mkdir -p "$tmp/root/etc/systemd/system"
touch "$tmp/root/etc/systemd/system/tuya.service"
out="$(ACTIVE="mosquitto.service" bash "$script")"
[ "$(get 'd["units"]["tuya.service"]')" = inactive ] || fail "installierter, gestoppter Dienst nicht inactive" "$out"
rm "$tmp/root/etc/systemd/system/tuya.service"
out="$(ACTIVE="mosquitto.service shelly-rpc.service" LISTENING="1883" \
       TS_STATUS_RC=0 bash "$script")"
[ "$(get 'd["ports"]["1883"]')" = True ] || fail "1883 nicht als offen erkannt" "$out"
[ "$(get 'd["ports"]["8080"]')" = False ] || fail "8080 faelschlich offen" "$out"
[ "$(get 'd["config"]["config.json"]')" = True ] || fail "config.json nicht erkannt" "$out"
[ "$(get 'sorted(d["config"]["manifests"])')" = "['shelly', 'tuya']" ] || fail "Manifeste falsch" "$out"
[ "$(get 'd["tailscale"]["angemeldet"]')" = True ] || fail "tailscale nicht angemeldet" "$out"

# --- Neustart-Markierung ----------------------------------------------------
out="$(bash "$script")"
python3 -c 'import json,sys; r=json.load(sys.stdin); sys.exit(0 if r["reboot_required"] is False else 1)' <<<"$out" \
  || fail "reboot_required ohne Markierung nicht false" "$out"
mkdir -p "$tmp/root/run"
touch "$tmp/root/run/reboot-required"
out="$(bash "$script")"
python3 -c 'import json,sys; r=json.load(sys.stdin); sys.exit(0 if r["reboot_required"] is True else 1)' <<<"$out" \
  || fail "reboot_required mit Markierung nicht true" "$out"
rm -f "$tmp/root/run/reboot-required"

# --- tailscale liegt in /usr/sbin, das im PATH einer SSH-Sitzung fehlt ----
# Eine nicht-interaktive Sitzung auf Debian hat nur /usr/local/bin:/usr/bin:
# /bin:/usr/games im PATH. "command -v tailscale" fand das Programm dort nie
# und meldete einen angemeldeten Node als ausgeloggt. Ueber sudo greift
# dessen secure_path - die Attrappe stellt /usr/sbin nur fuer den
# umschlossenen Aufruf voran.
mkdir -p "$tmp/sbin"
mv "$tmp/bin/tailscale" "$tmp/sbin/tailscale"
cat > "$tmp/bin/fakesudo" <<SH
#!/usr/bin/env bash
PATH="$tmp/sbin:\$PATH" exec "\$@"
SH
chmod +x "$tmp/bin/fakesudo"
out="$(EN_SUDO="$tmp/bin/fakesudo" ACTIVE="" LISTENING="" TS_STATUS_RC=0 bash "$script")"
[ "$(get 'd["tailscale"]["angemeldet"]')" = True ] \
  || fail "tailscale in /usr/sbin (nicht im PATH) nicht gefunden" "$out"
out="$(EN_SUDO="$tmp/bin/fakesudo" ACTIVE="" LISTENING="" TS_STATUS_RC=1 bash "$script")"
[ "$(get 'd["tailscale"]["angemeldet"]')" = False ] \
  || fail "abgemeldet ueber sudo nicht erkannt" "$out"
mv "$tmp/sbin/tailscale" "$tmp/bin/tailscale"

# --- Shelly-Wake-Webhook: nur mit Opt-in (35) und Shelly-Dienst (83) -----
# UFW_ALLOWED listet die Ports, fuer die "ufw status" eine ALLOW-Regel zeigt.
cat > "$tmp/bin/ufw" <<'SH'
#!/usr/bin/env bash
[ "$1" = status ] || exit 1
printf 'Status: active\n\nTo                         Action      From\n--                         ------      ----\n'
for p in ${UFW_ALLOWED:-}; do printf '%s/tcp                   ALLOW       Anywhere\n' "$p"; done
SH
chmod +x "$tmp/bin/ufw"
has_webhook() { get '"shelly_webhook" in d'; }

rm -f "$EN_STATE_DIR/selection.json"
out="$(UFW_ALLOWED="8082" LISTENING="8082" bash "$script")"
[ "$(has_webhook)" = False ] || fail "Webhook ohne Auswahl gemeldet" "$out"
printf '{"steps":{"35":false}}\n' > "$EN_STATE_DIR/selection.json"
out="$(UFW_ALLOWED="8082" LISTENING="8082" bash "$script")"
[ "$(has_webhook)" = False ] || fail "Webhook trotz Abwahl gemeldet" "$out"
printf '{"steps":{"35":true,"83":false}}\n' > "$EN_STATE_DIR/selection.json"
out="$(UFW_ALLOWED="8082" LISTENING="8082" bash "$script")"
[ "$(has_webhook)" = False ] || fail "Webhook ohne Shelly-Dienst gemeldet" "$out"

printf '{"steps":{"35":true}}\n' > "$EN_STATE_DIR/selection.json"
out="$(UFW_ALLOWED="1883 8082" LISTENING="1883" bash "$script")"
[ "$(get 'd["shelly_webhook"]["port"]')" = 8082 ] || fail "Webhook-Port falsch" "$out"
[ "$(get 'd["shelly_webhook"]["firewall"]')" = True ] || fail "Firewall-Regel nicht erkannt" "$out"
[ "$(get 'd["shelly_webhook"]["listening"]')" = False ] || fail "Listener faelschlich erkannt" "$out"
# Der feste Port-Check bleibt unberuehrt: 8082 ist kein Pflichtport.
[ "$(get '"8082" in d["ports"]')" = False ] || fail "8082 in den Pflichtports" "$out"

out="$(UFW_ALLOWED="" LISTENING="8082" bash "$script")"
[ "$(get 'd["shelly_webhook"]["firewall"]')" = False ] || fail "fehlende Regel nicht erkannt" "$out"
[ "$(get 'd["shelly_webhook"]["listening"]')" = True ] || fail "Listener nicht erkannt" "$out"

# Ein im Dashboard geaenderter webhook_port gilt auch hier.
printf '{"services":{"shelly":{"webhook_port":9090}}}\n' > "$tmp/root/etc/energy-node/config.json"
out="$(UFW_ALLOWED="9090" LISTENING="9090" bash "$script")"
[ "$(get 'd["shelly_webhook"]["port"]')" = 9090 ] || fail "webhook_port aus config.json ignoriert" "$out"
[ "$(get 'd["shelly_webhook"]["firewall"]')" = True ] || fail "Regel fuer 9090 nicht erkannt" "$out"
rm -f "$EN_STATE_DIR/selection.json"

# --- installierte Versionen und Geraete je Dienst ------------------------
# Ohne installed-manifest.json: leere Versionen, und das Geraeteverzeichnis
# kommt aus dem Bundle-Manifest (target_base) bzw. $HOME.
[ "$(get 'd["versions"]')" = "{'components': {}, 'services': {}}" ] || fail "Versionen ohne installiertes Manifest nicht leer" "$out"
[ "$(get 'd["devices"]')" = "{}" ] || fail "Geraete ohne Geraetedatei gemeldet" "$out"

mkdir -p "$bundle/services/shelly/devices"
printf '[]\n' > "$bundle/services/shelly/devices/shelly_devices.json"
printf '{}\n' > "$bundle/services/shelly/devices/shelly_devices.schema.json"
cat > "$EN_STATE_DIR/installed-manifest.json" <<'JSON'
{
  "version": "v0.1.0",
  "target_base": "/home/en",
  "components": { "bootstrap": "v0.1.10", "energy_node_common": "v0.4.7" },
  "steps": [
    { "id": "10", "optional": false },
    { "id": "83", "dir": "shelly", "unit": "shelly-rpc.service", "version": "v0.4.2" }
  ]
}
JSON
mkdir -p "$tmp/root/home/en/devices"
cat > "$tmp/root/home/en/devices/shelly_devices.json" <<'JSON'
[
  { "id": "plug", "name": "Plug S+ Küche" },
  { "id": "em3" },
  "kein Objekt"
]
JSON
out="$(bash "$script")"
[ "$(get 'd["versions"]["components"]["energy_node_common"]')" = v0.4.7 ] || fail "Komponentenversion fehlt" "$out"
[ "$(get 'd["versions"]["services"]["shelly-rpc.service"]')" = v0.4.2 ] || fail "Dienstversion fehlt" "$out"
[ "$(get '[(x["id"], x["name"]) for x in d["devices"]["shelly-rpc.service"]]')" = "[('plug', 'Plug S+ Küche'), ('em3', 'em3')]" ] \
  || fail "Geraete falsch gelesen" "$out"

# EN_TARGET_BASE schlaegt target_base aus dem Manifest.
mkdir -p "$tmp/root/other/devices"
printf '[{"id":"x","name":"Anderes"}]\n' > "$tmp/root/other/devices/shelly_devices.json"
out="$(EN_TARGET_BASE=/other bash "$script")"
[ "$(get 'd["devices"]["shelly-rpc.service"][0]["name"]')" = Anderes ] || fail "EN_TARGET_BASE ignoriert" "$out"

# Eine kaputte Geraetedatei ist null, kein Abbruch.
printf '{kaputt' > "$tmp/root/home/en/devices/shelly_devices.json"
out="$(bash "$script")"
[ "$(get 'd["devices"]["shelly-rpc.service"]')" = None ] || fail "kaputte Geraetedatei nicht als null gemeldet" "$out"

# --- kaputter Node: trotzdem Exit 0 und vollstaendiges JSON --------------
rm -rf "$tmp/root" "$tmp/state"
set +e
out="$(ACTIVE="" LISTENING="" TS_STATUS_RC=1 bash "$script")"
rc=$?
set -e
[ "$rc" -eq 0 ] || fail "diagnose.sh bricht bei kaputtem Node ab" "$rc"
[ "$(get 'd["config"]["config.json"]')" = False ] || fail "fehlende config nicht gemeldet" "$out"
[ "$(get 'd["config"]["manifests"]')" = "[]" ] || fail "fehlende Manifeste nicht leer" "$out"
[ "$(get 'd["tailscale"]["angemeldet"]')" = False ] || fail "abgemeldet nicht erkannt" "$out"
[ "$(get 'd["steps"]')" = "{}" ] || fail "Stempel ohne Verzeichnis nicht leer" "$out"

# --- keine ##STEP-Marker --------------------------------------------------
grep -q '^##STEP' <<<"$out" && fail "diagnose.sh gibt Schritt-Marker aus" "$out"

echo "OK: $(basename "$0")"
