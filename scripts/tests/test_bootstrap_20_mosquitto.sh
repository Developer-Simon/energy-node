#!/usr/bin/env bash
# Test for scripts/bootstrap/20-mosquitto.sh
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
script="$here/../bootstrap/20-mosquitto.sh"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
fail() { echo "FAIL: $1"; [ -n "${2:-}" ] && printf '%s\n' "$2"; exit 1; }

mkdir -p "$tmp/bin"
# Das echte mosquitto_passwd (2.0.11, Debian 12) kennt keinen stdin-Weg: das
# Passwort kommt entweder vom Terminal oder mit -b als Argument. Ohne Terminal
# scheitert es mit "Error: Empty password." - die Attrappe verhaelt sich
# genauso. Der Schritt darf es gar nicht aufrufen: -b hiesse, das Passwort
# stuende in /proc/<pid>/cmdline.
cat > "$tmp/bin/mosquitto_passwd" <<'SH'
#!/usr/bin/env bash
printf 'mosquitto_passwd %s\n' "$*" >> "$MOSQ_LOG"
echo "Error: Empty password." >&2
exit 1
SH
cat > "$tmp/bin/systemctl" <<'SH'
#!/usr/bin/env bash
printf 'systemctl %s\n' "$*" >> "$SYSTEMCTL_LOG"
SH
chmod +x "$tmp/bin/mosquitto_passwd" "$tmp/bin/systemctl"

export PATH="$tmp/bin:$PATH"
export EN_STATE_DIR="$tmp/state" EN_ROOT="$tmp/root" EN_BUNDLE_VERSION=v1.0.0 EN_SUDO=""
export MOSQ_LOG="$tmp/mosq.log" SYSTEMCTL_LOG="$tmp/systemctl.log"
: > "$MOSQ_LOG"

# check_hash prueft eine passwd-Zeile unabhaengig vom Schritt: Mosquitto
# schreibt user:$7$<Runden>$<Salt base64>$<PBKDF2-HMAC-SHA512 base64>, das
# Salt geht dekodiert (12 Bytes) in PBKDF2 ein.
check_hash() {
  python3 - "$1" "$2" "$3" <<'PY'
import base64, hashlib, sys
path, user, password = sys.argv[1], sys.argv[2], sys.argv[3]
lines = open(path, encoding="utf-8").read().splitlines()
if len(lines) != 1:
    sys.exit("passwd hat %d Zeilen, erwartet 1" % len(lines))
name, _, rest = lines[0].partition(":")
if name != user:
    sys.exit("Benutzer %r, erwartet %r" % (name, user))
_, ident, rounds, salt_b64, hash_b64 = rest.split("$")
if ident != "7" or rounds != "101":
    sys.exit("unerwartetes Format: $%s$%s" % (ident, rounds))
salt = base64.b64decode(salt_b64)
if len(salt) != 12:
    sys.exit("Salt hat %d Bytes, erwartet 12" % len(salt))
digest = hashlib.pbkdf2_hmac("sha512", password.encode("utf-8"), salt, int(rounds))
if base64.b64encode(digest).decode() != hash_b64:
    sys.exit("Hash passt nicht zum Passwort")
PY
}

# --- der Pruefer selbst stimmt mit dem echten mosquitto_passwd ueberein ----
# Diese Zeile hat mosquitto_passwd 2.0.11 auf einem Pi fuer das Passwort
# "dummy-pw-123" erzeugt (mosquitto_passwd -c -b probe.pw probe dummy-pw-123).
real="$tmp/real.pw"
# shellcheck disable=SC2016 # das $ gehoert zum Hash-Format
printf '%s\n' 'probe:$7$101$OS5cVWyH+tCAKvPj$rPDK4rkIwbJnH2zmOWFaRN/k17hix2UdGrfcUgBIUBbsio4OxDZua86q1FBP+InMryM6kA9had1+TlD4hYA0LQ==' > "$real"
check_hash "$real" probe 'dummy-pw-123' || fail "der Pruefer erkennt die echte mosquitto_passwd-Zeile nicht"
if check_hash "$real" probe 'falsch' 2>/dev/null; then
  fail "der Pruefer akzeptiert ein falsches Passwort"
fi

pw="$tmp/mqtt.pw"
printf 'geheim123\n' > "$pw"
chmod 600 "$pw"

run() { bash "$script" --user knoten --password-file "$pw"; }

