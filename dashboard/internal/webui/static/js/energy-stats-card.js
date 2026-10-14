// Kachel energy_stats: Kennzahlen der Energiestatistik fuer heute, diese
// Woche, diesen Monat oder dieses Jahr, ohne Diagramm. Die Zahlen aendern
// sich hoechstens einmal je Stunde. Das SSE-Thema energy_stats traegt die
// Revision der Statistik, und die Kachel laedt nur, wenn sie von der
// geladenen abweicht. Der 60-Sekunden-Cache bleibt als Schutz gegen doppelte
// Abfragen mehrerer Kacheln.
(() => {
  const CACHE_MS = 60 * 1000;
  const CARD_KPIS = ['consumption', 'pv', 'grid_import', 'grid_export', 'autarky'];
  const STORAGE_PREFIX = 'energy-stats-period:';
  // Zeitraum -> {at, promise, revision}. revision ist undefined, solange die
  // Antwort aussteht.
  const cache = new Map();
  const periodByItem = new Map();
  // Die lebenden Kacheln. Alpine ruft destroy(), wenn eine Kachel aus dem
  // DOM faellt.
  const cards = new Set();
  let knownRevision = null;

  function cached(period, now = Date.now()) {
    const hit = cache.get(period);
    if (hit && now - hit.at < CACHE_MS) return hit.promise;
    const entry = {at: now, promise: window.EnergyStatsModel.fetchStats(period, ''), revision: undefined};
    cache.set(period, entry);
    entry.promise.then(data => { entry.revision = data && data.revision; }, () => {
      // Ein Fehler soll beim naechsten Versuch neu fragen.
      if (cache.get(period) === entry) cache.delete(period);
    });
    return entry.promise;
  }

  // Ohne Speicher (privates Fenster, gesperrte Website-Daten) gilt der
  // Standardzeitraum.
  function storedPeriod(itemId) {
    try {
      const value = window.localStorage.getItem(STORAGE_PREFIX + itemId);
      return window.EnergyStatsModel.PERIODS.includes(value) ? value : '';
    } catch (error) {
      return '';
    }
  }

  function storePeriod(itemId, period) {
    try {
      window.localStorage.setItem(STORAGE_PREFIX + itemId, period);
    } catch (error) {
      // Ohne Speicher bleibt der Zeitraum nur bis zum Neuladen.
    }
  }

  // Wird je SSE-Ereignis mit dem Zweig energy_stats gerufen, also auch im
  // Leerlauf mit derselben Revision. Nur eine neue Revision verwirft die
  // gecachten Antworten mit anderer Revision und laedt jede Kachel in root
  // neu, deren Daten nicht passen.
  function applyRevision(root, branch) {
    const revision = branch && branch.rev;
    if (typeof revision !== 'number' || revision === knownRevision) return;
    knownRevision = revision;
    for (const [period, hit] of cache) {
      if (hit.revision !== undefined && hit.revision !== revision) cache.delete(period);
    }
    for (const card of cards) {
      if (card.revision === undefined || card.revision === revision) continue;
      if (root && card.$root && !root.contains(card.$root)) continue;
      card.load();
    }
  }

  const energyStatsCard = () => ({
    period: 'day',
    itemId: '',
    kpis: [],
    coverage: '',
    coverageParts: [],
    error: '',
    data: null,
    revision: undefined,

    init() {
      const host = this.$root && this.$root.closest('[data-layout-item-id]');
      this.itemId = (host && host.dataset.layoutItemId) || 'energy-stats';
      this.period = periodByItem.get(this.itemId) || storedPeriod(this.itemId) || 'day';
      cards.add(this);
      return this.load();
    },

    destroy() {
      cards.delete(this);
    },

    async load() {
      const period = this.period;
      try {
        const data = await cached(period);
        if (period !== this.period) return;
        const model = window.EnergyStatsModel;
        this.data = data;
        this.revision = data.revision;
        this.error = '';
        this.kpis = model.kpis(data.total, CARD_KPIS);
        this.coverage = model.coverageText(data.total && data.total.hours);
        this.coverageParts = model.coverageParts(data.total && data.total.hours);
      } catch (error) {
        this.error = error.message;
      }
    },

    select(period) {
      this.period = period;
      periodByItem.set(this.itemId, period);
      storePeriod(this.itemId, period);
      return this.load();
    },

    open() {
      const at = (this.data && this.data.at) || '';
      window.dispatchEvent(new CustomEvent('dashboard-open-panel', {detail: {panel: 'stats-panel', statsFocus: {period: this.period, at}}}));
    },
  });

  energyStatsCard.cached = cached;
  energyStatsCard.applyRevision = applyRevision;
  energyStatsCard.clearCache = () => { cache.clear(); periodByItem.clear(); cards.clear(); knownRevision = null; };
  energyStatsCard.CARD_KPIS = CARD_KPIS;
  energyStatsCard.STORAGE_PREFIX = STORAGE_PREFIX;

  window.energyStatsCards = {applyRevision};

  const register = () => {
    if (window.Alpine) window.Alpine.data('energyStatsCard', energyStatsCard);
  };
  if (window.Alpine) register(); else document.addEventListener('alpine:init', register, {once: true});
})();
