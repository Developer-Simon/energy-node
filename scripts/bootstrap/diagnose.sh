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
# Vor step.sh merken: step.sh setzt EN_TARGET_BASE ohne Vorgabe auf $HOME,
# die Diagnose nimmt dann lieber target_base aus dem installierten Manifest.
DIAG_TARGET_BASE="${EN_TARGET_BASE:-}"
# shellcheck source=scripts/bootstrap/lib/step.sh
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib/step.sh"
# shellcheck source=scripts/bootstrap/lib/shelly_webhook.sh
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib/shelly_webhook.sh"

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
  local units=("${FIXED_UNITS[@]}") unit state
  local service_units=()
  if [[ -f "${EN_BUNDLE_DIR}/manifest.json" ]]; then
    while IFS= read -r unit; do
      [[ -n "${unit}" ]] && units+=("${unit}") && service_units+=("${unit}")
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
  # Eine Dienst-Unit, die nicht laeuft und deren Unit-Datei fehlt (die legt
  # service_step.sh an), ist nicht installiert - der Dienst wurde nie
  # gewaehlt. Das ist kein Fehler, sondern "not-installed".
  for unit in "${units[@]}"; do
    state="$(systemctl is-active "${unit}" 2>/dev/null || true)"
    if [[ "${state}" != active && " ${service_units[*]} " == *" ${unit} "* \
          && ! -f "${EN_ROOT}/etc/systemd/system/${unit}" ]]; then
      state=not-installed
    fi
    printf 'unit%s%s%s%s\n' "$tab" "${unit}" "$tab" "${state}"
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

  # Shelly-Wake-Webhook: nur, wenn der Betreiber die Freigabe gewaehlt hat
  # (Schritt 35) und der Shelly-Dienst installiert ist. Firewall-Regel und
  # Listener getrennt: der Dienst lauscht erst, wenn webhook_enabled im
  # Dashboard an ist - eine offene Regel ohne Listener ist der Normalfall
  # direkt nach der Installation, kein Fehler.
  if shelly_webhook_wanted 2>/dev/null; then
    local wport rules="" allowed=false listens=false
    wport="$(shelly_webhook_port)"
    rules="$("${SUDO[@]}" ufw status 2>/dev/null || true)"
    grep -qE "^${wport}/tcp[[:space:]]+ALLOW" <<<"${rules}" && allowed=true
    grep -qE "[:.]${wport}[[:space:]]" <<<"${listening}" && listens=true
    printf 'webhook%sport%s%s\n' "$tab" "$tab" "${wport}"
    printf 'webhook%sfirewall%s%s\n' "$tab" "$tab" "${allowed}"
    printf 'webhook%slistening%s%s\n' "$tab" "$tab" "${listens}"
  fi

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

  # Installierte Versionen (aus installed-manifest.json, das der Installer
  # nach jedem vollstaendigen Lauf ablegt) und die konfigurierten Geraete je
  # Dienst. Welche *_devices.json zu welchem Dienst gehoert, steht im Bundle
  # unter services/<dir>/devices/. Beides ist reine Information, keine
  # Pruefung. JSON in der dritten Spalte enthaelt nie einen rohen Tab.
  python3 - "${EN_STATE_DIR}/installed-manifest.json" "${EN_BUNDLE_DIR}" \
    "${DIAG_TARGET_BASE}" "${EN_ROOT}" "${HOME:-}" <<'PY'
import json, pathlib, sys

installed_path, bundle_dir, explicit_base, root, home = sys.argv[1:6]

def load(path):
    try:
        return json.loads(pathlib.Path(path).read_text(encoding="utf-8"))
    except (OSError, ValueError):
        return None

installed = load(installed_path)
bundle = load(pathlib.Path(bundle_dir) / "manifest.json") or {}
if isinstance(installed, dict):
    for name, version in sorted((installed.get("components") or {}).items()):
        print("version\tcomponent:%s\t%s" % (name, version))
    for step in installed.get("steps") or []:
        if step.get("unit") and step.get("version"):
            print("version\tservice:%s\t%s" % (step["unit"], step["version"]))

base = explicit_base or (installed or {}).get("target_base") or bundle.get("target_base") or home
devices_dir = pathlib.Path(root + base) / "devices"
for step in bundle.get("steps") or []:
    unit, directory = step.get("unit"), step.get("dir")
    if not unit or not directory:
        continue
    source = pathlib.Path(bundle_dir) / "services" / directory / "devices"
    names = sorted(p.name for p in source.glob("*_devices.json")) if source.is_dir() else []
    items, readable, found = [], True, False
    for name in names:
        path = devices_dir / name
        if not path.is_file():
            continue
        found = True
        data = load(path)
        if not isinstance(data, list):
            readable = False
            continue
        for entry in data:
            if isinstance(entry, dict) and entry.get("id"):
                items.append({"id": str(entry["id"]), "name": str(entry.get("name") or entry["id"])})
    if found:
        print("devices\t%s\t%s" % (unit, json.dumps(items if readable else None, ensure_ascii=False)))
PY

  # tailscale liegt in /usr/sbin, das im PATH einer nicht-interaktiven
  # SSH-Sitzung fehlt (Debian: /usr/local/bin:/usr/bin:/bin:/usr/games). Ein
  # "command -v" fand es dort nie und meldete einen angemeldeten Node als
  # ausgeloggt. Ueber sudo greift dessen secure_path, wie in Schritt 40.
  if "${SUDO[@]}" tailscale status >/dev/null 2>&1; then
    printf 'tailscale%sangemeldet%strue\n' "$tab" "$tab"
  else
    printf 'tailscale%sangemeldet%sfalse\n' "$tab" "$tab"
  fi

  # Debians Markierung (Schritt 15 legt sie nach Kernel-Updates an). Sie
  # liegt in /run und verschwindet beim Neustart von selbst.
  if [[ -f "${EN_ROOT}/run/reboot-required" ]]; then
    printf 'reboot%srequired%strue\n' "$tab" "$tab"
  else
    printf 'reboot%srequired%sfalse\n' "$tab" "$tab"
  fi

  # Ausstehende Systempakete, letzter Stand der Paketlisten (ohne apt-get
  # update). Eine Zeile JSON oder null.
  printf 'apt%spending%s%s\n' "$tab" "$tab" \
    "$(EN_ROOT="${EN_ROOT}" python3 "$(dirname "${BASH_SOURCE[0]}")/lib/apt_pending.py")"
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
    # installierte Versionen: components wie im Manifest, services je Unit.
    "versions": {"components": {}, "services": {}},
    # Geraete je Unit: Liste aus id/name, null = Geraetedatei nicht lesbar.
    "devices": {},
    "reboot_required": False,
    "system_updates": None,
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
    elif kind == "version":
        group, _, name = key.partition(":")
        report["versions"]["components" if group == "component" else "services"][name] = value
    elif kind == "devices":
        report["devices"][key] = json.loads(value)
    elif kind == "apt":
        report["system_updates"] = json.loads(value)
    elif kind == "reboot":
        report["reboot_required"] = value == "true"
    elif kind == "webhook":
        # Nur vorhanden, wenn die Freigabe gewaehlt ist (Schritt 35).
        hook = report.setdefault("shelly_webhook", {})
        hook[key] = value if key == "port" else value == "true"

report["config"]["manifests"].sort()
print(json.dumps(report, indent=2, ensure_ascii=False))
PY

collect | python3 "$tmp_py"
exit 0
