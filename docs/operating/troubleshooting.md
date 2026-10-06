---
title: "Troubleshooting"
---

# Troubleshooting

## Common symptoms

| Symptom | Likely cause |
|---|---|
| Node cannot ping or route to any other tailnet peer | `--tun=userspace-networking` is set in `/etc/default/tailscaled` — remove it ([section 3.1](../install/index.md#31-tailscale--install-1620-then-update)) |
| A client's traffic to the main site's LAN breaks after IP forwarding is enabled, SSH stays up | Node still advertises a subnet route — `sudo tailscale up --reset --advertise-routes=` ([section 3.1](../install/index.md#31-tailscale--install-1620-then-update)) |
| Node drops off the tailnet after months | Key expiry was not disabled in the admin console |
| `pip` refuses with "externally-managed-environment" | Missing `--break-system-packages` ([section 3.2](../install/index.md#32-python-packages-on-an-externally-managed-system)) |
| A service starts, then dies immediately | `/etc/energy-node/config.json` or `mqtt.pw` missing or unreadable for group `energynode` — fail-closed by design |
| Services run an old version of the shared package | The wheel was installed for a different interpreter than the unit's `/usr/bin/python3`; reinstall and `systemctl restart` the services |
| Devices show as unavailable in Home Assistant, no errors in the log | Check the bridge really connected to the broker; the bridges log paho-internal callback exceptions only because `enable_logger()` is on |
| Dashboard reachable but admin login rejected | Admin sessions require HTTPS — use the Caddy endpoint, not port 8080 |
| Sporadic freezes / throttling on a Pi 1 | Power supply. Watch the node's own undervoltage sensor |

## Diagnosing with the installer

The desktop installer reads the node's state and explains what it finds.
See [Diagnosing a node](../installer.md#diagnosing-a-node).

## Home Assistant integrations

- [Energy Node Companion](../ha/companion.md#troubleshooting)
- [Battery SoC integration](../ha/battery-soc.md)
