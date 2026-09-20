#!/usr/bin/env bash
# Test for scripts/bootstrap/70-caddy.sh
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
script="$here/../bootstrap/70-caddy.sh"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
fail() { echo "FAIL: $1"; [ -n "${2:-}" ] && printf '%s\n' "$2"; exit 1; }

mkdir -p "$tmp/bin"
cat > "$tmp/bin/caddy" <<'SH'
#!/usr/bin/env bash
printf 'caddy %s\n' "$*" >> "$CADDY_LOG"
exit "${CADDY_RC:-0}"
SH
cat > "$tmp/bin/systemctl" <<'SH'
#!/usr/bin/env bash
printf 'systemctl %s\n' "$*" >> "$SYSTEMCTL_LOG"
SH
# dpkg -S: DPKG_RC=0 heisst "die Datei gehoert einem Paket".
cat > "$tmp/bin/dpkg" <<'SH'
#!/usr/bin/env bash
exit "${DPKG_RC:-1}"
SH
chmod +x "$tmp/bin/caddy" "$tmp/bin/systemctl" "$tmp/bin/dpkg"
export PATH="$tmp/bin:$PATH"
export CADDY_LOG="$tmp/caddy.log" SYSTEMCTL_LOG="$tmp/systemctl.log"

bundle="$tmp/bundle"
mkdir -p "$bundle/caddy" "$bundle/dashboard"
printf '#!/bin/sh\n'              > "$bundle/caddy/caddy"
printf ':443 {\n  tls internal\n}\n' > "$bundle/dashboard/Caddyfile"

export EN_STATE_DIR="$tmp/state" EN_ROOT="$tmp/root" EN_BUNDLE_DIR="$bundle"
export EN_BUNDLE_VERSION=v1.0.0 EN_SUDO="" EN_SELECTION="$tmp/selection.json"

caddyfile="$tmp/root/etc/caddy/Caddyfile"

# --- abgewaehlt -----------------------------------------------------------
printf '{"steps":{"70":false}}\n' > "$EN_SELECTION"
out="$(bash "$script")"
grep -q '^##STEP 70 skip nicht ausgewaehlt$' <<<"$out" || fail "nicht abgewaehlt" "$out"
[ -e "$tmp/root" ] && fail "abgewaehlter Schritt hat Dateien angelegt"
rm -f "$EN_SELECTION"

# --- erster Lauf ----------------------------------------------------------
out="$(bash "$script")"
grep -q '^##STEP 70 ok$' <<<"$out" || fail "kein ok-Marker" "$out"
[ -x "$tmp/root/usr/bin/caddy" ] || fail "Binary nicht installiert"
[ -f "$caddyfile" ] || fail "Caddyfile nicht installiert"
grep -q 'caddy validate' "$CADDY_LOG" || fail "keine Validierung" "$(cat "$CADDY_LOG")"
grep -q 'systemctl enable --now caddy' "$SYSTEMCTL_LOG" \
  || fail "Caddy nicht gestartet" "$(cat "$SYSTEMCTL_LOG")"

# --- zweiter Lauf ueberspringt --------------------------------------------
: > "$CADDY_LOG"
out="$(bash "$script")"
grep -q '^##STEP 70 skip bereits erledigt$' <<<"$out" || fail "nicht uebersprungen" "$out"
[ -s "$CADDY_LOG" ] && fail "zweiter Lauf hat caddy aufgerufen"

# --- eigene Caddyfile bleibt stehen und wird gemeldet ---------------------
rm -rf "$tmp/state"
printf ':8443 {\n}\n' > "$caddyfile"
out="$(bash "$script")"
grep -q '^##STEP 70 ok$' <<<"$out" || fail "Lauf mit eigener Caddyfile nicht ok" "$out"
grep -qx ':8443 {' "$caddyfile" || fail "eigene Caddyfile ueberschrieben" "$(cat "$caddyfile")"
grep -qi 'weicht ab' <<<"$out" || fail "Abweichung nicht gemeldet" "$out"

