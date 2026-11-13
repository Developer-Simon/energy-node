#!/usr/bin/env bash
#
# Baut ein Bundle fuer eine Node-Zielarchitektur (Komponente B der Spec).
#
# Alles, was Rechenzeit oder Netz braucht, passiert hier auf dem
# Bau-Rechner (E4): Go-Cross-Compile, Wheel-Beschaffung aus piwheels,
# Unit-Rendering, Hashes, Signatur. Der Node packt nur noch aus.
#
# Die Schalter --dashboard-binary und --skip-wheels
# schleusen fertige Artefakte ein, statt sie zu erzeugen. Sie existieren fuer
# den Test und den CI-Doppellauf, der weder Netz noch Go-Toolchain braucht.
set -euo pipefail

BUILD_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(git -C "${BUILD_DIR}" rev-parse --show-toplevel)"
# shellcheck source=scripts/build/lib/manifests.sh
source "${BUILD_DIR}/lib/manifests.sh"
# shellcheck source=scripts/build/lib/wheels.sh
source "${BUILD_DIR}/lib/wheels.sh"
# shellcheck source=scripts/build/lib/manifest.sh
source "${BUILD_DIR}/lib/manifest.sh"
# shellcheck source=scripts/build/lib/render.sh
source "${BUILD_DIR}/lib/render.sh"

ARCH=""
PYTHON_MINOR=""
ABI=""
BUNDLE_USER=""
BUNDLE_BASE=""
OUT_DIR="${REPO_ROOT}/dist"
SIGN_KEY=""
DASHBOARD_BINARY=""
SKIP_WHEELS=false
EMBED_WHEELS=false
DEV_VERSION=false
WHEEL_WORK=""
BINARY_NAME="energy-node-dashboard"

# Kern laeuft immer; optional ist waehlbar, Vorgabe an (E7).
CORE_STEPS=(10 20 30 50 60 65 67)
OPTIONAL_STEPS=(15 17 35 40 70)
# 15 (Systempakete aktualisieren) ist optional mit Vorgabe an: abwaehlen
# laesst nur die Pruefung uebrig (scripts/bootstrap/15-system-upgrade.sh).
# Opt-in: optionale Schritte mit Vorgabe aus. 35 gibt den Port des Shelly-
# Wake-Webhooks in der Firewall frei - das muss der Betreiber ausdruecklich
# waehlen (scripts/bootstrap/35-ufw-shelly-webhook.sh). 17 legt das Journal in
# den Arbeitsspeicher: spart Schreibzyklen auf der SD-Karte, kostet aber die
# Logs nach einem Neustart (scripts/bootstrap/17-journal-volatile.sh).
OPT_IN_STEPS=(17 35)
# requires: ein optionaler Schritt, der nur zusammen mit einem anderen Sinn
# ergibt. Ist der benoetigte Schritt abgewaehlt, gilt dieser als abgewaehlt
# (plan.sh, Schritt-Skript, Oberflaeche). 35 oeffnet den Port eines
# Listeners im Shelly-Dienst (83).
declare -A STEP_REQUIRES=([35]="83")
# dashboard_key je optionalem System-Schritt (nicht Dienst-Schritt), der
# einen eigenen Dashboard-Tab hat. 70 (Caddy/HTTPS) bleibt bewusst aussen
# vor - das Dashboard blendet dafuer nichts aus (E7).
declare -A OPTIONAL_STEP_DASHBOARD_KEYS=([40]="tailscale")

