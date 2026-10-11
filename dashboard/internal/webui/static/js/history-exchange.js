// dashboard/internal/webui/static/js/history-exchange.js
// Der Zustandsteil des Verlauf-Austauschs: SSE-Verbindung zum Server,
// Angebot, Nachfrage, Lieferung. Die Rechenlogik liegt vollstaendig in
// history-coverage.js, das Lesen und Schreiben in history-store.js.
//
// Nur der fuehrende Tab tauscht (siehe history-recorder.js): alle Tabs
// eines Browsers teilen sich eine IndexedDB, und mehrere gleichzeitig
// tauschende Tabs boten denselben Bestand mehrfach an und schrieben
// gegeneinander.
//
// Die Rohstufe wird nie getauscht. Jedes Geraet zeichnet mit eigener
// Abtastphase auf, die Rohzeitstempel liegen versetzt und waeren nicht
// entdoppelbar; erst bucket() in history-rollup.js richtet die
// Verdichtungsstufen an absoluten Epoch-Vielfachen aus, und erst dadurch
// fallen die Saetze zweier Geraete auf identische Schluessel.
(() => {
  const t = (key, params) => (window.I18n ? window.I18n.t(key, params) : key);

  const PROTOCOL = 1;
  const RECONNECT_MIN_MS = 2000;
  const RECONNECT_MAX_MS = 60000;

  // Der Modus gilt je Geraet, nicht je Knoten: ein dauernd offener Desktop
  // soll Quelle bleiben, ein Handy nur holen, was es anzeigt. Deshalb
  // localStorage statt der Knoten-Einstellungen. Alles ausser "always" ist
  // "on-demand", auch ein unlesbarer Speicher (privater Modus).
  const MODE_KEY = 'history-exchange-mode';

  const readMode = () => {
    try {
      return window.localStorage.getItem(MODE_KEY) === 'always' ? 'always' : 'on-demand';
    } catch (error) {
      return 'on-demand';
    }
  };

  // Eine Anmeldung, die nicht erneuert wird, verfaellt. Seite und Kachel
  // melden bei jedem Laden neu an, ein geschlossener Tab dagegen nie ab.
  const WATCH_TTL_MS = 10 * 60 * 1000;

  let options = {basePath: '', fetchImpl: null, eventSourceImpl: null, now: () => Date.now()};
  let announcement = null;
  let source = null;
  let peerId = '';
  let peers = [];
  let offers = {};
  let labels = {};
  let lastOffer = '';
  let stopped = true;
  let reconnectDelay = RECONNECT_MIN_MS;
  let reconnectTimer = null;
  let pending = new Set();
  let mode = readMode();
  // Sichtbare Zeitraeume im Modus on-demand, Schluessel "<tab>:<ansicht>".
  let watched = new Map();
  // Zaehlt eingehende Angebote. Ein neues Angebot ist der einzige Grund,
  // einen unveraenderten Zeitraum vor Ablauf der Frist erneut zu planen.
  let offerCount = 0;
  const state = {addedRows: 0, lastPeer: '', lastSource: '', lastAt: 0, reason: ''};

  const fetcher = (...args) => (options.fetchImpl || window.fetch.bind(window))(...args);
  const url = suffix => `${options.basePath}/api/v1/history/exchange${suffix}`;
  const now = () => options.now();

  const post = async (suffix, body) => {
    const response = await fetcher(url(suffix), {
      method: 'POST',
      headers: {'Content-Type': 'application/json'}, // i18n-ignore
      body: JSON.stringify({peer: peerId, ...body}),
    });
    return response.ok;
  };

  // Das eigene Deckungsraster ueber beide tauschbaren Stufen.
  const ownCoverage = async () => {
    const coverage = window.HistoryCoverage;
    const result = {};
    for (const tier of coverage.TIERS) {
      const span = coverage.rasterWindow(tier, now());
      // eslint-disable-next-line no-await-in-loop
      result[tier] = await window.HistoryStore.coverage(tier, span.from, span.to, span.stepMs);
    }
    return result;
  };

  // Ein neues Angebot nur bei tatsaechlicher Aenderung. Ohne diesen Vergleich
  // erzeugte jeder Verdichtungslauf ein Angebot an alle - im Minutentakt,
  // ueber alle verbundenen Geraete.
  const refreshOffer = async () => {
    // Im Modus on-demand ist dieses Geraet keine Quelle fuer andere.
    if (!peerId || mode !== 'always') return;
    const coverage = await ownCoverage();
    const fingerprint = JSON.stringify(coverage);
    if (fingerprint === lastOffer) return;
    lastOffer = fingerprint;
    await post('/offer', {coverage});
  };

  const requestFrom = async job => {
    const key = `${job.peer}|${job.tier}|${job.series}`;
    if (pending.has(key)) return;
    pending.add(key);
    const reqId = `${peerId}-${now()}-${Math.random().toString(16).slice(2, 8)}`;
    const sent = await post('/request', {
      to: job.peer, req_id: reqId, tier: job.tier, series: job.series, ranges: job.ranges,
    });
    if (!sent) pending.delete(key);
    // Laeuft die Nachfrage ins Leere, gibt die Frist den Schluessel wieder
    // frei; der naechste Angebotsvergleich versucht es erneut, notfalls bei
    // einem anderen Peer.
    const timeoutMs = (announcement.request_timeout_seconds || 30) * 1000;
    window.setTimeout(() => pending.delete(key), timeoutMs);
  };

  // Der volle Abgleich gegen alle bekannten Angebote (Modus always).
  const planAll = async () => {
    const jobs = window.HistoryCoverage.plan(await ownCoverage(), offers, now());
    for (const job of jobs) {
      // eslint-disable-next-line no-await-in-loop
      await requestFrom(job);
    }
  };

  // Plant die angemeldeten Zeitraeume. Die eigene Deckung wird ueber das
  // ganze Rasterfenster gezaehlt, sonst passten eigenes und fremdes Raster
  // nicht aufeinander; der Cache in history-store.js haelt das billig.
  // Gefragt wird nur innerhalb von [from, to).
  //
  // Eine Lieferung laedt Seite und Kachel neu, und die melden denselben
  // Zeitraum erneut an. Ohne die Signatur entstuende daraus eine zweite
  // Nachfrage und so fort.
  const planWatched = async ids => {
    if (!peerId || mode !== 'on-demand') return;
    const coverage = window.HistoryCoverage;
    const timeoutMs = ((announcement && announcement.request_timeout_seconds) || 30) * 1000;
    for (const id of ids) {
      const entry = watched.get(id);
      if (!entry) continue;
      if (now() - entry.at > WATCH_TTL_MS) {
        watched.delete(id);
        continue;
      }
      const step = coverage.RASTER[entry.tier].stepMs;
      const signature = `${entry.tier}|${Math.floor(entry.from / step)}|${Math.ceil(entry.to / step)}|${offerCount}`;
      if (entry.plannedFor === signature && now() - entry.plannedAt < timeoutMs) continue;
      entry.plannedFor = signature;
      entry.plannedAt = now();
      const span = coverage.rasterWindow(entry.tier, now());
      // eslint-disable-next-line no-await-in-loop
      const own = {[entry.tier]: await window.HistoryStore.coverage(entry.tier, span.from, span.to, span.stepMs)};
      const jobs = coverage.plan(own, offers, now(), {tier: entry.tier, from: entry.from, to: entry.to});
      for (const job of jobs) {
        // eslint-disable-next-line no-await-in-loop
        await requestFrom(job);
      }
    }
  };

  // Die Rohstufe wird nie getauscht, Rohluecken fuellt der Chart aus 1m
  // (fillRawGaps in history-rollup.js). Deshalb wird sie als 1m angefragt.
  const watchRange = async (id, range) => {
    const tier = range.tier === 'raw' ? '1m' : range.tier;
    if (!window.HistoryCoverage.TIERS.includes(tier)) return;
    const previous = watched.get(id) || {};
    watched.set(id, {
      tier, from: Number(range.from), to: Number(range.to), at: now(),
      plannedFor: previous.plannedFor || '', plannedAt: previous.plannedAt || 0,
    });
    await planWatched([id]);
  };

  const unwatchRange = id => {
    watched.delete(id);
  };

  const onOffer = async payload => {
    if (!payload.peer || payload.peer === peerId) return;
    offers[payload.peer] = payload.coverage || {};
    // Fremd-Peers wie Home Assistant nennen sich, Browser nicht.
    if (payload.label) labels[payload.peer] = String(payload.label);
    // Im Modus on-demand nur merken: geplant wird erst, wenn ein Verlauf
    // angezeigt wird.
    offerCount += 1;
    if (mode === 'always') await planAll();
    else await planWatched([...watched.keys()]);
  };

  const onRequest = async payload => {
    if (!payload.peer || !payload.ranges || !payload.ranges.length) return;
    const limit = announcement.max_rows_per_deliver || 500;
    const collected = [];
    for (const span of payload.ranges) {
      // eslint-disable-next-line no-await-in-loop
      const rows = await window.HistoryStore.readRange(payload.tier, payload.series, span[0], span[1] - 1);
      collected.push(...rows);
      if (collected.length >= (announcement.max_rows_per_request || 20000)) break;
    }
    let seq = 0;
    for (let start = 0; start < collected.length; start += limit) {
      const chunk = collected.slice(start, start + limit);
      // eslint-disable-next-line no-await-in-loop
      await post('/deliver', {
        to: payload.peer, req_id: payload.req_id, seq, tier: payload.tier,
        final: start + limit >= collected.length, rows: chunk,
      });
      seq += 1;
    }
    if (!collected.length) {
      await post('/deliver', {
        to: payload.peer, req_id: payload.req_id, seq: 0, tier: payload.tier, final: true, rows: [],
      });
    }
  };

  const onDeliver = async payload => {
    if (!window.HistoryCoverage.TIERS.includes(payload.tier)) return;
    const rows = payload.rows || [];
    if (rows.length) {
      const added = await window.HistoryStore.writeMissing(payload.tier, rows);
      state.addedRows += added;
      state.lastPeer = payload.peer || '';
      state.lastSource = labels[payload.peer] || '';
      state.lastAt = now();
      if (added) {
        window.dispatchEvent(new CustomEvent('dashboard-history-exchanged', {
          detail: {peer: payload.peer, tier: payload.tier, rows: added},
        }));
        window.dispatchEvent(new CustomEvent('dashboard-history-updated'));
      }
    }
    if (payload.final) {
      [...pending].forEach(key => { if (key.startsWith(`${payload.peer}|${payload.tier}|`)) pending.delete(key); });
      await refreshOffer();
    }
  };

  const handler = (name, fn) => event => {
    let payload = {};
    try {
      payload = JSON.parse(event.data);
    } catch (error) {
      return;
    }
    Promise.resolve(fn(payload)).catch(error => {
      state.reason = t('history.exchange.handler_failed', {name, message: error.message});
      // QuotaExceededError ist kein voruebergehender Fehler - weiterlaufen
      // hiesse, im Minutentakt Fehler zu erzeugen.
      if (error && error.name === 'QuotaExceededError') stop(); // i18n-ignore
    });
  };

  const connect = () => {
    const EventSourceImpl = options.eventSourceImpl || window.EventSource;
    if (!EventSourceImpl) {
      state.reason = t('history.exchange.no_sse_support');
      return;
    }
    source = new EventSourceImpl(url('/stream'));
    source.addEventListener('hello', handler('hello', async payload => {
      if (Number(payload.protocol) !== PROTOCOL) {
        state.reason = t('history.exchange.wrong_protocol');
        stop();
        return;
      }
      peerId = payload.peer_id || '';
      peers = payload.peers || [];
      offers = {};
      lastOffer = '';
      reconnectDelay = RECONNECT_MIN_MS;
      await refreshOffer();
    }));
    source.addEventListener('offer', handler('offer', onOffer));
    source.addEventListener('request', handler('request', onRequest));
    source.addEventListener('deliver', handler('deliver', onDeliver));
    source.addEventListener('peer-joined', handler('peer-joined', async payload => {
      if (payload.peer && !peers.includes(payload.peer)) peers.push(payload.peer);
      // Der Neue kennt unser Angebot noch nicht - deshalb hier erneut, auch
      // wenn sich am Bestand nichts geaendert hat.
      lastOffer = '';
      await refreshOffer();
    }));
    source.addEventListener('peer-left', handler('peer-left', payload => {
      peers = peers.filter(name => name !== payload.peer);
      delete offers[payload.peer];
      delete labels[payload.peer];
    }));
    source.addEventListener('error', () => {
      if (stopped) return;
      if (source) source.close();
      source = null;
      peerId = '';
      reconnectTimer = window.setTimeout(connect, reconnectDelay);
      reconnectDelay = Math.min(reconnectDelay * 2, RECONNECT_MAX_MS);
    });
  };

  const start = async settings => {
    options = {...options, ...(settings || {})};
    stopped = false;
    mode = readMode();
    try {
      const response = await fetcher(url(''));
      if (!response.ok) {
        state.reason = t('history.exchange.no_server_support');
        stopped = true;
        return false;
      }
      announcement = await response.json();
    } catch (error) {
      state.reason = t('history.exchange.announcement_unreadable', {message: error.message});
      stopped = true;
      return false;
    }
    if (Number(announcement.protocol) !== PROTOCOL) {
      state.reason = t('history.exchange.wrong_protocol');
      stopped = true;
      return false;
    }
    connect();
    return true;
  };

  function stop() {
    stopped = true;
    if (reconnectTimer) window.clearTimeout(reconnectTimer);
    reconnectTimer = null;
    if (source) source.close();
    source = null;
    peerId = '';
    peers = [];
    offers = {};
    labels = {};
    lastOffer = '';
    pending = new Set();
    watched = new Map();
  }

  // Fuettert den Ringpuffer des Servers. Der Server misst nicht selbst: die
  // Vorzeichenlogik der Energie-Rollen liegt in EnergyModel im Browser.
  const pushBuffer = async rows => {
    if (!peerId || !rows || !rows.length) return;
    const limit = announcement.max_rows_per_deliver || 500;
    for (let start = 0; start < rows.length; start += limit) {
      // eslint-disable-next-line no-await-in-loop
      await post('/buffer', {rows: rows.slice(start, start + limit)});
    }
  };

  // Wirkt ohne Neuverbinden. Beim Wechsel auf "always" sofort anbieten und
  // gegen die bekannten Angebote planen, statt auf das naechste zu warten.
  const applyMode = async value => {
    if (value === mode) return;
    mode = value;
    if (mode !== 'always' || !peerId) return;
    lastOffer = '';
    await refreshOffer();
    await planAll();
  };

  const setMode = async next => {
    const value = next === 'always' ? 'always' : 'on-demand';
    try {
      window.localStorage.setItem(MODE_KEY, value);
    } catch (error) {
      // Privater Modus: die Wahl gilt bis zum Neuladen.
    }
    await applyMode(value);
  };

  // Andere Tabs desselben Browsers schalten ueber localStorage um. Das
  // storage-Ereignis erreicht nur die ANDEREN Tabs, der eigene ruft setMode().
  window.addEventListener('storage', event => {
    if (event.key === MODE_KEY) applyMode(readMode()).catch(() => {});
  });

  const status = () => ({
    connected: Boolean(peerId),
    peerId,
    peers: [...peers],
    addedRows: state.addedRows,
    lastPeer: state.lastPeer,
    lastSource: state.lastSource,
    lastAt: state.lastAt,
    reason: state.reason,
    mode,
  });

  window.HistoryExchange = {
    start, stop, status, pushBuffer, refreshOffer, mode: () => mode, setMode, watchRange, unwatchRange,
  };
})();
