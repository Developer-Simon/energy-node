"""Batterie-Konfiguration und Validierung fuer battery_soc_mqtt.py.

Enthaelt die BatteryConfig-Dataclass, Konfigurationsladung mit Validierung,
und die Projektion zu SocParams fuer Physik-Checks."""

import dataclasses
from dataclasses import dataclass
from pathlib import Path
from typing import Optional

from energy_node_common import config as common_config
from battery_soc_core.params import SocParams

SOC_PARAM_FIELDS = frozenset(SocParams.field_names())


@dataclass(frozen=True)
class BatteryConfig:
    id: str
    name: str
    via_device: str = "energy-node"
    # Leerer json_key = das Payload ist eine nackte Zahl (so publizieren die
    # Trucki-Sticks jedes Feld). Hier steht bewusst KEIN Herstellername wie
    # "apower": das Dashboard speichert ein leeres optionales Feld als
    # weggelassenen Key, "kein JSON-Key" waere mit einem geratenen Default
    # ueber die Oberflaeche also gar nicht erreichbar. Vorschlaege liefert
    # dort die <datalist> aus dem zuletzt gesehenen Payload.
    charger_power_topic: str = ""
    charger_power_json_key: str = ""
    inverter_power_topic: str = ""
    inverter_power_json_key: str = ""
    # Optionale DC-seitige Leistungsmessung je Wandler (z. B. DCPOWER vom
    # Trucki T2MG). Ist sie konfiguriert und frisch, ersetzt sie den
    # AC-Eingang derselben Seite - ohne Wirkungsgradfaktor, denn dieser Wert
    # steht schon auf dem Gleichstrombus. Wird sie aelter als dc_max_age_s,
    # uebernimmt wieder die AC-Messung.
    charger_dc_power_topic: str = ""
    charger_dc_power_json_key: str = ""
    inverter_dc_power_topic: str = ""
    inverter_dc_power_json_key: str = ""
    dc_max_age_s: float = 60.0
    bank_a_voltage_topic: str = ""
    bank_a_voltage_json_key: str = ""
    bank_a_voltage_scale: float = 1.0
    bank_b_voltage_topic: str = ""
    bank_b_voltage_json_key: str = ""
    bank_b_voltage_scale: float = 1.0
    bank_a_cell_count: int = 8
    bank_a_capacity_ah: float = 100.0
    bank_b_cell_count: int = 8
    bank_b_capacity_ah: float = 100.0
    # "parallel": beide Baenke haengen am selben Gleichstrombus und sind
    # elektrisch EINE Batterie - es gibt nur eine Busspannung.
    # "series": die Baenke sind gestapelt, fuehren denselben Strom und haben
    # je eine eigene, aussagekraeftige Spannung.
    topology: str = "parallel"
    # Beide Shellys messen die NETZseite, der Coulomb-Zaehler rechnet aber auf
    # dem Gleichstrombus - je Wandler ein eigener Wirkungsgrad.
    charger_ac_dc_efficiency: float = 0.9
    inverter_dc_ac_efficiency: float = 0.9
    # Rein coulombscher Term (Ladungsverlust IN der Zelle), nicht Wandlerverlust.
    charge_efficiency: float = 0.98
    # Reine Ablage fuer spaetere Chemien - Kalibrierschwellen und Simulations-
    # kurve gehen bisher unveraendert von LiFePO4 aus.
    battery_chemistry: str = "lifepo4"
    # Schluessel aus SOC_CURVES. Die Kurve legt nur die FORM fest; wo sie
    # absolut liegt, entscheiden die beiden Schwellen darunter.
    soc_curve: str = "generic_lifepo4"
    empty_v_per_cell: float = 2.7
    full_v_per_cell: float = 3.5
    # Optionaler Override der LOAD_OFFSET_TABLE_MV: None = Tabelle benutzen.
    internal_resistance_mohm_per_cell: Optional[float] = None
    # Wie weit die Kalibrierschwellen bei kleinem Strom aufgeweicht werden
    # duerfen, siehe CALIBRATION_TAPER_C_RATE. 0 schaltet das Fenster ab und
    # stellt das alte Verhalten mit harten Schwellen wieder her.
    calibration_tolerance_v_per_cell: float = 0.08
    full_taper_c_rate: Optional[float] = None
    calibration_tolerance_empty_v_per_cell: Optional[float] = None
    calibration_tolerance_full_v_per_cell: Optional[float] = None
    calibration_grace_s: float = 0.0
    bank_b_enabled: bool = True
    imbalance_warn_v: float = 0.5
    calibration_hold_s: float = 120.0
    # Grobe Plausibilitaetspruefung: weicht die aus der (lastkorrigierten)
    # Spannung geschaetzte SoC zu stark vom Coulomb-Zaehler ab, deutet das auf
    # eine falsch kalibrierte Kapazitaet oder einen driftenden Zaehler hin. Im
    # flachen LiFePO4-Mittelbereich ist die Spannungsschaetzung selbst sehr
    # unsicher - daher die grosszuegige Standardschwelle und eine Haltezeit
    # gegen einzelne Ausreisser, analog zu calibration_hold_s.
    voltage_soc_mismatch_warn_pct: float = 25.0
    voltage_mismatch_hold_s: float = 300.0
    state_file: Path = Path("/home/energynode/battery_soc/state.json")
    stale_input_s: float = 120.0
    # False (Standard): ein veralteter Eingang haelt die Berechnung NICHT an -
    # veraltete Energiefluss-Topics (Lade-/Umrichterleistung) zaehlen als 0 W,
    # veraltete Spannungsmessungen behalten ihren letzten bekannten Wert und
    # zaehlen weiter (nur die 0%/100%-Kalibrierung bleibt IMMER an eine
    # frische Spannung gebunden, unabhaengig von diesem Schalter). True
    # erzwingt das alte, strengere Verhalten: die Coulomb-Zaehlung pausiert
    # komplett, sobald irgendein konfigurierter Eingang veraltet ist.
    require_fresh_inputs: bool = False
    simulation_charger_power_w: float = 200.0
    simulation_inverter_power_w: float = 50.0

    @property
    def base_topic(self):
        return f"outstation/{self.id}"

    def soc_params(self) -> SocParams:
        """Projektion zu SocParams fuer Physik-Validierung. Nur die Felder
        in SocParams werden verwendet; Transport- und Persistenz-Felder
        (bank_*_voltage_topic, state_file, etc.) fallen weg."""
        return SocParams.from_dict(dataclasses.asdict(self))


