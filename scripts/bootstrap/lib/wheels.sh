#!/usr/bin/env bash
#
# Python-Wheels fuer Schritt 50: die Festlegungen aus manifest.wheels (Datei,
# Adresse, SHA-256) werden in EN_WHEELS_DIR gebracht.
#
# wheels_sync holt nur, was fehlt oder falsch ist:
#   1. Datei dort mit passender Pruefsumme: nichts tun.
#   2. Datei in ${EN_BUNDLE_DIR}/wheels/ (Bundle mit --embed-wheels): kopieren.
#   3. Sonst herunterladen, python3 -I mit urllib. Nur https auf den Hosts der
#      Erlaubnisliste, jede Weiterleitung wird ebenso geprueft.
# Jede Datei kommt zuerst als .part, wird geprueft und erst dann umbenannt.
# Dateien in EN_WHEELS_DIR ohne Festlegung werden geloescht, so kann eine
# untergeschobene .whl nie in pip geraten.
#
# Das Verzeichnis gehoert root (0755), die Dateien sind 0644. Das Verzeichnis
# liegt unter EN_STATE_DIR und nicht im Auftragsverzeichnis, aus dem der
# Updater das Bundle holt - dort haette das Dashboard-Konto Schreibrecht.
# Schritte laufen als root (Updater) oder mit sudo (Installer), deshalb gehen
# alle Schreibzugriffe ueber "${SUDO[@]}".
#
# Testvariable EN_WHEEL_HOSTS_TEST: nur Tests setzen sie. Sie ersetzt die
# Erlaubnisliste (Leerzeichen-getrennte Hosts) und erlaubt zusaetzlich http,
# damit der Test einen lokalen Server nimmt. Ohne sie gilt die feste Liste
# und nur https.

WHEELS_LIB_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=scripts/bootstrap/lib/step.sh
source "${WHEELS_LIB_DIR}/step.sh"

EN_WHEELS_DIR="${EN_WHEELS_DIR:-${EN_STATE_DIR}/wheels}"

# Hosts der Wheel-Adressen: piwheels und PyPI, wie im Bundle-Bau verifiziert.
WHEEL_HOSTS_DEFAULT="files.pythonhosted.org www.piwheels.org archive1.piwheels.org"

# wheels_declared: Erfolg, wenn das Manifest eine Liste manifest.wheels hat.
# Ein altes Bundle ohne das Feld liefert 1, Schritt 50 bleibt dann wie frueher.
wheels_declared() {
  [[ -f "${EN_BUNDLE_DIR}/manifest.json" ]] || return 1
  python3 -I - "${EN_BUNDLE_DIR}/manifest.json" 2>/dev/null <<'PY'
import json, sys
try:
    wheels = json.load(open(sys.argv[1], encoding="utf-8")).get("wheels")
except (OSError, ValueError, AttributeError):
    sys.exit(1)
sys.exit(0 if isinstance(wheels, list) else 1)
PY
}

# wheel_pins: je Festlegung eine Zeile "Datei<TAB>Adresse<TAB>SHA-256".
# Eine ungueltige Festlegung beendet mit 1 und nennt sie auf stderr.
wheel_pins() {
  python3 -I - "${EN_BUNDLE_DIR}/manifest.json" <<'PY'
import json, re, sys
entries = json.load(open(sys.argv[1], encoding="utf-8")).get("wheels") or []
seen = set()
for entry in entries:
    if not isinstance(entry, dict):
        print("wheels: ungueltige Festlegung %r" % (entry,), file=sys.stderr)
        sys.exit(1)
    file, url, sha = entry.get("file"), entry.get("url"), entry.get("sha256")
    valid = (
        all(isinstance(value, str) for value in (file, url, sha))
        and re.fullmatch(r"[A-Za-z0-9._+~-]+\.whl", file)
        and re.fullmatch(r"[0-9a-f]{64}", sha)
        and not re.search(r"\s", url)
        and file not in seen
    )
    if not valid:
        print("wheels: ungueltige Festlegung %r" % (entry,), file=sys.stderr)
        sys.exit(1)
    seen.add(file)
    print("%s\t%s\t%s" % (file, url, sha))
PY
}

# wheel_matches <pfad> <sha>: Datei existiert und hat genau diese Pruefsumme.
wheel_matches() {
  [[ -f "$1" ]] && [[ "$(sha256sum "$1" | cut -d' ' -f1)" == "$2" ]]
}

