#!/usr/bin/env bash
# Test for scripts/build/update_packages.sh (Caddy- und Tailscale-.deb je Architektur).
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
script="$here/../build/update_packages.sh"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
fail() { echo "FAIL: $1"; [ -n "${2:-}" ] && printf '%s\n' "$2"; exit 1; }

# 64 Hex-Zeichen aus einer Ziffer, z.B. h 1
h() { local s; s="$(printf '%064d' 0)"; echo "${s//0/$1}"; }

# --- Fixtures ----------------------------------------------------------------
fx="$tmp/fx"
mkdir -p "$fx" "$tmp/bin"
printf '{"tag_name": "v9.9.9"}\n' > "$fx/api.json"
for a in arm64 amd64; do
  if [ "$a" = arm64 ]; then sha="$(h 1)"; else sha="$(h 2)"; fi
  cat > "$fx/ts-$a" <<EOF
Package: other
Version: 0.0.1
Filename: pool/other_0.0.1_$a.deb
SHA256: $(h 9)

Package: tailscale
Version: 1.0.0
Filename: pool/tailscale_1.0.0_$a.deb
SHA256: $(h 8)

Package: tailscale
Version: 9.9.9
Architecture: $a
Filename: pool/tailscale_9.9.9_$a.deb
SHA256: $sha
EOF
done

# Fake-.debs; checksums.txt traegt wie die echte Caddy-Datei SHA-512-Zeilen
for a in amd64 arm64 armv6; do printf 'fake-deb-%s\n' "$a" > "$fx/caddy_9.9.9_linux_$a.deb"; done
{
  for a in amd64 arm64 armv6; do
    printf '%s  caddy_9.9.9_linux_%s.deb\n' "$(sha512sum "$fx/caddy_9.9.9_linux_$a.deb" | cut -c1-128)" "$a"
  done
  printf '%s  caddy_9.9.9_linux_amd64.tar.gz\n' "$(h 6)"
} > "$fx/checksums.txt"
grep -v 'armv6' "$fx/checksums.txt" > "$fx/checksums-missing.txt"
bad0="$(printf '0%.0s' $(seq 128))"
sed "/arm64\.deb/s/^[0-9a-f]*/$bad0/" "$fx/checksums.txt" > "$fx/checksums-badsum.txt"

# Erwartete SHA-256 der geladenen Dateien
sha256_amd64="$(sha256sum "$fx/caddy_9.9.9_linux_amd64.deb" | cut -c1-64)"
sha256_arm64="$(sha256sum "$fx/caddy_9.9.9_linux_arm64.deb" | cut -c1-64)"
sha256_armv6="$(sha256sum "$fx/caddy_9.9.9_linux_armv6.deb" | cut -c1-64)"

# curl-Stub: liefert je Adresse eine Fixture-Datei, sonst Fehler
cat > "$tmp/bin/curl" <<'SH'
#!/usr/bin/env bash
url="${@: -1}"
printf '%s\n' "$url" >> "$CURL_LOG"
case "$url" in
  https://api.github.com/repos/caddyserver/caddy/releases/latest) f=api.json ;;
  https://github.com/caddyserver/caddy/releases/download/v9.9.9/caddy_9.9.9_checksums.txt) f="${FX_CHECKSUMS:-checksums.txt}" ;;
  https://github.com/caddyserver/caddy/releases/download/v9.9.9/caddy_9.9.9_linux_*.deb) f="${url##*/}" ;;
  https://pkgs.tailscale.com/stable/debian/dists/trixie/main/binary-arm64/Packages) f=ts-arm64 ;;
  https://pkgs.tailscale.com/stable/debian/dists/trixie/main/binary-amd64/Packages) f=ts-amd64 ;;
  *) echo "unbekannte URL: $url" >&2; exit 22 ;;
esac
cat "$FX/$f"
SH
chmod +x "$tmp/bin/curl"

pj="$tmp/packages.json"
export FX="$fx" CURL_LOG="$tmp/curl.log"
run() { PATH="$tmp/bin:$PATH" EN_PACKAGES_JSON="$pj" bash "$script" "$@"; }

seed() { printf '{"arm64": {"caddy": {"version": "0.0.1"}}}\n' > "$pj"; }

