Template: energy-node/too-small
Type: error
Description: Internal disk too small
 The internal disk ${DISK} has ${SIZE} GB. Energy Node needs at least 3 GB.
 .
 Nothing has been written. Press Enter to switch the device off.
Description-de.UTF-8: Interne Platte zu klein
 Die interne Platte ${DISK} hat ${SIZE} GB. Energy Node braucht mindestens 3 GB.
 .
 Es wurde nichts geschrieben. Enter schaltet das Gerät aus.

Template: energy-node/no-disk
Type: error
Description: No internal disk found
 This device has no internal disk to install on. USB drives and SD cards are never used.
 .
 Nothing has been written. Press Enter to switch the device off.
Description-de.UTF-8: Keine interne Platte gefunden
 Das Gerät hat keine interne Platte für die Installation. USB-Laufwerke und SD-Karten werden nie verwendet.
 .
 Es wurde nichts geschrieben. Enter schaltet das Gerät aus.

Template: energy-node/many-disks
Type: error
Description: More than one internal disk
 This device has several internal disks: ${DISKS}. The stick does not guess which one to erase.
 .
 Nothing has been written. Remove all but one disk or install Debian by hand. Press Enter to switch the device off.
Description-de.UTF-8: Mehr als eine interne Platte
 Das Gerät hat mehrere interne Platten: ${DISKS}. Der Stick rät nicht, welche gelöscht werden soll.
 .
 Es wurde nichts geschrieben. Bis auf eine Platte alle entfernen oder Debian von Hand installieren. Enter schaltet das Gerät aus.

Template: energy-node/confirm
Type: boolean
Default: false
Description: Erase ${DISK} and install Debian?
 Disk ${DISK} (${MODEL}, ${SIZE} GB) will be erased completely. Everything on it is lost.
 .
 Choose No to switch the device off without writing anything.
Description-de.UTF-8: ${DISK} löschen und Debian installieren?
 Die Platte ${DISK} (${MODEL}, ${SIZE} GB) wird vollständig gelöscht. Alles darauf geht verloren.
 .
 Mit Nein schaltet sich das Gerät aus, ohne etwas zu schreiben.
