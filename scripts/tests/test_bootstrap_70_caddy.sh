#!/usr/bin/env bash
# Test for scripts/bootstrap/70-caddy.sh (Caddy aus dem gepinnten .deb)
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
script="$here/../bootstrap/70-caddy.sh"
repo_keyrings="$here/../bootstrap/keyrings"

tmp="$(mktemp -d)"
trap 'rm -rf "${tmp:?}"' EXIT
fail() { echo "FAIL: $1"; [ -n "${2:-}" ] && printf '%s\n' "$2"; exit 1; }

mkdir -p "$tmp/bin"
# caddy: validate-Lauf protokollieren, CADDY_RC=1 laesst ihn scheitern.
cat > "$tmp/bin/caddy" <<'SH'
#!/usr/bin/env bash
printf 'caddy %s\n' "$*" >> "$CADDY_LOG"
exit "${CADDY_RC:-0}"
SH
cat > "$tmp/bin/systemctl" <<'SH'
#!/usr/bin/env bash
printf 'systemctl %s\n' "$*" >> "$SYSTEMCTL_LOG"
SH
cat > "$tmp/bin/apt-get" <<'SH'
#!/usr/bin/env bash
printf 'apt-get %s\n' "$*" >> "$PKG_LOG"
exit "${APT_RC:-0}"
SH
cat > "$tmp/bin/apt-mark" <<'SH'
#!/usr/bin/env bash
printf 'apt-mark %s\n' "$*" >> "$PKG_LOG"
SH
# dpkg-query: "${Version}" liefert CADDY_DPKG_VERSION (leer = nicht installiert),
# "${Conffiles}" liefert die Paketvorgabe der Caddyfile als md5.
cat > "$tmp/bin/dpkg-query" <<'SH'
#!/usr/bin/env bash
printf 'dpkg-query %s\n' "$*" >> "$PKG_LOG"
case "$*" in
  *Conffiles*) [ -n "${CADDY_CONFFILE_MD5:-}" ] && printf ' /etc/caddy/Caddyfile %s\n' "$CADDY_CONFFILE_MD5" ;;
  *) printf '%s' "${CADDY_DPKG_VERSION:-}" ;;