usage() {
  sed -n '2,12p' "${BASH_SOURCE[0]}"
  cat <<TXT

  --arch <armv6|arm64|amd64>   Pflicht.
  --python-minor <3.13>        Python-Minor des Zielsystems; Vorgabe je Architektur
                               (armv6: 3.11, arm64 und amd64: 3.13).
  --abi <cp313>                ABI-Tag; ohne Angabe aus --python-minor.
  --user <name>                Bundle fuer diesen Benutzer festlegen (ohne: allgemein).
  --base <pfad>                Zielbasis; ohne Angabe /home/<user> (nur mit --user).
  --out <pfad>                 Ausgabeverzeichnis (Vorgabe dist/).
  --sign-key <pfad>            ed25519-Schluessel; ohne bleibt die Signatur aus.
  --dashboard-binary <pfad>    Fertiges Binary statt Cross-Compile.
  --skip-wheels               Keine Wheels beschaffen (manifest.wheels bleibt leer).
  --embed-wheels              Fremd-Wheels zusaetzlich ins Archiv legen.
  --dev-version                Version um -dev.<commit> (und .dirty) ergaenzen.
TXT
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --arch) ARCH="${2:-}"; shift 2 ;;
    --python-minor) PYTHON_MINOR="${2:-}"; shift 2 ;;
    --abi) ABI="${2:-}"; shift 2 ;;
    --user) BUNDLE_USER="${2:-}"; shift 2 ;;
    --base) BUNDLE_BASE="${2:-}"; shift 2 ;;
    --out) OUT_DIR="${2:-}"; shift 2 ;;
    --sign-key) SIGN_KEY="${2:-}"; shift 2 ;;
    --dashboard-binary) DASHBOARD_BINARY="${2:-}"; shift 2 ;;
    --skip-wheels) SKIP_WHEELS=true; shift ;;
    --embed-wheels) EMBED_WHEELS=true; shift ;;
    --dev-version) DEV_VERSION=true; shift ;;
    --help|-h) usage; exit 0 ;;
    *) echo "Unbekannter Schalter: $1" >&2; usage >&2; exit 1 ;;
  esac
done

[[ -n "${ARCH}" ]] || { echo "--arch fehlt." >&2; exit 1; }
if [[ "${EMBED_WHEELS}" == true && "${SKIP_WHEELS}" == true ]]; then
  echo "--embed-wheels und --skip-wheels schliessen sich aus." >&2
  exit 1
fi
# Prueft die Architektur, bevor irgendetwas gebaut wird.
arch_platform_tags "${ARCH}" >/dev/null
# Vorgabe je Architektur: das Legacy-Image fuer Pi 1 (armv6) ist Debian 12 mit
# Python 3.11, die empfohlenen 64-Bit-Images (arm64, amd64) sind Debian 13 mit
# Python 3.13. Ein Bundle traegt Wheels fuer genau eine Python-Version.
if [[ -z "${PYTHON_MINOR}" ]]; then
  case "${ARCH}" in
    armv6) PYTHON_MINOR="3.11" ;;
    *) PYTHON_MINOR="3.13" ;;
  esac
fi
ABI="${ABI:-cp${PYTHON_MINOR//./}}"

# Ohne --user/--base bleibt das Bundle allgemein: Units, Helfer, Sudoers und
# die Konfigurationsvorlage behalten den Platzhalter "energynode", das
# Manifest traegt kein Ziel, und die Schritte rendern auf dem Node (siehe
# scripts/bootstrap/lib/render.sh). Mit --user und/oder --base wird das Bundle
# wie frueher fuer genau dieses Ziel festgelegt.
if [[ -n "${BUNDLE_USER}${BUNDLE_BASE}" ]]; then
  BUNDLE_USER="${BUNDLE_USER:-energynode}"
  BUNDLE_BASE="${BUNDLE_BASE:-/home/${BUNDLE_USER}}"
  target_is_valid "${BUNDLE_USER}" "${BUNDLE_BASE}" \
    || { echo "--user/--base ungueltig: ${BUNDLE_USER} ${BUNDLE_BASE}" >&2; exit 1; }
fi

# stage_template <src> <dst>: festgelegt = rendern, allgemein = unveraendert.
stage_template() {
  if [[ -n "${BUNDLE_USER}" ]]; then
    render_unit_as "$1" "$2" "${BUNDLE_USER}" "${BUNDLE_BASE}"
  else
    cp "$1" "$2"
  fi
}

# Die Paketversion (Archivname, manifest.json "version", was die Oberflaeche
# ueberall als "Paketversion" zeigt) geht vom Dashboard aus, nicht vom
# Installer - das Dashboard ist der Teil, den Betreiber als "die Version"
# wahrnehmen. bootstrap/ traegt trotzdem seine eigene Version weiter, als
# eigener Eintrag in components (siehe Manifest-Kopf unten).
VERSION="$(tr -d '[:space:]' < "${REPO_ROOT}/dashboard/VERSION")"
# --dev-version: ein Bau aus dem Arbeitsstand (Entwickler-CLI) bekommt eine
# eigene Version. Die Schritte mit Fingerabdruck laufen ohnehin, sobald sich
# ihre Eingaben aendern, 15, 35 und 65 haengen aber weiter an der
# Bundle-Version. Das Dashboard zeigt den Commit an, und fuer den
# Update-Check bleibt v0.7.9-dev.* kleiner als das Release v0.7.9.
if [[ "${DEV_VERSION}" == true ]]; then
  VERSION="${VERSION}-dev.$(git -C "${REPO_ROOT}" rev-parse --short HEAD)"
  git -C "${REPO_ROOT}" diff --quiet HEAD -- || VERSION="${VERSION}.dirty"
