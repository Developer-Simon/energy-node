// Kachel energy_stats: Kennzahlen der Energiestatistik fuer heute, diese
// Woche, diesen Monat oder dieses Jahr, ohne Diagramm. Die Uebersicht
// tauscht ihre Karten bis zu einmal je Sekunde per outerHTML aus. Damit
// nicht jeder Tausch eine Abfrage ausloest, liegen Antworten 60 s in einem
// Modul-Cache, und der gewaehlte Zeitraum je Kachel ueberlebt den Tausch.
(() => {
  const CACHE_MS = 60 * 1000;
  const CARD_KPIS = ['consumption', 'pv', 'grid_import', 'grid_export', 'autarky'];
  const cache = new Map();
  const periodByItem = new Map();

  function cached(period, now = Date.now()) {
    const hit = cache.get(period);
    if (hit && now - hit.at < CACHE_MS) return hit.promise;
    const promise = window.EnergyStatsModel.fetchStats(period, '');
    cache.set(period, {at: now, promise});
    // Ein Fehler soll beim naechsten Versuch neu fragen.
    promise.catch(() => { if (cache.get(period)?.promise === promise) cache.delete(period); });
    return promise;
  }

  const energyStatsCard = () => ({
    period: 'day',
    itemId: '',
    kpis: [],
    coverage: '',
    coverageParts: [],
    error: '',
    data: null,

    init() {
      const host = this.$root && this.$root.closest('[data-layout-item-id]');
      this.itemId = (host && host.dataset.layoutItemId) || 'energy-stats';
      this.period = periodByItem.get(this.itemId) || 'day';
      return this.load();
    },

    async load() {
      const period = this.period;
      try {
        const data = await cached(period);
        if (period !== this.period) return;
        const model = window.EnergyStatsModel;
        this.data = data;
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
      return this.load();
    },

    open() {
      const at = (this.data && this.data.at) || '';
      window.dispatchEvent(new CustomEvent('dashboard-open-panel', {detail: {panel: 'stats-panel', statsFocus: {period: this.period, at}}}));
    },
  });

  energyStatsCard.cached = cached;
  energyStatsCard.clearCache = () => { cache.clear(); periodByItem.clear(); };
  energyStatsCard.CARD_KPIS = CARD_KPIS;

  const register = () => {
    if (window.Alpine) window.Alpine.data('energyStatsCard', energyStatsCard);
  };
  if (window.Alpine) register(); else document.addEventListener('alpine:init', register, {once: true});
})();
