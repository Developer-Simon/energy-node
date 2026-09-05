// dashboard/internal/webui/static/js/history-coverage.js
// Rechenlogik des Verlauf-Austauschs: Deckungsraster bauen, zwei Raster
// vergleichen, daraus Nachfragen planen. Bewusst ohne DOM und ohne
// IndexedDB - derselbe Schnitt wie bei history-rollup.js, damit der
// gesamte Rechenteil ohne Browser pruefbar ist.
//
// Ein Angebot kann nicht jeden Zeitstempel aufzaehlen. Stattdessen wird die
// Zeit in grobe Raster geschnitten und je Raster nur die ANZAHL vorhandener
// Saetze gemeldet. Sieben Tage Minutenwerte sind so 168 Zahlen je Serie.
(() => {
  const RASTER = {
    '1m': {stepMs: 3600000, windowMs: 7 * 24 * 3600000},
    '5m': {stepMs: 21600000, windowMs: 30 * 24 * 3600000},
  };

  const TIERS = Object.keys(RASTER);

  const rasterWindow = (tier, now) => {
    const raster = RASTER[tier];
    if (!raster) throw new Error(`Unbekannte Verdichtungsstufe: ${tier}`);
    const from = Math.floor((now - raster.windowMs) / raster.stepMs) * raster.stepMs;
    return {from, to: now, stepMs: raster.stepMs, buckets: Math.ceil((now - from) / raster.stepMs)};
  };

  const census = (tier, timestamps, now) => {
    const {from, stepMs, buckets} = rasterWindow(tier, now);
    const n = new Array(buckets).fill(0);
    timestamps.forEach(ts => {
      if (ts < from || ts >= now) return;
      n[Math.floor((ts - from) / stepMs)] += 1;
    });
    return {from, step: stepMs, n};
  };

  const emptyCensus = reference => ({from: reference.from, step: reference.step, n: []});

  // Der Bucket, in den "jetzt" faellt, wird immer ausgelassen: dort sind
  // zwei Geraete naturgemaess unterschiedlich weit, und ohne diese Ausnahme
  // entstuende aus dem Vergleich eine Dauerschleife aus Nachfragen.
  const missingRanges = (own, peer, now) => {
    if (!peer || !peer.n || !peer.n.length) return [];
    const base = own && own.n ? own : emptyCensus(peer);
    if (base.from !== peer.from || base.step !== peer.step) return [];
    const current = Math.floor((now - peer.from) / peer.step);
    const ranges = [];
    let open = null;
    for (let index = 0; index < peer.n.length; index += 1) {
      const wanted = index !== current && (peer.n[index] || 0) > (base.n[index] || 0);
      if (wanted && open === null) open = index;
      if (!wanted && open !== null) {
        ranges.push([peer.from + open * peer.step, peer.from + index * peer.step]);
        open = null;
      }
    }
    if (open !== null) ranges.push([peer.from + open * peer.step, peer.from + peer.n.length * peer.step]);
    return ranges;
  };

  const surplus = (own, peer, now) => {
    if (!peer || !peer.n) return 0;
    const base = own && own.n ? own : emptyCensus(peer);
    const current = Math.floor((now - peer.from) / peer.step);
    let total = 0;
    for (let index = 0; index < peer.n.length; index += 1) {
      if (index === current) continue;
      const difference = (peer.n[index] || 0) - (base.n[index] || 0);
      if (difference > 0) total += difference;
    }
    return total;
  };

  // Je Stufe und Serie geht die Nachfrage an GENAU EINEN Peer - den mit dem
  // groessten Ueberschuss. Alle zu fragen hiesse, dieselben Saetze mehrfach
  // durch den Server zu ziehen.
  const plan = (ownCoverage, offers, now) => {
    const jobs = [];
    TIERS.forEach(tier => {
      const seriesNames = new Set();
      Object.values(offers).forEach(coverage => {
        Object.keys((coverage && coverage[tier]) || {}).forEach(series => seriesNames.add(series));
      });
      seriesNames.forEach(series => {
        const own = ((ownCoverage || {})[tier] || {})[series];
        let best = null;
        Object.entries(offers).forEach(([peer, coverage]) => {
          const theirs = ((coverage || {})[tier] || {})[series];
          const score = surplus(own, theirs, now);
          if (score > 0 && (best === null || score > best.score)) best = {peer, theirs, score};
        });
        if (!best) return;
        const ranges = missingRanges(own, best.theirs, now);
        if (ranges.length) jobs.push({peer: best.peer, tier, series, ranges});
      });
    });
    return jobs;
  };

  window.HistoryCoverage = {RASTER, TIERS, rasterWindow, census, missingRanges, surplus, plan};
})();