# --- vorhandenes Caddy-Binary bleibt stehen, wenn es nicht unser ist -------
# Ein Node, dessen Caddy aus dem Debian-Paket stammt (oder schon neuer ist
# als das Bundle), darf nicht durch das Bundle-Binary ersetzt werden: dpkg
# haelt die Datei, und ein spaeteres apt upgrade wuerde sie wieder tauschen.
bundled_caddy() {
  printf '#!/bin/sh\necho "v2.9.1 h1:bundled"\n' > "$bundle/caddy/caddy"
  chmod +x "$bundle/caddy/caddy"
}
installed_caddy() { # <erste Zeile von "caddy version">
  rm -rf "$tmp/root" "$tmp/state"
  mkdir -p "$tmp/root/usr/bin" "$tmp/root/etc/caddy"
  printf '#!/bin/sh\n# vorhandenes caddy\necho "%s"\n' "$1" > "$tmp/root/usr/bin/caddy"
  chmod +x "$tmp/root/usr/bin/caddy"
  printf ':443 {\n  tls internal\n}\n' > "$caddyfile"
}
mkdir -p "$bundle/caddy"; bundled_caddy

installed_caddy "v2.6.2 h1:debian"
out="$(DPKG_RC=0 bash "$script")"
grep -q '^##STEP 70 ok$' <<<"$out" || fail "Paket-Caddy: kein ok-Marker" "$out"
grep -q '# vorhandenes caddy' "$tmp/root/usr/bin/caddy" || fail "vom Paketmanager verwaltetes Caddy wurde ersetzt"
grep -qi 'paketmanager' <<<"$out" || fail "Paket-Caddy: kein Hinweis" "$out"

installed_caddy "v2.10.0 h1:newer"
out="$(bash "$script")"
grep -q '^##STEP 70 ok$' <<<"$out" || fail "neueres Caddy: kein ok-Marker" "$out"
grep -q '# vorhandenes caddy' "$tmp/root/usr/bin/caddy" || fail "neueres Caddy wurde durch das Bundle-Binary ersetzt"

installed_caddy "v2.9.1 h1:same"
out="$(bash "$script")"
grep -q '# vorhandenes caddy' "$tmp/root/usr/bin/caddy" || fail "gleich neues Caddy wurde ersetzt"

# Die Konfiguration wird trotzdem geprueft und der Dienst angestossen.
: > "$CADDY_LOG"; : > "$SYSTEMCTL_LOG"; rm -rf "$tmp/state"
out="$(bash "$script")"
grep -q 'caddy validate' "$CADDY_LOG" || fail "bei behaltenem Binary keine Validierung" "$(cat "$CADDY_LOG")"
grep -q 'systemctl enable --now caddy' "$SYSTEMCTL_LOG" || fail "bei behaltenem Binary kein enable --now"

# Ein eigenes, aelteres Binary (nicht vom Paketmanager) wird ersetzt.
installed_caddy "v2.6.2 h1:manual"
out="$(bash "$script")"
grep -q '^##STEP 70 ok$' <<<"$out" || fail "aelteres Caddy: kein ok-Marker" "$out"
grep -q '# vorhandenes caddy' "$tmp/root/usr/bin/caddy" && fail "aelteres, nicht paketverwaltetes Caddy wurde nicht ersetzt"

# --- ungueltige Konfiguration --------------------------------------------
rm -rf "$tmp/state"
set +e
out="$(CADDY_RC=1 bash "$script")"
rc=$?
set -e
[ "$rc" -eq 1 ] || fail "ungueltige Konfiguration nicht abgelehnt" "$rc"
grep -q '^##STEP 70 fail CADDY_CONFIG_INVALID$' <<<"$out" || fail "falscher Code" "$out"

# --- fehlendes Beipack ----------------------------------------------------
rm -rf "$tmp/state" "$bundle/caddy"
set +e
out="$(bash "$script")"
set -e
grep -q '^##STEP 70 fail CADDY_BINARY_MISSING$' <<<"$out" || fail "falscher Code" "$out"

echo "OK: $(basename "$0")"
