// Die Verlaufskachel der Uebersicht (Layout-Typ history_view). Kein Alpine:
// #overview-live wird per htmx outerHTML getauscht, und eine Alpine-
// Komponente wuerde jedes Mal neu montiert - samt ApexCharts, das auf einem
// Pi-Browser spuerbar Zeit kostet. Der Container traegt hx-preserve, htmx
// verschiebt ihn samt Apex-Instanz in den neuen DOM. Dieses Modul haelt nur
// Buch: neue Container aufbauen, verschwundene abbauen, alle 60 s nachladen.
(() => {
  const t = (key, params) => (window.I18n ? window.I18n.t(key, params) : key);
  const REFRESH_MS = 60 * 1000;

  // DOM-ID -> {element, config, missing, chart, rows, gaps, knownSeries}
  const entries = new Map();
  let timer = null;

  // Dieselbe Einstellung, mit der die Energiekacheln den Hausverbrauch
  // rechnen - sie steht schon im Schnappschuss von #energy-snapshot-initial.
  const interpretation = () => {
    const node = document.getElementById('energy-snapshot-initial');
    if (!node) return null;
    try {
      return (JSON.parse(node.textContent) || {}).interpretation || null;
    } catch (error) {
      return null;
    }
  };

  const recorderConfig = () => (window.dashboardHistorizer ? window.dashboardHistorizer.config() : {rawWindowHours: 24, minuteWindowDays: 7});

  // An- und Abmeldung beim Austausch (Modus on-demand, siehe
  // history-exchange.js). Die DOM-ID ist die Kennung der Kachel.
  const watch = (id, range) => {
    if (window.dashboardHistorizer && window.dashboardHistorizer.watchRange) window.dashboardHistorizer.watchRange(id, range);
  };

  const unwatch = id => {
    if (window.dashboardHistorizer && window.dashboardHistorizer.unwatchRange) window.dashboardHistorizer.unwatchRange(id);
  };

  // Eine Lieferung kommt in Stuecken zu 500 Saetzen. Erst nach einer Sekunde
  // Ruhe nachladen, statt jedes Stueck einzeln zu zeichnen.
  const EXCHANGE_SETTLE_MS = 1000;
  let exchangeTimer = null;

  function setStatus(entry, text) {
    const status = entry.element.querySelector('[data-history-view-status]');
    if (!status) return;
    status.textContent = text || '';
    status.hidden = !text;
  }

  // Leere Serienliste heisst "alle aufgezeichneten", wie bei der Sicht.
  function visibleRows(entry) {
    const wanted = entry.config.series || [];
    if (!wanted.length) return entry.rows;
    const selected = new Set(wanted);
    return entry.rows.filter(row => selected.has(row.series));
  }

  function render(entry) {
    const plot = entry.element.querySelector('[data-history-view-plot]');
    if (!plot || !window.ApexCharts) return;
    const rows = visibleRows(entry);
    if (!rows.length) {
      if (entry.chart) {
        entry.chart.destroy();
        entry.chart = null;
      }
      setStatus(entry, t('history.status.empty'));
      return;
    }
    setStatus(entry, '');
    const options = window.HistoryChart.buildOptions({
      rows, gaps: entry.gaps, aggregate: entry.config.aggregate, knownSeries: entry.knownSeries, compact: true,
    });
    if (entry.chart) {
      entry.chart.updateOptions(options, false, false);
      return;
    }
    entry.chart = new window.ApexCharts(plot, options);
    entry.chart.render();
  }

  async function refresh(entry) {
    if (!window.HistoryStore || !window.HistoryChart) {
      setStatus(entry, t('overview.history_view.no_store'));
      return;
    }
    try {
      const {from, to} = window.HistoryChart.rangeBounds(entry.config, Date.now());
      const result = await window.HistoryChart.loadRows({from, to, interpretation: interpretation(), recorderConfig: recorderConfig()});
      // Waehrend des Lesens abgebaut (Layout gespeichert, Seite gewechselt).
      if (entries.get(entry.element.id) !== entry) return;
      watch(entry.element.id, {from, to, tier: result.tier});
      entry.rows = result.rows;
      entry.gaps = result.gaps;
      entry.knownSeries = [...new Set(result.rows.map(row => row.series))].sort();
      render(entry);
    } catch (error) {
      setStatus(entry, t('overview.history_view.read_failed', {message: error.message}));
    }
  }

  function mount(element) {
    if (element.hasAttribute('data-history-view-missing')) {
      entries.set(element.id, {element, missing: true, ready: Promise.resolve()});
      return Promise.resolve();
    }
    let config;
    try {
      config = JSON.parse(element.dataset.historyView || '{}');
    } catch (error) {
      config = {};
    }
    const entry = {element, config, missing: false, chart: null, rows: [], gaps: new Map(), knownSeries: []};
    entries.set(element.id, entry);
    const range = element.querySelector('[data-history-view-range]');
    if (range && window.HistoryChart) range.textContent = window.HistoryChart.rangeLabel(config);
    setStatus(entry, t('history.status.loading'));
    entry.ready = refresh(entry);
    return entry.ready;
  }

  function unmount(id) {
    const entry = entries.get(id);
    if (entry && entry.chart) entry.chart.destroy();
    entries.delete(id);
    unwatch(id);
  }

  function scan() {
    const present = new Set();
    document.querySelectorAll('[data-history-view]').forEach(element => {
      present.add(element.id);
      if (!entries.has(element.id)) mount(element);
    });
    [...entries.keys()].forEach(id => { if (!present.has(id)) unmount(id); });
    if (entries.size && !timer) timer = setInterval(tick, REFRESH_MS);
    if (!entries.size && timer) {
      clearInterval(timer);
      timer = null;
    }
    return Promise.all([...entries.values()].map(entry => entry.ready));
  }

  // Ein Hintergrund-Tab liest nicht. Kommt er zurueck, laedt visibilitychange
  // sofort nach, statt bis zu 60 s alte Linien zu zeigen.
  function tick() {
    if (document.visibilityState !== 'visible') return Promise.resolve();
    return Promise.all([...entries.values()].filter(entry => !entry.missing).map(refresh));
  }

  function start() {
    // Jeder Tausch kann Kacheln bringen oder mitnehmen. scan() ist billig
    // (ein querySelectorAll), darum ohne Filter auf das Ziel.
    document.addEventListener('htmx:afterSettle', () => scan());
    // Ein verborgener Tab zeigt nichts an und meldet seine Kacheln ab.
    document.addEventListener('visibilitychange', () => {
      if (document.visibilityState === 'visible') tick();
      else entries.forEach((entry, id) => unwatch(id));
    });
    window.addEventListener('dashboard-history-exchanged', () => {
      if (exchangeTimer) clearTimeout(exchangeTimer);
      exchangeTimer = setTimeout(() => {
        exchangeTimer = null;
        tick();
      }, EXCHANGE_SETTLE_MS);
    });
    // Ein Theme-Wechsel aendert die Serienfarben, die Daten bleiben.
    if (window.DashboardTheme) window.DashboardTheme.onChange(() => entries.forEach(entry => { if (entry.chart) render(entry); }));
    scan();
  }

  window.HistoryViewCard = {scan, tick, entries, REFRESH_MS};
  // Als defer-Skript laeuft diese Datei vor history-store.js (base.html).
  // Erst nach DOMContentLoaded steht window.HistoryStore.
  if (document.readyState === 'complete') start();
  else document.addEventListener('DOMContentLoaded', start, {once: true});
})();
