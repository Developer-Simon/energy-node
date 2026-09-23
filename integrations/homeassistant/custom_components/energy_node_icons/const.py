"""Konstanten der Icon-Integration."""

DOMAIN = "energy_node_icons"
# Unter diesem Pfad liefert Home Assistant das Modul aus; der Name taucht in
# jeder Dashboard-URL-Liste auf, deshalb der Domain-Prefix.
FRONTEND_URL_BASE = "/energy_node_icons_frontend"
FRONTEND_FILES = ("energy-node-icons.js",)
# Der Prefix, unter dem die Symbole waehlbar sind: energy-node:solar-panel.
ICON_PREFIX = "energy-node"