def input_topics(config):
    """Alle Eingangs-Topics dieser Anlage. Bank B hat nur in Reihenschaltung
    eine eigene Spannung - parallel liegt beiden dieselbe Busspannung an, und
    ein zweites Topic waere dort gar nicht erst erlaubt."""
    topics = [config.charger_power_topic, config.charger_dc_power_topic,
              config.inverter_power_topic, config.inverter_dc_power_topic,
              config.bank_a_voltage_topic]
    if config.topology == "series" and config.bank_b_enabled:
        topics.append(config.bank_b_voltage_topic)
    return topics


def load_configs(path):
    raw = common_config.load_json(path)
    if not isinstance(raw, list) or not raw:
        raise ValueError(f"Batterie-Konfiguration {path} muss eine nichtleere JSON-Liste sein")

    configs = []
    for index, item in enumerate(raw):
        if not isinstance(item, dict):
            raise ValueError(f"Eintrag {index} in {path} ist kein Objekt")
        missing = {"id", "name"} - set(item)
        if missing:
            raise ValueError(f"Eintrag {index} in {path} fehlt Pflichtfelder: {sorted(missing)}")
        values = dict(item)
        values["id"] = str(values["id"]).strip()
        values["name"] = str(values["name"]).strip()
        if not values["id"] or not values["name"]:
            raise ValueError(f"Eintrag {index} in {path} braucht nichtleere id und name")
        values["state_file"] = Path(values.get("state_file", "/home/energynode/battery_soc/state.json"))
        try:
            config = BatteryConfig(**values)
        except (TypeError, ValueError) as exc:
            raise ValueError(f"Ungueltige Batterie-Konfiguration in Eintrag {index}: {exc}") from exc

        # Physik-Validierung: delegiere an SocParams.validate()
        try:
            config.soc_params().validate()
        except ValueError as exc:
            raise ValueError(f"Ungueltige Batterie-Konfiguration in Eintrag {index}: {exc}") from exc

        # Transport/Struktur-Checks: bleiben hier
        if config.topology == "parallel":
            if config.bank_b_voltage_topic:
                raise ValueError(
                    f"bank_b_voltage_topic ist bei topology=parallel nicht erlaubt "
                    f"({config.id}): beide Baenke haengen am selben Bus, ihre "
                    f"Klemmenspannung ist dieselbe Groesse. Eine Messung genuegt "
                    f"(bank_a_voltage_topic)."
                )
        else:
            if not config.bank_a_voltage_topic or not config.bank_b_voltage_topic:
                raise ValueError(
                    f"topology=series braucht beide Spannungs-Topics ({config.id}): "
                    f"gestapelte Baenke haben je eine eigene, aussagekraeftige Spannung"
                )
            if config.bank_a_voltage_topic == config.bank_b_voltage_topic:
                raise ValueError(
                    f"topology=series braucht verschiedene Spannungs-Topics "
                    f"({config.id}): {config.bank_a_voltage_topic}"
                )
        configs.append(config)

    ids = [config.id for config in configs]
    if len(ids) != len(set(ids)):
        raise ValueError(f"Doppelte Batterie-IDs in {path}: {ids}")
    topics = {}
    for config in configs:
        for topic in input_topics(config):
            if topic:
                topics.setdefault(topic, []).append(config.id)
    # Zwei verschiedene Fehler, die frueher zusammenfielen: dasselbe Topic in
    # ZWEI Anlagen (echte Kollision) und dasselbe Topic ZWEIMAL in derselben
    # Anlage. Letzteres meldete frueher "mehreren Batterieanlagen" und nannte
    # dieselbe id doppelt - eine irrefuehrende Meldung.
    shared = {topic: sorted(set(ids)) for topic, ids in topics.items()
              if len(set(ids)) > 1}
    if shared:
        raise ValueError(f"Eingangs-Topics mehreren Batterieanlagen zugeordnet: {shared}")
    doubled = {topic: ids[0] for topic, ids in topics.items()
               if len(ids) > 1 and len(set(ids)) == 1}
    if doubled:
        raise ValueError(
            f"Dasselbe Topic mehrfach in derselben Batterieanlage: {doubled}"
        )
    return configs
