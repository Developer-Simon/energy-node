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

  const pfPage = () => ({
    // The answer of GET /api/v1/performance, as the page last received it.
    data: { ...EMPTY },
    timer: 0,
    // True after a failed load until the next one succeeds, so a failing API
    // raises one toast per failure streak instead of one every 30 s.
    failing: false,

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

    async load() {
      try {
        this.data = { ...EMPTY, ...(await requestJSON('/api/v1/performance')) };
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
      return this.data.target_pct - (this.data.deduction?.pct || 0);
    },
    free() {
      return this.available() - this.data.today_pct;
    },

    headTone() {
      if (this.measuring()) return 'info';
      if (this.data.today_pct > this.available()) return 'bad';
      if (this.free() < 3) return 'warn';
      return 'ok';
    },
    headTitle() {
      const used = pct(this.data.today_pct);
      const free = this.free();
      if (free < 0) return t('performance.js.head.used_over', { used, over: pct(-free) });
      return t('performance.js.head.used_free', { used, free: pct(free) });
    },
    headNote() {
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
      const fixed = this.data.base_pct;
      const parts = [
        { cls: 'pf-m-fixed', w: fixed },
        { cls: 'pf-m-now', w: this.data.today_pct - fixed },
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
  });

  const register = () => {
    if (!window.Alpine) return;
    Alpine.data('pfPage', pfPage);
  };
  if (window.Alpine) register(); else document.addEventListener('alpine:init', register, {once: true});
})();
