"""engine.py — the transport-free heart of the SoC service.

`tick()` consumes SocParams + SocState + SocInputs and returns a TickResult
whose `.outputs` dict is byte-for-byte what battery_soc_mqtt.compute_and_publish
publishes to `<base_topic>/state`. No MQTT, no Home Assistant, no simulation
substitution, no persistence — those live in the adapter.
"""
from __future__ import annotations

from dataclasses import dataclass
from typing import List, Optional

from .calibration import (
    apply_calibration, apply_voltage_plausibility, calibration_tolerance,
    corrected_voltage_per_cell, voltage_based_soc_pct,
)
from .curves import MAX_TICK_HOURS, MIN_TIME_ESTIMATE_W
from .electrical import estimate_current
from .inputs import sample_is_fresh, stale_groups


# ---------------------------------------------------------------------------
# Strommodell und Coulomb-Zaehlung
# ---------------------------------------------------------------------------
def unit_currents(params, net_power_w, voltages):
    """Strom je gefuehrter Einheit.

    Parallel: eine Einheit, durch die der gesamte Netto-Strom laeuft.
    Serie: beide Baenke fuehren denselben Strom, bestimmt aus der
    SUMMENspannung des Stapels - nicht aus der jeweiligen Bankspannung."""
    if params.topology == "series":
        if any(v is None for v in voltages):
            return [None, None]
        current_a = estimate_current(net_power_w, sum(voltages))
        return [current_a, current_a]
    return [estimate_current(net_power_w, voltages[0])]


def integrate_coulomb(params, bank, current_a, dt_hours):
    """Getrennt von estimate_current(), damit der Strom auch dann publiziert
    werden kann, wenn nicht integriert werden darf (veraltete Eingaenge)."""
    if current_a is None or dt_hours <= 0:
        return
    efficiency = params.charge_efficiency if current_a > 0 else 1.0
    delta_ah = current_a * dt_hours * efficiency
    bank.coulomb_ah = max(0.0, min(bank.capacity_ah, bank.coulomb_ah + delta_ah))


def time_estimates(net_power_w, units, voltage_v, stale):
    """Zeit bis voll / bis leer fuer das Gesamtpaket, in Stunden.

    `voltage_v` ist die Spannung, an der der Paketstrom haengt: parallel die
    Busspannung, in Reihe die Summenspannung des Stapels. Die nutzbare Ladung
    ist in Reihe die der SCHWAECHSTEN Bank - der Stapel ist leer, sobald eine
    Bank leer ist -, parallel schlicht die des einen Zaehlers.

    Bewusst ungefiltert: dieser Dienst glaettet nirgends."""
    if stale or voltage_v is None or voltage_v <= 0 or abs(net_power_w) < MIN_TIME_ESTIMATE_W:
        return None, None
    current_a = net_power_w / voltage_v
    remaining_ah = min(unit.coulomb_ah for unit in units)
    headroom_ah = min(unit.capacity_ah - unit.coulomb_ah for unit in units)
    if current_a > 0:
        return round(min(999.0, headroom_ah / current_a), 2), None
    return None, round(min(999.0, remaining_ah / abs(current_a)), 2)


# ---------------------------------------------------------------------------
# Leistungsbilanz
# ---------------------------------------------------------------------------
@dataclass(frozen=True)
class EffectivePower:
    net_power_w: float
    charger_source: str          # "ac" / "dc"
    inverter_source: str         # "ac" / "dc"
    ac_fallback_active: bool


