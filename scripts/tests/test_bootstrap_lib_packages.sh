#!/usr/bin/env bash
# shellcheck disable=SC1090  # Testrahmen sourct absichtlich einen dynamischen Pfad
# Test for scripts/bootstrap/lib/packages.sh
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
lib="$here/../bootstrap/lib/packages.sh"
repo_keyrings="$here/../bootstrap/keyrings"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
fail() { echo "FAIL: $1"; [ -n "${2:-}" ] && printf '%s\n' "$2"; exit 1; }

# Stubs fuer die privilegierten bzw. systemnahen Befehle. Jeder Aufruf landet
# in einem Protokoll, damit der Test die Argumente pruefen kann.
mkdir -p "$tmp/bin"
cat > "$tmp/bin/dpkg-query" <<'SH'
#!/usr/bin/env bash
printf 'dpkg-query %s\n' "$*" >> "$PKG_LOG"
[ -n "${PKG_INSTALLED:-}" ] && printf '%s' "${PKG_INSTALLED}"
exit 0
SH
cat > "$tmp/bin/apt-get" <<'SH'
#!/usr/bin/env bash
printf 'apt-get %s DEBIAN_FRONTEND=%s\n' "$*" "${DEBIAN_FRONTEND:-}" >> "$PKG_LOG"
SH
cat > "$tmp/bin/apt-mark" <<'SH'
#!/usr/bin/env bash
printf 'apt-mark %s\n' "$*" >> "$PKG_LOG"
SH
chmod +x "$tmp/bin/dpkg-query" "$tmp/bin/apt-get" "$tmp/bin/apt-mark"
export PATH="$tmp/bin:$PATH"
export PKG_LOG="$tmp/pkg.log"

export EN_STATE_DIR="$tmp/state" EN_ROOT="$tmp/root" EN_SUDO=""
export EN_BUNDLE_DIR="$tmp/bundle" EN_BUNDLE_VERSION=v1.0.0
export EN_PACKAGES_DIR="$tmp/state/packages"
export EN_SELECTION="$tmp/selection.json"
mkdir -p "$EN_BUNDLE_DIR" "$EN_PACKAGES_DIR" "$tmp/root/etc"

# Ein Fake-.deb mit bekannter Pruefsumme im Manifest.
deb_name="caddy_2.11.9_linux_amd64.deb"
printf 'fake deb payload\n' > "$EN_PACKAGES_DIR/$deb_name"
good_sha="$(sha256sum "$EN_PACKAGES_DIR/$deb_name" | cut -d' ' -f1)"
cat > "$EN_BUNDLE_DIR/manifest.json" <<JSON
{
  "version": "v1.0.0",
  "packages": {
    "caddy": {
      "version": "2.11.9",
      "file": "$deb_name",
      "url": "https://example.invalid/$deb_name",
      "sha256": "$good_sha",
      "update": "apt"
    }
  }
}
JSON

run() { bash -c 'set -euo pipefail; source "$1"; shift; "$@"' _ "$lib" "$@"; }

# --- pkg_pin liest das Manifest, leer ohne Eintrag ------------------------
[ "$(run pkg_pin caddy version)" = "2.11.9" ] || fail "pkg_pin version"
[ "$(run pkg_pin caddy sha256)" = "$good_sha" ] || fail "pkg_pin sha256"
[ -z "$(run pkg_pin tailscale version)" ] || fail "pkg_pin ohne Eintrag ist nicht leer"
[ -z "$(run pkg_pin caddy nichtda)" ] || fail "pkg_pin unbekanntes Feld ist nicht leer"

# --- pkg_file: passende Datei gibt den Pfad aus ----------------------------
out="$(run pkg_file caddy)" || fail "pkg_file bei passender Datei fehlgeschlagen"
[ "$out" = "$EN_PACKAGES_DIR/$deb_name" ] || fail "pkg_file gibt falschen Pfad aus" "$out"

# --- pkg_file: fehlende Datei -> Rueckgabe 1, kein Fehlermarker -----------
rc=0
out="$(run pkg_file tailscale)" || rc=$?
[ "$rc" -eq 1 ] || fail "pkg_file ohne Eintrag muss 1 liefern (war $rc)"
rm -f "$EN_PACKAGES_DIR/$deb_name"
rc=0
out="$(run pkg_file caddy)" || rc=$?
[ "$rc" -eq 1 ] || fail "pkg_file bei fehlender Datei muss 1 liefern (war $rc)"
[ -z "$out" ] || fail "pkg_file bei fehlender Datei gibt Text aus" "$out"