fi
BOOTSTRAP_VERSION="$(tr -d '[:space:]' < "${REPO_ROOT}/scripts/bootstrap/VERSION")"
STAGE="$(mktemp -d)"
trap 'rm -rf "${STAGE}" "${WHEEL_WORK}"' EXIT

echo "==> Bundle ${VERSION} fuer ${ARCH} (Python ${PYTHON_MINOR}, ${ABI})"

# --- bootstrap/ -----------------------------------------------------------
mkdir -p "${STAGE}/bootstrap"
cp -a "${REPO_ROOT}/scripts/bootstrap/." "${STAGE}/bootstrap/"
rm -f "${STAGE}/bootstrap/VERSION"

# --- dashboard/ -----------------------------------------------------------
mkdir -p "${STAGE}/dashboard"
if [[ -n "${DASHBOARD_BINARY}" ]]; then
  install -m 0755 "${DASHBOARD_BINARY}" "${STAGE}/dashboard/${BINARY_NAME}"
else
  dashboard_version="${VERSION}"
  echo "==> Cross-Compile ${BINARY_NAME} ${dashboard_version}"
  go_env=()
  while IFS= read -r line; do go_env+=("${line}"); done < <(arch_go_env "${ARCH}")
  ( cd "${REPO_ROOT}/dashboard" &&
    env "${go_env[@]}" GOOS=linux CGO_ENABLED=0 \
      go build -trimpath -ldflags "-X main.buildVersion=${dashboard_version}" \
      -o "${STAGE}/dashboard/${BINARY_NAME}" ./cmd/dashboard )
fi
for file in "${BINARY_NAME}.service" "${BINARY_NAME}-system-action" \
            "${BINARY_NAME}-system-action.sudoers" \
            energy-node-updater.service energy-node-updater.path; do
  stage_template "${REPO_ROOT}/dashboard/${file}" "${STAGE}/dashboard/${file}"
done
cp "${REPO_ROOT}/dashboard/Caddyfile" "${STAGE}/dashboard/Caddyfile"
install -m 0755 "${REPO_ROOT}/dashboard/energy-node-updater.sh" \
  "${STAGE}/dashboard/energy-node-updater"
install -m 0755 "${REPO_ROOT}/dashboard/energy-node-storage-helper" \
  "${STAGE}/dashboard/energy-node-storage-helper"
# Notfall-Hotspot und WLAN-Waechter (Schritt 67): der Waechter und seine
# Units liegen wie der Speicher-Helfer unveraendert im Bundle.
install -m 0755 "${REPO_ROOT}/dashboard/energy-node-network-watch" \
  "${STAGE}/dashboard/energy-node-network-watch"
cp "${REPO_ROOT}/dashboard/energy-node-network-watch.service" \
  "${REPO_ROOT}/dashboard/energy-node-network-watch.timer" "${STAGE}/dashboard/"
cp "${REPO_ROOT}/installer/internal/bundle/signing_key.pub.pem" \
  "${STAGE}/dashboard/signing_key.pub.pem"

# --- services/ und config/manifests/ --------------------------------------
load_service_table "${REPO_ROOT}/services"
mkdir -p "${STAGE}/config/manifests"

