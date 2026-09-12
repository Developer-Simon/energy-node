#!/usr/bin/env bash
#
# Zustandsbericht des Node als JSON (INSTALLATION.md 9 und 10).
#
# Endet IMMER mit Exit 0, auch wenn nichts laeuft: ein Bericht, der bei
# schlechtem Zustand abbricht, berichtet nichts. Kein ##STEP-Marker - dies
# ist kein Schritt.
#
# Bash sammelt Zeilen der Form "art<TAB>schluessel<TAB>wert", Python baut
# daraus das JSON. So steckt das Escaping an genau einer Stelle.
set -uo pipefail
# shellcheck source=scripts/bootstrap/lib/step.sh
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib/step.sh"

PORTS=(1883 8080 443)
FIXED_UNITS=(
  mosquitto.service
  energy-node-dashboard.service
  tailscaled.service
  caddy.service
)

collect() {
  local tab=$'\t'

  printf 'bundle%sversion%s%s\n' "$tab" "$tab" "${EN_BUNDLE_VERSION}"

  # Stempel: Schritt-ID -> Bundle-Version, aus der er stammt.
  local stamp id
  for stamp in "${EN_STATE_DIR}"/steps/*; do
    [[ -f "${stamp}" ]] || continue
    id="$(basename "${stamp}")"
    printf 'step%s%s%s%s\n' "$tab" "${id}" "$tab" \
      "$(sed -n 's/^bundle=//p' "${stamp}" | head -n 1)"
  done

  # Units: die festen plus jede unit aus der Schrittliste des Manifests.
  local units=("${FIXED_UNITS[@]}") unit
  if [[ -f "${EN_BUNDLE_DIR}/manifest.json" ]]; then
    while IFS= read -r unit; do
      [[ -n "${unit}" ]] && units+=("${unit}")
    done < <(python3 -c '
import json, sys
try:
    data = json.load(open(sys.argv[1], encoding="utf-8"))
except Exception:
    sys.exit(0)
for entry in data.get("steps", []):
    if entry.get("unit"):
        print(entry["unit"])
' "${EN_BUNDLE_DIR}/manifest.json")
  fi
  for unit in "${units[@]}"; do
    printf 'unit%s%s%s%s\n' "$tab" "${unit}" "$tab" \
      "$(systemctl is-active "${unit}" 2>/dev/null || true)"
  done

  # Lauschende Ports. ss fehlt auf manchen Minimal-Images; dann gilt nichts
  # als offen, statt dass der Bericht ausfaellt.
  local listening="" port
  if command -v ss >/dev/null 2>&1; then
    listening="$(ss -ltn 2>/dev/null || true)"
  fi
  for port in "${PORTS[@]}"; do
    if grep -qE "[:.]${port}[[:space:]]" <<<"${listening}"; then
      printf 'port%s%s%strue\n' "$tab" "${port}" "$tab"
    else
      printf 'port%s%s%sfalse\n' "$tab" "${port}" "$tab"
    fi
  done

  local etc="${EN_ROOT}/etc/energy-node"
  if [[ -f "${etc}/config.json" ]]; then
    printf 'config%sconfig.json%strue\n' "$tab" "$tab"
  else
    printf 'config%sconfig.json%sfalse\n' "$tab" "$tab"
  fi
  local manifest
  for manifest in "${etc}"/manifests/*.json; do
    [[ -f "${manifest}" ]] || continue
    printf 'manifest%s%s%s\n' "$tab" "$(basename "${manifest}" .json)" "$tab"
  done

  if command -v tailscale >/dev/null 2>&1 && tailscale status >/dev/null 2>&1; then
    printf 'tailscale%sangemeldet%strue\n' "$tab" "$tab"
  else
    printf 'tailscale%sangemeldet%sfalse\n' "$tab" "$tab"
  fi
}

tmp_py="$(mktemp)"
trap 'rm -f "$tmp_py"' EXIT

cat > "$tmp_py" <<'PY'
import json, sys

report = {
    "bundle_version": "",
    "steps": {},
    "units": {},
    "ports": {},
    "config": {"config.json": False, "manifests": []},
    "tailscale": {"angemeldet": False},
}

for line in sys.stdin:
    parts = line.rstrip("\n").split("\t")
    kind, key = parts[0], parts[1]
    value = parts[2] if len(parts) > 2 else ""
    if kind == "bundle":
        report["bundle_version"] = value
    elif kind == "step":
        report["steps"][key] = value
    elif kind == "unit":
        report["units"][key] = value or "unbekannt"
    elif kind == "port":
        report["ports"][key] = value == "true"
    elif kind == "config":
        report["config"][key] = value == "true"
    elif kind == "manifest":
        report["config"]["manifests"].append(key)
    elif kind == "tailscale":
        report["tailscale"][key] = value == "true"

report["config"]["manifests"].sort()
print(json.dumps(report, indent=2, ensure_ascii=False))
PY

collect | python3 "$tmp_py"
exit 0