# wheel_download <adresse> <teil> <ziel> <sha> <datei> <hosts> <schemata>
# Laedt nach <teil>, prueft die Pruefsumme und benennt dann nach <ziel> um.
# Rueckgabe 0 bei Erfolg, 2 bei Netz- oder Host-Fehler, 3 bei falscher
# Pruefsumme. Die Meldung auf stderr nennt die Datei.
wheel_download() {
  "${SUDO[@]}" python3 -I - "$@" <<'PY'
import hashlib, os, sys, urllib.parse, urllib.request

url, part, dest, want, name, hosts, schemes = sys.argv[1:8]
allowed_hosts = set(hosts.split())
allowed_schemes = set(schemes.split())


class Refused(Exception):
    pass


def check(target):
    parts = urllib.parse.urlsplit(target)
    if parts.scheme not in allowed_schemes:
        raise Refused("Schema %s nicht erlaubt" % (parts.scheme or "fehlt"))
    if parts.hostname not in allowed_hosts:
        raise Refused("Host %s nicht erlaubt" % (parts.hostname or "fehlt"))


class Guarded(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        check(newurl)
        return super().redirect_request(req, fp, code, msg, headers, newurl)


def drop():
    try:
        os.unlink(part)
    except FileNotFoundError:
        pass


digest = hashlib.sha256()
try:
    check(url)
    with urllib.request.build_opener(Guarded).open(url, timeout=120) as response, open(part, "wb") as handle:
        for chunk in iter(lambda: response.read(1 << 20), b""):
            digest.update(chunk)
            handle.write(chunk)
except (Refused, OSError) as exc:
    drop()
    print("%s: Download fehlgeschlagen (%s)" % (name, exc), file=sys.stderr)
    sys.exit(2)
if digest.hexdigest() != want:
    drop()
    print("%s: Prüfsumme nach dem Download stimmt nicht" % name, file=sys.stderr)
    sys.exit(3)
os.chmod(part, 0o644)
os.replace(part, dest)
PY
}

# wheels_sync: bringt jede Festlegung aus manifest.wheels nach EN_WHEELS_DIR
# und entfernt alles andere dort. Setzt WHEELS_DOWNLOADED auf die Zahl der
# Downloads. Ruft step_fail direkt auf, nie aus einer Befehlsersetzung.
wheels_sync() {
  local pins_out line file url sha dest part src rc name entry
  local allowed_hosts="${WHEEL_HOSTS_DEFAULT}" allowed_schemes="https"
  local -A wanted=()
  local -a lines=()
  WHEELS_DOWNLOADED=0

  if [[ -n "${EN_WHEEL_HOSTS_TEST:-}" ]]; then
    allowed_hosts="${EN_WHEEL_HOSTS_TEST}"
    allowed_schemes="http https"
  fi

  pins_out="$(wheel_pins)" || step_fail MANIFEST_PARSE_FAILED
  mapfile -t lines <<< "${pins_out}"

  "${SUDO[@]}" mkdir -p "${EN_WHEELS_DIR}"
  "${SUDO[@]}" chmod 0755 "${EN_WHEELS_DIR}"

  for line in "${lines[@]}"; do
    [[ -n "${line}" ]] || continue
    IFS=$'\t' read -r file url sha <<< "${line}"
    wanted["${file}"]=1
  done

  for line in "${lines[@]}"; do
    [[ -n "${line}" ]] || continue
    IFS=$'\t' read -r file url sha <<< "${line}"
    dest="${EN_WHEELS_DIR}/${file}"
    part="${dest}.part"

    if wheel_matches "${dest}" "${sha}"; then
      continue
    fi

    src="${EN_BUNDLE_DIR}/wheels/${file}"
    if [[ -f "${src}" ]]; then
      "${SUDO[@]}" install -m 0644 "${src}" "${part}"
      if ! wheel_matches "${part}" "${sha}"; then
        "${SUDO[@]}" rm -f -- "${part}"
        step_log "${file}: die eingebettete Datei hat nicht die festgelegte Prüfsumme."
        step_fail WHEEL_FILE_INVALID
      fi
      "${SUDO[@]}" mv -f -- "${part}" "${dest}"
      step_log "${file} aus dem Bundle übernommen."
      continue
    fi

    step_log "Lade ${file} vom Node."
    rc=0
    wheel_download "${url}" "${part}" "${dest}" "${sha}" "${file}" "${allowed_hosts}" "${allowed_schemes}" || rc=$?
    case "${rc}" in
      0) WHEELS_DOWNLOADED=$((WHEELS_DOWNLOADED + 1)) ;;
      3) step_fail WHEEL_FILE_INVALID ;;
      *) step_fail WHEELS_DOWNLOAD_FAILED ;;
    esac
  done

  while IFS= read -r -d '' entry; do
    name="$(basename -- "${entry}")"
    if [[ -z "${wanted[${name}]+x}" ]]; then
      step_log "Entferne ${name}, keine Festlegung im Manifest."
      "${SUDO[@]}" rm -rf -- "${entry}"
    fi
  done < <(find "${EN_WHEELS_DIR}" -mindepth 1 -maxdepth 1 -print0)
  return 0
}