# --- erster Lauf schreibt conf und passwd ----------------------------------
out="$(run)"
grep -q '^##STEP 20 ok$' <<<"$out" || fail "kein ok-Marker" "$out"
conf="$tmp/root/etc/mosquitto/conf.d/default.conf"
[ -f "$conf" ] || fail "default.conf fehlt"
grep -qx 'listener 1883' "$conf" || fail "listener fehlt" "$(cat "$conf")"
grep -qx 'allow_anonymous false' "$conf" || fail "allow_anonymous fehlt" "$(cat "$conf")"
grep -qx 'password_file /etc/mosquitto/passwd' "$conf" || fail "password_file fehlt" "$(cat "$conf")"
passwd_file="$tmp/root/etc/mosquitto/passwd"
[ -f "$passwd_file" ] || fail "passwd fehlt"
check_hash "$passwd_file" knoten 'geheim123' || fail "passwd-Zeile falsch" "$(cat "$passwd_file")"
[ -z "$(find "$passwd_file" -perm /o+w)" ] || fail "passwd ist fuer alle beschreibbar"

# --- mosquitto_passwd wird nie aufgerufen, das Passwort steht nirgends -----
[ -s "$MOSQ_LOG" ] && fail "mosquitto_passwd wurde aufgerufen" "$(cat "$MOSQ_LOG")"
grep -q 'geheim123' "$passwd_file" && fail "Passwort im Klartext in der passwd"
grep -q 'geheim123' <<<"$out" && fail "Passwort in der Ausgabe" "$out"

# --- zweiter Lauf ueberspringt --------------------------------------------
before="$(cat "$passwd_file")"
out="$(run)"
grep -q '^##STEP 20 skip' <<<"$out" || fail "zweiter Lauf nicht uebersprungen" "$out"
[ "$before" = "$(cat "$passwd_file")" ] || fail "zweiter Lauf hat die passwd veraendert"

# --- neues Salt je Lauf; abschliessende Zeilenumbrueche zaehlen nicht ------
# Wie libs/energy_node_common/appconfig.py: der Dienst schneidet nachlaufenden
# Weissraum ab, also muss der Hash dasselbe Passwort meinen. Sonderzeichen
# gehen ohne Quoting-Probleme durch, weil das Passwort nie in argv steht.
rm -rf "$tmp/state"
# shellcheck disable=SC2016 # Sonderzeichen sind Absicht
printf 'p$a:s"s w\047rd\r\n\n' > "$pw"
run >/dev/null
# shellcheck disable=SC2016
check_hash "$passwd_file" knoten 'p$a:s"s w'"'"'rd' || fail "Sonderzeichen-Passwort falsch gehasht" "$(cat "$passwd_file")"
[ "$before" != "$(cat "$passwd_file")" ] || fail "kein neues Salt"

# --- fremde Konfiguration wird nicht angefasst ----------------------------
rm -rf "$tmp/state"
printf 'listener 8883\n' > "$conf"
set +e
out="$(run)"
rc=$?
set -e
[ "$rc" -eq 1 ] || fail "fremde conf nicht abgelehnt" "$rc"
grep -q '^##STEP 20 fail MOSQUITTO_CONF_FOREIGN$' <<<"$out" || fail "falscher Fehlercode" "$out"
grep -qx 'listener 8883' "$conf" || fail "fremde conf wurde veraendert" "$(cat "$conf")"

# --- leeres Passwort wird abgelehnt, die alte passwd bleibt ---------------
rm -rf "$tmp/state" "$conf"
good="$(cat "$passwd_file")"
printf '\n' > "$pw"
set +e
out="$(run)"
rc=$?
set -e
[ "$rc" -eq 1 ] || fail "leeres Passwort nicht abgelehnt" "$rc"
grep -q '^##STEP 20 fail MOSQUITTO_PASSWD_FAILED$' <<<"$out" || fail "falscher Fehlercode bei leerem Passwort" "$out"
[ "$good" = "$(cat "$passwd_file")" ] || fail "passwd wurde trotz Fehler veraendert"

# --- fehlende Argumente ----------------------------------------------------
set +e
out="$(bash "$script")"
set -e
grep -q '^##STEP 20 fail MOSQUITTO_ARGS_MISSING$' <<<"$out" || fail "fehlende Argumente nicht erkannt" "$out"

echo "OK: $(basename "$0")"