def effective_power(params, inputs, now, groups_stale) -> EffectivePower:
    """Netto-Batterieleistung nach Wandler-Wirkungsgrad.

    Wirkungsgrad JE Wandler anwenden, dann erst netto rechnen - beide Shellys
    messen die NETZseite, und beide Wandler koennen gleichzeitig laufen, so dass
    sich die Verluste nicht gegeneinander wegkuerzen. max(0.0, ...) faengt die
    kleinen negativen Messwerte ab, die ein Shelly Plug S liefern kann und die
    die Division sonst verstaerken wuerde.

    Liegt fuer eine Seite ein frischer DC-Messwert vor, ersetzt er die
    AC-Messung dieser Seite komplett - und OHNE Wirkungsgrad, denn er steht
    schon auf dem Gleichstrombus. Beide Seiten entscheiden das unabhaengig."""
    charger_power_w = inputs.charger_power_w
    inverter_power_w = inputs.inverter_power_w

    if sample_is_fresh(inputs.charger_dc_power_configured,
                       inputs.charger_dc_power_ts, now, params.dc_max_age_s):
        dc_charge_w = max(0.0, inputs.charger_dc_power_w)
        charger_source = "dc"
    else:
        dc_charge_w = max(0.0, charger_power_w) * params.charger_ac_dc_efficiency
        charger_source = "ac"
    if sample_is_fresh(inputs.inverter_dc_power_configured,
                       inputs.inverter_dc_power_ts, now, params.dc_max_age_s):
        dc_discharge_w = max(0.0, inputs.inverter_dc_power_w)
        inverter_source = "dc"
    else:
        dc_discharge_w = max(0.0, inverter_power_w) / params.inverter_dc_ac_efficiency
        inverter_source = "ac"

    # Lenient-Modus (require_fresh_inputs=False, Standard): ein veralteter
    # Energiefluss-Eingang liefert keine verlaessliche Leistung mehr - 0 W ist
    # die sichere Annahme. Im strengen Modus greift stattdessen der komplette
    # Rechenstopp der Coulomb-Zaehlung in tick().
    if not params.require_fresh_inputs:
        if groups_stale.get("Ladeleistung"):
            dc_charge_w = 0.0
        if groups_stale.get("Umrichterleistung"):
            dc_discharge_w = 0.0
    net_power_w = dc_charge_w - dc_discharge_w

    # Nur wo DC ueberhaupt konfiguriert ist, ist AC ein Rueckfall; sonst ist es
    # der Normalbetrieb und die Warnung stuende dauerhaft an.
    ac_fallback_active = (
        (inputs.charger_dc_power_configured and charger_source == "ac")
        or (inputs.inverter_dc_power_configured and inverter_source == "ac")
    )
    return EffectivePower(net_power_w, charger_source, inverter_source,
                          ac_fallback_active)


# ---------------------------------------------------------------------------
# Der reine Tick
# ---------------------------------------------------------------------------
@dataclass(frozen=True)
class TickResult:
    outputs: dict


