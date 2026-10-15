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
  const t = (key, params) => (window.I18n ? window.I18n.t(key, params) : key);

  const RASTER = {
    '1m': {stepMs: 3600000, windowMs: 7 * 24 * 3600000},
    '5m': {stepMs: 21600000, windowMs: 30 * 24 * 3600000},
  };

  const TIERS = Object.keys(RASTER);

  // Minuten-Werte gelten erst nach dieser Zeit als abgeschlossen, bis dahin
  // koennen Geraete noch unterschiedlich viele Saetze haben.
  const SETTLE_MS = 30 * 60000;
  // Eine Nachfrage fuer den offenen Bucket lohnt erst ab dieser Laenge.
  const MIN_OPEN_MS = 5 * 60000;

  const rasterWindow = (tier, now) => {
    const raster = RASTER[tier];
    if (!raster) throw new Error(t('history.store.unknown_tier', {level: tier}));
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

  // Ohne cutoff wird der Bucket, in den "jetzt" faellt, immer ausgelassen:
  // dort sind zwei Geraete naturgemaess unterschiedlich weit, und ohne diese
  // Ausnahme entstuende aus dem Vergleich eine Dauerschleife aus Nachfragen.
  // Mit cutoff werden Buckets ab der Grenze ausgelassen, der Bucket mit der
  // Grenze zaehlt mit und wird auf die Grenze gekuerzt.
  const skipper = (peer, now, cutoff) => (index => (cutoff === undefined
    ? index === Math.floor((now - peer.from) / peer.step)
    : peer.from + index * peer.step >= cutoff));

  const missingRanges = (own, peer, now, cutoff) => {
    if (!peer || !peer.n || !peer.n.length) return [];
    const base = own && own.n ? own : emptyCensus(peer);
    if (base.from !== peer.from || base.step !== peer.step) return [];
    const skip = skipper(peer, now, cutoff);
    const ranges = [];
    let open = null;
    for (let index = 0; index < peer.n.length; index += 1) {
      const wanted = !skip(index) && (peer.n[index] || 0) > (base.n[index] || 0);
      if (wanted && open === null) open = index;
      if (!wanted && open !== null) {
        ranges.push([peer.from + open * peer.step, peer.from + index * peer.step]);
        open = null;
      }
    }
    if (open !== null) ranges.push([peer.from + open * peer.step, peer.from + peer.n.length * peer.step]);
    return cutoff === undefined ? ranges : clip(ranges, -Infinity, cutoff);
  };

  const surplus = (own, peer, now, cutoff) => {
    if (!peer || !peer.n) return 0;
    const base = own && own.n ? own : emptyCensus(peer);
    const skip = skipper(peer, now, cutoff);
    let total = 0;
    for (let index = 0; index < peer.n.length; index += 1) {
      if (skip(index)) continue;
      const difference = (peer.n[index] || 0) - (base.n[index] || 0);
      if (difference > 0) total += difference;
    }
    return total;
  };

  // Schneidet Spannen auf [from, to) zu, leere fallen weg.
  const clip = (ranges, from, to) => ranges
    .map(([start, end]) => [Math.max(start, from), Math.min(end, to)])
    .filter(([start, end]) => end > start);

  // Der Bucket mit der Grenze bleibt bei beiden Geraeten unterschiedlich
  // voll, auch nach einer Lieferung. Ohne askedUntil hiesse das bei jedem
  // Angebot dieselbe Nachfrage.
  const trimOpen = (ranges, openStart, cutoff, asked) => ranges
    .map(([start, end]) => (end > openStart && asked !== undefined ? [Math.max(start, asked), end] : [start, end]))
    .filter(([start, end]) => end > start && (end <= openStart || end - start >= MIN_OPEN_MS));

  // Je Stufe und Serie geht die Nachfrage an GENAU EINEN Peer - den mit dem
  // groessten Ueberschuss. Alle zu fragen hiesse, dieselben Saetze mehrfach
  // durch den Server zu ziehen.
  //
  // filter (optional) ist {tier, from, to}: nur diese Stufe planen und jede
  // Spanne auf den angezeigten Zeitraum zuschneiden (Modus on-demand in
  // history-exchange.js). Hat der beste Peer im Zeitraum nichts, kommt der
  // naechstbeste dran.
  //
  // options (optional) ist {cutoff, askedUntil}: cutoff gilt nur fuer die
  // Stufe 1m (siehe missingRanges), askedUntil ist {"<peer>|<tier>|<series>":
  // ms} und begrenzt den offenen Bucket auf Spannen nach der letzten Nachfrage.
  const plan = (ownCoverage, offers, now, filter, options = {}) => {
    const jobs = [];
    TIERS.forEach(tier => {
      if (filter && filter.tier !== tier) return;
      const cutoff = tier === '1m' ? options.cutoff : undefined;
      const seriesNames = new Set();
      Object.values(offers).forEach(coverage => {
        Object.keys((coverage && coverage[tier]) || {}).forEach(series => seriesNames.add(series));
      });
      seriesNames.forEach(series => {
        const own = ((ownCoverage || {})[tier] || {})[series];
        const candidates = [];
        Object.entries(offers).forEach(([peer, coverage]) => {
          const theirs = ((coverage || {})[tier] || {})[series];
          const score = surplus(own, theirs, now, cutoff);
          if (score > 0) candidates.push({peer, theirs, score});
        });
        // sort() ist stabil: bei Gleichstand gewinnt wie bisher der zuerst
        // gesehene Peer.
        candidates.sort((left, right) => right.score - left.score);
        for (const candidate of candidates) {
          let ranges = missingRanges(own, candidate.theirs, now, cutoff);
          if (cutoff !== undefined) {
            const {from, step} = candidate.theirs;
            const openStart = Math.floor((cutoff - from) / step) * step + from;
            const asked = options.askedUntil
              ? options.askedUntil[`${candidate.peer}|${tier}|${series}`]
              : undefined;
            ranges = trimOpen(ranges, openStart, cutoff, asked);
          }
          if (filter) ranges = clip(ranges, filter.from, filter.to);
          if (ranges.length) {
            jobs.push({peer: candidate.peer, tier, series, ranges});
            return;
          }
        }
      });
    });
    return jobs;
  };

  window.HistoryCoverage = {
    RASTER, TIERS, SETTLE_MS, MIN_OPEN_MS, rasterWindow, census, missingRanges, surplus, plan,
  };
})();
