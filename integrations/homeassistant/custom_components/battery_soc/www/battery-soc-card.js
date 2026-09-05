// Lovelace-Karte "Speicher-Status". Zweiter Wirt derselben Karte: gerechnet
// und gezeichnet wird in battery-card-core.js (vendoriert aus dem Dashboard,
// siehe scripts/vendor_card.py), hier steht nur die Uebersetzung von
// hass.states in das BatteryInput des Kerns.
//
// Konfiguration:
//   type: custom:battery-soc-card
//   display: column | trajectory      (Vorgabe: column)
//   soc_entity: sensor.speicher_soc_combined      (Pflicht)
//   power_entity: sensor.speicher_net_power       (+ = laedt)
//   capacity_kwh: 12.8
//   reserve_percent: 10               (0 = keine Reserve)
//   invert_power: false
//   runtime_entity: sensor.speicher_time_to_empty (optional)
//   window: 6                         (Stunden Verlauf und Fortschreibung, nur trajectory)
//   projection_window: 6              (optional, ueberschreibt nur die Fortschreibung)
//   title: Speicher
(() => {
  'use strict';

  const DEFAULT_RESERVE = 10;
  const DEFAULT_WINDOW_HOURS = 6;
  const REFRESH_MS = 60000;

  // window gilt fuer beide Haelften, projection_window ueberschreibt nur die
  // Fortschreibung. Ungueltiges oder fehlendes window -> {} und der Kern
  // nimmt seinen Sechs-Stunden-Default.
  function windowHours(config) {
    const win = Number(config.window);
    if (!(win > 0)) return {};
    const proj = Number(config.projection_window);
    return {historyHours: win, forecastHours: proj > 0 ? proj : win};
  }

  // Der Kern wird als eigene Datei geladen und ist bei der Registrierung
  // dieses Elements womoeglich noch nicht da. Statt eines harten Imports
  // wird auf das Global gewartet - Lovelace montiert die Karte ohnehin
  // asynchron. onTimer reicht die Interval-ID an den Aufrufer zurueck, damit
  // ein Element, das vor dem Laden des Kerns wieder entfernt wird, das
  // Polling stoppen kann (siehe disconnectedCallback) statt es bis in alle
  // Ewigkeit weiterlaufen zu lassen.
  const coreReady = onTimer => new Promise(resolve => {
    if (window.BatteryCardCore) { resolve(window.BatteryCardCore); return; }
    const timer = window.setInterval(() => {
      if (window.BatteryCardCore) { window.clearInterval(timer); resolve(window.BatteryCardCore); }
    }, 50);
    if (onTimer) onTimer(timer);
  });

  const numberState = (hass, entityId) => {
    const entity = entityId && hass && hass.states ? hass.states[entityId] : null;
    if (!entity) return null;
    if (entity.state === 'unavailable' || entity.state === 'unknown' || entity.state === '') return null;
    const value = Number(entity.state);
    return Number.isFinite(value) ? value : null;
  };

  function inputFromHass(hass, config, history, nowTs) {
    const soc = numberState(hass, config.soc_entity);
    const rawWatts = numberState(hass, config.power_entity) || 0;
    const reserve = config.reserve_percent;
    const win = windowHours(config);
    return {
      soc,
      capacity: Number(config.capacity_kwh) || 0,
      watts: config.invert_power ? -rawWatts : rawWatts,
      reserve: Number.isFinite(Number(reserve)) ? Number(reserve) : DEFAULT_RESERVE,
      // Kein Wert heisst hier dasselbe wie "veraltet" im Dashboard: die
      // Karte zeigt den letzten Stand grau statt eine 0 zu behaupten.
      stale: soc === null,
      nowTs,
      history,
      runtimeHours: config.runtime_entity ? numberState(hass, config.runtime_entity) : null,
      historyHours: win.historyHours,
      forecastHours: win.forecastHours,
    };
  }

  // minimal_response spart die Attribute; die Antwort ist dann
  // {entity_id: [{s: Zustand, lu: Sekunden seit Epoche}]}.
  function historyReader(hass, entityId) {
    return async (fromTs, toTs) => {
      if (!hass || typeof hass.callWS !== 'function' || !entityId) return [];
      try {
        const answer = await hass.callWS({
          type: 'history/history_during_period',
          start_time: new Date(fromTs).toISOString(),
          end_time: new Date(toTs).toISOString(),
          entity_ids: [entityId],
          minimal_response: true,
          no_attributes: true,
        });
        return (answer[entityId] || []).map(row => ({ts: Math.round(row.lu * 1000), v: Number(row.s)}));
      } catch (error) {
        return [];
      }
    };
  }

  class BatterySocCard extends HTMLElement {
    static getStubConfig(hass) {
      const soc = Object.keys((hass && hass.states) || {})
        .find(id => id.startsWith('sensor.') && hass.states[id].attributes.device_class === 'battery');
      return {display: 'column', soc_entity: soc || '', capacity_kwh: 10, reserve_percent: DEFAULT_RESERVE};
    }

    setConfig(config) {
      if (!config || !config.soc_entity) {
        throw new Error('battery-soc-card: soc_entity ist erforderlich');
      }
      this._config = {display: 'column', invert_power: false, ...config};
      this._card = null;
      if (this.shadowRoot) this.shadowRoot.textContent = '';
      this._mount();
    }

    getCardSize() {
      return this._config && this._config.display === 'trajectory' ? 4 : 3;
    }

    set hass(hass) {
      this._hass = hass;
      this._paint();
      if (this._config.display === 'trajectory' && !this._timer) {
        this._refreshHistory();
        this._timer = window.setInterval(() => this._refreshHistory(), REFRESH_MS);
      }
    }

    disconnectedCallback() {
      if (this._timer) { window.clearInterval(this._timer); this._timer = null; }
      if (this._coreWaitTimer) { window.clearInterval(this._coreWaitTimer); this._coreWaitTimer = null; }
      if (this._card && this._card.destroy) this._card.destroy();
    }

    _mount() {
      if (window.BatteryCardCore) {
        this._finishMount(window.BatteryCardCore);
      } else {
        coreReady(timer => { this._coreWaitTimer = timer; }).then(core => {
          this._coreWaitTimer = null;
          this._finishMount(core);
        });
      }
    }

    _finishMount(core) {
      if (!this.shadowRoot) this.attachShadow({mode: 'open'});
      core.injectStyle(this.shadowRoot);
      const host = document.createElement('section');
      this.shadowRoot.append(host);
      this._core = core;
      this._card = this._config.display === 'trajectory' ? core.mountTrajectory(host) : core.mountColumn(host);
      this._history = [];
      this._paint();
    }

    async _refreshHistory() {
      if (!this._core || !this._hass) return;
      const now = Date.now();
      const win = windowHours(this._config);
      const spanMs = (win.historyHours > 0 ? win.historyHours : DEFAULT_WINDOW_HOURS) * 3600000;
      this._history = await this._core.readHistory(
        historyReader(this._hass, this._config.soc_entity), now - spanMs, now);
      this._paint();
    }

    _paint() {
      if (!this._card || !this._hass) return;
      const input = inputFromHass(this._hass, this._config, this._history || [], Date.now());
      this._card.update(this._core.viewFrom(input));
    }
  }

  BatterySocCard.inputFromHass = inputFromHass;
  BatterySocCard.historyReader = historyReader;
  BatterySocCard.windowHours = windowHours;

  if (!window.customElements.get('battery-soc-card')) {
    window.customElements.define('battery-soc-card', BatterySocCard);
  }
  window.customCards = window.customCards || [];
  // Ein zweites Laden derselben Datei (z. B. ein erneutes add_extra_js_url
  // nach einem Frontend-Reload) darf den Karten-Picker nicht doppelt fuehren.
  if (!window.customCards.some(entry => entry.type === 'battery-soc-card')) {
    window.customCards.push({
      type: 'battery-soc-card',
      name: 'Speicher-Status',
      description: 'Vorrat, Restlaufzeit und Verlauf des Batteriespeichers.',
    });
  }
})();