esac
exit 0
SH
chmod +x "$tmp/bin"/*
export PATH="$tmp/bin:$PATH"
export CADDY_LOG="$tmp/caddy.log" SYSTEMCTL_LOG="$tmp/systemctl.log" PKG_LOG="$tmp/pkg.log"

bundle="$tmp/bundle"
export EN_STATE_DIR="$tmp/state" EN_ROOT="$tmp/root" EN_BUNDLE_DIR="$bundle"
export EN_BUNDLE_VERSION=v1.0.0 EN_SUDO="" EN_SELECTION="$tmp/selection.json"
export EN_PACKAGES_DIR="$tmp/packages"

caddyfile="$tmp/root/etc/caddy/Caddyfile"
caddy_bin="$tmp/root/usr/bin/caddy"
template_text=':443 {
  tls internal
}'

arch=amd64

# setup <pin> <update: apt|installer> <deb: ja|nein> [dpkg-Version] [Version
# eines Binarys ohne dpkg]. Baut Bundle, Manifest und Zielsystem neu.
setup() {
  local pin="$1" update="$2" deb="$3" dpkg_ver="${4:-}" bin_ver="${5:-}" sha
  DEB="caddy_${pin}_linux_${arch}.deb"
  rm -rf "${tmp:?}/root" "${tmp:?}/state" "${tmp:?}/packages" "${bundle:?}"
  mkdir -p "$tmp/root/etc/caddy" "$tmp/packages" "$bundle/bootstrap/keyrings" "$bundle/dashboard"
  cp "$repo_keyrings/caddy.gpg" "$repo_keyrings/tailscale.gpg" "$bundle/bootstrap/keyrings/"
  printf 'PRETTY_NAME="Debian"\nVERSION_CODENAME=trixie\n' > "$tmp/root/etc/os-release"
  printf '%s\n' "$template_text" > "$bundle/dashboard/Caddyfile"
  printf 'fake deb\n' > "$tmp/packages/$DEB"
  sha="$(sha256sum "$tmp/packages/$DEB" | cut -d' ' -f1)"
  [ "$deb" = ja ] || rm -f "$tmp/packages/$DEB"
  cat > "$bundle/manifest.json" <<JSON
{
  "version": "v1.0.0",
  "packages": {
    "caddy": {
      "version": "$pin",
      "file": "$DEB",
      "url": "https://example.invalid/$DEB",
      "sha256": "$sha",
      "update": "$update"
    }
  }
}
JSON
  if [ -n "$bin_ver" ]; then
    mkdir -p "$tmp/root/usr/bin" "$tmp/root/etc/caddy"
    printf '#!/bin/sh\necho "%s h1:manual"\n' "$bin_ver" > "$caddy_bin"
    printf ':8080 {\n}\n' > "$caddyfile"
    chmod +x "$caddy_bin"
  fi
  export CADDY_DPKG_VERSION="$dpkg_ver" CADDY_CONFFILE_MD5="" APT_RC=0
  : > "$CADDY_LOG"; : > "$PKG_LOG"; : > "$SYSTEMCTL_LOG"
}

# run: fuehrt den Schritt aus, Ausgabe in $out, Code in $code.
run() {
  set +e
  out="$(bash "$script" 2>&1)"
  code=$?
  set -e
}

# --- abgewaehlt -----------------------------------------------------------
printf '{"steps":{"70":false}}\n' > "$EN_SELECTION"
out="$(bash "$script")"
grep -q '^##STEP 70 skip nicht ausgewaehlt$' <<<"$out" || fail "nicht abgewaehlt" "$out"
[ -e "$tmp/root" ] && fail "abgewaehlter Schritt hat Dateien angelegt"
rm -f "$EN_SELECTION"

# --- Zeile 1: nicht installiert, .deb da, amd64 (update=apt) ---------------
setup 2.11.9 apt ja
run
[ "$code" -eq 0 ] || fail "nicht installiert mit .deb: Code $code" "$out"
grep -q '^##STEP 70 ok$' <<<"$out" || fail "nicht installiert mit .deb: kein ok" "$out"
grep -q "apt-get install .*--force-confold .*$DEB" "$PKG_LOG" \
  || fail "Paket nicht per apt aus der Datei installiert" "$(cat "$PKG_LOG")"
grep -q 'dl.cloudsmith.io/public/caddy/stable/deb/debian' "$tmp/root/etc/apt/sources.list.d/caddy.list" \
  || fail "apt-Quelle fuer Caddy fehlt"
grep -qx 'apt-mark unhold caddy' "$PKG_LOG" || fail "apt-Modus: kein unhold" "$(cat "$PKG_LOG")"
grep -q 'apt-mark hold' "$PKG_LOG" && fail "apt-Modus: hold gesetzt"
grep -q 'caddy validate' "$CADDY_LOG" || fail "keine Validierung"
grep -q 'systemctl enable --now caddy' "$SYSTEMCTL_LOG" || fail "Caddy nicht gestartet"
[ -f "$tmp/state/steps/70" ] || fail "kein Stempel nach Installation"

# --- Zeile 1, armv6: nicht installiert, .deb da, update=installer ----------
arch=armhf
setup 2.11.9 installer ja
run
arch=amd64
[ "$code" -eq 0 ] || fail "armv6 nicht installiert: Code $code" "$out"
grep -q 'apt-get install .*caddy_2.11.9_linux_armhf.deb' "$PKG_LOG" \
  || fail "armv6: .deb nicht installiert" "$(cat "$PKG_LOG")"
grep -qx 'apt-mark hold caddy' "$PKG_LOG" || fail "armv6: kein hold" "$(cat "$PKG_LOG")"
[ -e "$tmp/root/etc/apt/sources.list.d/caddy.list" ] && fail "armv6: Repo angelegt statt hold"

# --- armv6 mit hold aus einem frueheren Lauf: unhold, install, hold --------
# Der apt-mark-Stub fuehrt keinen Zustand; die Reihenfolge im Protokoll ist
# der Nachweis, dass der hold vor der Installation faellt.
arch=armhf
setup 2.11.9 installer ja
run
arch=amd64
[ "$code" -eq 0 ] || fail "armv6 mit hold: Code $code" "$out"
u_line="$(grep -n -x 'apt-mark unhold caddy' "$PKG_LOG" | head -n 1 | cut -d: -f1)"
i_line="$(grep -n 'apt-get install .*caddy_2.11.9_linux_armhf.deb' "$PKG_LOG" | head -n 1 | cut -d: -f1)"
h_line="$(grep -n -x 'apt-mark hold caddy' "$PKG_LOG" | head -n 1 | cut -d: -f1)"
if [ -z "$u_line" ] || [ -z "$i_line" ] || [ -z "$h_line" ]; then
  fail "armv6 mit hold: unhold/install/hold fehlt" "$(cat "$PKG_LOG")"
fi
if [ "$u_line" -ge "$i_line" ] || [ "$i_line" -ge "$h_line" ]; then
  fail "armv6 mit hold: Reihenfolge falsch (unhold $u_line, install $i_line, hold $h_line)" "$(cat "$PKG_LOG")"
fi

# --- Zeile 2: nicht installiert, .deb fehlt -------------------------------
setup 2.11.9 apt nein
run
[ "$code" -eq 1 ] || fail "fehlendes .deb: Code $code (erwartet 1)" "$out"
grep -q '^##STEP 70 fail CADDY_PACKAGE_MISSING$' <<<"$out" || fail "falscher Code bei fehlendem .deb" "$out"
grep -q 'apt-get' "$PKG_LOG" && fail "fehlendes .deb: trotzdem apt aufgerufen"

# --- Zeile 3: installiert, nicht aelter, .deb da (apt): Repo, sonst unangetastet
setup 2.11.9 apt ja 2.11.9-1
run
grep -q '^##STEP 70 ok$' <<<"$out" || fail "gleiche Version: kein ok" "$out"
grep -q 'apt-get install' "$PKG_LOG" && fail "gleiche Version: Paket erneut installiert"
grep -qx 'apt-mark unhold caddy' "$PKG_LOG" || fail "gleiche Version: Repo nicht eingerichtet" "$(cat "$PKG_LOG")"
[ -f "$tmp/root/etc/apt/sources.list.d/caddy.list" ] || fail "gleiche Version: apt-Quelle fehlt"

# Zeile 3, neuer und ohne .deb im Bundle: trotzdem unangetastet
setup 2.11.9 apt nein 2.12.0-1
run
grep -q '^##STEP 70 ok$' <<<"$out" || fail "neuere Version ohne .deb: kein ok" "$out"
grep -q 'apt-get install' "$PKG_LOG" && fail "neuere Version: Paket angefasst"
[ -f "$tmp/root/etc/apt/sources.list.d/caddy.list" ] || fail "neuere Version: Repo fehlt bei dpkg-Verwaltung"

# Zeile 3, armv6 (installer): nichts ausser dem Ok
arch=armhf
setup 2.11.9 installer ja 2.11.9-1
run
arch=amd64
grep -q '^##STEP 70 ok$' <<<"$out" || fail "armv6 gleiche Version: kein ok" "$out"
grep -q 'apt-get install' "$PKG_LOG" && fail "armv6 gleiche Version: Paket angefasst"
[ -e "$tmp/root/etc/apt/sources.list.d/caddy.list" ] && fail "armv6 gleiche Version: Repo angelegt"

# Zeile 4: vorhanden, nicht per dpkg verwaltet, nicht aelter: nur Log
setup 2.11.9 apt ja "" 2.11.9
run
grep -q 'nicht per dpkg verwaltet' <<<"$out" || fail "Binary ohne dpkg: kein Log" "$out"
grep -q '^##STEP 70 ok$' <<<"$out" || fail "Binary ohne dpkg: kein ok" "$out"
grep -q 'apt-get' "$PKG_LOG" && fail "Binary ohne dpkg: apt angefasst"
grep -q 'h1:manual' "$caddy_bin" || fail "Binary ohne dpkg: Binary ersetzt"
[ -e "$tmp/root/etc/apt/sources.list.d/caddy.list" ] && fail "Binary ohne dpkg: Repo angelegt"

# Zeile 5: aelter, .deb da: Installation, unverwaltetes Binary wird ersetzt
setup 2.11.9 apt ja "" 2.6.2
run
grep -q '^##STEP 70 ok$' <<<"$out" || fail "aelter mit .deb: kein ok" "$out"
grep -q 'apt-get install .*'"$DEB" "$PKG_LOG" || fail "aelter mit .deb: kein Paket" "$(cat "$PKG_LOG")"
grep -qx 'apt-mark unhold caddy' "$PKG_LOG" || fail "aelter mit .deb: kein Repo" "$(cat "$PKG_LOG")"

# Zeile 6: aelter, .deb fehlt (apt): unangetastet, Log, ok
setup 2.11.9 apt nein 2.6.2
run
grep -q 'Update über apt' <<<"$out" || fail "aelter ohne .deb: kein Log 'Update über apt'" "$out"
grep -q '^##STEP 70 ok$' <<<"$out" || fail "aelter ohne .deb: kein ok" "$out"
grep -q 'apt-get' "$PKG_LOG" && fail "aelter ohne .deb: apt angefasst"

# Zeile 6, armv6 (installer): Log fuer den naechsten Installer-Lauf
arch=armhf
setup 2.11.9 installer nein 2.6.2
run
arch=amd64
grep -q 'Update beim nächsten Installer-Lauf' <<<"$out" \
  || fail "armv6 aelter ohne .deb: kein Installer-Log" "$out"
grep -q '^##STEP 70 ok$' <<<"$out" || fail "armv6 aelter ohne .deb: kein ok" "$out"

# --- Caddyfile nach der Installation --------------------------------------
# Die Paketvorgabe (md5 aus dpkg) wird durch die Bundle-Vorlage ersetzt.
setup 2.11.9 apt ja
printf ':8080 {\n  respond "paket"\n}\n' > "$caddyfile"
CADDY_CONFFILE_MD5="$(md5sum "$caddyfile" | cut -d' ' -f1)"
export CADDY_CONFFILE_MD5
run
[ "$code" -eq 0 ] || fail "Paketvorgabe: Code $code" "$out"
grep -qxF "$template_text" "$caddyfile" || fail "Paketvorgabe nicht durch die Vorlage ersetzt" "$(cat "$caddyfile")"
grep -q 'caddy validate' "$CADDY_LOG" || fail "Paketvorgabe: keine Validierung"

# Eigene Caddyfile bleibt stehen und wird gemeldet.
setup 2.11.9 apt ja
printf ':8443 {\n}\n' > "$caddyfile"
CADDY_CONFFILE_MD5="0123456789abcdef0123456789abcdef"
export CADDY_CONFFILE_MD5
run
[ "$code" -eq 0 ] || fail "eigene Caddyfile: Code $code" "$out"
grep -qx ':8443 {' "$caddyfile" || fail "eigene Caddyfile ueberschrieben" "$(cat "$caddyfile")"
grep -qi 'weicht ab' <<<"$out" || fail "Abweichung nicht gemeldet" "$out"

# Ohne Caddyfile legt der Schritt die Vorlage an.
setup 2.11.9 apt ja
run
grep -qxF "$template_text" "$caddyfile" || fail "fehlende Caddyfile nicht aus der Vorlage angelegt"

# --- Pruefsumme falsch -> PACKAGE_FILE_INVALID, nichts installiert --------
setup 2.11.9 apt ja
printf 'manipulated\n' > "$tmp/packages/$DEB"
run
[ "$code" -eq 1 ] || fail "falsche Pruefsumme nicht abgelehnt" "$out"
grep -q '^##STEP 70 fail PACKAGE_FILE_INVALID$' <<<"$out" || fail "kein PACKAGE_FILE_INVALID-Marker" "$out"
grep -q 'apt-get' "$PKG_LOG" && fail "falsche Pruefsumme: trotzdem apt aufgerufen"

# --- ungueltige Konfiguration --------------------------------------------
setup 2.11.9 apt ja
set +e
out="$(CADDY_RC=1 bash "$script" 2>&1)"
rc=$?
set -e
[ "$rc" -eq 1 ] || fail "ungueltige Konfiguration nicht abgelehnt" "$rc"
grep -q '^##STEP 70 fail CADDY_CONFIG_INVALID$' <<<"$out" || fail "falscher Code" "$out"

# --- zweiter Lauf ueberspringt --------------------------------------------
setup 2.11.9 apt ja
run
: > "$CADDY_LOG"
run
grep -q '^##STEP 70 skip bereits erledigt$' <<<"$out" || fail "nicht uebersprungen" "$out"
[ -s "$CADDY_LOG" ] && fail "zweiter Lauf hat caddy aufgerufen"

# --- fehlendes .deb und fehlende Vorlage: Abbruch vor allem anderen --------
setup 2.11.9 apt nein
rm -f "$bundle/dashboard/Caddyfile"
run
grep -q '^##STEP 70 fail CADDY_BINARY_MISSING$' <<<"$out" || fail "fehlende Vorlage nicht erkannt" "$out"

echo "OK: $(basename "$0")"
