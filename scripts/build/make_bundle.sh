#!/usr/bin/env bash
#
# Baut ein Bundle fuer eine Node-Zielarchitektur (Komponente B der Spec).
#
# Alles, was Rechenzeit oder Netz braucht, passiert hier auf dem
# Bau-Rechner (E4): Go-Cross-Compile, Wheel-Beschaffung aus piwheels,
# Unit-Rendering, Hashes, Signatur. Der Node packt nur noch aus.
#
# Die Schalter --dashboard-binary, --tailscale-tarball und --skip-wheels
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
PYTHON_MINOR="3.11"
ABI=""
BUNDLE_USER="energynode"
BUNDLE_BASE=""
OUT_DIR="${REPO_ROOT}/dist"
SIGN_KEY=""
DASHBOARD_BINARY=""
TAILSCALE_TARBALL=""
CADDY_BINARY=""
SKIP_WHEELS=false
TAILSCALE_VERSION="1.62.0"
BINARY_NAME="energy-node-dashboard"

# Kern laeuft immer; optional ist waehlbar, Vorgabe an (E7).
CORE_STEPS=(10 20 30 50 60)
OPTIONAL_STEPS=(40 70)

usage() {
  sed -n '2,12p' "${BASH_SOURCE[0]}"
  cat <<TXT

  --arch <armv6|arm64|amd64>   Pflicht.
  --python-minor <3.11>        Python-Minor des Zielsystems.
  --abi <cp311>                ABI-Tag; ohne Angabe aus --python-minor.
  --user <name>                Zielbenutzer fuer das Unit-Rendering.
  --base <pfad>                Zielbasis; ohne Angabe /home/<user>.
  --out <pfad>                 Ausgabeverzeichnis (Vorgabe dist/).
  --sign-key <pfad>            ed25519-Schluessel; ohne bleibt die Signatur aus.
  --dashboard-binary <pfad>    Fertiges Binary statt Cross-Compile.
  --tailscale-tarball <pfad>   Fertiger Tarball statt Download.
  --caddy-binary <pfad>        Erzeugt das getrennte Caddy-Beipack.
  --skip-wheels                Keine Wheels beschaffen.
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
    --tailscale-tarball) TAILSCALE_TARBALL="${2:-}"; shift 2 ;;
    --caddy-binary) CADDY_BINARY="${2:-}"; shift 2 ;;
    --skip-wheels) SKIP_WHEELS=true; shift ;;
    --help|-h) usage; exit 0 ;;
    *) echo "Unbekannter Schalter: $1" >&2; usage >&2; exit 1 ;;
  esac
done

[[ -n "${ARCH}" ]] || { echo "--arch fehlt." >&2; exit 1; }
# Prueft die Architektur, bevor irgendetwas gebaut wird.
arch_platform_tags "${ARCH}" >/dev/null
ABI="${ABI:-cp${PYTHON_MINOR//./}}"
BUNDLE_BASE="${BUNDLE_BASE:-/home/${BUNDLE_USER}}"

VERSION="$(tr -d '[:space:]' < "${REPO_ROOT}/scripts/bootstrap/VERSION")"
STAGE="$(mktemp -d)"
trap 'rm -rf "${STAGE}"' EXIT

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
  dashboard_version="$(tr -d '[:space:]' < "${REPO_ROOT}/dashboard/VERSION")"
  echo "==> Cross-Compile ${BINARY_NAME} ${dashboard_version}"
  go_env=()
  while IFS= read -r line; do go_env+=("${line}"); done < <(arch_go_env "${ARCH}")
  ( cd "${REPO_ROOT}/dashboard" &&
    env "${go_env[@]}" GOOS=linux CGO_ENABLED=0 \
      go build -trimpath -ldflags "-X main.buildVersion=${dashboard_version}" \
      -o "${STAGE}/dashboard/${BINARY_NAME}" ./cmd/dashboard )
fi
for file in "${BINARY_NAME}.service" "${BINARY_NAME}-system-action" \
            "${BINARY_NAME}-system-action.sudoers"; do
  render_unit_as "${REPO_ROOT}/dashboard/${file}" "${STAGE}/dashboard/${file}" \
    "${BUNDLE_USER}" "${BUNDLE_BASE}"
done
cp "${REPO_ROOT}/dashboard/Caddyfile" "${STAGE}/dashboard/Caddyfile"

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
  render_unit_as "${src}/${unit}" "${dst}/${unit}" "${BUNDLE_USER}" "${BUNDLE_BASE}"

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
cp "${REPO_ROOT}/services/energy-node.config.json" "${STAGE}/config/config.json"
cp "${REPO_ROOT}/services/VERSION" "${STAGE}/config/services-VERSION"

# --- wheels/ ---------------------------------------------------------------
mkdir -p "${STAGE}/wheels"
if [[ "${SKIP_WHEELS}" == true ]]; then
  echo "==> --skip-wheels: keine Wheels im Bundle"
  # Ein leeres Verzeichnis ueberlebt tar nicht und wuerde auf dem Node
  # fehlen; eine Merkdatei haelt es am Leben.
  printf 'ohne Wheels gebaut (--skip-wheels)\n' > "${STAGE}/wheels/LEER"