service_dirs=()
for entry in "${SERVICE_TABLE[@]}"; do
  unit="$(cut -d: -f1 <<<"${entry}")"
  dir="$(cut -d: -f3 <<<"${entry}")"
  service_dirs+=("${dir}")
  src="${REPO_ROOT}/services/${dir}"
  dst="${STAGE}/services/${dir}"
  mkdir -p "${dst}/devices"

  cp "${src}"/*.py "${dst}/"
  stage_template "${src}/${unit}" "${dst}/${unit}"

  # Jede .json im Dienstverzeichnis ist eine Geraetedatei - ausser den
  # beiden, die es nicht sind: manifest.json ist Installer-Wissen,
  # config.schema.json das Schema-Fragment der Vorarbeit.
  for json in "${src}"/*.json; do
    case "$(basename "${json}")" in
      manifest.json|config.schema.json) continue ;;
    esac
    cp "${json}" "${dst}/devices/"
  done

  service_id="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["service_id"])' \
    "${src}/manifest.json")"
  cp "${src}/manifest.json" "${STAGE}/config/manifests/${service_id}.json"
done

# --- config/ ---------------------------------------------------------------
cp "${REPO_ROOT}/dashboard/internal/appconfig/energy-node.config.json" "${STAGE}/config/config.json"

# --- wheels/ ---------------------------------------------------------------
# Fremd-Wheels laden wir in ein Arbeitsverzeichnis ausserhalb von STAGE. Ins
# Archiv kommen nur die eigenen Wheels; die Fremd-Wheels stehen als Festlegung
# (Adresse, SHA-256) in manifest.wheels. --embed-wheels legt sie zusaetzlich
# nach wheels/.
mkdir -p "${STAGE}/wheels"
WHEEL_WORK="$(mktemp -d)"
mkdir -p "${WHEEL_WORK}/download"
if [[ "${SKIP_WHEELS}" == true ]]; then
  echo "==> --skip-wheels: keine Wheels im Bundle"
  # Ein leeres Verzeichnis ueberlebt tar nicht und wuerde auf dem Node
  # fehlen; eine Merkdatei haelt es am Leben.
  printf 'ohne Wheels gebaut (--skip-wheels)\n' > "${STAGE}/wheels/LEER"
  echo '[]' > "${WHEEL_WORK}/pins.json"
else
  echo "==> Wheels aus piwheels fuer ${ARCH}/${ABI}"
  fetch_thirdparty_wheels "${WHEEL_WORK}/download" "${ARCH}" "${PYTHON_MINOR}" "${ABI}"
  python3 "${BUILD_DIR}/lib/wheel_pins.py" "${WHEEL_WORK}/download" "${WHEEL_INDEXES[@]}" \
    > "${WHEEL_WORK}/pins.json"
  if [[ "${EMBED_WHEELS}" == true ]]; then
    cp "${WHEEL_WORK}/download/"*.whl "${STAGE}/wheels/"
  fi
  build_local_wheels "${STAGE}/wheels"
fi

# --- Manifest-Kopf ---------------------------------------------------------
# Tailscale und Caddy reisen nicht mehr im Bundle mit: manifest.packages
# traegt nur ihre Festlegungen (Version, Datei, Adresse, SHA-256) aus
# scripts/build/packages.json fuer diese Architektur.
mkdir -p "${OUT_DIR}"
ARCH="${ARCH}" VERSION="${VERSION}" BOOTSTRAP_VERSION="${BOOTSTRAP_VERSION}" PYTHON_MINOR="${PYTHON_MINOR}" ABI="${ABI}" \
PACKAGES_JSON="${BUILD_DIR}/packages.json" WHEEL_PINS_JSON="${WHEEL_WORK}/pins.json" \
BUNDLE_USER="${BUNDLE_USER}" BUNDLE_BASE="${BUNDLE_BASE}" REPO_ROOT="${REPO_ROOT}" \
UNAME_MACHINES="$(arch_uname_machines "${ARCH}" | paste -sd, -)" \
CORE_STEPS="${CORE_STEPS[*]}" OPTIONAL_STEPS="${OPTIONAL_STEPS[*]}" OPT_IN_STEPS="${OPT_IN_STEPS[*]}" \
SERVICE_ROWS="$(printf '%s\n' "${SERVICE_TABLE[@]}")" \
SERVICE_STEP_ROWS="$(printf '%s\n' "${SERVICE_STEPS[@]}")" \
STEP_REQUIRES_ROWS="$(for id in "${!STEP_REQUIRES[@]}"; do printf '%s:%s\n' "$id" "${STEP_REQUIRES[$id]}"; done)" \
OPTIONAL_STEP_DASHBOARD_KEY_ROWS="$(for id in "${!OPTIONAL_STEP_DASHBOARD_KEYS[@]}"; do printf '%s:%s\n' "$id" "${OPTIONAL_STEP_DASHBOARD_KEYS[$id]}"; done)" \
python3 > "${STAGE}/manifest.head.json" <<'PY'
import datetime, json, os, pathlib

repo = pathlib.Path(os.environ["REPO_ROOT"])

def version_of(rel):
    path = repo / rel
    return path.read_text(encoding="utf-8").strip() if path.is_file() else None

optional_dashboard_keys = {}
for row in os.environ["OPTIONAL_STEP_DASHBOARD_KEY_ROWS"].splitlines():
    if not row.strip():
        continue
    step_id, key = row.split(":", 1)
    optional_dashboard_keys[step_id] = key

steps = []
for step_id in os.environ["CORE_STEPS"].split():
    steps.append({"id": step_id, "optional": False})
opt_in = set(os.environ["OPT_IN_STEPS"].split())
requires = dict(row.split(":", 1) for row in os.environ["STEP_REQUIRES_ROWS"].splitlines() if row.strip())
for step_id in os.environ["OPTIONAL_STEPS"].split():
    entry = {"id": step_id, "optional": True, "default": step_id not in opt_in}
    if step_id in requires:
        entry["requires"] = requires[step_id]
    if step_id in optional_dashboard_keys:
        entry["dashboard_key"] = optional_dashboard_keys[step_id]
    steps.append(entry)

units = {}
for row in os.environ["SERVICE_ROWS"].splitlines():
    if not row.strip():
        continue
    unit, _path, directory, _deploy = row.split(":")
    units[directory] = unit
for row in os.environ["SERVICE_STEP_ROWS"].splitlines():
    if not row.strip():
        continue
    directory, step_id = row.split(":")
    manifest = json.loads((repo / "services" / directory / "manifest.json").read_text(encoding="utf-8"))
    entry = {
        "id": step_id,
        "optional": True,
        "default": True,
        "service_id": manifest["service_id"],
        "kind": manifest["kind"],
        "dir": directory,
        "unit": units[directory],
        "dashboard_key": manifest["service_id"],
    }
    # Jeder Dienst traegt seit v0.4.0 seine eigene Version im Manifest. Sie
    # kommt hier mit ins Bundle-Manifest, damit Installer und Dashboard die
    # Version eines einzelnen Dienstes zeigen koennen, ohne das mitgelieferte
    # config/manifests/<id>.json nachzuladen.
    if manifest.get("version"):
        entry["version"] = "v%s" % manifest["version"].lstrip("v")
    steps.append(entry)
steps.sort(key=lambda item: item["id"])

head = {
    "version": os.environ["VERSION"],
    "built_at": datetime.datetime.now().astimezone().isoformat(timespec="seconds"),
    "arch": os.environ["ARCH"],
    "uname_machine": os.environ["UNAME_MACHINES"].split(","),
    "python_minor": os.environ["PYTHON_MINOR"],
    "python_abi": os.environ["ABI"],
    "components": {
        "bootstrap": os.environ["BOOTSTRAP_VERSION"],
        "dashboard": version_of("dashboard/VERSION"),
        "energy_node_common": version_of("libs/energy_node_common/VERSION"),
        "battery_soc_core": version_of("libs/battery_soc_core/VERSION"),
    },
    "steps": steps,
}
if os.environ.get("BUNDLE_USER"):
    head["target_user"] = os.environ["BUNDLE_USER"]
    head["target_base"] = os.environ["BUNDLE_BASE"]
with open(os.environ["PACKAGES_JSON"], encoding="utf-8") as handle:
    head["packages"] = json.load(handle)[os.environ["ARCH"]]
with open(os.environ["WHEEL_PINS_JSON"], encoding="utf-8") as handle:
    head["wheels"] = json.load(handle)
print(json.dumps(head, indent=2, ensure_ascii=False))
PY

# --- changelog ----------------------------------------------------------------
# changelog/<version>.json je Bundle-Version (die letzten 20), dazu
# changelog.json mit nur der neuesten fuer aeltere Installer und Updater. Vor
# write_bundle_manifest, damit jede Datei ihren SHA-256 in manifest.files
# bekommt und von verify_bundle.sh gedeckt ist.
python3 "${BUILD_DIR}/make_changelog_json.py" --repo "${REPO_ROOT}" \
  --out "${STAGE}/changelog.json" --out-dir "${STAGE}/changelog" --bundle-version "${VERSION}"

write_bundle_manifest "${STAGE}"
if [[ -n "${SIGN_KEY}" ]]; then
  sign_bundle_manifest "${STAGE}" "${SIGN_KEY}"
  echo "==> Manifest signiert"
else
  echo "==> Kein --sign-key: das Bundle reist unsigniert"
fi

archive="${OUT_DIR}/energy-node-${VERSION}-${ARCH}.tar.gz"
tar -czf "${archive}" -C "${STAGE}" .
echo "==> ${archive}"
echo "    Dienste: ${service_dirs[*]}"
