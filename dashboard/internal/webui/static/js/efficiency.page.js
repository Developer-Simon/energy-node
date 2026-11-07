// Renders the efficiency tab. The live state of the chains comes from the
// registry-updated events (detail.energy.chains), the 30 day figures and the
// efficiency curve from GET /api/v1/energy/efficiency. The page reloads that
// endpoint only when the energy statistics change (energy_stats.rev) or the set
// of chains does. Writes (tolerance, relearn) go through the energy chain API
// with the CSRF token of the session.
//
// i18n-keys: efficiency.curve.title, efficiency.curve.unavailable, efficiency.curve.learning,
// efficiency.diagnosis.title, efficiency.diagnosis.ok, efficiency.diagnosis.learning,
// efficiency.diagnosis.since, efficiency.segment.manual, efficiency.segment.adjust,
// efficiency.segment.tolerance_w, efficiency.segment.tolerance_pct, efficiency.segment.auto,
// efficiency.segment.relearn, efficiency.segment.relearn_confirm, efficiency.segment.invalid,
// chains.suggestion.apply
(() => {
  const CHAIN_KEY = 'efficiency.chain';
  // Long enough for the card intro (250 ms plus 40 ms per card) and the bar stagger (300 ms plus 20 ms per bar).
  const INTRO_MS = 700;

  // Reduced motion is read on each use, so a change of the system setting applies at the next draw.
  const prefersReducedMotion = () => typeof window.matchMedia === 'function'
    && window.matchMedia('(prefers-reduced-motion: reduce)').matches;

  const t = (key, params) => window.I18n.t(key, params);

  // Same contract as energy.page.js: a CSRF token goes in with the write options.
  const requestJSON = async (url, options) => {
    const response = await fetch(`${window.__DASHBOARD_BASE_PATH__ || ''}${url}`, options);
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

  // A tolerance field: empty is null (automatic), a number from 0 is a value, anything else is invalid.
  const parseDraft = text => {
    const trimmed = String(text || '').trim();
    if (trimmed === '') return {ok: true, value: null};
    const value = Number(trimmed.replace(',', '.'));
    return Number.isFinite(value) && value >= 0 ? {ok: true, value} : {ok: false, value: null};
  };

  const sinceText = since => {
    const date = new Date(since);
    return Number.isNaN(date.getTime()) ? '' : window.I18n.formatDateTime(date);
  };

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
    // Editing rights and CSRF token of the session, as energy.page.js reads them.
    canEdit: true,
    csrfToken: '',
    // The curve: the chart instance, what it was drawn for, and the note shown instead.
    chart: null,
    drawnKey: '',
    drawnHost: null,
    // The live power the chart shows its operating line at (null: no line).
    drawnPower: null,
    // The chain the chart was drawn for, and whether the current-value line sits in place yet.
    drawnChainId: '',
    nowPlaced: false,
    // The intro (cards and bars) plays once per page load, after the first load.
    introPlayed: false,
    introTimer: null,
    themeOff: null,
    curveNote: '',
    // The segment whose tolerance form is open ('a|b'), and its two draft fields.
    editing: '',
    draftW: '',
    draftPct: '',

    init() {
      this.registryListener = event => this.onRegistry(event.detail || {});
      window.addEventListener('registry-updated', this.registryListener);
      this.rolesListener = () => this.loadCategories();
      window.addEventListener('energy-roles-changed', this.rolesListener);
      // The curve takes its colours from the theme, so a theme change draws it again.
      if (window.DashboardTheme) this.themeOff = window.DashboardTheme.onChange(() => this.redrawCurve());
      this.loadCategories();
      return Promise.all([this.loadSession(), this.load()]);
    },

    destroy() {
      if (this.registryListener) window.removeEventListener('registry-updated', this.registryListener);
      if (this.rolesListener) window.removeEventListener('energy-roles-changed', this.rolesListener);
      if (this.themeOff) this.themeOff();
      this.themeOff = null;
      if (this.introTimer) window.clearTimeout(this.introTimer);
      this.introTimer = null;
      this.destroyCurve();
    },

    // The intro starts with the first loaded chains and ends once its longest step is over.
    startIntro() {
      if (this.introPlayed || this.introTimer) return;
      this.introTimer = window.setTimeout(() => {
        this.introPlayed = true;
        this.introTimer = null;
      }, INTRO_MS);
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
      return {tone: result.tone, text: result.text};
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

    // The segments of the live chain with their normal band and tolerance.
    get segments() {
      return this.state ? window.EfficiencyModel.segmentRows(this.state) : [];
    },

    // Findings with their text, since-time and suggestion. The suggestion can be taken over with edit rights.
    get diagnosis() {
      const state = this.state || {};
      const findings = (state.findings || []).map(finding => {
        const since = sinceText(finding.since);
        const suggestion = window.EnergyChains.suggestionText(state, finding);
        return {
          key: `${finding.kind}|${finding.from || ''}|${finding.to || ''}`,
          text: window.EnergyChains.findingText(state, finding),
          sinceText: since ? t('efficiency.diagnosis.since', {time: since}) : '',
          suggestion,
          canApply: this.canEdit && Boolean(finding.suggestion),
          finding,
        };
      });
      return {
        findings,
        quiet: findings.length === 0,
        learning: Boolean(state.learning),
        segments: this.segments,
      };
    },

    // The confirmation line of the diagnosis card when nothing is found.
    get diagnosisLine() {
      return this.diagnosis.learning ? t('efficiency.diagnosis.learning') : t('efficiency.diagnosis.ok');
    },

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
      this.syncCurve();
    },

    // The energy page is the place to add a chain, so the empty state links there.
    openEnergy() {
      window.dispatchEvent(new CustomEvent('dashboard-open-panel', {detail: {panel: 'energy-panel'}}));
    },

    // Hands a finding's suggestion to the energy page as a draft. Nothing is saved there before the user saves.
    applySuggestion(item) {
      if (!this.canEdit || !item || !item.canApply) return;
      window.dispatchEvent(new CustomEvent('dashboard-open-panel', {
        detail: {panel: 'energy-panel', energyDraft: item.finding.suggestion},
      }));
    },

    rowKey(row) { return `${row.a}|${row.b}`; },

    isEditing(row) { return this.editing === this.rowKey(row); },

    // Opens the tolerance form of a segment with its current values, or closes it.
    openEdit(row) {
      if (!this.canEdit) return;
      if (this.isEditing(row)) {
        this.editing = '';
        return;
      }
      this.editing = this.rowKey(row);
      this.draftW = row.tolerance && row.tolerance.w != null ? String(row.tolerance.w) : '';
      this.draftPct = row.tolerance && row.tolerance.pct != null ? String(row.tolerance.pct) : '';
    },

    // Saves the two fields of the segment. Empty fields send null, so both empty removes the tolerance.
    async saveTolerance(row) {
      if (!this.canEdit || !this.currentId) return;
      const w = parseDraft(this.draftW);
      const pct = parseDraft(this.draftPct);
      if (!w.ok || !pct.ok) {
        this.$store.toasts.push(t('efficiency.segment.invalid'), 'critical');
        return;
      }
      await this.writeChain('PUT', 'tolerance', {a: row.a, b: row.b, w: w.value, pct: pct.value});
    },

    // The automatic band: both fields empty.
    clearTolerance(row) {
      if (!this.canEdit) return Promise.resolve();
      this.draftW = '';
      this.draftPct = '';
      return this.saveTolerance(row);
    },

    // Forgets what was learned about the segment, after a confirmation.
    async relearn(row) {
      if (!this.canEdit || !this.currentId) return;
      const confirmed = await this.$store.modal.confirm({
        title: t('efficiency.segment.relearn'),
        body: t('efficiency.segment.relearn_confirm'),
        confirmLabel: t('efficiency.segment.relearn'),
        danger: true,
      });
      if (!confirmed) return;
      await this.writeChain('POST', 'relearn', {a: row.a, b: row.b});
    },

    // One write on the chain API. On success the form closes and the figures reload; on failure the input stays.
    async writeChain(method, action, body) {
      try {
        await requestJSON(`/api/v1/energy/chains/${encodeURIComponent(this.currentId)}/${action}`, {
          method,
          headers: {'Content-Type': 'application/json', 'X-CSRF-Token': this.csrfToken},
          body: JSON.stringify(body),
        });
        this.editing = '';
        this.draftW = '';
        this.draftPct = '';
        await this.load();
      } catch (error) {
        this.$store.toasts.push(error.message, 'critical');
      }
    },

    // The curve series of the current chain, as the model names them.
    get curveSeries() {
      const chain = this.current;
      if (!chain) return [];
      return window.EfficiencyModel.curveSeries(chain.curve || {}, this.state || {}, this.categories);
    },

    // The legend under the curve: the best range and the operating line, when drawn.
    get curveLegend() {
      const series = this.curveSeries;
      return window.EfficiencyModel.curveLegend(series, this.curveBest(series), this.livePower);
    },

    // The live power of the current chain in W, null when it shows none.
    get livePower() {
      return window.EfficiencyModel.livePowerOf(this.state);
    },

    // The best range of the direction the first series shows.
    curveBest(series) {
      const chain = this.current;
      const curve = (chain && chain.curve) || {};
      const reversed = Boolean(this.state) && this.state.direction === 'reversed';
      const byKey = {
        charge: reversed ? 'best_up' : 'best_down',
        discharge: reversed ? 'best_down' : 'best_up',
        down: 'best_down',
        up: 'best_up',
      };
      return series.length ? (curve[byKey[series[0].key]] || null) : null;
    },

    // What the curve is drawn for. A new revision, a new chain, or the first live state of the chain changes it.
    curveKey() {
      return `${this.currentId}|${this.dataRevision}|${this.state ? 'live' : 'none'}`;
    },

    // Draws the curve again only when its key changes, never on every registry tick.
    // Between two draws only the operating line moves. A new chain draws in, a new
    // revision of the same chain morphs into its new values, a theme change redraws still.
    syncCurve(still = false) {
      const host = this.$refs && this.$refs.curve;
      if (!host || !this.current) return;
      const key = this.curveKey();
      if (key === this.drawnKey && host === this.drawnHost) {
        this.syncOperatingLine();
        return;
      }
      const sameChain = Boolean(this.chart) && this.drawnChainId === this.currentId && host === this.drawnHost;
      const switched = Boolean(this.chart) && this.drawnChainId !== this.currentId;
      this.drawnKey = key;
      this.drawnHost = host;
      this.drawnChainId = this.currentId;
      let mode = 'draw';
      if (still) mode = 'still';
      else if (sameChain) mode = 'morph';
      this.drawCurve(host, mode, switched);
    },

    // Moves the operating line when the live power changed by at least 1 W. The axis max
    // grows to keep the line on the plot. That update has no animation, so the series stays put.
    syncOperatingLine() {
      if (!this.chart) return;
      const power = this.livePower;
      if (samePower(power, this.drawnPower)) return;
      this.drawnPower = power;
      const series = this.curveSeries;
      const marks = window.EfficiencyModel.curveMarks(series, this.curveBest(series), power);
      Promise.resolve(this.chart.updateOptions({xaxis: {max: marks.max === null ? undefined : marks.max}}, false, false))
        .then(() => this.positionNow());
    },

    // Draws the curve again with the current theme colours, without an animation.
    redrawCurve() {
      this.drawnKey = '';
      this.syncCurve(true);
    },

    // Places the current-value line over the plot. Its x comes from the chart's own x scale,
    // so a new power glides (a transform transition) and a missing power hides it.
    positionNow() {
      const el = this.$refs && this.$refs.now;
      if (!el) return;
      const globals = this.chart && this.chart.w ? this.chart.w.globals : null;
      const offset = globals
        ? window.EfficiencyModel.operatingOffset(this.livePower, globals.minX, globals.maxX, globals.gridWidth)
        : null;
      el.classList.toggle('is-static', prefersReducedMotion());
      if (offset === null) {
        el.style.opacity = '0';
        return;
      }
      el.style.top = `${globals.translateY}px`;
      el.style.height = `${globals.gridHeight}px`;
      const x = `translateX(${globals.translateX + offset}px)`;
      if (!this.nowPlaced) {
        // The first placement of a chart does not glide in from the left edge.
        el.classList.add('is-initial');
        el.style.transform = x;
        void el.offsetWidth;
        el.classList.remove('is-initial');
        this.nowPlaced = true;
      } else {
        el.style.transform = x;
      }
      el.style.opacity = '1';
    },

    // Draws the chart for a mode (see syncCurve). A morph updates the existing chart in place;
    // every other mode destroys the old chart first. Without points there is a note; without ApexCharts a text.
    drawCurve(host, mode, switched) {
      const series = this.curveSeries;
      if (!series.length) {
        this.destroyCurve();
        this.curveNote = t('efficiency.curve.learning');
        return;
      }
      if (!window.ApexCharts) {
        this.destroyCurve();
        this.curveNote = t('efficiency.curve.unavailable');
        return;
      }
      this.curveNote = '';
      this.drawnPower = this.livePower;
      const reduced = prefersReducedMotion();
      const options = window.EfficiencyModel.curveOptions(series, this.curveBest(series), host.clientWidth, this.drawnPower, mode, reduced);
      options.chart.events = {mounted: () => this.positionNow(), updated: () => this.positionNow()};
      if (mode === 'morph' && this.chart) {
        Promise.resolve(this.chart.updateOptions(options, false, true)).then(() => this.positionNow());
        return;
      }
      this.destroyCurve();
      // A chain switch fades the host in, instead of swapping the chart in one step.
      if (switched && typeof host.animate === 'function') {
        host.animate([{opacity: 0}, {opacity: 1}], {duration: reduced ? 120 : 160, easing: 'cubic-bezier(0.23, 1, 0.32, 1)'});
      }
      this.chart = new window.ApexCharts(host, options);
      Promise.resolve(this.chart.render()).then(() => this.positionNow());
    },

    destroyCurve() {
      this.nowPlaced = false;
      if (!this.chart) return;
      try { this.chart.destroy(); } catch (error) { /* the chart is gone with its host */ }
      this.chart = null;
    },

    // A live update carries the chain states. A new or deleted chain also
    // reloads the list, and a new energy statistics revision reloads the figures.
    onRegistry(detail) {
      let reload = false;
      if (detail.energy) {
        this.live = Object.fromEntries((detail.energy.chains || []).map(chain => [chain.id, chain]));
        reload = !sameIds(Object.keys(this.live), this.chains.map(chain => chain.id));
        this.syncCurve();
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
        this.startIntro();
      } catch (error) {
        if (seq === this.seq) this.error = error.message;
      } finally {
        if (seq === this.seq) this.loading = false;
      }
      if (seq === this.seq) {
        // The template renders the chart host after the data; draw once it exists.
        if (this.$nextTick) this.$nextTick(() => this.syncCurve()); else this.syncCurve();
      }
    },

    // Rights and token of the session, as energy.page.js reads them. Without a session API the instance runs without login.
    async loadSession() {
      try {
        const session = await requestJSON('/api/v1/auth/session');
        this.csrfToken = session.csrf_token || '';
        this.canEdit = session.edit_energy !== false;
      } catch (error) {
        this.csrfToken = '';
        this.canEdit = true;
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

  // Two power values are the same when both are missing or they differ by less than 1 W.
  const samePower = (first, second) => {
    if (first === null || second === null) return first === second;
    return Math.abs(first - second) < 1;
  };

  const register = () => {
    if (window.Alpine) window.Alpine.data('efficiencyPanel', efficiencyPanel);
  };
  if (window.Alpine) register(); else document.addEventListener('alpine:init', register, {once: true});
})();
