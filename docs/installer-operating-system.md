---
title: "Operating system"
component: installer
---

# Operating system

Use a Lite image without a desktop. The node runs headless, and on the small
models every megabyte of RAM counts. Pick the image by model:

| Hardware | Image | Installation package | Tested |
|---|---|---|---|
| Pi 1 (A, A+, B, B+) | Raspberry Pi OS (Legacy, 32-bit) Lite | `armv6` | ✓ |
| Pi Zero, Zero W | Raspberry Pi OS (Legacy, 32-bit) Lite | `armv6` | ✗ |
| Pi 2 | Raspberry Pi OS Lite (32-bit) | `armv6` | ✗ |
| Pi Zero 2 W | Raspberry Pi OS Lite (64-bit) | `arm64` | ✗ |
| Pi 3, 3+ | Raspberry Pi OS Lite (64-bit) | `arm64` | ✗ |
| Pi 4, 400 | Raspberry Pi OS Lite (64-bit) | `arm64` | ✗ |
| Pi 5, 500 | Raspberry Pi OS Lite (64-bit) | `arm64` | ✗ |
| x86 64-bit (AMD GX-424CC, Intel N100, N150) | Debian 13 netinst | `amd64` | ✓ |
| x86 32-bit (Intel Quark X1000, Vortex86EX2) | not supported | none | ✗ |

The 64-bit images do not run on ARMv6, so a Pi 1 or the first Zero always needs
a 32-bit image. The Legacy image is the one the reference node runs on a Pi 1.
A 32-bit image on a newer Pi works too and gets the `armv6` package. The
installer picks the package from the node's `uname -m`.

The reference node runs Python 3.11.2, which is enough for every service in
this repository (`requires-python >= 3.9`). An installation package carries
its Python dependencies as wheels for one Python version: 3.11 (Debian 12
"Bookworm") for `armv6`, 3.13 (Debian 13 "Trixie") for `arm64` and `amd64`.
Those match the images in the table above. On an image with a different
Python version, for example a 64-bit Bookworm, the installer reports the
mismatch when it prepares the package and offers to let the node load the
dependencies from the internet instead. You confirm that once, the node then
needs internet access, and those files are not covered by the package
signature. Everything else in the package stays verified.

## Raspberry Pi with the Raspberry Pi Imager

- Pick the image from the table above.
- Set hostname, user, SSH, WiFi and timezone in the Imager before it writes
  the card. On a headless remote site you do not want to find out afterwards
  that SSH is off.
- Leave the root partition alone. It resizes itself on first boot.
- With these settings the card needs nothing else. Boot the Pi and point the
  energy-node installer at `<hostname>.local`. The installer updates the
  system packages itself, which takes a long time on a Pi 1.

## x86 with Debian

Before you start:

- The CPU must support 64-bit (x86-64). There is no package for 32-bit x86,
  and the installer stops on such a machine because it finds no package for
  its `uname -m` (`i586` or `i686`).
- A minimal Debian 13 takes about 1.5 GB, so 4 GB of internal storage leaves
  room for the node.
- The netinst image is about 750 MB and fits on a 2 GB USB stick. Everything
  else comes from the internet during the installation, so connect the
  machine by network cable.
- Used thin clients often have a BIOS password set or USB boot switched off.
  Clear that first.
- Installing Debian erases the previous system, including a thin client OS
  and its licence.

Write the USB stick:

1. Download `debian-13.x.y-amd64-netinst.iso` and `SHA512SUMS` from
   <https://www.debian.org/CD/netinst/> (amd64). Check the image, the command
   must print `OK` for it:

   ```sh
   sha512sum -c SHA512SUMS --ignore-missing
   ```

2. Plug in the stick and find it. It is the device with `usb` in the `TRAN`
   column:

   ```sh
   lsblk -o NAME,SIZE,MODEL,TRAN
   ```

3. Write the image to the whole device, for example `/dev/sdX`, not to a
   partition like `/dev/sdX1`. This erases everything on the stick:

   ```sh
   sudo dd if=debian-13.x.y-amd64-netinst.iso of=/dev/sdX bs=4M status=progress conv=fsync
   ```

   GNOME Disks (Restore Disk Image) does the same on a Linux desktop, Rufus
   or balenaEtcher on Windows.

Install Debian:

1. Boot from the stick through the boot menu or the BIOS setup and pick
   Install. The text installer is enough.
2. Set language, location and keyboard, then a hostname, for example `node`.
3. Leave the root password empty and create your user. Debian then gives
   this user `sudo`.
4. Partitioning: Guided, use entire disk, then All files in one partition.
5. Pick a Debian mirror close to you.
6. Software selection: untick the desktop and keep only SSH server and
   standard system utilities.
7. Install the boot loader to the internal disk, remove the stick and reboot.

Partitioning tips:

- If the installer only offers manual partitioning, create a new GPT
  partition table on the whole disk and two partitions in its free space:
  256 MB as EFI System Partition at the beginning and the rest as Ext4
  mounted at `/`.
- A machine booted in legacy BIOS mode needs no EFI partition.
- The node needs no swap with 1 GB of RAM or more. On small storage skip it
  and continue past the installer's warning. `zram-tools` adds a compressed
  swap in RAM later if you want one, and it does not write to the disk.

After the installation:

- Connect with `ssh <user>@<ip-address>` and point the energy-node installer
  at that address. The IP address shows in your router, or on the machine
  with `ip -br addr`.
- A minimal Debian has no mDNS, so `<hostname>.local` only works once you
  install `avahi-daemon`.
- The installer needs `sudo` without a password, which a Raspberry Pi OS user
  has by default. On Debian it asks for the `sudo` password once and sets
  it up, see [sudo password](installer.md#sudo-password).

The installer also updates the node's system packages (`apt-get update`, then
`apt-get upgrade`) once per installer version. You can switch this off on its
configuration screen (**Update system packages**). Switched off, it still
checks and writes the number of available updates to the log. It never removes
packages and never reboots. When a kernel or firmware update needs a reboot,
the result screen and the diagnose say so. Reboot the node yourself with
`sudo reboot`.

On a small SD card you can keep the system journal in RAM only. Tick **Keep the
journal in RAM only** on the installer's configuration screen (step 17, off by
default). The logs then no longer go to the card, which saves write cycles and
some space, and the installer removes the journal that is already on the card.
The price is that every log, including the service logs in the dashboard, is
gone after a reboot. The RAM use is capped at 50 MB. Untick the step later and
the installer restores the system default on its next run.
