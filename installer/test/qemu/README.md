# QEMU-Matrix für den Debian-Stick

Prüft den Debian-Medium-Stick (`installer/cmd/mediastick`) in virtuellen
Maschinen: Stick bauen, von ihm booten, Installation laufen lassen und das
installierte System per SSH prüfen. Die Skripte ersetzen die Spike-Skripte aus
`tmp/spike/`.

## Voraussetzungen

- `qemu-system-x86_64` und `qemu-system-aarch64`, `qemu-img`, edk2 (OVMF, AAVMF)
- `go` (GOTOOLCHAIN=auto holt die im `go.mod` angegebene Version)
- `shellcheck` 0.9.0 (wie in der CI)
- Die Debian-Netinst-ISOs `debian-13.7.0-amd64-netinst.iso` und
  `debian-13.7.0-arm64-netinst.iso` (siehe `SHA512SUMS` im Debian-Mirror)
- Installer-Testschlüssel: `installer.pub` (Anmeldeschlüssel), `hostkey` und
  `hostkey.pub` (fester Host-Schlüssel, damit `verify.sh` ihn vorab einträgt)

## Umgebung

| Variable | Bedeutung |
|---|---|
| `EN_QEMU_ISO_DIR` | Verzeichnis mit den Netinst-ISOs (Pflicht) |
| `EN_QEMU_KEY_DIR` | Verzeichnis mit den Testschlüsseln (Pflicht) |
| `EN_QEMU_WORK_DIR` | Arbeitsverzeichnis für Sticks, Läufe und Logs (Standard `~/.cache/energy-node-qemu`) |
| `QDISPLAY` | Anzeige für `run.sh`, Standard `-display none` |
| `TMO` | Obergrenze pro Lauf, Standard `90m` |

## Ablauf

```sh
installer/test/qemu/build.sh amd64 "$EN_QEMU_WORK_DIR/out/en-amd64.iso"
installer/test/qemu/run.sh <lauf> amd64 uefi sata:16G
installer/test/qemu/verify.sh <lauf> 2222
```

`build.sh` übergibt zusätzliche Argumente an `mediastick`, zum Beispiel
`-ask` für den Lauf mit Nachfrage oder `-lang de`. Für arm64 wird
`build.sh arm64 <out.iso>` verwendet und `run.sh <lauf> arm64 uefi ...`.
`run.sh` startet die VM und wartet, bis sie sich ausschaltet. `verify.sh`
startet die installierte Platte ohne Stick und prüft sie über SSH.

## Matrix

| Lauf | Firmware | Platten | Erwartet |
|---|---|---|---|
| D | UEFI | SATA 2 GB | Abbruch „zu klein“: Meldung auf Deutsch mit den Werten, Enter schaltet aus, 0 Blöcke geschrieben |
| E | UEFI | SATA 16 GB + NVMe 16 GB | Abbruch „mehrere Platten“: Meldung nennt beide, Enter schaltet aus, beide Platten unberührt |
| C | UEFI | virtio 4 GB | klein: EFI 243 MiB + `/`, zram aktiv (50 % RAM), kein Swap, Journal 50M |
| A | BIOS (isolinux) | SATA 16 GB | mittel: BIOS-Boot 1 MB + `/` (GPT), Swap-Datei 1 GB, Journal 200M |
| F | BIOS | SATA 16 GB + USB-Platte 8 GB | mittel, USB-Platte unberührt: wie A, USB-Platte 0 Blöcke geschrieben |
| B | UEFI Secure Boot | NVMe 64 GB | groß: „Secure boot enabled“, EFI 487 MiB + `/`, Swap-Datei = RAM, kein Journal-Limit |
| G | ARM64 UEFI (TCG, emuliert) | virtio 16 GB | mittel: EFI 487 MiB + `/`, Swap-Datei 1 GB, Journal 200M (emuliert rund 23 min) |
| H | UEFI | SATA 16 GB, mit Nachfrage (`-ask`) | Nein: Frage nennt Platte, Modell und Größe, Fokus auf Nein, Enter schaltet aus, 0 Blöcke geschrieben |
| I | BIOS | SATA 16 GB, mit Nachfrage (`-ask`) | Ja: Installation läuft normal durch, alle Prüfungen grün |

## Notfall-Passwort

Der Lauf mit gesetztem Notfall-Passwort (Hash in `passwd/user-password-crypted`)
ist noch nicht gemacht. Der Spike hat nur das gesperrte Konto (`!`) geprüft.

Ergebnis: offen
