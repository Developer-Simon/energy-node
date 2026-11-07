// Renders the efficiency tab. The live state of the chains comes from the
// registry-updated events (detail.energy.chains), the 30 day figures from
// GET /api/v1/energy/efficiency. The page reloads that endpoint only when the
// energy statistics change (energy_stats.rev) or the set of chains does.
(() => {
  const CHAIN_KEY = 'efficiency.chain';

  const t = (key, params) => window.I18n.t(key, params);

  const requestJSON = async url => {
    const response = await fetch(`${window.__DASHBOARD_BASE_PATH__ || ''}${url}`);
    const body = await response.json().catch(() => ({}));
    if (!response.ok) throw new Error(window.I18n.error(body, 'common.request_failed'));
    return body;
  };

  // The chosen chain survives a reload, per browser. Storage can be blocked.
  const readChain = () => {
    try { return window.localStorage.getItem(CHAIN_KEY) || ''; } catch (error) { return ''; }
  };
  const writeChain = id => {
    try { window.localStorage.setItem(CHAIN_KEY, id); } catch (error) { /* only this page view keeps the choice */ }
  };

  const sameIds = (first, second) => first.length === second.length
    && [...first].sort().join('\n') === [...second].sort().join('\n');

  // A chain without any recorded hour has no 30 day figures and no day rows.
  const hasHistory = chain => Boolean(chain)
    && (chain.round_trip_30d != null || chain.standby_w_30d != null || chain.loss_month_kwh > 0
      || (chain.days || []).some(day => day.has_data));

  // The segment between two neighbouring points, in either flow direction.
  const segmentBetween = (state, first, second) => (state.segments || []).find(item => (item.from === first && item.to === second)
    || (item.from === second && item.to === first)) || null;

  const efficiencyPanel = () => ({
    // Entries of GET /api/v1/energy/efficiency, one per chain.
    chains: [],
    // Revision of the statistics the loaded chains were computed from.
    dataRevision: null,
    // Live ChainState per chain id, from the last registry-updated event.
    live: {},
    // Roles with their categories, needed to tell custom storage apart.
    categories: {},
    chainId: readChain(),
    loading: false,
    error: '',
    seq: 0,
    seenRevision: null,
    rolesSeq: 0,
    registryListener: null,
    rolesListener: null,

    init() {
      this.registryListener = event => this.onRegistry(event.detail || {});
      window.addEventListener('registry-updated', this.registryListener);
      this.rolesListener = () => this.loadCategories();
      window.addEventListener('energy-roles-changed', this.rolesListener);
      this.loadCategories();
      return this.load();
    },

    destroy() {
      if (this.registryListener) window.removeEventListener('registry-updated', this.registryListener);
      if (this.rolesListener) window.removeEventListener('energy-roles-changed', this.rolesListener);
    },

    get empty() { return !this.loading && !this.error && this.chains.length === 0; },

    // The chosen chain, or the first one when the stored ID is unknown or deleted.
    get current() {
      if (!this.chains.length) return null;
      return this.chains.find(chain => chain.id === this.chainId) || this.chains[0];
    },
    get currentId() { return this.current ? this.current.id : ''; },
    get state() { return this.current ? (this.live[this.current.id] || null) : null; },
    get chainName() {
      const chain = this.current;
      return chain ? this.labelOf(chain) : '';
    },
    get status() {
      const result = window.EfficiencyModel.statusOf(this.state || {no_current_values: true});
      return {tone: result.tone, text: t(result.key, result.params)};
    },
    get monthLabel() {
      const chain = this.current;
      if (!chain || !chain.month) return '';
      return window.I18n.formatDate(window.EnergyStatsModel.localDate(`${chain.month}-01T00:00`), {month: 'long'});
    },
    get kpis() {
      const chain = this.current;
      // Without recorded hours the 30 day figures stay empty, not zero.
      const figures = hasHistory(chain) ? chain : {};
      return window.EfficiencyModel.kpiTiles(this.state, figures, this.monthLabel);
    },
    get noHistory() { return Boolean(this.current) && !hasHistory(this.current); },
    get days() {
      const chain = this.current;
      return chain ? window.EfficiencyModel.dayRows(chain.days || []) : [];
    },
    get points() { return this.state ? (this.state.points || []) : []; },

    labelOf(chain) {
      const live = this.live[chain.id];
      return (live && live.label) || chain.label || chain.id;
    },

    stateLabel(state) {
      const key = state ? window.EfficiencyModel.stateKey(state, this.categories) : 'no_values';
      return t(`efficiency.state.${key}`);
    },

    chainStateText(chain) { return this.stateLabel(this.live[chain.id] || null); },

    chainTone(chain) {
      return window.EfficiencyModel.statusOf(this.live[chain.id] || {no_current_values: true}).tone;
    },

    pointText(point) {
      return point && point.has_value ? window.DeviceMapModel.formatPower(point.value) : '–';
    },

    // The number behind pointText, for x-roll. Empty sets the text at once.
    pointRaw(point) {
      return point && point.has_value ? Number(point.value) : '';
    },

    // Link i sits between point i and point i + 1.
    linkSegment(index) {
      const first = this.points[index];
      const second = this.points[index + 1];
      if (!first || !second) return null;
      return segmentBetween(this.state, first.entity_id, second.entity_id);
    },

    linkLossText(index) { return window.EnergyChains.lossPowerOf(this.linkSegment(index)); },

    linkLossRaw(index) {
      const segment = this.linkSegment(index);
      return segment && segment.available ? Number(segment.loss_w) || 0 : '';
    },

    linkEfficiencyText(index) {
      const percent = window.EnergyChains.efficiencyPercent(this.linkSegment(index));
      return percent === null ? '' : window.EnergyChains.percentText(percent);
    },

    linkEfficiencyRaw(index) {
      const percent = window.EnergyChains.efficiencyPercent(this.linkSegment(index));
      return percent === null ? '' : percent;
    },

    // Chosen by the list or the select. Unknown IDs are ignored.
    selectChain(id) {
      if (!this.chains.some(chain => chain.id === id)) return;
      this.chainId = id;
      writeChain(id);
    },

    // The energy page is the place to add a chain, so the empty state links there.
    openEnergy() {
      window.dispatchEvent(new CustomEvent('dashboard-open-panel', {detail: {panel: 'energy-panel'}}));
    },

    // A live update carries the chain states. A new or deleted chain also
    // reloads the list, and a new energy statistics revision reloads the figures.
    onRegistry(detail) {
      let reload = false;
      if (detail.energy) {
        this.live = Object.fromEntries((detail.energy.chains || []).map(chain => [chain.id, chain]));
        reload = !sameIds(Object.keys(this.live), this.chains.map(chain => chain.id));
      }
      const branch = detail.energy_stats;
      const revision = branch && branch.rev;
      if (typeof revision === 'number' && revision !== this.seenRevision) {
        this.seenRevision = revision;
        if (revision !== this.dataRevision) reload = true;
      }
      return reload ? this.load() : Promise.resolve();
    },

    async load() {
      const seq = ++this.seq;
      this.loading = true;
      try {
        const body = await requestJSON('/api/v1/energy/efficiency');
        // A slower answer to an older request must not overwrite a newer one.
        if (seq !== this.seq) return;
        this.chains = Array.isArray(body.chains) ? body.chains : [];
        this.dataRevision = typeof body.revision === 'number' ? body.revision : null;
        this.error = '';
      } catch (error) {
        if (seq === this.seq) this.error = error.message;
      } finally {
        if (seq === this.seq) this.loading = false;
      }
    },

    // Categories only name the storage roles. Without them custom storage counts as active.
    async loadCategories() {
      const seq = ++this.rolesSeq;
      try {
        const roles = await requestJSON('/api/v1/energy/roles');
        if (seq === this.rolesSeq) this.categories = roles.categories || {};
      } catch (error) {
        // Keep the last known categories.
      }
    },
  });

  const register = () => {
    if (window.Alpine) window.Alpine.data('efficiencyPanel', efficiencyPanel);
  };
  if (window.Alpine) register(); else document.addEventListener('alpine:init', register, {once: true});
})();
