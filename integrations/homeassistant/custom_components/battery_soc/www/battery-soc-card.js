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

  // The core's own texts (BUILTIN_TEXTS in battery-card-core.js) are the
  // fallback for a bare page. This card knows hass.language, so it passes
  // its own table through instead - de word-for-word as BUILTIN_TEXTS, en
  // filled in once localization reaches the HA card (task 8).
  const CARD_TEXTS = {
    de: {
      'battery.card.no_source': 'keine Speicher-Quelle',
      'battery.card.no_source_note': 'Erst eine Entität als Ladezustand zuordnen.',
      'battery.card.holding': 'hält den Stand',
      'battery.card.idle_note': 'Weder Laden noch Entladen.',
      'battery.card.no_capacity_note': 'Ohne nutzbare Kapazität keine Restlaufzeit.',
      'battery.card.remaining': 'noch {time}',
      'battery.card.remaining_note': '{bound} bei {power} kW',
      'battery.card.bound.full': 'bis voll',
      'battery.card.bound.empty': 'bis leer',
      'battery.card.bound.reserve': 'bis Reserve',
      'battery.card.reserve_label': 'Reserve',
      'battery.card.reserve_note': 'bleibt für den Netzausfall stehen',
      'battery.card.stock_label': 'Vorrat',
      'battery.card.stock_note': 'bis 0 % nutzbar, keine Reserve gesetzt',
      'battery.card.coverage_label': 'Deckung',
      'battery.card.capacity_label': 'Kapazität',
      'battery.card.capacity_segment_note': 'ein Segment {value} kWh',
      'battery.card.capacity_missing_note': 'Kapazität hinterlegen',
      'battery.card.reserve_value': 'Reserve {value} %',
      'battery.card.stale': 'Werte veraltet',
      'battery.card.charging': 'Lädt · {power} kW',
      'battery.card.discharging': 'Entlädt · {power} kW',
      'battery.card.idle': 'Ruht',
      'battery.card.kpi.runtime_label': 'Restlaufzeit',
      'battery.card.kpi.usable_label': 'Abrufbar',
      'battery.card.kpi.flow_label': 'Fluss',
      'battery.card.crossing.full': 'voll',
      'battery.card.crossing.empty': 'leer',
      'battery.card.crossing.reserve': 'Reserve',
      'battery.card.hint.none': 'Kein Verlauf aufgezeichnet, nur die Fortschreibung.',
      'battery.card.hint.partial': 'Erst {hours} h aufgezeichnet.',
      'battery.card.now': 'jetzt',
      'battery.card.ticks.past': '−{hours} h',
      'battery.card.ticks.future': '+{hours} h',
      'battery.card.title.column': 'Speicher',
      'battery.card.title.trajectory': 'Speicher · Verlauf und Fortschreibung',
      'battery.card.percent_soc': '% Ladestand',
      'battery.card.forecast_footer': 'Fortschreibung bei konstanter Leistung',
      'battery.card.soc_aria': 'Ladestand {value}',
      'battery.card.chart_aria': 'Ladestand {value}. {hint}',
      'battery.card.chart_hint_default': 'Verlauf und Fortschreibung des Ladestands.',
    },
    en: {
      'battery.card.no_source': 'no battery source',
      'battery.card.no_source_note': 'Assign an entity as state of charge first.',
      'battery.card.holding': 'holding steady',
      'battery.card.idle_note': 'Neither charging nor discharging.',
      'battery.card.no_capacity_note': 'No runtime without usable capacity.',
      'battery.card.remaining': '{time} left',
      'battery.card.remaining_note': '{bound} at {power} kW',
      'battery.card.bound.full': 'until full',
      'battery.card.bound.empty': 'until empty',
      'battery.card.bound.reserve': 'until reserve',
      'battery.card.reserve_label': 'Reserve',
      'battery.card.reserve_note': 'kept back for a power cut',
      'battery.card.stock_label': 'Stored',
      'battery.card.stock_note': 'usable down to 0 %, no reserve set',
      'battery.card.coverage_label': 'Coverage',
      'battery.card.capacity_label': 'Capacity',
      'battery.card.capacity_segment_note': 'one segment {value} kWh',
      'battery.card.capacity_missing_note': 'Enter capacity',
      'battery.card.reserve_value': 'Reserve {value} %',
      'battery.card.stale': 'Values stale',
      'battery.card.charging': 'Charging · {power} kW',
      'battery.card.discharging': 'Discharging · {power} kW',
      'battery.card.idle': 'Idle',
      'battery.card.kpi.runtime_label': 'Runtime left',
      'battery.card.kpi.usable_label': 'Usable',
      'battery.card.kpi.flow_label': 'Flow',
      'battery.card.crossing.full': 'full',
      'battery.card.crossing.empty': 'empty',
      'battery.card.crossing.reserve': 'reserve',
      'battery.card.hint.none': 'No history recorded — projection only.',
      'battery.card.hint.partial': 'Only {hours} h recorded so far.',
      'battery.card.now': 'now',
      'battery.card.ticks.past': '−{hours} h',
      'battery.card.ticks.future': '+{hours} h',
      'battery.card.title.column': 'Battery',
      'battery.card.title.trajectory': 'Battery · history and projection',
      'battery.card.percent_soc': '% state of charge',
      'battery.card.forecast_footer': 'Projection at constant power',
      'battery.card.soc_aria': 'State of charge {value}',
      'battery.card.chart_aria': 'State of charge {value}. {hint}',
      'battery.card.chart_hint_default': 'History and projection of the state of charge.',
    },
  };

  function cardT(table) {
    return (key, params) => {
      const text = table[key] || key;
      return params ? text.replace(/\{(\w+)\}/g, (m, name) => (name in params ? String(params[name]) : m)) : text;
    };
  }

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

  // Mirrors Home Assistant's own number_format choices. "language" and
  // "system" follow the user's language or browser, the others force a
  // separator style regardless of language.
  const HA_NUMBER_LOCALES = {comma_decimal: 'en-US', decimal_comma: 'de', space_comma: 'fr'};

  function numberFormatter(hass) {
    const locale = (hass && hass.locale) || {};
    const choice = locale.number_format || 'language';
    const tag = HA_NUMBER_LOCALES[choice] || (choice === 'system' ? undefined : locale.language || (hass && hass.language));
    return (value, digits) => value.toLocaleString(tag, {
      minimumFractionDigits: digits,
      maximumFractionDigits: digits,
      useGrouping: choice !== 'none',
    });
  }

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
      formatNumber: numberFormatter(hass),
      t: cardT(BatterySocCard.texts(hass)),
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
    // hass.language is a bare tag ("de", "en-GB", ...). Only German is its
    // own table for now; every other tag falls back to English (task 8
    // fills CARD_TEXTS.en in, this only routes to it).
    static texts(hass) {
      const lang = ((hass && hass.language) || '').toLowerCase();
      return CARD_TEXTS[lang.startsWith('de') ? 'de' : 'en'];
    }

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
  BatterySocCard.numberFormatter = numberFormatter;
  BatterySocCard.historyReader = historyReader;
  BatterySocCard.windowHours = windowHours;
  BatterySocCard.CARD_TEXTS = CARD_TEXTS;

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
