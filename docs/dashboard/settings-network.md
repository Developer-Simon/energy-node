---
title: "Settings: Network"
---

# Settings: Network

The tab **Network** under *Settings*. It shows how the node is connected to the
network, through the LAN cable and through Wi-Fi. You can scan for Wi-Fi
networks, connect to one, and forget a saved one. The tab is only shown when
your account has the permission *Set up network* (`network`). Changes work
only over HTTPS. The permission is set under
[Settings, Users](settings-users.md#permissions).

| Permission | Key | Allows |
|---|---|---|
| Set up network | `network` | See the connection, scan for Wi-Fi networks, connect, and forget saved networks |

## Connection

The top card shows the two ways the node can reach the network. Each one has
its own state:

| State | Meaning |
|---|---|
| Connected | The link is up and has an address |
| Not connected | The link is present but has no connection |
| Switched off | Wi-Fi is switched off by rfkill |
| No cable | No cable is plugged into the LAN port |
| No module | This device has no Wi-Fi module |
| Error | The link has a fault |

The link the node uses first is marked *Used primarily*. Each link
shows facts such as the IP address, the gateway, the DNS server, the speed of
the cable, or the network name, band and signal strength of Wi-Fi. The status
line shows *Internet reachable* or *No internet*.

If the node does not use NetworkManager, the page only shows the status. A
note says so, and all changes are locked.

A device without a Wi-Fi module shows a note in place of the network list. A
USB Wi-Fi stick that Linux supports appears there once it is plugged in.

## Wi-Fi networks

The list shows the networks in range. Each row shows the
signal strength, the band, the security, and the signal in dBm. Encrypted
networks have a lock. A network that is connected is marked *Connected*, and
one that is saved on the node is marked *Saved*. The line above the list shows
how many networks were found and how long ago the search ran.

Use **Scan again** to search for networks. The search takes a few seconds.

Open a row to see its actions.

### Connecting

- For an encrypted network that is not saved, enter the Wi-Fi password. It
  must have at least 8 characters. The password is only sent with the request
  and is not kept on the page afterwards.
- For a saved network the password is already stored, so you only press
  **Connect**.
- While the node connects, the steps are shown one after another: connecting
  to the network, obtaining an address, and checking the gateway and the
  internet.

The result is either *Connected*, with the address of the dashboard in the new
network, or *Back to the previous connection*, with the reason.

If you are connected over Wi-Fi when you start a connection, the new network
has to work within 90 seconds. If it does not, the node returns to the old
connection after 90 seconds. If the LAN cable is the active connection, the LAN stays
active during the attempt, and the failed network does not replace it.

The errors are:

| Message | Meaning |
|---|---|
| Password rejected | The network refused the password |
| Connection failed | The node could not connect |
| Network not found | The network was not in range during the attempt |
| No address received | The network did not assign an address |
| No gateway found | The network has no gateway |

The last failed attempt is shown with its network name, the time, and the
error.

When the LAN cable has a network, the saved Wi-Fi network does not take over
from it. The LAN stays the main connection as long as the cable is plugged in.

### Forgetting a network

**Forget** removes a saved network and its password from the node. The network
stays in the list while it is in range, but it is no longer marked as saved.

### Other networks

The entry **Other network** at the end of the list connects to a network that
is hidden or not in the list. Enter the network name and its password.

## Wi-Fi as backup

The card **Wi-Fi as backup** decides what Wi-Fi does while the LAN cable has a
network. The choice is saved as soon as you tap it, there is no save button.

| Choice | Behavior |
|---|---|
| Always ready | Wi-Fi stays connected alongside the cable, and the cable has priority. If the cable fails, everything continues over Wi-Fi without a pause. This is the default. |
| Only on failure | Wi-Fi stays off as long as the cable has an address. If the cable or its address is missing for 30 seconds, the node connects to a saved Wi-Fi network. It disconnects again as soon as the cable is back. |

In *Only on failure* the node also disconnects a Wi-Fi network you connected
by hand, within 15 seconds, as long as the cable has a network. To test a
Wi-Fi network, pull the cable first or choose *Always ready*.

Without a saved Wi-Fi network the card says so, because the node then has
nothing to fall back to. A device without a Wi-Fi module shows the card as not available.

## Emergency hotspot

If the node has neither LAN nor Wi-Fi for 5 minutes, it opens a Wi-Fi network of
its own. Connect a phone or laptop to it, open `https://10.42.0.1`
and set up the Wi-Fi network of the building. The switch **Open a hotspot when
no network is reachable** turns this on or off. It is on by default.

- The network is called `EnergyNode-` followed by four characters, for example
  `EnergyNode-4F2A`. The password has three short words and two digits, for
  example `sonne-wolke-42`. Both are created once per node, unless you set your
  own password in the installer, see [Configuration](../installer.md#3-configuration).
- The installer shows name and password on its [result screen](../installer.md#6-result).
  Write them down. The page shows them in plain text to everyone who may use
  the network page, even while the hotspot switch is off. The node is asked when
  the page opens, and the password is not kept after you leave the page.
- The hotspot closes as soon as the cable has a network again. While nobody is
  connected to it, the node checks every 5 minutes whether a known Wi-Fi network is
  back, and closes the hotspot if one is.
- The hotspot uses the 2.4 GHz band. Without a country code for Wi-Fi it uses
  channel 1, and the page shows a note.
- The WLAN profiles and the hotspot password are not part of a
  [backup](settings-backup.md).
