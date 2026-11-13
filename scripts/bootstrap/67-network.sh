#!/usr/bin/env bash
#
# Schritt 67: Notfall-Hotspot und WLAN-Waechter (Spec "Netzwerk und WLAN").
#
# Kern-Schritt ohne Dienstauswahl. Die Zugangsdaten des Hotspots entstehen
# einmal, in /etc/energy-node/hotspot.json: fehlt die Datei, bekommt auch ein
# bestehender Node sie beim Update. Das NetworkManager-Profil schreibt der
# Schritt bei jedem Lauf neu, damit ein spaeter gesetzter Laendercode greift.
# Dazu kommen der Waechter und sein Timer. Ohne NetworkManager oder ohne
# WLAN-Geraet bleibt es bei hotspot.json, der Timer laeuft trotzdem und tut
# dann nichts.
#
# Kein Skip-Schritt (wie 65): er schreibt bei jedem Lauf.
set -euo pipefail
# shellcheck source=scripts/bootstrap/lib/step.sh
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib/step.sh"

step_begin 67

# Test-Hooks: EN_SYSFS (Vorgabe /sys) fuer das WLAN-Geraet, EN_ROOT wie ueberall.
SYSFS="${EN_SYSFS:-/sys}"
WLAN_DEV=wlan0
PROFILE_ID=energy-node-hotspot
PROFILE_UUID=6f1d3c2e-8a4b-4e7d-9c52-3b8e1a0f47d6
WATCH_NAME=energy-node-network-watch
WATCH_TIMER="${WATCH_NAME}.timer"

# 256 deutsche Alltagswoerter ohne Umlaute. Eine Ziffernfolge macht das
# Passwort lang genug; die Woerter sind bewusst kurz und leicht abzutippen.
# Aendert man die Liste, aendert man das Passwort fuer neue Nodes.
HOTSPOT_WORDS=(
  apfel arzt bahn balkon banane bank baum becher
  beere berg besen biber biene birne blatt blume
  boden bogen brief brot busch dach damm decke
  deich dorf draht druck ecke eimer eis elster
  engel erbse essig fahrrad falke faden farbe feder
  fenster ferien feuer fisch flasche flagge floss fluss
  fracht frosch gabel garten gans gasse geige gurke
  gipfel glas glocke gold graben gras grill grund
  gurt hafen hahn hammer hanf hase haus hecke
  heft heide helm herd hering hirsch hirte hof
  holz honig hose huhn hund hut igel insel
  jacke jagd kabel kaffee kahn kakao kamm kanne
  karte kasten kater kastanie kessel kissen klee klinge
  knopf koffer kohl korb kran kreide kugel kuchen
  kurs lampe lappen laterne leiter linse lotse luft
  magnet mantel markt maus meer mehl messer mond
  motor mulde nadel nagel nebel nelke nest netz
  nudel nuss ofen oase orgel paket palme pappe
  papier pfad pfanne pfeil pfote pilz pinsel pizza
  plakat platz polster pumpe puppe quark quelle rabe
  radio rahmen rakete rauch raum regal reifen reis
  riese ring rinde rock rose rucksack ruder sack
  saft salbe salz sand satz schal schere schiff
  schnur schuh seife seil sessel sichel socke sonne
  spiegel spinne sprosse stab stern stift stiel stuhl
  suppe tafel tasse taube teller tiger tinte tisch
  topf torte traum treppe tuch turm uhr vase
  vogel wagen wald wanne wecker welle wiese wind
  wolke wurst zange zaun zauber ziege ziegel zimmer
  zitrone zucker zug zwiebel zelt zirkus kerze schaukel
  sattel kompass hafer bauer eule fuchs kalender kartoffel
  leuchte mauer tor ente nase hals ohr hand
)

# Ein Byte aus /dev/urandom als Zahl 0 bis 255.
random_byte() { od -An -N1 -tu1 /dev/urandom | tr -d ' \n'; }

# Ein Wort: jedes Byte ist gleichverteilt auf 256 Eintraege, ohne Verzerrung.
random_word() { printf '%s' "${HOTSPOT_WORDS[$(random_byte)]}"; }

# Zwei Ziffern 00 bis 99: Bytes ab 200 werden verworfen, damit mod 100
# gleichverteilt bleibt.
random_digits() {
  local b
  while :; do
    b="$(random_byte)"
    if (( b < 200 )); then
      printf '%02d' $(( b % 100 ))
      return 0
    fi
  done
}

hotspot_password() {
  printf '%s-%s-%s-%s' "$(random_word)" "$(random_word)" "$(random_word)" "$(random_digits)"
}

# Das WLAN-Geraet nach der Pruefung des Waechters: ein wireless-Eintrag.
has_wlan() { [[ -d "${SYSFS}/class/net/${WLAN_DEV}/wireless" ]]; }

# Die letzten vier Hex-Zeichen der MAC von wlan0, ohne WLAN-Geraet aus
# /etc/machine-id. Das Ergebnis landet in SUFFIX (keine Unterschale, damit
# step_fail hier wirklich beendet).
device_suffix() {
  local source text
  if [[ -r "${SYSFS}/class/net/${WLAN_DEV}/address" ]]; then
    source="${SYSFS}/class/net/${WLAN_DEV}/address"
  elif [[ -r "${EN_ROOT}/etc/machine-id" ]]; then
    source="${EN_ROOT}/etc/machine-id"
  else
    step_fail NODE_ID_MISSING
  fi
  text="$(tr -d ':\n' < "${source}")"
  SUFFIX="$(printf '%s' "${text: -4}" | tr 'a-f' 'A-F')"
}

