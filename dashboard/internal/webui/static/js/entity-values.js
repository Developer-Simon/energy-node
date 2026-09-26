// Gemeinsame Primitive fuers Nachziehen von Entitaetswerten.
//
// Zwei Module ziehen Werte nach: overview-values.js (Wertkarten und
// Gruppen-Chips der Uebersicht) und device-tile-values.js (Geraetekacheln im
// Geraete-Tab und als "device"-Karte im Raster). Beide muessen dieselben
// Fallunterscheidungen treffen - "fehlt im Push", "Befehl haengt",
// "Verfuegbarkeit unbekannt". Steht das zweimal da, laufen die Ansichten
// frueher oder spaeter auseinander; genau diese Doppelung kostet
// classifyEntityCategory heute schon einmal (Go und JS).
(() => {
  const t = (key, params) => (window.I18n ? window.I18n.t(key, params) : key);

  const PENDING_MESSAGE_KEY = 'entity.command_status.awaiting';
  const TIMEOUT_MESSAGE_KEY = 'entity.timeout';

  // Fehlt eine Entitaet im Push, gilt sie als unbekannt - nicht als
  // unveraendert. Sonst zeigte eine geloeschte Entitaet ihre letzte Zahl
  // weiter, als waere sie aktuell.
  const MISSING = {value: '', has_value: false, available: false, has_availability: false, stale: false, pending: false, last_command_result: ''};

  const merge = value => ({...MISSING, ...(value || {})});

  function statusMessage(value) {
    if (value.pending) return t(PENDING_MESSAGE_KEY);
    if (value.last_command_result === 'timeout') return t(TIMEOUT_MESSAGE_KEY);
    return '';
  }

  function setDotClass(dot, prefix, value) {
    if (!dot) return;
    for (const suffix of ['ok', 'bad', 'warn', 'info', 'unknown']) dot.classList.remove(`${prefix}-${suffix}`);
    dot.classList.add(`${prefix}-${value}`);
  }

  function availabilityState(value) {
    if (!value.has_availability) return 'unknown';
    return value.available ? 'ok' : 'bad';
  }

  // Der Rohwert muss auch dann erhalten bleiben, wenn renderTimestamps() in
  // dashboard.js den Text durch eine relative Angabe ersetzt - deshalb
  // steht er im data-Attribut und nicht nur im Text.
  function timestampNode(document, value) {
    const time = document.createElement('time');
    time.dataset.relativeTimestamp = value;
    time.textContent = value;
    return time;
  }

  window.entityValues = {PENDING_MESSAGE_KEY, TIMEOUT_MESSAGE_KEY, MISSING, merge, statusMessage, setDotClass, availabilityState, timestampNode};
})();
