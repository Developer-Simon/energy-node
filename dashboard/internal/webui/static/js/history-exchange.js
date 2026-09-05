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
  const PROTOCOL = 1;
  const RECONNECT_MIN_MS = 2000;
  const RECONNECT_MAX_MS = 60000;

  let options = {basePath: '', fetchImpl: null, eventSourceImpl: null, now: () => Date.now()};
  let announcement = null;
  let source = null;
  let peerId = '';
  let peers = [];
  let offers = {};
  let lastOffer = '';
  let stopped = true;
  let reconnectDelay = RECONNECT_MIN_MS;
  let reconnectTimer = null;
  let pending = new Set();
  const state = {addedRows: 0, lastPeer: '', lastAt: 0, reason: ''};

  const fetcher = (...args) => (options.fetchImpl || window.fetch.bind(window))(...args);
  const url = suffix => `${options.basePath}/api/v1/history/exchange${suffix}`;
  const now = () => options.now();

  const post = async (suffix, body) => {
    const response = await fetcher(url(suffix), {
      method: 'POST',
      headers: {'Content-Type': 'application/json'},
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
    if (!peerId) return;
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

  const onOffer = async payload => {
    if (!payload.peer || payload.peer === peerId) return;
    offers[payload.peer] = payload.coverage || {};
    const jobs = window.HistoryCoverage.plan(await ownCoverage(), offers, now());
    for (const job of jobs) {
      // eslint-disable-next-line no-await-in-loop
      await requestFrom(job);
    }
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
      state.reason = `${name} fehlgeschlagen: ${error.message}`;
      // QuotaExceededError ist kein voruebergehender Fehler - weiterlaufen
      // hiesse, im Minutentakt Fehler zu erzeugen.
      if (error && error.name === 'QuotaExceededError') stop();
    });
  };

  const connect = () => {
    const EventSourceImpl = options.eventSourceImpl || window.EventSource;
    if (!EventSourceImpl) {
      state.reason = 'Der Browser kennt keine Server-Sent-Events.';
      return;
    }
    source = new EventSourceImpl(url('/stream'));
    source.addEventListener('hello', handler('hello', async payload => {
      if (Number(payload.protocol) !== PROTOCOL) {
        state.reason = 'Der Server spricht eine andere Protokollversion.';
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
    try {
      const response = await fetcher(url(''));
      if (!response.ok) {
        state.reason = 'Der Server bietet keinen Verlauf-Austausch an.';
        stopped = true;
        return false;
      }
      announcement = await response.json();
    } catch (error) {
      state.reason = `Ankuendigung nicht lesbar: ${error.message}`;
      stopped = true;
      return false;
    }
    if (Number(announcement.protocol) !== PROTOCOL) {
      state.reason = 'Der Server spricht eine andere Protokollversion.';
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
    lastOffer = '';
    pending = new Set();
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

  const status = () => ({
    connected: Boolean(peerId),
    peerId,
    peers: [...peers],
    addedRows: state.addedRows,
    lastPeer: state.lastPeer,
    lastAt: state.lastAt,
    reason: state.reason,
  });

  window.HistoryExchange = {start, stop, status, pushBuffer, refreshOffer};
})();