else
  echo "==> Wheels aus piwheels fuer ${ARCH}/${ABI}"
  fetch_thirdparty_wheels "${STAGE}/wheels" "${ARCH}" "${PYTHON_MINOR}" "${ABI}"
  build_local_wheels "${STAGE}/wheels"
fi

# --- tailscale/ ------------------------------------------------------------
mkdir -p "${STAGE}/tailscale"
if [[ -n "${TAILSCALE_TARBALL}" ]]; then
  cp "${TAILSCALE_TARBALL}" "${STAGE}/tailscale/$(basename "${TAILSCALE_TARBALL}")"
else
  ts_arch="arm"
  [[ "${ARCH}" == arm64 ]] && ts_arch="arm64"
  [[ "${ARCH}" == amd64 ]] && ts_arch="amd64"
  ts_name="tailscale_${TAILSCALE_VERSION}_${ts_arch}.tgz"
  cache="${OUT_DIR}/cache"
  mkdir -p "${cache}"
  if [[ ! -f "${cache}/${ts_name}" ]]; then
    echo "==> Lade ${ts_name}"
    curl -fsSL "https://pkgs.tailscale.com/stable/${ts_name}" -o "${cache}/${ts_name}"
  fi
  cp "${cache}/${ts_name}" "${STAGE}/tailscale/${ts_name}"
fi

# --- Caddy-Beipack, getrennt vom Bundle (E13) -----------------------------
mkdir -p "${OUT_DIR}"
caddy_version=""
caddy_file=""
caddy_sha=""
if [[ -n "${CADDY_BINARY}" ]]; then
  caddy_version="$("${CADDY_BINARY}" version 2>/dev/null | head -n 1 | cut -d' ' -f1 || true)"
  caddy_version="${caddy_version:-unbekannt}"
  caddy_file="caddy-${caddy_version}-${ARCH}.tar.gz"
  pack="$(mktemp -d)"
  install -m 0755 "${CADDY_BINARY}" "${pack}/caddy"
  tar -czf "${OUT_DIR}/${caddy_file}" -C "${pack}" caddy
  rm -rf "${pack}"
  caddy_sha="$(sha256sum "${OUT_DIR}/${caddy_file}" | cut -d' ' -f1)"
  echo "==> Caddy-Beipack: ${caddy_file}"
fi

# --- Manifest-Kopf ---------------------------------------------------------
ARCH="${ARCH}" VERSION="${VERSION}" PYTHON_MINOR="${PYTHON_MINOR}" ABI="${ABI}" \
BUNDLE_USER="${BUNDLE_USER}" BUNDLE_BASE="${BUNDLE_BASE}" REPO_ROOT="${REPO_ROOT}" \
UNAME_MACHINES="$(arch_uname_machines "${ARCH}" | paste -sd, -)" \
CORE_STEPS="${CORE_STEPS[*]}" OPTIONAL_STEPS="${OPTIONAL_STEPS[*]}" \
SERVICE_ROWS="$(printf '%s\n' "${SERVICE_TABLE[@]}")" \
SERVICE_STEP_ROWS="$(printf '%s\n' "${SERVICE_STEPS[@]}")" \
CADDY_VERSION="${caddy_version}" CADDY_FILE="${caddy_file}" CADDY_SHA="${caddy_sha}" \
python3 > "${STAGE}/manifest.head.json" <<'PY'
import datetime, json, os, pathlib

repo = pathlib.Path(os.environ["REPO_ROOT"])

def version_of(rel):
    path = repo / rel
    return path.read_text(encoding="utf-8").strip() if path.is_file() else None

steps = []
for step_id in os.environ["CORE_STEPS"].split():
    steps.append({"id": step_id, "optional": False})
for step_id in os.environ["OPTIONAL_STEPS"].split():
    steps.append({"id": step_id, "optional": True, "default": True})

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
    steps.append({
        "id": step_id,
        "optional": True,
        "default": True,
        "service_id": manifest["service_id"],
        "dir": directory,
        "unit": units[directory],
    })
steps.sort(key=lambda item: item["id"])

head = {
    "version": os.environ["VERSION"],
    "built_at": datetime.datetime.now().astimezone().isoformat(timespec="seconds"),
    "arch": os.environ["ARCH"],
    "uname_machine": os.environ["UNAME_MACHINES"].split(","),
    "python_minor": os.environ["PYTHON_MINOR"],
    "python_abi": os.environ["ABI"],
    "target_user": os.environ["BUNDLE_USER"],
    "target_base": os.environ["BUNDLE_BASE"],
    "components": {
        "bootstrap": os.environ["VERSION"],
        "dashboard": version_of("dashboard/VERSION"),
        "services": version_of("services/VERSION"),
        "energy_node_common": version_of("libs/energy_node_common/VERSION"),
        "battery_soc_core": version_of("libs/battery_soc_core/VERSION"),
    },
    "steps": steps,
}
if os.environ.get("CADDY_FILE"):
    head["caddy"] = {
        "version": os.environ["CADDY_VERSION"],
        "file": os.environ["CADDY_FILE"],
        "sha256": os.environ["CADDY_SHA"],
    }
print(json.dumps(head, indent=2, ensure_ascii=False))
PY

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
