"""Nachweis fuer die Node-ID-Normalisierung energy-node -> energy_node.

Kontext: MQTT-Topics, HA-identifiers, via_device, unique_id. NICHT betroffen:
Pfade (/etc/energy-node/, energy-node-dashboard*, energy-node.config.json).
"""

import re
import subprocess
from pathlib import Path

REPO = Path(__file__).resolve().parents[3]

# Zeilen, die energy-node NUR als Pfad/Dateiname/Unit fuehren, sind erlaubt.
ALLOWED = re.compile(
    r"/etc/energy-node/|energy-node-dashboard|energy-node\.service|energy-node\.config\.json|energy-node-bridge|Repo|clone"
)


def test_no_hyphen_node_id_in_mqtt_context():
    out = subprocess.run(
        ["git", "grep", "-n", "energy-node", "--", "services/", "libs/"],
        cwd=REPO, capture_output=True, text=True,
    ).stdout.splitlines()
    offenders = [
        line for line in out
        if not ALLOWED.search(line)
        and ("via_device" in line or "outstation/energy-node" in line
             or "identifiers" in line or "unique_id" in line
             or "homeassistant/" in line)
    ]
    assert offenders == [], "energy-node (Bindestrich) im MQTT-Kontext:\n" + "\n".join(offenders)
