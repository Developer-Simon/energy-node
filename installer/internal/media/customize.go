package media

import (
	"regexp"
	"strings"
)

// Wifi holds the WLAN credentials written into the first boot files.
type Wifi struct{ SSID, Password, Country string }

// Settings is everything the installer writes into the image for first boot.
type Settings struct {
	Hostname      string
	User          string
	AuthorizedKey string // one authorized_keys line (installer key)
	PasswordHash  string // "" = account locked for password login; otherwise sha512-crypt
	SSHPassword   bool   // das Notfall-Passwort gilt auch fuer SSH; nur mit Passwort
	Timezone      string // "Europe/Berlin"
	Keyboard      string // "de"
	Wifi          *Wifi  // nil = no WLAN
}

var (
	hostnamePattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)
	userPattern     = regexp.MustCompile(`^[a-z_][a-z0-9_-]{0,31}$`)
	yamlEscaper     = strings.NewReplacer(`\`, `\\`, `"`, `\"`)
)

// ValidHostname reports whether name is a usable RFC 1123 host label.
func ValidHostname(name string) bool {
	return hostnamePattern.MatchString(name)
}

// ValidUser reports whether name is an allowed login name, excluding root.
func ValidUser(name string) bool {
	return name != "root" && userPattern.MatchString(name)
}

// yamlQuote writes a double-quoted YAML string with backslash and quote escaped.
func yamlQuote(s string) string {
	return `"` + yamlEscaper.Replace(s) + `"`
}

// shellQuote wraps s in single quotes for POSIX sh.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// UserData renders the cloud-init user-data for Raspberry Pi OS (cloudinit-rpi).
func UserData(s Settings, hk HostKey) []byte {
	var b strings.Builder
	b.WriteString("#cloud-config\n")
	b.WriteString("hostname: " + yamlQuote(s.Hostname) + "\n")
	b.WriteString("manage_etc_hosts: true\n")
	b.WriteString("packages:\n- avahi-daemon\n")
	b.WriteString("timezone: " + yamlQuote(s.Timezone) + "\n")
	b.WriteString("keyboard:\n  model: pc105\n")
	b.WriteString("  layout: " + yamlQuote(s.Keyboard) + "\n")
	b.WriteString("user:\n")
	b.WriteString("  name: " + yamlQuote(s.User) + "\n")
	b.WriteString("  shell: /bin/bash\n")
	if s.PasswordHash == "" {
		b.WriteString("  lock_passwd: true\n")
	} else {
		b.WriteString("  lock_passwd: false\n")
		b.WriteString("  passwd: " + yamlQuote(s.PasswordHash) + "\n")
	}
	b.WriteString("  ssh_authorized_keys:\n")
	b.WriteString("    - " + yamlQuote(s.AuthorizedKey) + "\n")
	b.WriteString("  sudo: ALL=(ALL) NOPASSWD:ALL\n")
	if s.SSHPassword && s.PasswordHash != "" {
		b.WriteString("ssh_pwauth: true\n")
	} else {
		b.WriteString("ssh_pwauth: false\n")
	}
	b.WriteString("ssh_deletekeys: true\n")
	b.WriteString("ssh_genkeytypes: []\n")
	b.WriteString("ssh_keys:\n")
	b.WriteString("  ed25519_private: |\n")
	for _, line := range strings.Split(strings.TrimRight(string(hk.PrivatePEM), "\n"), "\n") {
		b.WriteString("    " + line + "\n")
	}
	b.WriteString("  ed25519_public: " + yamlQuote(hk.PublicLine) + "\n")
	if s.Wifi != nil {
		// Der Imager setzt das Land nur ueber die Oberflaeche in cmdline.txt.
		// Ohne Land haelt Raspberry Pi OS das WLAN per rfkill gesperrt.
		b.WriteString("bootcmd:\n")
		b.WriteString("  - [ cloud-init-per, once, en-wifi-country, sh, -c, " +
			yamlQuote("raspi-config nonint do_wifi_country "+s.Wifi.Country+"; rfkill unblock wifi") + " ]\n")
	}
	b.WriteString("runcmd:\n  - [ systemctl, enable, --now, ssh ]\n")
	return []byte(b.String())
}

// NetworkConfig renders the netplan network-config for WLAN, or nil without WLAN.
func NetworkConfig(s Settings) []byte {
	if s.Wifi == nil {
		return nil
	}
	var b strings.Builder
	b.WriteString("network:\n")
	b.WriteString("  version: 2\n")
	b.WriteString("  ethernets:\n")
	b.WriteString("    eth0:\n")
	b.WriteString("      dhcp4: true\n")
	b.WriteString("      dhcp6: true\n")
	b.WriteString("      optional: true\n")
	b.WriteString("  wifis:\n")
	b.WriteString("    wlan0:\n")
	b.WriteString("      dhcp4: true\n")
	b.WriteString("      regulatory-domain: " + yamlQuote(s.Wifi.Country) + "\n")
	b.WriteString("      access-points:\n")
	b.WriteString("        " + yamlQuote(s.Wifi.SSID) + ":\n")
	b.WriteString("          password: " + yamlQuote(s.Wifi.Password) + "\n")
	b.WriteString("      optional: true\n")
	return []byte(b.String())
}

