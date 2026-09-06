// dashboard/internal/webui/static/js/history-recorder.js
// Zeichnet die Verlaufs-Historie in die IndexedDB auf. Standard sind
// ausschliesslich die Energie-Rollen ("role:pv" usw.); einzelne Entitaeten
// kommen ueber history_extra_entities aus den Einstellungen dazu.
//
// Zwei Punkte, die die Vorgaengerfassung nicht hatte:
//
// 1. Das Abtastintervall kommt aus den Einstellungen statt fest aus dem Code.
// 2. Es zeichnet nur noch EIN Tab auf. Vorher pollte jeder offene Tab
//    eigenstaendig; zwei Tabs schreiben minimal versetzte Zeitstempel, die
//    der zusammengesetzte Schluessel NICHT entdoppelt - das blaeht den
//    Speicher auf und macht Verdichtung und Aufraeumen rennanfaellig. Die
//    Fuehrung wird ueber die Web-Locks-API vergeben und fuer die Lebensdauer
//    des Tabs gehalten; die uebrigen Tabs lesen nur und werden ueber einen
//    BroadcastChannel benachrichtigt.
(() => {
  const LOCK_NAME = 'energy-node-historizer';
  const CHANNEL_NAME = 'energy-node-history';
  const DEFAULT_READ_WINDOW_MS = 24 * 60 * 60 * 1000;
  const MAINTENANCE_INTERVAL_MS = 60000;

  const DEFAULTS = {
    intervalSeconds: 10,
    retentionMode: 'time',
    retentionHours: 6,
    budgetMB: 512,
    rawWindowHours: 24,
    minuteWindowDays: 7,
    extraEntities: [],
    exchangeDisabled: false,
  };

  let config = {...DEFAULTS};
  let leader = false;
  let timer = null;
  let maintenanceTimer = null;
  let collecting = false;
  let channel = null;
  let paused = false;
  let pauseReason = '';
  let persisted = null;

  // Loest auf, sobald becomeLeader() seinen Bootstrap abgeschlossen hat
  // (persist(), meta('recording_paused'), erster collect()). Tests warten
  // darauf, statt einen festen Timeout zu raten - auf langsamen CI-Laeufern
  // reichte der geratene Wert nicht und der Bootstrap ueberschrieb den im Test
  // gesetzten Zustand nachtraeglich.
  let signalReady;
  const ready = new Promise(resolve => { signalReady = resolve; });

  const basePath = () => window.__DASHBOARD_BASE_PATH__ || '';

  const announceUpdate = () => {
    window.dispatchEvent(new CustomEvent('dashboard-history-updated'));
    if (channel) channel.postMessage({type: 'updated'});
  };

  const announceError = message => window.dispatchEvent(new CustomEvent('dashboard-history-error', {detail: {message}}));

  const runMaintenance = async () => {
    if (!leader || !window.HistoryMaintenance) return;
    try {
      const result = await window.HistoryMaintenance.run(config, Date.now());
      await afterMaintenance(result.compacted || {});
    } catch (error) {
      announceError(`Verdichtung der Historie fehlgeschlagen: ${error.message}`);
    }
  };

  // Nach jedem Verdichtungslauf: die frisch entstandenen Minutenwerte in den
  // Server-Ringpuffer schieben und das eigene Angebot nachfuehren. Genau
  // hier - und nicht bei jedem Messpunkt - weil sich der tauschbare Bestand
  // nur durch Verdichtung aendert.
  const afterMaintenance = async ({minuteRows = []} = {}) => {
    if (!window.HistoryExchange || config.exchangeDisabled) return;
    try {
      if (minuteRows.length) await window.HistoryExchange.pushBuffer(minuteRows);
      await window.HistoryExchange.refreshOffer();
    } catch (error) {
      // Der Austausch ist eine Ergaenzung, kein Betriebskriterium: seine
      // Fehler duerfen die Aufzeichnung nicht anhalten.
      announceError(`Verlauf-Austausch: ${error.message}`);
    }
  };

  const startExchange = async () => {
    if (!window.HistoryExchange || config.exchangeDisabled) return false;
    return window.HistoryExchange.start({basePath: basePath()});
  };

  // Rollenwerte werden hier und nicht in Go abgeleitet: die Vorzeichenlogik
  // fuer Batterie und Netz steckt in EnergyModel, und sie ein zweites Mal in
  // Go zu fuehren waere die teurere Haelfte des Tauschs.
  const roleSamples = snapshot => {
    if (!window.EnergyModel) return [];
    const model = window.EnergyModel;
    const has = (...roles) => roles.some(role => model.hasValue(snapshot, role));
    const values = {};
    if (has('pv')) values.pv = model.roleValue(snapshot, 'pv');
    if (has('battery', 'battery_charge', 'battery_discharge')) {
      values.battery = model.batteryChargePower(snapshot) - model.batteryDischargePower(snapshot);
    }
    if (has('grid', 'grid_import', 'grid_export')) {
      values.grid = model.gridImportPower(snapshot) - model.gridExportPower(snapshot);
    }
    if (has('load')) values.load = model.roleValue(snapshot, 'load');
    if (has('wallbox')) values.wallbox = model.roleValue(snapshot, 'wallbox');
    if (has('heat_pump')) values.heat_pump = model.roleValue(snapshot, 'heat_pump');
    const ts = Date.parse(snapshot.at);
    const rows = Object.entries(values).map(([role, value]) => ({series: `role:${role}`, ts, v: value, u: 'W'}));
    // Der Batterie-Fuellstand ist eine Prozentrolle, keine Leistung: eigene
    // Serie mit eigener Einheit, damit Chart-Achse und Export ihn richtig
    // beschriften. Er faellt bewusst NICHT in die Hausverbrauch-Ableitung in
    // history.js - die kennt ihn ueber HISTORY_ROLES nicht.
    if (has('battery_soc')) {
      rows.push({series: 'role:battery_soc', ts, v: model.roleValue(snapshot, 'battery_soc'), u: '%'});
    }
    return rows;
  };

  const loadConfig = async () => {
    try {
      const response = await fetch(`${basePath()}/api/v1/settings`);
      if (!response.ok) return;
      const value = await response.json();
      applyConfig({
        intervalSeconds: value.history_sample_interval_seconds,
        retentionMode: value.history_retention_mode,
        retentionHours: value.history_retention_hours,
        budgetMB: value.history_budget_mb,
        rawWindowHours: value.history_raw_window_hours,
        minuteWindowDays: value.history_minute_window_days,
        extraEntities: value.history_extra_entities,
        exchangeDisabled: value.history_exchange_disabled,
      });
    } catch (error) {
      // Die Einstellungen sind eine Verfeinerung, kein Startkriterium: ohne
      // sie laeuft die Aufzeichnung mit den Defaults weiter.
    }
  };

  const applyConfig = partial => {
    const next = {...config};
    if (Number(partial.intervalSeconds) >= 5) next.intervalSeconds = Number(partial.intervalSeconds);
    if (partial.retentionMode === 'time' || partial.retentionMode === 'size') next.retentionMode = partial.retentionMode;
    if (Number(partial.retentionHours) > 0) next.retentionHours = Number(partial.retentionHours);
    if (Number(partial.budgetMB) > 0) next.budgetMB = Number(partial.budgetMB);
    if (Number(partial.rawWindowHours) > 0) next.rawWindowHours = Number(partial.rawWindowHours);
    if (Number(partial.minuteWindowDays) > 0) next.minuteWindowDays = Number(partial.minuteWindowDays);
    if (Array.isArray(partial.extraEntities)) next.extraEntities = [...partial.extraEntities];
    if (typeof partial.exchangeDisabled === 'boolean') next.exchangeDisabled = partial.exchangeDisabled;
    const intervalChanged = next.intervalSeconds !== config.intervalSeconds;
    const exchangeChanged = next.exchangeDisabled !== config.exchangeDisabled;
    config = next;
    if (intervalChanged && leader) restartTimer();
    if (exchangeChanged && leader) {
      if (config.exchangeDisabled) {
        if (window.HistoryExchange) window.HistoryExchange.stop();
      } else {
        startExchange();
      }
    }
  };

  const extraSamples = async at => {
    if (!config.extraEntities.length) return [];
    const response = await fetch(`${basePath()}/api/v1/history/entities`);
    if (!response.ok) throw new Error('Zusätzliche Verlaufs-Entitäten konnten nicht gelesen werden');
    const body = await response.json();
    const ts = Date.parse(body.at || at);
    return (body.samples || [])
      .filter(sample => typeof sample.value === 'number' && Number.isFinite(sample.value))
      .map(sample => ({series: sample.entity_id, ts, v: sample.value, u: sample.unit || ''}));
  };

  const collect = async () => {
    if (collecting || paused) return;
    collecting = true;
    try {
      const response = await fetch(`${basePath()}/api/v1/energy`);
      if (!response.ok) throw new Error('Energie-Historie konnte Live-Daten nicht laden');
      const snapshot = await response.json();
      const rows = roleSamples(snapshot).concat(await extraSamples(snapshot.at));
      if (rows.length) await window.HistoryStore.writeRaw(rows);
      announceUpdate();
    } catch (error) {
      // QuotaExceededError ist kein voruebergehender Fehler: jeder weitere
      // Schreibversuch scheitert genauso. Weiterlaufen hiesse, im Sekunden-
      // takt Fehler zu erzeugen und dem Nutzer vorzuspielen, es werde noch
      // aufgezeichnet.
      if (error && error.name === 'QuotaExceededError') {
        paused = true;
        pauseReason = 'Der Browser-Speicher ist voll. Die Aufzeichnung ist angehalten — Budget verkleinern oder Aufbewahrung verkürzen, dann fortsetzen.';
        window.dispatchEvent(new CustomEvent('dashboard-history-paused', {detail: {reason: pauseReason}}));
        try { await window.HistoryStore.setMeta('recording_paused', true); } catch (metaError) { /* Metadaten sind hier nachrangig */ }
      }
      announceError(error.message);
    } finally {
      collecting = false;
    }
  };

  const resume = async () => {
    paused = false;
    pauseReason = '';
    try { await window.HistoryStore.setMeta('recording_paused', false); } catch (error) { /* nachrangig */ }
    await collect();
  };

  const restartTimer = () => {
    if (timer) window.clearInterval(timer);
    timer = window.setInterval(collect, config.intervalSeconds * 1000);
  };

  const becomeLeader = async () => {
    leader = true;
    persisted = await window.HistoryStore.persist();
    paused = Boolean(await window.HistoryStore.meta('recording_paused'));
    if (paused) {
      pauseReason = 'Die Aufzeichnung wurde wegen vollem Browser-Speicher angehalten.';
      window.dispatchEvent(new CustomEvent('dashboard-history-paused', {detail: {reason: pauseReason}}));
    }
    await collect();
    restartTimer();
    // Vorab einmal verdichten, statt bis zu MAINTENANCE_INTERVAL_MS auf den
    // ersten Timer-Tick zu warten: erst danach kennt die 1m-Stufe die frueh
    // verdichteten Saetze von heute (siehe EXCHANGE_GRACE_MS in
    // history-maintenance.js), und erst dann darf startExchange() unten das
    // erste Angebot mit ihnen fuellen.
    await runMaintenance();
    maintenanceTimer = window.setInterval(runMaintenance, MAINTENANCE_INTERVAL_MS);
    startExchange();
    signalReady();
  };

  // Der Lock wird gehalten, solange der Tab lebt: das Promise loest nie auf.
  // Schliesst der Tab, gibt der Browser den Lock frei und ein anderer Tab
  // uebernimmt automatisch.
  const claimLeadership = async () => {
    if (!navigator.locks || !navigator.locks.request) {
      await becomeLeader();
      return;
    }
    navigator.locks.request(LOCK_NAME, {mode: 'exclusive'}, () => new Promise(() => { becomeLeader(); }));
  };

  const openChannel = () => {
    if (!window.BroadcastChannel) return;
    channel = new window.BroadcastChannel(CHANNEL_NAME);
    channel.onmessage = event => {
      if (event.data && event.data.type === 'updated' && !leader) {
        window.dispatchEvent(new CustomEvent('dashboard-history-updated'));
      }
    };
  };

  // Altform-Fassade fuer energy-board.js (Sparklines) und energy-day.js
  // (Tagesleiste). Beide rufen readSamples() ohne Argument und filtern selbst
  // - deshalb MUSS hier gedeckelt werden, sonst zoege ein Aufruf bei
  // Speicherbudget-Betrieb die halbe Datenbank in den Speicher.
  const readSamples = async ({sinceMs = DEFAULT_READ_WINDOW_MS} = {}) => {
    const now = Date.now();
    const rows = await window.HistoryStore.readRange('raw', null, now - sinceMs, now);
    return window.HistoryRollup.normalize(rows).map(row => ({
      entity_id: row.series,
      timestamp: new Date(row.ts).toISOString(),
      value: row.avg,
      unit: row.u,
      source: 'browser',
    }));
  };

  const start = () => {
    if (timer || leader) return;
    openChannel();
    loadConfig().then(claimLeadership);
    document.addEventListener('history-settings-changed', event => applyConfig(event.detail || {}));
  };

  window.dashboardHistorizer = {
    start,
    readSamples,
    roleSamples,
    resume,
    // whenReady loest auf, sobald der automatische Fuehrungsantritt fertig ist
    // - der Testeinstieg, um genau danach den Zustand zu manipulieren.
    whenReady: () => ready,
    // collectOnce ist der Testeinstieg in genau einen Sammeldurchgang -
    // der Timer-getriebene Pfad ist in jsdom nicht beobachtbar.
    collectOnce: collect,
    config: () => ({...config}),
    status: () => ({paused, persisted, reason: pauseReason}),
    isLeader: () => leader,
    startExchange,
    afterMaintenance,
  };

  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', start);
  } else {
    start();
  }
})();
