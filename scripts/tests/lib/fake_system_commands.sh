#!/usr/bin/env bash
# scripts/tests/lib/fake_system_commands.sh
#
# Attrappen fuer apt-get, systemctl usw., die scripts/tests/bootstrap_container.sh
# schon einzeln aufbaute. Extrahiert, weil test_updater_end_to_end.sh
# dieselben Attrappen braucht, ohne die Liste ein drittes Mal zu pflegen.
install_fake_system_commands() {
  local bindir="$1"
  mkdir -p "${bindir}"
  for cmd in apt-get dpkg-query ufw systemctl visudo caddy mosquitto_passwd tailscale ss; do
    cat > "${bindir}/${cmd}" <<SH
#!/usr/bin/env bash
printf '${cmd} %s\n' "\$*" >> "\${CMD_LOG}"
case "${cmd}" in
  dpkg-query) exit 1 ;;
  tailscale) [ "\$1" = status ] && exit 0 ;;
esac
exit 0
SH
    chmod +x "${bindir}/${cmd}"
  done
}