# --- Ohne --write: Datei bleibt, Ausgabe nennt die Aenderungen ---------------
seed
cp "$pj" "$tmp/seed.json"
out="$(run 2>&1)" || fail "Lauf ohne --write schlug fehl" "$out"
cmp -s "$pj" "$tmp/seed.json" || fail "Datei ohne --write veraendert"
grep -q '9\.9\.9' <<<"$out" || fail "Ausgabe nennt die neue Version nicht" "$out"

# --- Mit --write: neue Werte je Architektur ----------------------------------
seed
out="$(run --write 2>&1)" || fail "--write schlug fehl" "$out"
python3 -I - "$pj" "$sha256_amd64" "$sha256_arm64" "$sha256_armv6" <<'PY' || fail "packages.json hat falsche Werte"
import json, sys
d = json.load(open(sys.argv[1]))
sha_amd64, sha_arm64, sha_armv6 = sys.argv[2:5]
def check(cond, msg):
    if not cond:
        print("falsch:", msg, file=sys.stderr)
        sys.exit(1)
ts_base = "https://pkgs.tailscale.com/stable/debian/"
gh_base = "https://github.com/caddyserver/caddy/releases/download/v9.9.9/"
# armv6-Tailscale bleibt fest auf 1.62.0, die url zeigt auf den Projekt-Spiegel
check(d["armv6"]["tailscale"] == {
    "version": "1.62.0",
    "file": "tailscale_1.62.0_armhf.deb",
    "url": "https://github.com/Developer-Simon/energy-node/releases/download/pinned-packages/tailscale_1.62.0_armhf.deb",
    "sha256": "b8db12b68f17f707adb4ffe035abb3c6a7347b5bea93894f4c787d3bb1d95125",
    "update": "installer",
}, "armv6 tailscale")
check(d["armv6"]["caddy"] == {
    "version": "9.9.9",
    "file": "caddy_9.9.9_linux_armv6.deb",
    "url": gh_base + "caddy_9.9.9_linux_armv6.deb",
    "sha256": sha_armv6,
    "update": "installer",
}, "armv6 caddy")
for arch, sha_ts, sha_caddy in (("arm64", "1" * 64, sha_arm64), ("amd64", "2" * 64, sha_amd64)):
    t = d[arch]["tailscale"]
    check(t["version"] == "9.9.9", arch + " tailscale version")
    check(t["file"] == f"tailscale_9.9.9_{arch}.deb", arch + " tailscale file")
    check(t["url"] == ts_base + f"pool/tailscale_9.9.9_{arch}.deb", arch + " tailscale url")
    check(t["sha256"] == sha_ts, arch + " tailscale sha")
    check(t["update"] == "apt", arch + " tailscale update")
    c = d[arch]["caddy"]
    check(c["version"] == "9.9.9", arch + " caddy version")
    check(c["file"] == f"caddy_9.9.9_linux_{arch}.deb", arch + " caddy file")
    check(c["url"] == gh_base + f"caddy_9.9.9_linux_{arch}.deb", arch + " caddy url")
    check(c["sha256"] == sha_caddy, arch + " caddy sha256 (der geladenen Datei)")
    check(c["update"] == "apt", arch + " caddy update")
PY

# --- Fehlende Zeile in checksums.txt: Exit 1, Datei unveraendert -------------
seed
cp "$pj" "$tmp/seed.json"
set +e
out="$(FX_CHECKSUMS=checksums-missing.txt run --write 2>&1)"
rc=$?
set -e
[ "$rc" -ne 0 ] || fail "fehlende Pruefsumme nicht bemaengelt" "$out"
grep -q 'caddy_9.9.9_linux_armv6.deb' <<<"$out" || fail "Meldung nennt die Datei nicht" "$out"
cmp -s "$pj" "$tmp/seed.json" || fail "Datei bei Fehler veraendert"

# --- SHA-512-Abweichung der geladenen .deb: Exit 1, Datei unveraendert -------
seed
cp "$pj" "$tmp/seed.json"
set +e
out="$(FX_CHECKSUMS=checksums-badsum.txt run --write 2>&1)"
rc=$?
set -e
[ "$rc" -ne 0 ] || fail "falsche SHA-512 nicht bemaengelt" "$out"
grep -q 'SHA-512' <<<"$out" || fail "Meldung nennt SHA-512 nicht" "$out"
cmp -s "$pj" "$tmp/seed.json" || fail "Datei bei SHA-512-Fehler veraendert"

echo "OK: $(basename "$0")"
