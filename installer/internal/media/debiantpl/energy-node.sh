#!/bin/sh
# Energy Node d-i helper (spike). Called as "early" from partman/early_command
# and as "late" from preseed/late_command.
set -u
. /cdrom/energy-node.env
LOG=/var/log/energy-node.log
log() { echo "energy-node: $*" >>"$LOG"; echo "energy-node: $*" >&2; }

abort() { # template, then name=value substitutions
	tpl=$1
	shift
	. /usr/share/debconf/confmodule
	db_x_loadtemplatefile /cdrom/energy-node.tpl energy-node
	for kv in "$@"; do
		db_subst "energy-node/$tpl" "${kv%%=*}" "${kv#*=}"
	done
	db_fset "energy-node/$tpl" seen false
	db_input critical "energy-node/$tpl" || true
	db_go || true
	log "abort $tpl, powering off"
	poweroff -f
	exit 1
}

boot_disk() {
	dev=$(awk '$2 == "/cdrom" { print $1; exit }' /proc/mounts)
	n=${dev#/dev/}
	if [ -e "/sys/class/block/$n/partition" ]; then
		n=$(basename "$(readlink -f "/sys/class/block/$n/..")")
	fi
	echo "$n"
}

early() {
	boot=$(boot_disk)
	log "boot medium: ${boot:-unknown}"
	cands=""
	for dev in $(list-devices disk); do
		n=${dev#/dev/}
		path=$(readlink -f "/sys/block/$n")
		rm=$(cat "/sys/block/$n/removable" 2>/dev/null || echo 1)
		why=""
		if [ "$n" = "$boot" ]; then
			why="boot medium"
		elif case "$path" in *usb*) true ;; *) false ;; esac; then
			why="usb"
		elif [ "$rm" != 0 ]; then
			why="removable"
		elif case "$n" in mmcblk*) true ;; *) false ;; esac &&
			[ "$(cat "/sys/block/$n/device/type" 2>/dev/null)" != MMC ]; then
			why="sd card"
		fi
		if [ -n "$why" ]; then
			log "skip $dev ($why, $path)"
			continue
		fi
		log "candidate $dev ($path)"
		cands="$cands $dev"
	done
	set -- $cands
	[ $# -eq 0 ] && abort no-disk
	[ $# -gt 1 ] && abort many-disks "DISKS=$(echo $cands | sed 's/ /, /g')"
	disk=$1
	n=${disk#/dev/}
	gb=$(awk '{ printf "%.1f", $1 * 512 / 1000000000 }' "/sys/block/$n/size")
	class=$(echo "$gb" | awk '{ if ($1 < 3) print "none"; else if ($1 < 8) print "small"; else if ($1 <= 34) print "medium"; else print "large" }')
	log "target $disk, $gb GB, class $class"
	[ "$class" = none ] && abort too-small "DISK=$disk" "SIZE=$gb"

	if [ "${EN_CONFIRM:-no}" = yes ]; then
		model=$(cat "/sys/block/$n/device/model" 2>/dev/null | sed 's/ *$//')
		. /usr/share/debconf/confmodule
		db_x_loadtemplatefile /cdrom/energy-node.tpl energy-node
		db_subst energy-node/confirm DISK "$disk"
		db_subst energy-node/confirm MODEL "${model:-?}"
		db_subst energy-node/confirm SIZE "$gb"
		db_fset energy-node/confirm seen false
		db_input critical energy-node/confirm || true
		db_go || { log "confirm: back, powering off"; poweroff -f; exit 1; }
		db_get energy-node/confirm
		if [ "$RET" != true ]; then
			log "confirm: no, powering off"
			poweroff -f
			exit 1
		fi
		log "confirm: yes"
	fi

	if [ -d /sys/firmware/efi ]; then
		esp=512
		[ "$class" = small ] && esp=256
		boot_part="$esp $esp $esp free \$iflabel{ gpt } \$reusemethod{ } method{ efi } format{ } ."
	else
		boot_part="1 1 1 free \$iflabel{ gpt } \$reusemethod{ } method{ biosgrub } ."
	fi
	root_part="1000 10000 -1 ext4 \$primary{ } \$bootable{ } method{ format } format{ } use_filesystem{ } filesystem{ ext4 } mountpoint{ / } ."
	debconf-set partman-auto/disk "$disk"
	debconf-set partman-auto/expert_recipe "energynode :: $boot_part $root_part"
	debconf-set grub-installer/bootdev "$disk"
	if [ "$class" = small ]; then
		debconf-set pkgsel/include "openssh-server avahi-daemon libnss-mdns sudo zram-tools"
	fi
	echo "$class" >/tmp/energy-node.class
}

late() {
	T=/target
	class=$(cat /tmp/energy-node.class)
	log "late, class $class"

	install -d -m 700 "$T/home/$EN_USER/.ssh"
	cp /cdrom/energy-node.pub "$T/home/$EN_USER/.ssh/authorized_keys"
	chmod 600 "$T/home/$EN_USER/.ssh/authorized_keys"
	in-target chown -R "$EN_USER:$EN_USER" "/home/$EN_USER/.ssh"

	echo "$EN_USER ALL=(ALL) NOPASSWD:ALL" >"$T/etc/sudoers.d/energy-node"
	chmod 440 "$T/etc/sudoers.d/energy-node"

	cp /cdrom/energy-node.key "$T/etc/ssh/ssh_host_ed25519_key"
	cp /cdrom/energy-node.hpub "$T/etc/ssh/ssh_host_ed25519_key.pub"
	chmod 600 "$T/etc/ssh/ssh_host_ed25519_key"
	chmod 644 "$T/etc/ssh/ssh_host_ed25519_key.pub"
	if [ "$EN_PASSWORD_LOGIN" != yes ]; then
		printf 'PasswordAuthentication no\nKbdInteractiveAuthentication no\n' >"$T/etc/ssh/sshd_config.d/energy-node.conf"
	fi

	mkdir -p "$T/etc/systemd/journald.conf.d"
	case $class in
	small)
		printf 'ALGO=zstd\nPERCENT=50\n' >"$T/etc/default/zramswap"
		printf '[Journal]\nSystemMaxUse=50M\n' >"$T/etc/systemd/journald.conf.d/energy-node.conf"
		;;
	medium)
		mb=1024
		printf '[Journal]\nSystemMaxUse=200M\n' >"$T/etc/systemd/journald.conf.d/energy-node.conf"
		;;
	large)
		mb=$(awk '/^MemTotal:/ { m = int($2 / 1024); if (m > 4096) m = 4096; print m }' /proc/meminfo)
		;;
	esac
	if [ "$class" != small ]; then
		in-target fallocate -l "${mb}M" /swapfile
		chmod 600 "$T/swapfile"
		in-target mkswap /swapfile
		echo '/swapfile none swap sw 0 0' >>"$T/etc/fstab"
	fi
	if [ -n "${EN_TARGET_LOCALE:-}" ]; then
		in-target update-locale "LANG=$EN_TARGET_LOCALE"
	fi
	sed -i '\#^/dev/sr0#d' "$T/etc/fstab"
	cp "$LOG" "$T/var/log/energy-node-install.log" 2>/dev/null || true
}

"$@"
