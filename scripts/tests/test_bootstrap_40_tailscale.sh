#!/usr/bin/env bash
# Test for scripts/bootstrap/40-tailscale.sh (Tailscale aus dem gepinnten .deb)
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
script="$here/../bootstrap/40-tailscale.sh"
repo_keyrings="$here/../bootstrap/keyrings"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
fail() { echo "FAIL: $1"; [ -n "${2:-}" ] && printf '%s\n' "$2"; exit 1; }

mkdir -p "$tmp/bin"
# TS_STATUS_RC=0 bedeutet angemeldet; echtes `tailscale status` endet mit
# einem Fehler, solange der Node ausgeloggt ist.
cat > "$tmp/bin/tailscale" <<'SH'
#!/usr/bin/env bash
printf 'tailscale %s\n' "$*" >> "$TS_LOG"
printf 'tailscale %s\n' "$*" >> "$ORDER_LOG"
case "$1" in
  status) exit "${TS_STATUS_RC:-0}" ;;
  update) exit "${TS_UPDATE_RC:-0}" ;;
  up) printf 'To authenticate, visit:\n\nhttps://login.tailscale.com/a/abc123\n' ;;
esac
SH
cat > "$tmp/bin/systemctl" <<'SH'
#!/usr/bin/env bash
printf 'systemctl %s\n' "$*" >> "$SYSTEMCTL_LOG"
printf 'systemctl %s\n' "$*" >> "$ORDER_LOG"
SH
# Ein installiertes tailscale-.deb meldet danach seine Version aus dem Dateinamen.
cat > "$tmp/bin/apt-get" <<'SH'
#!/usr/bin/env bash
printf 'apt-get %s\n' "$*" >> "$PKG_LOG"
last="${*: -1}"
case "$last" in
  */tailscale_*.deb)
    v="$(basename "$last")"; v="${v#tailscale_}"; v="${v%%_*}"
    printf '%s' "$v" > "$TS_INSTALLED_FILE" ;;
