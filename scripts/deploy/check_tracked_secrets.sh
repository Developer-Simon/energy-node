#!/usr/bin/env bash
#
# Prüft alle von Git getrackten Dateien auf Klartext-Zugangsdaten.
#
# Hintergrund: MQTT-Zugangsdaten gehören in die nicht versionierte
# /etc/energy-node/mqtt.pw auf dem Zielgerät, nicht in die *.env-Dateien
# im Repo (siehe docs/knowledge/konfiguration.md). Auch die zentrale
# Konfigurationsvorlage services/energy-node.config.json darf nie Zugangs­daten
# enthalten - diese gehören ausschliesslich in die per password_file /
# admin_password_file referenzierten Dateien auf dem Zielgeraet.
#
# Geprüft werden nur *.env- und *.conf-Dateien - das sind die einzigen
# Dateitypen, aus denen zur Laufzeit tatsächlich Zugangsdaten geladen
# werden. Python-Quellcode referenziert Schlüsselnamen wie MQTT_USER nur
# per os.environ.get(...), das ist kein Klartext-Secret und würde bei einer
# Prüfung aller getrackten Dateien fälschlich anschlagen.
#
# Exit 0 = sauber, Exit 1 = mindestens ein Fund.
#
# dashboard/internal/mqttbridge/testdata/golden.conf ist eine Testvorlage
# mit bewusst frei erfundenem Zugangsdatum für einen Golden-File-Test.

set -euo pipefail

REPO_ROOT="$(git -C "$(dirname "${BASH_SOURCE[0]}")" rev-parse --show-toplevel)"
cd "${REPO_ROOT}"

# Schlüssel, die niemals mit einem nicht-leeren Wert versioniert sein dürfen.
KEY_PATTERN='^[[:space:]]*(MQTT_PASSWORD|MQTT_USER|MQTT_USERNAME|remote_password|remote_username)[[:space:]=]'

EXCLUDE_FILES=(
  "scripts/deploy/check_tracked_secrets.sh"
  "dashboard/internal/mqttbridge/testdata/golden.conf"
)

is_excluded() {
  local candidate="$1"
  local excluded
  for excluded in "${EXCLUDE_FILES[@]}"; do
    [[ "${candidate}" == "${excluded}" ]] && return 0
  done
  return 1
}

findings=0

while IFS= read -r file; do
  is_excluded "${file}" && continue
  [[ -f "${file}" ]] || continue

  while IFS=: read -r lineno line; do
    # Kommentarzeilen sind Dokumentation, kein Secret.
    [[ "${line}" =~ ^[[:space:]]*# ]] && continue

    # Wert ist alles nach dem ersten '=' bzw. dem ersten Leerraum
    # (mosquitto-Syntax "remote_password wert").
    if [[ "${line}" == *=* ]]; then
      value="${line#*=}"
    else
      value="${line#* }"
    fi
    # Umgebende Anführungszeichen und Leerraum abstreifen.
    value="$(printf '%s' "${value}" | sed -E 's/^[[:space:]]*//; s/[[:space:]]*$//; s/^"(.*)"$/\1/; s/^'"'"'(.*)'"'"'$/\1/')"

    # Leer = Platzhalter, das ist der Sollzustand.
    [[ -z "${value}" ]] && continue
    # Spitze Klammern kennzeichnen einen dokumentierten Platzhalter.
    [[ "${value}" =~ ^\<.*\>$ ]] && continue

    key="$(printf '%s' "${line}" | sed -E 's/^[[:space:]]*([A-Za-z_]+).*/\1/')"
    printf '%s:%s:%s\n' "${file}" "${lineno}" "${key}"
    findings=$((findings + 1))
  done < <(grep -nE "${KEY_PATTERN}" "${file}" || true)
done < <(git ls-files -- '*.env' '*.conf' 'services/energy-node.config.json')

# Zusaetzliche Regel fuer die Konfigurationsvorlage: Zugangsdaten gehoeren
# nie in die Datei selbst, sondern ausschliesslich in die per
# password_file / admin_password_file referenzierten Dateien auf dem
# Zielgeraet (siehe docs/knowledge/konfiguration.md).
CONFIG_TEMPLATE="services/energy-node.config.json"
if [[ -f "${CONFIG_TEMPLATE}" ]]; then
  template_findings="$(python3 - "${CONFIG_TEMPLATE}" <<'PY'
import json
import sys

path = sys.argv[1]
allowed = {"password_file", "admin_password_file"}
findings = []


def walk(value, prefix):
    if isinstance(value, dict):
        for key, item in value.items():
            here = f"{prefix}.{key}" if prefix else key
            if "password" in key and key not in allowed and item not in ("", None):
                findings.append(here)
            walk(item, here)
    elif isinstance(value, list):
        for index, item in enumerate(value):
            walk(item, f"{prefix}[{index}]")


try:
    walk(json.loads(open(path, encoding="utf-8").read()), "")
except json.JSONDecodeError as exc:
    print(f"{path}: ungueltiges JSON: {exc}")
    sys.exit(0)

for finding in findings:
    print(f"{path}: {finding}")
PY
)"
  if [[ -n "${template_findings}" ]]; then
    printf '%s\n' "${template_findings}"
    findings=$((findings + $(printf '%s\n' "${template_findings}" | wc -l)))
  fi
fi

if [[ "${findings}" -gt 0 ]]; then
  printf '\n%s Klartext-Zugangsdaten in versionierten Dateien gefunden.\n' "${findings}" >&2
  printf 'Siehe docs/knowledge/dashboard/secrets-und-zugangsdaten.md\n' >&2
  exit 1
fi

echo "OK: keine Klartext-Zugangsdaten in versionierten Dateien."