// FirstRunScript renders the firstrun.sh executed once by the Raspberry Pi
// first-boot service (init_format: systemd).
func FirstRunScript(s Settings, hk HostKey) []byte {
	user := shellQuote(s.User)
	sudoers := shellQuote("/etc/sudoers.d/010_" + s.User + "-nopasswd")
	var b strings.Builder
	b.WriteString("#!/bin/sh\n")
	b.WriteString("set +e\n")
	b.WriteString("IMAGER_HOSTNAME=" + shellQuote(s.Hostname) + "\n")
	b.WriteString(`CURRENT_HOSTNAME=$(cat /etc/hostname | tr -d " \t\n\r")` + "\n")
	b.WriteString("if [ -f /usr/lib/raspberrypi-sys-mods/imager_custom ]; then\n")
	b.WriteString(`   /usr/lib/raspberrypi-sys-mods/imager_custom set_hostname "$IMAGER_HOSTNAME"` + "\n")
	b.WriteString("else\n")
	b.WriteString(`   echo "$IMAGER_HOSTNAME" >/etc/hostname` + "\n")
	b.WriteString(`   sed -i "s/127.0.1.1.*$CURRENT_HOSTNAME/127.0.1.1\t$IMAGER_HOSTNAME/g" /etc/hosts` + "\n")
	b.WriteString("fi\n")
	b.WriteString(`FIRSTUSER=$(getent passwd 1000 | cut -d: -f1)` + "\n")
	b.WriteString(`FIRSTUSERHOME=$(getent passwd 1000 | cut -d: -f6)` + "\n")
	b.WriteString("/usr/lib/raspberrypi-sys-mods/imager_custom enable_ssh -k " + shellQuote(s.AuthorizedKey) + "\n")
	if s.SSHPassword && s.PasswordHash != "" {
		// enable_ssh -k schaltet die Passwort-Anmeldung in sshd_config ab. Die
		// Dateien in sshd_config.d stehen vorne und der erste Wert gilt.
		b.WriteString("mkdir -p /etc/ssh/sshd_config.d\n")
		b.WriteString("echo 'PasswordAuthentication yes' >/etc/ssh/sshd_config.d/10-energy-node-password.conf\n")
	}
	b.WriteString("/usr/lib/userconf-pi/userconf " + user + " " + shellQuote(s.PasswordHash) + "\n")
	b.WriteString("echo " + shellQuote(s.User+" ALL=(ALL) NOPASSWD:ALL") + " >" + sudoers + "\n")
	b.WriteString("chmod 0440 " + sudoers + "\n")
	b.WriteString("systemctl disable regenerate_ssh_host_keys 2>/dev/null || true\n")
	b.WriteString("rm -f /etc/ssh/ssh_host_*\n")
	b.WriteString("cat >/etc/ssh/ssh_host_ed25519_key <<'EN_HOSTKEY'\n")
	b.WriteString(strings.TrimRight(string(hk.PrivatePEM), "\n") + "\n")
	b.WriteString("EN_HOSTKEY\n")
	b.WriteString("chmod 600 /etc/ssh/ssh_host_ed25519_key\n")
	b.WriteString("echo " + shellQuote(hk.PublicLine) + " >/etc/ssh/ssh_host_ed25519_key.pub\n")
	b.WriteString("chmod 644 /etc/ssh/ssh_host_ed25519_key.pub\n")
	if s.Wifi != nil {
		b.WriteString("/usr/lib/raspberrypi-sys-mods/imager_custom set_wlan -p " +
			shellQuote(s.Wifi.SSID) + " " + shellQuote(s.Wifi.Password) + " " + shellQuote(s.Wifi.Country) + "\n")
		b.WriteString("rfkill unblock wifi\n")
		b.WriteString("for filename in /var/lib/systemd/rfkill/*:wlan ; do echo 0 > $filename ; done\n")
	}
	b.WriteString("/usr/lib/raspberrypi-sys-mods/imager_custom set_keymap " + shellQuote(s.Keyboard) + "\n")
	b.WriteString("/usr/lib/raspberrypi-sys-mods/imager_custom set_timezone " + shellQuote(s.Timezone) + "\n")
	b.WriteString("rm -f /boot/firstrun.sh\n")
	b.WriteString(`sed -i 's| systemd.run.*||g' /boot/cmdline.txt` + "\n")
	b.WriteString("exit 0\n")
	return []byte(b.String())
}