esac
SH
cat > "$tmp/bin/apt-mark" <<'SH'
#!/usr/bin/env bash
printf 'apt-mark %s\n' "$*" >> "$PKG_LOG"
SH
# dpkg-query: installierte Version aus TS_DPKG_VERSION, leer = nicht installiert.
# Nach einer Installation durch den apt-get-Stub gilt die dort gemeldete Version.
cat > "$tmp/bin/dpkg-query" <<'SH'
#!/usr/bin/env bash
printf 'dpkg-query %s\n' "$*" >> "$PKG_LOG"
if [ -f "$TS_INSTALLED_FILE" ]; then cat "$TS_INSTALLED_FILE"; else printf '%s' "${TS_DPKG_VERSION:-}"; fi
exit 0
SH
# dpkg: nur --compare-versions A ge B. A ist gleich oder neuer, wenn die
# sortierte Liste (sort -V) A zuletzt nennt.
cat > "$tmp/bin/dpkg" <<'SH'
#!/usr/bin/env bash
[ "$1" = "--compare-versions" ] && [ "$3" = "ge" ] || exit 2
newest="$(printf '%s\n%s\n' "$2" "$4" | sort -V | tail -n 1)"
[ "$newest" = "$2" ]
SH
chmod +x "$tmp/bin"/*
export PATH="$tmp/bin:$PATH"
export TS_LOG="$tmp/ts.log" SYSTEMCTL_LOG="$tmp/systemctl.log" PKG_LOG="$tmp/pkg.log"
export ORDER_LOG="$tmp/order.log" TS_INSTALLED_FILE="$tmp/installed-version"

bundle="$tmp/bundle"
export EN_STATE_DIR="$tmp/state" EN_ROOT="$tmp/root" EN_BUNDLE_DIR="$bundle"
export EN_BUNDLE_VERSION=v1.0.0 EN_SUDO="" EN_SELECTION="$tmp/selection.json"
export EN_PACKAGES_DIR="$tmp/packages" EN_TAILSCALE_LOGIN_WAIT=3

defaults="$tmp/root/etc/default/tailscaled"
unit="$tmp/root/etc/systemd/system/tailscaled.service"
cli="$tmp/root/usr/sbin/tailscale"
daemon="$tmp/root/usr/sbin/tailscaled"
repo_list="$tmp/root/etc/apt/sources.list.d/tailscale.list"

arch=amd64

# setup <pin> <update: apt|installer> <deb: ja|nein> [dpkg-Version] [Version
# eines Tarball-Binarys ohne dpkg]. Baut Bundle, Manifest und Zielsystem neu.
setup() {
  local pin="$1" update="$2" deb="$3" dpkg_ver="${4:-}" bin_ver="${5:-}" sha
  DEB="tailscale_${pin}_${arch}.deb"
  rm -rf "$tmp/root" "$tmp/state" "$tmp/packages" "$bundle"
  mkdir -p "$tmp/root/etc" "$tmp/packages" "$bundle/bootstrap/keyrings"
  cp "$repo_keyrings/tailscale.gpg" "$repo_keyrings/caddy.gpg" "$bundle/bootstrap/keyrings/"
  printf 'PRETTY_NAME="Debian"\nVERSION_CODENAME=trixie\n' > "$tmp/root/etc/os-release"
  printf 'fake deb\n' > "$tmp/packages/$DEB"
  sha="$(sha256sum "$tmp/packages/$DEB" | cut -d' ' -f1)"
  [ "$deb" = ja ] || rm -f "$tmp/packages/$DEB"
  cat > "$bundle/manifest.json" <<JSON
{
  "version": "v1.0.0",
  "packages": {
    "tailscale": {
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
    mkdir -p "$tmp/root/usr/sbin" "$tmp/root/etc/systemd/system"
    printf '#!/bin/sh\necho "%s"\necho "  tailscale commit: abc"\n' "$bin_ver" > "$daemon"
    printf '#!/bin/sh\n# tarball-tailscale\n' > "$cli"
    printf '[Unit]\n# tarball-unit\n' > "$unit"
    chmod +x "$daemon" "$cli"
  fi
  export TS_DPKG_VERSION="$dpkg_ver"
  rm -f "$TS_INSTALLED_FILE"
  : > "$TS_LOG"; : > "$PKG_LOG"; : > "$SYSTEMCTL_LOG"; : > "$ORDER_LOG"
}

# run <TS_STATUS_RC>: fuehrt den Schritt aus, Ausgabe in $out, Code in $code.
run() {
  set +e
  out="$(TS_STATUS_RC="${1:-0}" bash "$script" 2>&1)"
  code=$?
  set -e
}

# --- abgewaehlt -----------------------------------------------------------
printf '{"steps":{"40":false}}\n' > "$EN_SELECTION"
out="$(bash "$script")"
grep -q '^##STEP 40 skip nicht ausgewaehlt$' <<<"$out" || fail "nicht abgewaehlt" "$out"
[ -e "$tmp/root" ] && fail "abgewaehlter Schritt hat Dateien angelegt"
rm -f "$EN_SELECTION"

# --- Zeile 1: nicht installiert, .deb da, amd64 (update=apt) ---------------
setup 1.104.5 apt ja
run 0
[ "$code" -eq 0 ] || fail "nicht installiert mit .deb: Code $code" "$out"
grep -q '^##STEP 40 ok$' <<<"$out" || fail "nicht installiert mit .deb: kein ok" "$out"
grep -q "apt-get install .*--force-confold .*$DEB" "$PKG_LOG" \
  || fail "Paket nicht per apt aus der Datei installiert" "$(cat "$PKG_LOG")"
grep -q 'pkgs.tailscale.com/stable/debian trixie main' "$repo_list" \
  || fail "apt-Quelle fuer Tailscale fehlt" "$(cat "$repo_list" 2>/dev/null || true)"
grep -qx 'apt-mark unhold tailscale' "$PKG_LOG" || fail "apt-Modus: kein unhold" "$(cat "$PKG_LOG")"
grep -q 'apt-mark hold' "$PKG_LOG" && fail "apt-Modus: hold gesetzt"
grep -q 'daemon-reload' "$SYSTEMCTL_LOG" || fail "kein daemon-reload nach der Installation"
grep -q 'tailscale update' "$TS_LOG" && fail "apt-Modus: tailscale update aufgerufen" "$(cat "$TS_LOG")"
[ -f "$tmp/state/steps/40" ] || fail "kein Stempel nach Installation"

# --- Zeile 1, armv6: nicht installiert, .deb da, update=installer ----------
arch=armhf
setup 1.62.0 installer ja
run 0
arch=amd64
[ "$code" -eq 0 ] || fail "armv6 nicht installiert: Code $code" "$out"
grep -q '^##STEP 40 ok$' <<<"$out" || fail "armv6 nicht installiert: kein ok" "$out"
grep -q 'apt-get install .*tailscale_1.62.0_armhf.deb' "$PKG_LOG" \
  || fail "armv6: .deb nicht installiert" "$(cat "$PKG_LOG")"
grep -q 'apt-mark hold' "$PKG_LOG" && fail "armv6: hold gesetzt" "$(cat "$PKG_LOG")"
[ -e "$repo_list" ] && fail "armv6: Repo angelegt"
[ "$(grep -c '^tailscale update --yes$' "$TS_LOG")" -eq 1 ] \
  || fail "armv6: tailscale update nicht genau einmal aufgerufen" "$(cat "$TS_LOG")"
e_line="$(grep -n -x 'systemctl enable --now tailscaled' "$ORDER_LOG" | head -n 1 | cut -d: -f1)"
t_line="$(grep -n -x 'tailscale update --yes' "$ORDER_LOG" | head -n 1 | cut -d: -f1)"
if [ -z "$e_line" ] || [ -z "$t_line" ] || [ "$e_line" -ge "$t_line" ]; then
  fail "armv6: tailscale update nicht nach enable" "$(cat "$ORDER_LOG")"
fi

# --- armv6 mit hold aus einem frueheren Lauf: unhold, install, hold --------
# Der apt-mark-Stub fuehrt keinen Zustand; die Reihenfolge der Aufrufe im
# Protokoll ist der Nachweis, dass der hold vor der Installation faellt.
arch=armhf
setup 1.62.0 installer ja
run 0
arch=amd64
[ "$code" -eq 0 ] || fail "armv6 mit hold: Code $code" "$out"
u_line="$(grep -n -x 'apt-mark unhold tailscale' "$PKG_LOG" | head -n 1 | cut -d: -f1)"
i_line="$(grep -n 'apt-get install .*tailscale_1.62.0_armhf.deb' "$PKG_LOG" | head -n 1 | cut -d: -f1)"
if [ -z "$u_line" ] || [ -z "$i_line" ]; then
  fail "armv6 mit hold: unhold/install fehlt" "$(cat "$PKG_LOG")"
fi
if [ "$u_line" -ge "$i_line" ]; then
  fail "armv6 mit hold: Reihenfolge falsch (unhold $u_line, install $i_line)" "$(cat "$PKG_LOG")"
fi
grep -q 'apt-mark hold' "$PKG_LOG" && fail "armv6 mit hold: wieder festgehalten" "$(cat "$PKG_LOG")"

# armv6, Update schlaegt fehl: der Schritt bleibt ok, 1.62.0 bleibt aktiv
arch=armhf
setup 1.62.0 installer ja
TS_UPDATE_RC=1 run 0
arch=amd64
[ "$code" -eq 0 ] || fail "armv6 Update fehlgeschlagen: Code $code" "$out"
grep -q '^##STEP 40 ok$' <<<"$out" || fail "armv6 Update fehlgeschlagen: kein ok" "$out"
grep -q 'tailscale update fehlgeschlagen. Fassung 1.62.0 bleibt aktiv.' <<<"$out" \
  || fail "armv6 Update fehlgeschlagen: Meldung fehlt" "$out"
[ -f "$tmp/state/steps/40" ] || fail "armv6 Update fehlgeschlagen: kein Stempel"

# --- Zeile 2: nicht installiert, .deb fehlt -------------------------------
setup 1.104.5 apt nein
run 0
[ "$code" -eq 1 ] || fail "fehlendes .deb: Code $code (erwartet 1)" "$out"
grep -q '^##STEP 40 fail TAILSCALE_PACKAGE_MISSING$' <<<"$out" || fail "falscher Code bei fehlendem .deb" "$out"
grep -q 'apt-get' "$PKG_LOG" && fail "fehlendes .deb: trotzdem apt aufgerufen"

# --- Zeile 3: installiert, nicht aelter, .deb da (apt): Repo, sonst unangetastet
setup 1.104.5 apt ja 1.104.5
run 0
grep -q '^##STEP 40 ok$' <<<"$out" || fail "gleiche Version: kein ok" "$out"
grep -q 'apt-get install' "$PKG_LOG" && fail "gleiche Version: Paket erneut installiert"
grep -qx 'apt-mark unhold tailscale' "$PKG_LOG" || fail "gleiche Version: Repo nicht eingerichtet" "$(cat "$PKG_LOG")"
[ -f "$repo_list" ] || fail "gleiche Version: apt-Quelle fehlt"

# Zeile 3, neuer und ohne .deb im Bundle: trotzdem unangetastet
setup 1.104.5 apt nein 1.110.0
run 0
grep -q '^##STEP 40 ok$' <<<"$out" || fail "neuere Version ohne .deb: kein ok" "$out"
grep -q 'apt-get install' "$PKG_LOG" && fail "neuere Version: Paket angefasst"
[ -f "$repo_list" ] || fail "neuere Version: Repo fehlt bei dpkg-Verwaltung"

# Zeile 3, armv6 (installer): nichts ausser dem Ok
arch=armhf
setup 1.62.0 installer ja 1.62.0
run 0
arch=amd64
grep -q '^##STEP 40 ok$' <<<"$out" || fail "armv6 gleiche Version: kein ok" "$out"
grep -q 'apt-get install' "$PKG_LOG" && fail "armv6 gleiche Version: Paket angefasst"
[ -e "$repo_list" ] && fail "armv6 gleiche Version: Repo angelegt"
grep -q 'tailscale update' "$TS_LOG" && fail "armv6 gleiche Version: tailscale update aufgerufen" "$(cat "$TS_LOG")"

# Zeile 4: vorhanden per Tarball (nicht dpkg), nicht aelter: nur Log
setup 1.104.5 apt ja "" 1.104.5
run 1
grep -q 'nicht per dpkg verwaltet' <<<"$out" || fail "Tarball ohne dpkg: kein Log" "$out"
grep -q '^##STEP 40 skip login ausstehend$' <<<"$out" || fail "Tarball ohne dpkg: Login-Ablauf fehlt" "$out"
grep -q 'apt-get' "$PKG_LOG" && fail "Tarball ohne dpkg: apt angefasst"
grep -q 'tarball-tailscale' "$cli" || fail "Tarball ohne dpkg: tailscale ersetzt"
grep -q 'tarball-unit' "$unit" || fail "Tarball ohne dpkg: Unit ersetzt"
[ -e "$repo_list" ] && fail "Tarball ohne dpkg: Repo angelegt"

# Zeile 5: aelter, .deb da: Installation (apt-Modus: Repo)
setup 1.104.5 apt ja 1.50.0
run 0
grep -q '^##STEP 40 ok$' <<<"$out" || fail "aelter mit .deb: kein ok" "$out"
grep -q 'apt-get install .*'"$DEB" "$PKG_LOG" || fail "aelter mit .deb: kein Paket" "$(cat "$PKG_LOG")"
grep -qx 'apt-mark unhold tailscale' "$PKG_LOG" || fail "aelter mit .deb: kein Repo" "$(cat "$PKG_LOG")"
grep -q 'tailscale update' "$TS_LOG" && fail "aelter mit .deb (apt): tailscale update aufgerufen"

# Zeile 6: aelter, .deb fehlt (apt): unangetastet, Log, ok
setup 1.104.5 apt nein 1.50.0
run 0
grep -q 'Update über apt' <<<"$out" || fail "aelter ohne .deb: kein Log 'Update über apt'" "$out"
grep -q '^##STEP 40 ok$' <<<"$out" || fail "aelter ohne .deb: kein ok" "$out"
grep -q 'apt-get' "$PKG_LOG" && fail "aelter ohne .deb: apt angefasst"

# Zeile 6, armv6 (installer): Log fuer den naechsten Installer-Lauf
arch=armhf
setup 1.62.0 installer nein 1.50.0
run 0
arch=amd64
grep -q 'Update beim nächsten Installer-Lauf' <<<"$out" \
  || fail "armv6 aelter ohne .deb: kein Installer-Log" "$out"
grep -q '^##STEP 40 ok$' <<<"$out" || fail "armv6 aelter ohne .deb: kein ok" "$out"

# --- Migration einer Tarball-Installation ---------------------------------
setup 1.104.5 apt ja "" 1.50.0
mkdir -p "$tmp/root/etc/default"
printf 'FLAGS="--verbose"\n' > "$defaults"
run 0
grep -q '^##STEP 40 ok$' <<<"$out" || fail "Migration: kein ok" "$out"
[ -e "$cli" ] && fail "Migration: alte tailscale-Datei nicht entfernt"
[ -e "$unit" ] && fail "Migration: alte Unit nicht entfernt"
grep -qx 'FLAGS="--verbose"' "$defaults" || fail "Migration: defaults veraendert" "$(cat "$defaults")"
grep -q -- '--force-confold' "$PKG_LOG" || fail "Migration: force-confold fehlt" "$(cat "$PKG_LOG")"
grep -q 'daemon-reload' "$SYSTEMCTL_LOG" || fail "Migration: kein daemon-reload"

# --- falsche Pruefsumme -> PACKAGE_FILE_INVALID, nichts installiert -------
setup 1.104.5 apt ja
printf 'manipulated\n' > "$tmp/packages/$DEB"
run 0
[ "$code" -eq 1 ] || fail "falsche Pruefsumme nicht abgelehnt" "$out"
grep -q '^##STEP 40 fail PACKAGE_FILE_INVALID$' <<<"$out" || fail "kein PACKAGE_FILE_INVALID-Marker" "$out"
grep -q 'apt-get' "$PKG_LOG" && fail "falsche Pruefsumme: trotzdem apt aufgerufen"

# --- der verbotene Schalter bricht ab, ohne etwas zu aendern --------------
setup 1.104.5 apt ja
mkdir -p "$tmp/root/etc/default"
printf 'FLAGS="--tun=userspace-networking"\n' > "$defaults"
run 0
[ "$code" -eq 1 ] || fail "verbotener Schalter nicht abgelehnt" "$out"
grep -q '^##STEP 40 fail TAILSCALE_FLAG_INVALID$' <<<"$out" || fail "falscher Code" "$out"
grep -qx 'FLAGS="--tun=userspace-networking"' "$defaults" \
  || fail "defaults trotz Abbruch veraendert" "$(cat "$defaults")"
grep -q 'apt-get' "$PKG_LOG" && fail "verbotener Schalter: trotzdem installiert"

# --- der Schalter nur als Kommentar ist kein Abbruchgrund ------------------
setup 1.104.5 apt ja
mkdir -p "$tmp/root/etc/default"
printf '# Extra flags.\n#FLAGS="--tun=userspace-networking"\nPORT="41641"\n' > "$defaults"
run 0
[ "$code" -eq 0 ] || fail "auskommentierter Schalter fuehrte zum Abbruch" "$out"
grep -q '^##STEP 40 ok$' <<<"$out" || fail "auskommentierter Schalter: kein ok" "$out"

# --- angemeldet: ok, Stempel, kein tailscale update mehr -------------------
setup 1.104.5 apt ja
run 0
grep -q '^##STEP 40 ok$' <<<"$out" || fail "angemeldet: kein ok-Marker" "$out"
[ -f "$tmp/state/steps/40" ] || fail "angemeldet: kein Stempel"
grep -q 'tailscale update' "$TS_LOG" && fail "tailscale update wird aufgerufen (apt-Modus)"

# --- zweiter Lauf ueberspringt --------------------------------------------
: > "$TS_LOG"
run 0
grep -q '^##STEP 40 skip bereits erledigt$' <<<"$out" || fail "nicht uebersprungen" "$out"
[ -s "$TS_LOG" ] && fail "zweiter Lauf hat tailscale aufgerufen"

# --- nicht angemeldet: installiert, aber kein Stempel ---------------------
setup 1.104.5 apt ja
run 1
grep -q '^##STEP 40 skip login ausstehend$' <<<"$out" || fail "kein skip" "$out"
grep -q 'https://login.tailscale.com/a/abc123' <<<"$out" || fail "Login-Adresse fehlt" "$out"
[ -e "$tmp/state/steps/40" ] && fail "unangemeldeter Node hat einen Stempel bekommen"

echo "OK: $(basename "$0")"