# --- pkg_file: falsche Pruefsumme -> PACKAGE_FILE_INVALID ------------------
printf 'manipulated\n' > "$EN_PACKAGES_DIR/$deb_name"
rc=0
out="$(STEP_ID=70 bash -c 'set -euo pipefail; source "$1"; pkg_file caddy' _ "$lib" 2>&1)" || rc=$?
[ "$rc" -ne 0 ] || fail "pkg_file akzeptiert falsche Pruefsumme"
grep -q '^##STEP 70 fail PACKAGE_FILE_INVALID$' <<<"$out" \
  || fail "kein PACKAGE_FILE_INVALID-Marker" "$out"
printf 'fake deb payload\n' > "$EN_PACKAGES_DIR/$deb_name"

# --- pkg_installed_version -------------------------------------------------
: > "$PKG_LOG"
[ "$(PKG_INSTALLED=2.11.9 run pkg_installed_version caddy)" = "2.11.9" ] \
  || fail "pkg_installed_version liefert keine Version"
grep -qx "dpkg-query -W -f=\${Version} caddy" "$PKG_LOG" \
  || fail "dpkg-query nicht mit erwarteten Argumenten aufgerufen" "$(cat "$PKG_LOG")"
[ -z "$(PKG_INSTALLED="" run pkg_installed_version caddy)" ] || fail "pkg_installed_version nicht leer"

# --- pkg_install_deb: install ohne update, mit force-confold -----------------
: > "$PKG_LOG"
run pkg_install_deb "$EN_PACKAGES_DIR/$deb_name" || fail "pkg_install_deb fehlgeschlagen"
grep -qx "apt-get install -y --no-install-recommends -o Dpkg::Options::=--force-confold $EN_PACKAGES_DIR/$deb_name DEBIAN_FRONTEND=noninteractive" "$PKG_LOG" \
  || fail "apt-get install falsch aufgerufen" "$(cat "$PKG_LOG")"
grep -q 'apt-get update' "$PKG_LOG" && fail "pkg_install_deb ruft apt-get update auf"

# --- pkg_setup_repo: Keyring und .list, trixie und bookworm ----------------
mkdir -p "$EN_BUNDLE_DIR/bootstrap/keyrings"
cp "$repo_keyrings/tailscale.gpg" "$repo_keyrings/caddy.gpg" "$EN_BUNDLE_DIR/bootstrap/keyrings/"

for codename in trixie bookworm; do
  printf 'PRETTY_NAME="Debian"\nVERSION_CODENAME=%s\n' "$codename" > "$tmp/root/etc/os-release"
  rm -rf "$tmp/root/usr" "$tmp/root/etc/apt"
  : > "$PKG_LOG"

  run pkg_setup_repo tailscale || fail "pkg_setup_repo tailscale ($codename)"
  cmp -s "$repo_keyrings/tailscale.gpg" "$tmp/root/usr/share/keyrings/tailscale-archive-keyring.gpg" \
    || fail "tailscale-Keyring nicht kopiert"
  want="deb [signed-by=/usr/share/keyrings/tailscale-archive-keyring.gpg] https://pkgs.tailscale.com/stable/debian $codename main"
  [ "$(cat "$tmp/root/etc/apt/sources.list.d/tailscale.list")" = "$want" ] \
    || fail "tailscale.list falsch ($codename)" "$(cat "$tmp/root/etc/apt/sources.list.d/tailscale.list")"

  run pkg_setup_repo caddy || fail "pkg_setup_repo caddy ($codename)"
  cmp -s "$repo_keyrings/caddy.gpg" "$tmp/root/usr/share/keyrings/caddy-archive-keyring.gpg" \
    || fail "caddy-Keyring nicht kopiert"
  want="deb [signed-by=/usr/share/keyrings/caddy-archive-keyring.gpg] https://dl.cloudsmith.io/public/caddy/stable/deb/debian any-version main"
  [ "$(cat "$tmp/root/etc/apt/sources.list.d/caddy.list")" = "$want" ] \
    || fail "caddy.list falsch ($codename)" "$(cat "$tmp/root/etc/apt/sources.list.d/caddy.list")"

  grep -qx 'apt-mark unhold tailscale' "$PKG_LOG" || fail "hold fuer tailscale nicht freigegeben ($codename)" "$(cat "$PKG_LOG")"
  grep -qx 'apt-mark unhold caddy' "$PKG_LOG" || fail "hold fuer caddy nicht freigegeben ($codename)" "$(cat "$PKG_LOG")"
done

# --- pkg_hold ---------------------------------------------------------------
: > "$PKG_LOG"
run pkg_hold caddy || fail "pkg_hold fehlgeschlagen"
grep -qx 'apt-mark hold caddy' "$PKG_LOG" || fail "apt-mark hold nicht aufgerufen" "$(cat "$PKG_LOG")"

echo "OK: test_bootstrap_lib_packages"
