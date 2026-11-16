#!/usr/bin/env bash
# Laufzeit-Test des Hotspot-Portals mit echtem Caddy: Clients aus dem
# Hotspot-Netz bekommen die Startseite auf jedem Pfad, andere Absender nicht.
# Ohne caddy oder curl wird der Test uebersprungen.
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_dash="$here/../../dashboard"

fail() { echo "FAIL: $1"; [ -n "${2:-}" ] && printf '%s\n' "$2"; exit 1; }

if ! command -v caddy >/dev/null 2>&1; then
  echo "SKIP: $(basename "$0") (caddy fehlt)"
  exit 0
fi
if ! command -v curl >/dev/null 2>&1; then
  echo "SKIP: $(basename "$0") (curl fehlt)"
  exit 0
fi

tmp="$(mktemp -d)"
pid=""
stop() {
  if [ -n "$pid" ]; then
    kill "$pid" 2>/dev/null || true
    wait "$pid" 2>/dev/null || true
    pid=""
  fi
}
trap 'stop; rm -rf "${tmp:?}"' EXIT

port=$((20000 + RANDOM % 20000))
mkdir -p "$tmp/hotspot"
cp "$repo_dash/hotspot/index.html" "$tmp/hotspot/index.html"

# start <matcher>: baut eine Testkopie des :80-Blocks (ohne :443, ohne
# Admin-Port), setzt den Matcher und die Seite auf die Testpfade und startet
# Caddy auf $port. Wartet, bis der Port antwortet.
start() {
  awk '/^:443 \{/{exit} {print}' "$repo_dash/Caddyfile" \
    | sed -e 's/^    auto_https disable_redirects$/&\n    admin off/' \
          -e "s/^:80 {/:$port {/" \
          -e "s#remote_ip 10.42.0.0/24#remote_ip $1#" \
          -e "s#root \* /usr/share/energy-node/hotspot#root * $tmp/hotspot#" \
    > "$tmp/Caddyfile"
  caddy run --config "$tmp/Caddyfile" --adapter caddyfile >"$tmp/caddy.log" 2>&1 &
  pid=$!
  for _ in $(seq 1 50); do
    if curl -s -o /dev/null "http://127.0.0.1:$port/"; then
      return 0
    fi
    sleep 0.2
  done
  fail "Caddy antwortet nicht auf Port $port" "$(cat "$tmp/caddy.log")"
}

# Matcher auf 127.0.0.0/8 umgestellt: der Testclient ist Hotspot-Client.
start 127.0.0.0/8
for p in / /hotspot-detect.html /generate_204 /a/b/c; do
  code="$(curl -s -o "$tmp/body" -w '%{http_code}' "http://127.0.0.1:$port$p")"
  [ "$code" = 200 ] || fail "portal $p: $code" "$(cat "$tmp/caddy.log")"
  grep -q 'https://10.42.0.1/' "$tmp/body" || fail "portal $p: link missing"
done
stop

# Unveraenderter Matcher 10.42.0.0/24: der Testclient (127.0.0.1) ist kein
# Hotspot-Client, also darf / nicht die Startseite liefern.
start 10.42.0.0/24
curl -s -o "$tmp/body" "http://127.0.0.1:$port/" || true
grep -q 'https://10.42.0.1/' "$tmp/body" && fail "Startseite ohne Hotspot-Matcher"
stop

echo "OK: $(basename "$0")"
