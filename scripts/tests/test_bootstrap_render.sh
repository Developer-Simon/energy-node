#!/usr/bin/env bash
# Test for scripts/bootstrap/lib/render.sh
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=scripts/bootstrap/lib/render.sh
source "$here/../bootstrap/lib/render.sh"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
fail() { echo "FAIL: $1"; [ -n "${2:-}" ] && printf '%s\n' "$2"; exit 1; }

cat > "$tmp/in" <<'EOF'
User=energynode
WorkingDirectory=/home/energynode/dashboard
username = energynode_client
energynode ALL=(root) NOPASSWD: /usr/local/sbin/x restart
EOF

render_unit_as "$tmp/in" "$tmp/out" orgelbau /opt/en || fail "gueltiges Ziel abgelehnt"
grep -qx 'User=orgelbau' "$tmp/out" || fail "Benutzer nicht ersetzt" "$(cat "$tmp/out")"
grep -qx 'WorkingDirectory=/opt/en/dashboard' "$tmp/out" || fail "Basis nicht ersetzt" "$(cat "$tmp/out")"
grep -qx 'username = energynode_client' "$tmp/out" || fail "energynode_client darf nicht angefasst werden"
grep -qx 'orgelbau ALL=(root) NOPASSWD: /usr/local/sbin/x restart' "$tmp/out" || fail "Sudoers nicht ersetzt"

# Ein zweites Rendern einer schon gerenderten Datei aendert nichts.
render_unit_as "$tmp/out" "$tmp/out2" orgelbau /opt/en
cmp -s "$tmp/out" "$tmp/out2" || fail "Rendern ist nicht idempotent"

# Ungueltige Ziele werden abgelehnt, und es entsteht keine Ausgabedatei.
for bad_user in "x y" 'a;b' Root '' '$(id)' 'a b' "$(printf 'a%.0s' {1..40})"; do
  rm -f "$tmp/bad"
  if render_unit_as "$tmp/in" "$tmp/bad" "$bad_user" /opt/en; then
    fail "ungueltiger Benutzer akzeptiert: '$bad_user'"
  fi
  [ ! -e "$tmp/bad" ] || fail "trotz ungueltigem Benutzer geschrieben: '$bad_user'"
done
for bad_base in relative / /a/../b /.. '/a b' '/a#b' '/a&b' '' '/a;b'; do
  rm -f "$tmp/bad"
  if render_unit_as "$tmp/in" "$tmp/bad" orgelbau "$bad_base"; then
    fail "ungueltige Basis akzeptiert: '$bad_base'"
  fi
  [ ! -e "$tmp/bad" ] || fail "trotz ungueltiger Basis geschrieben: '$bad_base'"
done

target_is_valid orgelbau /home/orgelbau || fail "orgelbau abgelehnt"
target_is_valid energynode /home/energynode || fail "energynode abgelehnt"
target_is_valid node-1 /opt/energy-node/base_2 || fail "Bindestrich/Unterstrich abgelehnt"

echo "OK"
