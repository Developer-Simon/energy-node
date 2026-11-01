"""Textkatalog-Renderer für Meldungen, die über Textschlüssel adressiert werden.

Reines stdlib, keine Abhängigkeiten. Dieses Modul wird zusätzlich in die
HA-Integration vendored, daher darf es nichts importieren, das dort fehlt.

Ein Katalog ordnet Sprachcodes Textschlüssel zu, zum Beispiel
{"de": {"a": "{n} Stück"}, "en": {"a": "{n} pieces"}}. Die Sprache wird in
dieser Reihenfolge gesucht: der vollständige Sprachcode (klein geschrieben),
dann sein Hauptteil vor "-" oder "_", dann Englisch. Der erste Katalog, der
den Schlüssel enthält, gewinnt.
"""

from __future__ import annotations

from typing import Any, Mapping, Optional


def _language_chain(lang: Optional[str]) -> list[str]:
    chain: list[str] = []
    if lang:
        full = lang.strip().lower()
        if full:
            chain.append(full)
            primary = full.replace("_", "-").split("-", 1)[0]
            if primary and primary not in chain:
                chain.append(primary)
    if "en" not in chain:
        chain.append("en")
    return chain


def render(
    catalogs: Mapping[str, Mapping[str, str]],
    lang: Optional[str],
    key: str,
    params: Optional[Mapping[str, Any]] = None,
) -> Optional[str]:
    """Liefert den formatierten Text für `key` in der besten verfügbaren Sprache.

    Gibt None zurück, wenn kein Katalog den Schlüssel enthält oder die
    Formatierung fehlschlägt (fehlender Parameter, unpassende Klammern).
    """
    for code in _language_chain(lang):
        catalog = catalogs.get(code)
        if catalog is None or key not in catalog:
            continue
        try:
            return catalog[key].format_map(params or {})
        except (KeyError, ValueError, IndexError):
            return None
    return None
