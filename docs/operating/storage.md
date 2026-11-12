---
title: "Additional drive"
---

# Additional drive

An additional drive takes the running data and the backups off the SD card. This
protects the card, which wears out under constant writes, and gives the node
room for backups. The drive is set up under *Settings, Storage*. The tab is only
shown when your account has the permission *Manage storage*
(`storage_manage`). Setting up and removing the drive work only over HTTPS.

## What moves to the drive

- **Running data**: statistics, the automation history and the battery state.
  The configuration stays on the system drive, so the node starts without the
  drive too.
- **Backups**: existing archives move along, and new backups go to the drive.

You choose both parts in the setup. Each one can be left out.

## Set up an additional drive

1. Plug in the drive. It appears in the list by itself. The system drive is
   never offered.
2. Choose the drive and go on. A drive that holds a file system other than
   ext4 must be formatted first. The node shows this with a warning, and you
   confirm that the data on the drive gets deleted.
3. Choose what moves to the drive and what happens when the drive is missing.
4. The node copies the data, checks every file and switches the services over.
   Running data stops for a short moment while the last changes are copied. The
   dashboard restarts at the end, and this page reconnects by itself.

The setup does not start while another job runs.

## When the drive is missing

If the drive is unplugged or fails, the node keeps running. The page shows the
state *Not reachable* and says where the running data goes now. You choose
between two places:

- **System drive**: nothing is lost. The SD card takes the load until the drive
  returns, and the data is merged back when it is plugged in again.
- **Memory**: the SD card stays protected. A restart erases the running data
  that was written since the drive went missing.

A change of this choice applies at the next start of the node.

## Remove the additional drive

*Remove additional drive* copies the running data and the backups back to the system drive
and stops using the drive. The drive keeps its content. The drive must be
connected for the removal.

*Switch additional drive* moves the running data and the backups to another drive while the
current one is connected. The old drive stays unchanged. *Replace additional drive* is
offered when the drive is missing: the new drive gets the replacement data from
the system drive, and the backups on the old drive are lost.

## Limits

- The node uses one additional drive.
- The drive must be ext4 for the node. The setup formats other file systems,
  and that deletes the data on the drive.
- The setup restarts the dashboard. Do not unplug the drive during the setup.

## Related

- [Settings: Backup](../dashboard/settings-backup.md) explains the archives,
  which can also live on the drive.
- [Data flows: Storage](../developing/data-flow-storage.md) lists the files and
  the setup steps.