# Laendercode aus iw reg get. Der Weltbereich 00 zaehlt nicht als Land.
country_code() {
  iw reg get 2>/dev/null \
    | sed -n 's/^country \([A-Z0-9][A-Z0-9]\):.*/\1/p' \
    | grep -vx '00' \
    | head -n 1 || true
}

# install_owned wie in 60-node-install.sh: Gruppe des Zielbenutzers, aber nur
# dort, wo root oder sudo zur Verfuegung steht.
install_owned() {
  local mode="$1" src="$2" dst="$3"
  if { [[ "${#SUDO[@]}" -gt 0 || "$(id -u)" -eq 0 ]] \
       && getent group "${EN_TARGET_USER}" >/dev/null 2>&1; }; then
    "${SUDO[@]}" install -o root -g "${EN_TARGET_USER}" -m "${mode}" "${src}" "${dst}"
  else
    install -m "${mode}" "${src}" "${dst}"
  fi
}

bundle_dash="${EN_BUNDLE_DIR}/dashboard"
for file in "${WATCH_NAME}" "${WATCH_NAME}.service" "${WATCH_TIMER}"; do
  [[ -f "${bundle_dash}/${file}" ]] || step_fail NETWORK_WATCH_MISSING
done

# --- Zugangsdaten: einmal anlegen, nie ueberschreiben ---------------------
etc="${EN_ROOT}/etc/energy-node"
hotspot_json="${etc}/hotspot.json"
"${SUDO[@]}" mkdir -p "${etc}"
if [[ -e "${hotspot_json}" ]]; then
  step_log "hotspot.json vorhanden, bleibt unangetastet."
else
  device_suffix
  ssid="EnergyNode-${SUFFIX}"
  password="$(hotspot_password)"
  new_json="$(mktemp)"
  python3 -c '
import json, sys
json.dump({"ssid": sys.argv[1], "password": sys.argv[2]}, sys.stdout)
sys.stdout.write("\n")
' "${ssid}" "${password}" > "${new_json}"
  if ! install_owned 0640 "${new_json}" "${hotspot_json}"; then
    rm -f "${new_json}"
    step_fail HOTSPOT_JSON_WRITE_FAILED
  fi
  rm -f "${new_json}"
  step_log "hotspot.json angelegt: ${ssid}."
fi

# Ssid und Passwort aus der Datei lesen, auch wenn sie schon da war. Die
# Datei gehoert root:<Zielbenutzer> 0640, daher liest sie sudo.
creds="$("${SUDO[@]}" cat "${hotspot_json}" | python3 -c '
import json, sys
doc = json.load(sys.stdin)
print(doc["ssid"] + "\t" + doc["password"])
')" || step_fail HOTSPOT_JSON_INVALID
ssid="${creds%%$'\t'*}"
password="${creds#*$'\t'}"

# --- NetworkManager-Profil: bei jedem Lauf neu ----------------------------
profile="${EN_ROOT}/etc/NetworkManager/system-connections/${PROFILE_ID}.nmconnection"
if command -v nmcli >/dev/null 2>&1 && has_wlan; then
  country="$(country_code)"
  profile_tmp="$(mktemp)"
  {
    printf '[connection]\nid=%s\nuuid=%s\ntype=wifi\nautoconnect=false\ninterface-name=%s\n\n' \
      "${PROFILE_ID}" "${PROFILE_UUID}" "${WLAN_DEV}"
    printf '[wifi]\nmode=ap\nband=bg\nssid=%s\n' "${ssid}"
    # Ohne Laendercode darf der Kanal nicht frei gewaehlt werden, sonst
    # scheitert der Hotspot auf manchen Geraeten.
    [[ -n "${country}" ]] || printf 'channel=1\n'
    printf '\n[wifi-security]\nkey-mgmt=wpa-psk\npsk=%s\n\n' "${password}"
    printf '[ipv4]\nmethod=shared\n'
  } > "${profile_tmp}"
  "${SUDO[@]}" mkdir -p "$(dirname "${profile}")"
  if ! "${SUDO[@]}" install -m 0600 "${profile_tmp}" "${profile}"; then
    rm -f "${profile_tmp}"
    step_fail HOTSPOT_PROFILE_WRITE_FAILED
  fi
  rm -f "${profile_tmp}"
  "${SUDO[@]}" nmcli connection reload >/dev/null 2>&1 || true
  step_log "Hotspot-Profil ${PROFILE_ID} geschrieben."
else
  step_log "kein NetworkManager oder kein WLAN-Geraet: kein Hotspot-Profil."
fi

# --- Waechter und Timer ----------------------------------------------------
"${SUDO[@]}" mkdir -p "${EN_ROOT}/usr/local/sbin" "${EN_ROOT}/etc/systemd/system"
"${SUDO[@]}" install -m 0755 "${bundle_dash}/${WATCH_NAME}" \
  "${EN_ROOT}/usr/local/sbin/${WATCH_NAME}"
"${SUDO[@]}" install -m 0644 "${bundle_dash}/${WATCH_NAME}.service" \
  "${EN_ROOT}/etc/systemd/system/${WATCH_NAME}.service"
"${SUDO[@]}" install -m 0644 "${bundle_dash}/${WATCH_TIMER}" \
  "${EN_ROOT}/etc/systemd/system/${WATCH_TIMER}"

"${SUDO[@]}" systemctl daemon-reload
"${SUDO[@]}" systemctl enable --now "${WATCH_TIMER}" \
  || step_fail NETWORK_TIMER_START_FAILED

step_log "WLAN-Waechter und Timer eingerichtet."
step_ok
