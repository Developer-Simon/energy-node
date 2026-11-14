---
title: "Trying the Debian install stick in QEMU"
component: installer
---

# Trying the Debian install stick in QEMU

The media mode of the installer writes a Debian install stick for x86 and arm64
devices. You do not need hardware to try the stick: `installer/test/qemu/dev.sh`
builds the same stick, installs it into a virtual machine and boots the result,
so you can connect the installer to it like to a real node.

## Requirements

- `qemu-system-x86_64`, `qemu-img`, `ssh`, `ssh-keygen`, `go` and the edk2 UEFI
  firmware (Fedora: `edk2-ovmf`, Debian: `ovmf`)
- Optional: `socat` (stops a VM cleanly from the script)
- Network access for the first run, the Debian netinst ISO is downloaded and
  checked against the signed `SHA512SUMS`, exactly like the installer does

Everything the script creates lives in `~/.cache/energy-node-qemu`
(`EN_QEMU_WORK_DIR`): the test keys, the ISO, the stick image and one directory
per VM. Keep the path short, QEMU limits the monitor socket path to 108 bytes.
Nothing is written into the repository.

## Walk-through

```sh
installer/test/qemu/dev.sh setup     # check the tools, create the test keys
installer/test/qemu/dev.sh stick     # build the stick
installer/test/qemu/dev.sh install   # install in a window, about 5 minutes
installer/test/qemu/dev.sh boot      # boot the installed disk, SSH on 127.0.0.1:2222
```

`install` opens a window with the Debian installer. It writes to a virtual
16 GB disk, wipes it without asking (the stick was built without the
confirmation) and powers the machine off when it is done. Use `QDISPLAY="-display none"`
for a headless run. A second VM gets its own name and port:
`dev.sh install other`, `dev.sh boot other 2223`.

`boot` prints how to connect the installer:

| Field | Value |
|---|---|
| Host | `127.0.0.1:2222` |
| User | `node` |
| Login | key, `~/.cache/energy-node-qemu/keys/installer` |

The host key of the VM is fixed by the stick, so its fingerprint is the same
after every reinstall. The ECDSA and RSA keys of Debian are new on every install.
`dev.sh setup` prints the fingerprint to expect.

The dashboard of the node is not reachable from the host directly, QEMU forwards
only SSH. `dev.sh tunnel` forwards it:
`https://localhost:8443` (self-signed certificate) and `http://localhost:8080`.

`dev.sh ssh [name] [command]` opens a shell on the node, `dev.sh stop [name]`
powers it off and `dev.sh clean [name]` deletes the VM.

The node has no password. The account is locked and login works with the key
only. Set one for the window console with `sudo passwd node` in a `dev.sh ssh` shell, or
build the stick with `dev.sh stick -passhash <hash> -sshpassword`
(`openssl passwd -6` creates the hash).

## Checks without a window

```sh
installer/test/qemu/dev.sh matrix
```

runs four unattended installs (UEFI with a 4 GB virtio disk, BIOS, BIOS with an
extra USB disk, Secure Boot with NVMe), boots each result and checks it over
SSH: partitions, swap, journal limit, SSH settings. Each run takes about
4 minutes. The runs for a disk that is too small, for several disks and for the
confirmation dialog stop at a question on the screen and need a key press, so
the matrix skips them (see the table in `installer/test/qemu/README.md`).

## What the QEMU node cannot show

- Wi-Fi, the emergency hotspot and the country code, a VM has no Wi-Fi device.
- The behaviour on real firmware, boot order and Secure Boot keys.
- arm64, it only runs emulated and takes about 23 minutes, see the README.

The scripts behind `dev.sh` are `build.sh`, `run.sh` and `verify.sh` in the same
directory. The README there describes the expected results of each run.
