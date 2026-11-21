(() => {
  const t = (key, params) => (window.I18n ? window.I18n.t(key, params) : key);
  const apiError = (body, fallbackKey) => (window.I18n ? window.I18n.error(body, fallbackKey) : (body && body.message) || fallbackKey || 'common.request_failed');

  // The single place where a performance URL gets the reverse-proxy prefix that
  // base.html puts into __DASHBOARD_BASE_PATH__; on direct access it is empty.
  const apiURL = (path) => `${window.__DASHBOARD_BASE_PATH__ || ''}${path}`;

  const requestJSON = async (path, options) => {
    const response = await fetch(apiURL(path), options);
    const body = await response.json().catch(() => ({}));
    if (!response.ok) {
      const error = new Error(apiError(body));
      error.code = body.code || '';
      // The whole answer, so a caller can read fields such as the 409 conflicts.
      error.body = body;
      throw error;
    }
    return body;
  };

  // The measurements move slowly, so the page asks every 30 s while it is shown.
  const POLL_MS = 30 * 1000;

  // Empty answer before the first load, so the template never reads undefined.
  const EMPTY = {
    measured_s: 0,
    min_measure_s: 0,
    cores: 0,
    target_pct: 0,
    default_target_pct: 0,
    deduction: { pct: 0, reasons: [] },
    base_pct: 0,
    today_pct: 0,
    over_since_s: null,
    clients: { max: 0, seen: false },
    device: { temp_c: 0, throttled: false, ram_free_pct: 0, swap_pages_s: 0 },
    external: null,
    rows: [],
    units: [],
    change: null,
  };
  // Device values the page shows as "–" while the API has no reading.
  EMPTY.device = { temp_c: null, throttled: false, ram_free_pct: null, swap_pages_s: 0 };

  // Names of the units. The API uses systemd unit names for the fixed units
  // (automation.service) and service ids for the device services (shelly,
  // apsystems, tuya). The ".service" suffix is dropped before the lookup. The
  // directory names apsystems_ez1 and tuya_mqtt stay as aliases.
  const UNIT_LABELS = {
    'energy-node-dashboard': 'performance.js.unit.dashboard',
    automation: 'performance.js.unit.automation',
    mosquitto: 'performance.js.unit.mosquitto',
    tailscaled: 'performance.js.unit.tailscaled',
    caddy: 'performance.js.unit.caddy',
    battery_soc: 'performance.js.unit.battery_soc',
    apsystems: 'performance.js.unit.apsystems',
    tuya: 'performance.js.unit.tuya',
    apsystems_ez1: 'performance.js.unit.apsystems',
    tuya_mqtt: 'performance.js.unit.tuya',
    shelly: 'performance.js.unit.shelly',
    trucki: 'performance.js.unit.trucki',
    other: 'performance.js.unit.rest',
  };

  // Percent with one decimal in the language's separator.
  const pct = (value) => `${window.I18n.formatNumber(value, 1)} %`;
  const seconds = (value) => t('performance.js.seconds', { value: window.I18n.formatNumber(value, 1) });
  const megabytes = (bytes) => Math.round(Number(bytes) / 1024 / 1024);

  // Whole hours and minutes, "2 h 10 min" style.
  const duration = (value) => {
    const minutes = Math.floor(Math.max(0, Number(value) || 0) / 60);
    return t('performance.js.duration', { hours: Math.floor(minutes / 60), minutes: minutes % 60 });
  };

  // Unknown units show the id as the API sent it.
  const unitName = (id) => {
    const key = UNIT_LABELS[String(id).replace(/\.service$/, '')];
    return key ? t(key) : id;
  };

  // Full keys per row and right, so the catalog scanner sees every key literally.
  const ROW_LABEL_KEYS = {
    live_update: 'performance.js.row.live_update',
    tick: 'performance.js.row.tick',
    regulator: 'performance.js.row.regulator',
    node: 'performance.js.row.node',
    shelly: 'performance.js.row.shelly',
    trucki: 'performance.js.row.trucki',
    battery_soc: 'performance.js.row.battery_soc',
    apsystems: 'performance.js.row.apsystems',
    tuya: 'performance.js.row.tuya',
  };
  const RIGHT_LABEL_KEYS = {
    edit_setup: 'performance.js.right.edit_setup',
    automations: 'performance.js.right.automations',
    system_actions: 'performance.js.right.system_actions',
  };

  // The effect check runs this long after an applied change.
  const CHECK_MINUTES = 15;
  // Full keys per state, so the catalog scanner sees every key literally.
  const CHECK_TITLE_KEYS = {
    running: 'performance.js.check.title.running',
    ok: 'performance.js.check.title.ok',
    bad: 'performance.js.check.title.bad',
    conflict: 'performance.js.check.title.conflict',
    undone: 'performance.js.check.title.undone',
  };
  const CHECK_NOTE_KEYS = {
    ok: 'performance.js.check.note.ok',
    bad: 'performance.js.check.note.bad',
    conflict: 'performance.js.check.note.conflict',
    undone: 'performance.js.check.note.undone',
  };
  const CHECK_TONES = { running: 'info', ok: 'ok', bad: 'bad', conflict: 'warn', undone: 'ok' };

  // Numbers as typed, with a German comma allowed.
  const parseNumber = (raw) => Number(String(raw).replace(',', '.'));

  // Breakdown of the load. Only these units have one, the others keep their
  // row as it is. The learning engine is a part of the dashboard.
  const UNIT_KIND = { 'energy-node-dashboard': 'dashboard', automation: 'automation' };
  // Full keys per entry, so the catalog scanner sees every key literally.
  const PART_KEYS = {
    learning: 'performance.js.breakdown.part.learning',
    push: 'performance.js.breakdown.part.push',
    http: 'performance.js.breakdown.part.http',
    mqtt: 'performance.js.breakdown.part.mqtt',
    node: 'performance.js.breakdown.part.node',
    background: 'performance.js.breakdown.part.background',
    gc: 'performance.js.breakdown.part.gc',
  };
  const GROUP_KEYS = {
    tick: 'performance.js.breakdown.group.tick',
    regulator: 'performance.js.breakdown.group.regulator',
  };
  const KIND_KEYS = {
    rule: 'performance.js.breakdown.kind.rule',
    scenario: 'performance.js.breakdown.kind.scenario',
  };
  const TAG_KEYS = {
    dashboard: 'performance.js.breakdown.tag.dashboard',
    automation: 'performance.js.breakdown.tag.automation',
  };
  const NOTE_KEYS = {
    dashboard: 'performance.js.breakdown.note.dashboard',
    automation: 'performance.js.breakdown.note.automation',
  };
  // Shares under 0.05 % read "< 0,1 %" instead of "0,0 %".
  const pctText = (value) => (value > 0 && value < 0.05
    ? t('performance.js.breakdown.below', { value: window.I18n.formatNumber(0.1, 1) })
    : pct(value));
  // Bar width of a share, relative to the value of the entry.
  const share = (value, of) => (of > 0 ? Math.min(1, Math.round((value / of) * 10000) / 10000) : 0);

  // Learning row. The selector order, the compute budget per round of each
  // level in seconds (spec 5.7) and the share of the load each level may take.
  // A higher level is projected from the measured share by the ratio of the
  // budgets, the API only gives the share of the current level.
  const LEARNING_LEVELS = ['auto', 'eco', 'balanced', 'full'];
  const LEARNING_BUDGET_S = { eco: 2, balanced: 10, full: 60 };
  const LEARNING_TARGET = { eco: 0.05, balanced: 0.10, full: 0.10 };

  const pfPage = () => ({
    // The answer of GET /api/v1/performance, as the page last received it.
    data: { ...EMPTY },
    timer: 0,
    // True after a failed load until the next one succeeds, so a failing API
    // raises one toast per failure streak instead of one every 30 s.
    failing: false,

    // Rate selection. cur holds the saved seconds per row id, sel the seconds
    // the user has picked, custom the raw input of a row's own value.
    // savedTarget is the saved limit in percent, targetDraft the edited one.
    cur: {},
    sel: {},
    custom: {},
    savedTarget: 0,
    targetDraft: 0,
    confirming: false,
    applying: false,
    // Rows changed since the rollback was applied, as {row: current seconds}.
    // Only set by a 409 rollback_conflict, cleared by a new change or a decision.
    conflict: {},
    undoing: false,
    // Open breakdowns by unit id. Kept across load(), so the 30 s poll does
    // not close them.
    breakdownOpen: {},
    // Learning row. learningSel is the drafted level, null while the saved one
    // stands. Saved by apply(), not by a pick.
    learningSel: null,

    init() {
      const first = this.load();
      // Fresh numbers every time the page is shown, not only on the first visit.
      this.$watch('activeSettingsPage', (page) => {
        if (page === 'performance') this.load();
      });
      this.timer = window.setInterval(() => {
        if (document.visibilityState !== 'visible') return;
        if (this.activeSettingsPage !== undefined && this.activeSettingsPage !== 'performance') return;
        this.load();
      }, POLL_MS);
      return first;
    },

    destroy() {
      window.clearInterval(this.timer);
      this.timer = 0;
    },

    // A poll keeps what the user picked but has not applied yet. Rows the user
    // has not touched follow the server, and so does the limit.
    async load() {
      try {
        const next = { ...EMPTY, ...(await requestJSON('/api/v1/performance')) };
        const rows = next.rows || [];
        const oldCur = this.cur;
        const newCur = {};
        const newSel = {};
        rows.forEach((row) => {
          newCur[row.id] = row.current;
          const touched = row.id in oldCur && this.sel[row.id] !== undefined && this.sel[row.id] !== oldCur[row.id];
          newSel[row.id] = touched ? this.sel[row.id] : row.current;
        });
        if (this.targetDraft === this.savedTarget) this.targetDraft = next.target_pct;
        this.savedTarget = next.target_pct;
        // A conflict belongs to one change, a new or vanished change clears it.
        const sameChange = Boolean(next.change) && Boolean(this.data.change) && next.change.at === this.data.change.at;
        if (!sameChange) this.conflict = {};
        this.cur = newCur;
        this.sel = newSel;
        this.data = next;
        this.failing = false;
      } catch (error) {
        if (!this.failing) this.$store.toasts.push(error.message, 'critical');
        this.failing = true;
      }
    },

    // Measuring runs until min_measure_s is reached, the budget is only
    // suggested after that.
    measuring() {
      return this.data.measured_s < this.data.min_measure_s;
    },
    available() {
      return this.targetDraft - (this.data.deduction?.pct || 0);
    },
    // The model total of the current selection, base load included.
    selectedPct() {
      return PerformanceCore.total(this.baseWithLearning(), this.data.rows || [], this.sel);
    },
    free() {
      return this.available() - this.selectedPct();
    },

    // After a bad check the measurement counts, not the estimate, unless the
    // user has edited the rates since.
    badCheck() {
      return this.checkState() === 'bad' && !this.ratesChanged();
    },
    headTone() {
      if (this.measuring()) return 'info';
      if (this.badCheck()) return 'bad';
      if (this.selectedPct() > this.available()) return 'bad';
      if (this.free() < 3) return 'warn';
      return 'ok';
    },
    headTitle() {
      if (this.badCheck()) {
        const measured = this.data.change.measured_pct;
        return t('performance.js.check.head_title', {
          measured: pct(measured),
          over: pct(measured - this.available()),
        });
      }
      const used = pct(this.selectedPct());
      const free = this.free();
      if (free < 0) return t('performance.js.head.used_over', { used, over: pct(-free) });
      return t('performance.js.head.used_free', { used, free: pct(free) });
    },
    headNote() {
      if (this.badCheck()) {
        return t('performance.js.check.head_note', { expected: pct(this.data.change.expected_pct) });
      }
      if (this.measuring()) {
        const remaining = t('performance.js.remaining', { duration: duration(this.data.min_measure_s - this.data.measured_s) });
        return t('performance.js.head.note_measuring', {
          since: duration(this.data.measured_s),
          hours: Math.round(this.data.min_measure_s / 3600),
          remaining,
        });
      }
      if (this.data.over_since_s !== null && this.data.over_since_s !== undefined) {
        return t('performance.js.head.note_over', { minutes: Math.round(this.data.over_since_s / 60) });
      }
      return t('performance.js.head.note_default');
    },

    // Two parts of the budget bar: the base load, then the polling share on top.
    meterParts() {
      const fixed = this.baseWithLearning();
      const parts = [
        { cls: 'pf-m-fixed', w: fixed },
        { cls: 'pf-m-now', w: this.selectedPct() - fixed },
      ];
      return parts.map((part) => ({ ...part, w: Math.max(0, Math.min(part.w, 100)) }));
    },

    // Two groups of the measurement card, each sorted by CPU. The share bar
    // is relative to the largest unit of the page, so both groups compare.
    measureGroups() {
      const units = this.data.units || [];
      const max = Math.max(0, ...units.map((unit) => Number(unit.cpu_pct) || 0));
      const rows = (fixed) => units
        .filter((unit) => Boolean(unit.fixed) === fixed)
        .map((unit) => {
          const cpu = Number(unit.cpu_pct) || 0;
          const ram = Number(unit.ram_bytes) > 0 ? t('performance.js.ram.mb', { mb: megabytes(unit.ram_bytes) }) : '';
          return {
            id: unit.id,
            name: unitName(unit.id),
            v: cpu,
            cpu: pct(cpu),
            ram,
            round: Number(unit.round_s) > 0 ? t('performance.js.round', { time: seconds(unit.round_s) }) : '',
            msg: Number(unit.msg_per_min) > 0 ? t('performance.js.msg', { count: unit.msg_per_min }) : '',
            share: max > 0 ? Math.round((cpu / max) * 10000) / 10000 : 0,
          };
        })
        .sort((a, b) => b.v - a.v);
      const group = (id, label, swatch, fixed) => {
        const list = rows(fixed);
        const sum = list.reduce((total, row) => total + row.v, 0);
        return { id, label, swatch, sum, sumText: pct(sum), rows: list };
      };
      return [
        group('polls', t('performance.js.group.polls'), 'pf-m-now', false),
        group('base', t('performance.js.group.base'), 'pf-m-fixed', true),
      ];
    },

    // ---- Rate selection -------------------------------------------------

    rowById(id) {
      return (this.data.rows || []).find((row) => row.id === id);
    },
    canEdit(row) {
      return Boolean(row.editable);
    },
    // The seconds a row runs with: the pick, or the saved value before any pick.
    selected(id) {
      return this.sel[id] !== undefined ? this.sel[id] : this.cur[id];
    },
    isCurrent(row, s) {
      return s === this.cur[row.id];
    },
    isSelected(row, s) {
      return s === this.selected(row.id);
    },
    ratesChanged() {
      return (this.data.rows || []).some((row) => this.selected(row.id) !== this.cur[row.id]);
    },
    // Rates or budget differ from the saved ones. Only these need the measuring
    // confirmation, the learning level is no rate.
    ratesOrTargetChanged() {
      return this.ratesChanged() || this.targetDraft !== this.savedTarget;
    },
    changed() {
      return this.ratesOrTargetChanged() || this.learningChanged();
    },
    ctx() {
      return {
        rows: this.data.rows || [],
        sel: this.sel,
        cur: this.cur,
        base: this.baseWithLearning(),
        available: this.available(),
        external: this.data.external,
        measuring: this.measuring(),
      };
    },

    // ---- Limit ----------------------------------------------------------

    setTarget(raw) {
      const v = Math.round(parseNumber(raw));
      if (Number.isFinite(v)) this.targetDraft = Math.min(95, Math.max(20, v));
    },
    stepTarget(dir) {
      const current = this.targetDraft;
      const next = dir > 0 ? Math.floor(current / 5) * 5 + 5 : Math.ceil(current / 5) * 5 - 5;
      this.setTarget(next);
    },

    // ---- Picks ----------------------------------------------------------

    pick(id, s) {
      const row = this.rowById(id);
      if (!row || !this.canEdit(row)) return;
      if (PerformanceCore.stepState(row, s, this.ctx()) !== 'ok') return;
      this.sel[id] = s;
      delete this.custom[id];
    },
    customInput(id, raw) {
      const row = this.rowById(id);
      this.custom[id] = raw;
      if (!row) return;
      const v = parseNumber(raw);
      if (!Number.isFinite(v) || v <= 0) return;
      // Follow rows may go below their hard floor, the soft floor only warns.
      const hard = row.follows ? 1 : row.floor;
      if (v >= hard) this.sel[id] = v;
    },
    customProblem(id) {
      const row = this.rowById(id);
      const raw = this.custom[id];
      if (row && row.follows && (raw === undefined || raw === '')) {
        // Auch ein schon eingestellter Wert unter der weichen Grenze bekommt den Hinweis.
        const soft = PerformanceCore.floorOf(row, this.data.rows || [], this.sel, this.data.external);
        if (this.selected(id) < soft) return { tone: 'info', text: t('performance.js.custom.below_source', { floor: seconds(soft) }) };
        return null;
      }
      if (!row || raw === undefined || raw === '') return null;
      const v = parseNumber(raw);
      if (!Number.isFinite(v) || v <= 0) return { tone: 'bad', text: t('performance.js.custom.invalid') };
      if (!row.follows && v < row.floor) {
        return { tone: 'bad', text: t('performance.js.custom.below_floor', { floor: seconds(row.floor) }) };
      }
      const over = this.selectedPct() - this.available();
      if (!this.measuring() && over > 0.05) {
        return { tone: 'warn', text: t('performance.js.custom.over_budget', { over: pct(over) }) };
      }
      if (row.follows) {
        const soft = PerformanceCore.floorOf(row, this.data.rows || [], this.sel, this.data.external);
        if (v < soft) return { tone: 'info', text: t('performance.js.custom.below_source', { floor: seconds(soft) }) };
      }
      return null;
    },
    even() {
      if (this.measuring()) return;
      const rows = this.data.rows || [];
      const editable = rows.filter((row) => this.canEdit(row)).map((row) => row.id);
      this.sel = PerformanceCore.distribute(rows, this.cur, this.available(), editable, this.ctx());
      this.custom = {};
    },
    reset() {
      this.sel = { ...this.cur };
      this.custom = {};
      this.targetDraft = this.savedTarget;
      this.learningSel = null;
      this.confirming = false;
    },

    // ---- Apply ----------------------------------------------------------

    // While measuring, the first press only asks for confirmation, but only when
    // rates or the budget change. A learning level alone is saved at once.
    requestApply() {
      if (!this.changed()) return;
      if (this.measuring() && !this.confirming && this.ratesOrTargetChanged()) {
        this.confirming = true;
        return;
      }
      return this.apply();
    },
    async apply() {
      if (this.applying || !this.changed()) return;
      const rates = {};
      (this.data.rows || []).forEach((row) => {
        const v = this.selected(row.id);
        if (v !== this.cur[row.id]) rates[row.id] = v;
      });
      const body = { rates };
      if (this.targetDraft !== this.savedTarget) body.target_pct = this.targetDraft;
      const writesRates = Object.keys(rates).length > 0 || body.target_pct !== undefined;
      const learningLevel = this.learningChanged() ? this.learningSetting() : '';

      this.applying = true;
      try {
        const session = await requestJSON('/api/v1/auth/session').catch(() => null);
        const csrfToken = (session && session.csrf_token) || '';
        if (learningLevel && session && session.edit_setup !== true) {
          throw new Error(t('performance.js.learning.locked'));
        }
        if (writesRates) {
          await requestJSON('/api/v1/performance/apply', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken }, // i18n-ignore
            body: JSON.stringify(body),
          });
        }
        // After the rates, so the settings write reads what the apply saved.
        if (learningLevel) await this.writeLearning(learningLevel, csrfToken);
        this.$store.toasts.push(t('performance.js.applied'), 'info');
        this.confirming = false;
        this.learningSel = null;
        if (rates.live_update !== undefined) {
          window.dispatchEvent(new CustomEvent('performance-live-update-changed', { detail: rates.live_update }));
        }
        await this.load();
      } catch (error) {
        this.$store.toasts.push(error.message, 'critical');
      } finally {
        this.applying = false;
      }
    },

    // ---- Effect check and rollback --------------------------------------

    // The state of the card: the server state, or conflict while a rollback
    // found rows changed since the apply and the user has not decided yet.
    checkState() {
      const change = this.data.change;
      if (!change) return null;
      if (change.state !== 'undone' && Object.keys(this.conflict).length > 0) return 'conflict';
      return change.state;
    },
    checkTone() {
      return CHECK_TONES[this.checkState()] || 'info';
    },
    checkTitle() {
      const state = this.checkState();
      if (state === 'running') return t(CHECK_TITLE_KEYS.running, { minutes: this.checkRemaining() });
      if (!state) return '';
      return t(CHECK_TITLE_KEYS[state]);
    },
    checkNote() {
      const change = this.data.change;
      const state = this.checkState();
      if (!change || !state) return '';
      const expected = pct(change.expected_pct);
      let note;
      if (state === 'running') {
        note = change.measured_pct === null || change.measured_pct === undefined
          ? t('performance.js.check.note.running', { expected })
          : t('performance.js.check.note.running_measured', { expected, measured: pct(change.measured_pct) });
        if (change.restarted) note = `${note} ${t('performance.js.check.note.restarted')}`;
        return note;
      }
      if (state === 'ok' || state === 'bad') {
        const params = { expected, measured: pct(change.measured_pct) };
        if (state === 'bad') params.available = pct(change.available_pct);
        return t(CHECK_NOTE_KEYS[state], params);
      }
      return t(CHECK_NOTE_KEYS[state]);
    },
    // Minutes since the change was applied, from the browser clock.
    checkElapsed() {
      const at = Date.parse(this.data.change?.at);
      if (!Number.isFinite(at)) return 0;
      return Math.max(0, (Date.now() - at) / 60000);
    },
    checkRemaining() {
      return Math.max(0, Math.ceil(CHECK_MINUTES - this.checkElapsed()));
    },
    checkProgress() {
      return Math.min(100, Math.max(0, (this.checkElapsed() / CHECK_MINUTES) * 100));
    },
    // The rows of the change, with the current value of each row that conflicts.
    changeList() {
      const change = this.data.change;
      if (!change) return [];
      return Object.keys(change.after || {}).map((id) => ({
        id,
        label: this.rowLabel(id),
        before: seconds(change.before[id]),
        after: seconds(change.after[id]),
        now: this.conflict[id] !== undefined ? seconds(this.conflict[id]) : null,
      }));
    },
    async rollback(onlyUnchanged) {
      const session = await requestJSON('/api/v1/auth/session').catch(() => null);
      const csrfToken = (session && session.csrf_token) || '';
      return requestJSON('/api/v1/performance/rollback', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken }, // i18n-ignore
        body: JSON.stringify({ only_unchanged: onlyUnchanged }),
      });
    },
    // Takes all changed rows back. A row changed since the apply stops the
    // rollback with a 409 and the conflict is shown instead.
    async tryUndo() {
      if (this.undoing) return;
      this.undoing = true;
      try {
        await this.rollback(false);
        this.conflict = {};
        this.$store.toasts.push(t('performance.js.check.undo_done'), 'info');
        await this.load();
      } catch (error) {
        if (error.code === 'rollback_conflict') {
          this.conflict = { ...(error.body.conflicts || {}) };
        } else {
          this.$store.toasts.push(error.message, 'critical');
        }
      } finally {
        this.undoing = false;
      }
    },
    // Takes back only the rows nobody has changed since the apply.
    async undoRest() {
      if (this.undoing) return;
      this.undoing = true;
      try {
        await this.rollback(true);
        this.conflict = {};
        this.$store.toasts.push(t('performance.js.check.undo_done'), 'info');
        await this.load();
      } catch (error) {
        this.$store.toasts.push(error.message, 'critical');
      } finally {
        this.undoing = false;
      }
    },
    // Keeps the changed rows as they are. The change then shows its server state.
    keepAll() {
      this.conflict = {};
    },

    // ---- Breakdown ------------------------------------------------------

    // The breakdown of a unit as {kind, parts} or {kind, groups}, or null
    // when the unit has none or the API has no data for it. The shares are
    // relative to the value of the unit, so a part reads as its share of it.
    breakdownOf(unitId) {
      const kind = UNIT_KIND[String(unitId).replace(/\.service$/, '')];
      const bd = this.data.breakdown;
      if (!kind || !bd || !bd[kind]) return null;
      const unit = (this.data.units || []).find((u) => u.id === unitId);
      const total = Number(unit?.cpu_pct) || 0;
      if (kind === 'dashboard') {
        // The learning part lists its categories (habit, energy, reliability,
        // other) under it, each as a share of the whole load like the parts.
        const parts = (bd.dashboard.parts || []).map((part) => ({
          ...part,
          name: this.partName(part.id),
          pctText: pctText(part.pct),
          share: share(part.pct, total),
          detectors: (part.detectors || []).map((detector) => ({
            ...detector,
            name: this.learningCategory(detector.id),
            pctText: pctText(detector.pct),
            share: share(detector.pct, total),
          })),
        }));
        return parts.length ? { kind, parts } : null;
      }
      const groups = (bd.automation.groups || []).map((group) => ({
        ...group,
        pctText: pctText(group.pct),
        morePctText: pctText(group.more_pct),
        moreShare: share(group.more_pct, total),
        items: group.items.map((item) => ({
          ...item,
          pctText: pctText(item.pct),
          share: share(item.pct, total),
        })),
      }));
      return groups.length ? { kind, groups } : null;
    },
    isOpen(unitId) {
      return Boolean(this.breakdownOpen[unitId]);
    },
    toggleBreakdown(unitId) {
      this.breakdownOpen = { ...this.breakdownOpen, [unitId]: !this.breakdownOpen[unitId] };
    },
    // Id of the panel the toggle controls, safe as an HTML id.
    panelId(unitId) {
      return `pb-${String(unitId).replace(/[^\w-]/g, '-')}`;
    },
    toggleLabel(row) {
      return t('performance.js.breakdown.toggle', { name: row.name });
    },
    // Dashboard parts have their own keys, the id is shown as it is otherwise.
    partName(id) {
      return PART_KEYS[id] ? t(PART_KEYS[id]) : id;
    },
    groupName(row) {
      return GROUP_KEYS[row] ? t(GROUP_KEYS[row]) : row;
    },
    kindName(kind) {
      return KIND_KEYS[kind] ? t(KIND_KEYS[kind]) : '';
    },
    clientsText(count) {
      return count <= 1
        ? t('performance.js.breakdown.clients_one')
        : t('performance.js.breakdown.clients', { count });
    },
    moreText(count) {
      return t('performance.js.breakdown.more', { count });
    },
    breakdownTag(kind) {
      return TAG_KEYS[kind] ? t(TAG_KEYS[kind]) : '';
    },
    breakdownNote(kind) {
      return NOTE_KEYS[kind] ? t(NOTE_KEYS[kind]) : '';
    },

    // ---- Row texts ------------------------------------------------------

    rowLabel(id) {
      return t(ROW_LABEL_KEYS[id] || id);
    },
    rowSub(id) {
      if (id === 'live_update') return t('performance.js.row_sub.live_update', { count: this.data.clients.max });
      return '';
    },
    lockText(row) {
      return t('performance.js.lock', { right: t(RIGHT_LABEL_KEYS[row.right] || row.right) });
    },
    // Budget and period notes of a row, as {tone, text}.
    notes(id) {
      const row = this.rowById(id);
      if (!row) return [];
      const out = [];
      if (row.round_s > 0) {
        const every = PerformanceCore.period(row, this.selected(id));
        out.push({ tone: '', text: t('performance.js.note.period', { every: seconds(every), round: seconds(row.round_s) }) });
      }
      if (this.measuring()) {
        const remaining = duration(this.data.min_measure_s - this.data.measured_s);
        out.push({ tone: '', text: t('performance.js.note.proposal_in', { duration: remaining }) });
        return out;
      }
      const note = PerformanceCore.budgetNote(row, this.ctx());
      if (note) out.push({ tone: note.tone || '', text: t(note.key, this.noteParams(note.params)) });
      return out;
    },
    // Seconds in note parameters are shown with the unit, as the other texts do.
    noteParams(params) {
      const out = { ...params };
      if (out.seconds !== undefined) out.seconds = seconds(out.seconds);
      return out;
    },

    // ---- Learning row ----------------------------------------------------

    learning() {
      return this.data.learning || null;
    },
    // The drafted level, or the saved one while no draft exists.
    learningSetting() {
      if (this.learningSel !== null) return this.learningSel;
      const l = this.learning();
      return l ? l.setting : 'auto';
    },
    // The draft differs from the saved level, so the apply writes it.
    learningChanged() {
      const l = this.learning();
      return Boolean(l) && this.learningSel !== null && this.learningSel !== l.setting;
    },
    // The level a setting stands for: auto is the resolved level of the answer.
    learningResolved(setting) {
      const l = this.learning();
      if (!l) return '';
      return setting === 'auto' ? l.level : setting;
    },
    learningLevels() {
      return LEARNING_LEVELS.map((id) => ({ id, label: t(`performance.js.learning.level.${id}`) }));
    },
    learningSummary() {
      const l = this.learning();
      if (!l) return '';
      return t('performance.js.learning.summary', {
        duration: seconds(l.per_run_ms / 1000),
        share: pct(l.share * 100),
      });
    },
    // The estimate of the drafted level: the measured share scaled by the ratio
    // of the budgets of the drafted and the current resolved level. over is true
    // only for a level above the current one whose estimate is above its target.
    learningEstimate() {
      const l = this.learning();
      if (!l) return null;
      const selected = this.learningResolved(this.learningSetting());
      const current = LEARNING_BUDGET_S[l.level];
      const chosen = LEARNING_BUDGET_S[selected];
      const ratio = current && chosen ? chosen / current : 1;
      const measured = l.share;
      const estimate = measured * ratio;
      const target = LEARNING_TARGET[selected] ?? LEARNING_TARGET.balanced;
      return { measured, estimate, target, over: ratio > 1 && estimate > target };
    },
    // Empty unless the drafted level is above the current one and its estimate
    // is above its target. The choice itself stays possible.
    learningOverNote() {
      const e = this.learningEstimate();
      if (!e || !e.over) return '';
      return t('performance.js.learning.over', {
        share: pct(e.measured * 100),
        projected: pct(e.estimate * 100),
        target: pct(e.target * 100),
      });
    },
    // The level the load can carry, marked on its button. Empty when none.
    learningSuggest() {
      const l = this.learning();
      return l && l.suggest ? l.suggest : '';
    },
    // The sentence why the suggested level fits, shown as the title of its button.
    learningSuggestText() {
      return this.learningSuggest() ? t('performance.js.learning.suggest') : '';
    },
    // The load the drafted level adds or saves, in percent of the budget: the
    // estimate minus the measured share. The saved base holds the measured
    // share, so the total moves by this amount. Zero without a draft.
    learningDeltaPct() {
      if (!this.learningChanged()) return 0;
      const e = this.learningEstimate();
      return e ? PerformanceCore.learningDeltaPct(e.measured, e.estimate, this.data.cores) : 0;
    },
    // The base load the total and the budget use: the saved base plus the draft.
    baseWithLearning() {
      return (this.data.base_pct || 0) + this.learningDeltaPct();
    },
    // The name of a learning category. Habit, energy and reliability use the
    // group names of the automations, every other id is other.
    learningCategory(id) {
      if (['habit', 'energy', 'reliability'].includes(id)) return t(`automations.learn.group.${id}`);
      return t('performance.js.learning.category_other');
    },
    // Drafts a level. Selecting never writes, apply() saves the draft.
    pickLearning(level) {
      if (!LEARNING_LEVELS.includes(level) || !this.learning()) return;
      this.learningSel = level;
    },
    // Writes learning_level like the learning page does: read the document, set
    // the field, write it back. apply() calls it once with the rates.
    async writeLearning(level, csrfToken) {
      const current = await requestJSON('/api/v1/settings');
      await requestJSON('/api/v1/settings', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken }, // i18n-ignore
        body: JSON.stringify({ ...current, learning_level: level }),
      });
    },
  });

  const register = () => {
    if (!window.Alpine) return;
    Alpine.data('pfPage', pfPage);
  };
  if (window.Alpine) register(); else document.addEventListener('alpine:init', register, {once: true});
})();
