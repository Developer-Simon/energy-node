#!/usr/bin/env bash
#
# Pinnt die Caddy- und Tailscale-.deb-Pakete je Zielarchitektur in packages.json.
#
#   scripts/build/update_packages.sh          zeigt die Aenderungen, schreibt nichts
#   scripts/build/update_packages.sh --write  schreibt packages.json
#
# Quellen: neueste Caddy-Stable (GitHub-API releases/latest mit
# caddy_<v>_checksums.txt) und neueste Tailscale-Stable aus dem Debian-Index
# (Felder Version, Filename, SHA256). armv6 bleibt bei Tailscale fest auf 1.62.0,
# weil es fuer armhf keinen neueren Stand gibt, den wir pinnen.
#
# Caddy: jede .deb wird geladen und gegen ihre SHA-512-Zeile in checksums.txt
# geprueft. In packages.json steht die SHA-256 der geladenen Datei (Feld sha256).
#
# Schreibt nur bei Erfolg: fehlt eine Pruefsumme oder stimmt sie nicht, bricht
# das Skript mit Exit 1 ab und die Datei bleibt unveraendert.

set -euo pipefail

# Projekt-Spiegel fuer das armv6-Tailscale-.deb (Release-Tag pinned-packages).
# Der Hersteller-Pool loescht alte Versionen, daher zeigt die url in
# packages.json auf den Spiegel. Die Pruefsumme bleibt die des Herstellers:
# sie stammt aus dem bookworm-armhf-Index (Stand 2026-11-07). Das Skript
# aktualisiert armv6-Tailscale nicht; die Festlegung 1.62.0 bleibt bestehen.
TS_ARMV6_MIRROR_BASE="https://github.com/Developer-Simon/energy-node/releases/download/pinned-packages/"

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
out="${EN_PACKAGES_JSON:-$here/packages.json}"
write=0
case "${1:-}" in
  --write) write=1 ;;
  "") ;;
  *) echo "Unbekannte Option: $1 (erlaubt: --write)" >&2; exit 2 ;;
esac

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

api="https://api.github.com/repos/caddyserver/caddy/releases/latest"

tag="$(curl -fsSL "$api" | python3 -I -c 'import json, sys; print(json.load(sys.stdin)["tag_name"])')"
cver="${tag#v}"
curl -fsSL "https://github.com/caddyserver/caddy/releases/download/$tag/caddy_${cver}_checksums.txt" \
  > "$tmp/checksums.txt"
for a in arm64 amd64; do
  curl -fsSL "https://pkgs.tailscale.com/stable/debian/dists/trixie/main/binary-$a/Packages" \
    > "$tmp/ts-$a"
done
for a in arm64 amd64 armv6; do
  curl -fsSL "https://github.com/caddyserver/caddy/releases/download/$tag/caddy_${cver}_linux_$a.deb" \
    > "$tmp/caddy-$a.deb"
done

python3 -I - "$tmp" "$out" "$write" "$cver" "$TS_ARMV6_MIRROR_BASE" <<'PY'
import hashlib, json, os, re, sys

tmp, out, write, cver = sys.argv[1], sys.argv[2], sys.argv[3] == "1", sys.argv[4]
ts_armv6_mirror = sys.argv[5]

TS_BASE = "https://pkgs.tailscale.com/stable/debian/"
GH_BASE = f"https://github.com/caddyserver/caddy/releases/download/v{cver}/"

def fail(msg):
    print(msg, file=sys.stderr)
    sys.exit(1)

def version_key(version):
    return tuple(int(n) for n in re.findall(r"\d+", version))

def tailscale_entry(path):
    # Der Index fuehrt je Paket alle Versionen: die hoechste Stable gewinnt.
    found = []
    for block in open(path).read().split("\n\n"):
        fields = {}
        for line in block.splitlines():
            if ": " in line and not line.startswith(" "):
                key, value = line.split(": ", 1)
                fields[key] = value
        if fields.get("Package") == "tailscale":
            found.append(fields)
    if not found:
        fail(f"kein Eintrag 'tailscale' in {path}")
    best = max(found, key=lambda f: version_key(f["Version"]))
    return {
        "version": best["Version"],
        "file": os.path.basename(best["Filename"]),
        "url": TS_BASE + best["Filename"],
        "sha256": best["SHA256"],
        "update": "apt",
    }

sums = {}
for line in open(os.path.join(tmp, "checksums.txt")):
    parts = line.split()
    if len(parts) == 2:
        sums[parts[1].lstrip("*")] = parts[0]

def caddy_entry(arch, update):
    name = f"caddy_{cver}_linux_{arch}.deb"
    if name not in sums:
        fail(f"Prüfsumme für {name} fehlt in checksums.txt")
    with open(os.path.join(tmp, f"caddy-{arch}.deb"), "rb") as f:
        data = f.read()
    # checksums.txt von Caddy ist SHA-512; geprueft wird die geladene Datei.
    if hashlib.sha512(data).hexdigest() != sums[name]:
        fail(f"SHA-512 von {name} stimmt nicht mit checksums.txt überein")
    return {
        "version": cver,
        "file": name,
        "url": GH_BASE + name,
        "sha256": hashlib.sha256(data).hexdigest(),
        "update": update,
    }

new = {
    "armv6": {
        "tailscale": {
            "version": "1.62.0",
            "file": "tailscale_1.62.0_armhf.deb",
            "url": ts_armv6_mirror + "tailscale_1.62.0_armhf.deb",
            "sha256": "b8db12b68f17f707adb4ffe035abb3c6a7347b5bea93894f4c787d3bb1d95125",
            "update": "installer",
        },
        "caddy": caddy_entry("armv6", "installer"),
    },
}
for arch in ("arm64", "amd64"):
    new[arch] = {
        "tailscale": tailscale_entry(os.path.join(tmp, f"ts-{arch}")),
        "caddy": caddy_entry(arch, "apt"),
    }

old = {}
if os.path.exists(out):
    old = json.load(open(out))

changes = []
for arch, comps in new.items():
    for comp, entry in comps.items():
        before = old.get(arch, {}).get(comp, {})
        if before == entry:
            continue
        if before.get("version") != entry["version"]:
            changes.append(f"{arch} {comp}: {before.get('version', '-')} -> {entry['version']}")
        else:
            changes.append(f"{arch} {comp}: Datei oder Prueffsumme geaendert ({entry['file']})")

if not changes:
    print("Keine Aenderungen.")
for line in changes:
    print(line)

if write:
    tmp_out = out + ".tmp"
    with open(tmp_out, "w") as f:
        json.dump(new, f, indent=2)
        f.write("\n")
    os.replace(tmp_out, out)
    print(f"Geschrieben: {out}")
else:
    print("Ohne --write nichts geschrieben.")
PY