def tick(params, state, inputs, now, *, dt_hours=None,
         voltages_override: Optional[List[Optional[float]]] = None) -> TickResult:
    units = state.units

    if dt_hours is None:
        dt_hours = (now - state.last_tick) / 3600.0
    # Schutz gegen Zeitspruenge (NTP-Korrektur, Suspend, haengender Scheduler).
    if dt_hours < 0 or dt_hours > MAX_TICK_HOURS:
        dt_hours = 0.0
    state.last_tick = now

    series = params.topology == "series"

    groups_stale = stale_groups(params, inputs, now)
    stale_names = [name for name, is_stale in groups_stale.items() if is_stale]
    stale = bool(stale_names)

    ep = effective_power(params, inputs, now, groups_stale)
    net_power_w = ep.net_power_w

    if voltages_override is not None:
        if len(voltages_override) != len(units):
            raise ValueError(
                f"voltages_override hat {len(voltages_override)} Werte, "
                f"erwartet {len(units)}"
            )
        voltages = list(voltages_override)
    elif series:
        voltages = [inputs.bank_a_voltage_v, inputs.bank_b_voltage_v]
    else:
        voltages = [inputs.bank_a_voltage_v]

    currents = unit_currents(params, net_power_w, voltages)
    corrected = [
        corrected_voltage_per_cell(voltage_v, unit.cell_count, current_a,
                                   unit.capacity_ah,
                                   params.internal_resistance_mohm_per_cell)
        for unit, voltage_v, current_a in zip(units, voltages, currents)
    ]

    # Die 0%/100%-Kalibrierung bleibt IMMER an eine frische Spannung gebunden,
    # unabhaengig von require_fresh_inputs: ein haengender Sensor duerfte sonst
    # ueber die Haltezeit hinweg eine falsche Voll-/Leerkalibrierung ausloesen.
    voltage_group_names = (["Spannung Bank A", "Spannung Bank B"] if series
                           else ["Busspannung"])
    for unit, corr, current_a, voltage_group in zip(units, corrected, currents,
                                                    voltage_group_names):
        voltage_stale = groups_stale.get(voltage_group, False)
        apply_calibration(params, unit, None if voltage_stale else corr, now,
                          current_a)
    calibration_tolerances = [
        calibration_tolerance(params, current_a, unit.capacity_ah)
        for unit, current_a in zip(units, currents)
    ]

    # Vollstaendiger Rechenstopp der Coulomb-Zaehlung nur im strengen Modus
    # (require_fresh_inputs=True). Im Lenient-Modus (Standard) laeuft sie mit
    # den in effective_power() schon entschaerften Werten weiter.
    if not (stale and params.require_fresh_inputs):
        for unit, current_a in zip(units, currents):
            integrate_coulomb(params, unit, current_a, dt_hours)

    # Grobe, spannungsbasierte Gegenprobe zum Coulomb-Zaehler.
    voltage_soc_estimates = [voltage_based_soc_pct(corr, params) for corr in corrected]
    for unit, voltage_soc_pct in zip(units, voltage_soc_estimates):
        apply_voltage_plausibility(params, unit, voltage_soc_pct, now)

    # Parallel gibt es genau einen Ladezustand. In Reihe ist das Paket so voll
    # wie seine schwaechste Bank - derselbe Strom hat sie alle durchflossen.
    soc_values = [unit.soc_pct for unit in units]
    soc_combined = None if any(v is None for v in soc_values) else min(soc_values)

    # Die Spannung, an der der Paketstrom haengt.
    pack_voltage_v = None
    if all(v is not None for v in voltages):
        pack_voltage_v = sum(voltages) if series else voltages[0]
    time_to_full_h, time_to_empty_h = time_estimates(
        net_power_w, units, pack_voltage_v, stale
    )

    outputs = {
        "soc_combined_pct": soc_combined,
        "net_power_w": round(net_power_w, 1),
        "charger_power_source": ep.charger_source,
        "inverter_power_source": ep.inverter_source,
        "ac_fallback_active": ep.ac_fallback_active,
        "inputs_stale": stale,
        "stale_inputs": ", ".join(stale_names),
        "time_to_full_h": time_to_full_h,
        "time_to_empty_h": time_to_empty_h,
    }
    for unit, voltage_v, current_a, corr, voltage_soc_pct, tolerance in zip(
        units, voltages, currents, corrected, voltage_soc_estimates,
        calibration_tolerances
    ):
        outputs.update({
            f"{unit.name}_voltage_v": voltage_v,
            f"{unit.name}_current_a": None if current_a is None else round(current_a, 2),
            f"{unit.name}_remaining_ah": round(unit.coulomb_ah, 2),
            f"{unit.name}_corrected_v_per_cell": None if corr is None else round(corr, 3),
            f"last_calibration_{unit.name}": unit.last_calibration_iso,
            f"{unit.name}_voltage_soc_pct": voltage_soc_pct,
            f"{unit.name}_voltage_soc_mismatch": unit.voltage_mismatch,
            f"{unit.name}_calibration_empty_v_per_cell":
                round(params.empty_v_per_cell + tolerance, 3),
            f"{unit.name}_calibration_full_v_per_cell":
                round(params.full_v_per_cell - tolerance, 3),
        })
    if series:
        outputs["soc_a_pct"] = units[0].soc_pct
        outputs["soc_b_pct"] = units[1].soc_pct
        delta = None
        if voltages[0] is not None and voltages[1] is not None:
            delta = round(voltages[0] - voltages[1], 3)
        outputs["voltage_delta_v"] = delta
        outputs["imbalance_warning"] = (
            delta is not None and abs(delta) > params.imbalance_warn_v
        )
    return TickResult(outputs=outputs)
