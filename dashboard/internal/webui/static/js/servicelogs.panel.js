(() => {
  const apiError = (body, fallbackKey) => (window.I18n ? window.I18n.error(body, fallbackKey) : (body && body.message) || fallbackKey || 'common.request_failed');

  const requestJSON = async (url, options) => {
    // Every URL literal in this file goes through here: behind a reverse
    // proxy base.html puts the prefix into __DASHBOARD_BASE_PATH__.
    const response = await fetch(`${window.__DASHBOARD_BASE_PATH__ || ''}${url}`, options);
    const body = await response.json().catch(() => ({}));
    if (!response.ok) {
      throw new Error(apiError(body));
    }
    return body;
  };

  const POLL_MS = 5000;
  const MAX_ENTRIES = 2000;
  const SEARCH_DEBOUNCE_MS = 300;

  const serviceLogsPanel = () => ({
    canView: false,
    options: null,
    unit: 'dashboard',
    since: 60,
    lines: 200,
    query: '',
    entries: [],
    cursor: '',
    limited: false,
    loading: false,
    loaded: false,
    error: '',
    autoRefresh: false,
    pollTimer: null,
    searchTimer: null,
    polling: false,
    // Bumped by every load(), so a poll answer that arrives after a reset
    // is dropped instead of appending lines of the old query.
    generation: 0,

    async init() {
      try {
        const session = await requestJSON('/api/v1/auth/session').catch(() => null);
        this.canView = Boolean(session && session.system_actions);
        if (!this.canView) return;
        this.options = await requestJSON('/api/v1/system/logs/units');
        this.unit = this.options.default_unit || this.unit;
        this.since = this.options.default_since_minutes || this.since;
        this.lines = this.options.default_lines || this.lines;
        await this.load();
      } catch (error) {
        this.error = error.message;
      }
    },

    requestParams(cursor) {
      const params = new URLSearchParams({unit: this.unit, since: String(this.since), lines: String(this.lines)});
      const query = this.query.trim();
      if (query) params.set('q', query);
      if (cursor) params.set('cursor', cursor);
      return params.toString();
    },

    async load() {
      const generation = ++this.generation;
      this.cursor = '';
      this.entries = [];
      this.limited = false;
      this.error = '';
      this.loading = true;
      try {
        const result = await requestJSON(`/api/v1/system/logs?${this.requestParams('')}`);
        if (generation !== this.generation) return;
        this.entries = result.entries || [];
        this.cursor = result.cursor || '';
        this.limited = Boolean(result.limited);
        this.loaded = true;
      } catch (error) {
        if (generation !== this.generation) return;
        this.error = error.message;
      } finally {
        if (generation === this.generation) this.loading = false;
      }
    },

    async poll() {
      if (!this.cursor || this.loading || this.polling) return;
      if (document.visibilityState === 'hidden') return;
      // Another top-level tab is active: x-show hides this section, and the
      // panel stays mounted, so the timer must not keep polling.
      if (this.$el && this.$el.offsetParent === null) return;
      const generation = this.generation;
      const cursor = this.cursor;
      this.polling = true;
      try {
        const result = await requestJSON(`/api/v1/system/logs?${this.requestParams(cursor)}`);
        if (generation !== this.generation) return;
        const list = this.$refs && this.$refs.list;
        const pinned = Boolean(list) && list.scrollHeight - list.scrollTop - list.clientHeight < 8;
        const added = result.entries || [];
        if (added.length) {
          const merged = this.entries.concat(added);
          this.entries = merged.length > MAX_ENTRIES ? merged.slice(merged.length - MAX_ENTRIES) : merged;
        }
        this.cursor = result.cursor || cursor;
        this.error = '';
        if (pinned) {
          this.$nextTick(() => { list.scrollTop = list.scrollHeight; });
        }
      } catch (error) {
        if (generation !== this.generation) return;
        this.error = error.message;
        this.setAutoRefresh(false);
      } finally {
        this.polling = false;
      }
    },

    setAutoRefresh(on) {
      this.autoRefresh = Boolean(on);
      this.stopTimer();
      if (this.autoRefresh) {
        this.pollTimer = window.setInterval(() => this.poll(), POLL_MS);
      }
    },

    stopTimer() {
      if (this.pollTimer) {
        window.clearInterval(this.pollTimer);
        this.pollTimer = null;
      }
    },

    // Typing in the search field reloads once the typing pauses.
    scheduleSearch() {
      if (this.searchTimer) window.clearTimeout(this.searchTimer);
      this.searchTimer = window.setTimeout(() => {
        this.searchTimer = null;
        this.load();
      }, SEARCH_DEBOUNCE_MS);
    },

    destroy() {
      this.stopTimer();
      if (this.searchTimer) {
        window.clearTimeout(this.searchTimer);
        this.searchTimer = null;
      }
    },

    priorityClass(entry) {
      const priority = Number(entry && entry.priority);
      if (priority <= 3) return 'log-error';
      if (priority === 4) return 'log-warning';
      if (priority === 7) return 'log-debug';
      return 'log-info';
    },

    formatTime(entry) {
      const date = new Date(entry.time);
      if (Number.isNaN(date.getTime())) return '';
      if (date.toDateString() === new Date().toDateString()) {
        return window.I18n.formatTime(date, {hour: '2-digit', minute: '2-digit', second: '2-digit'});
      }
      return window.I18n.formatDateTime(date, {day: '2-digit', month: '2-digit', hour: '2-digit', minute: '2-digit', second: '2-digit'});
    },
  });

  const register = () => {
    if (!window.Alpine) return;
    Alpine.data('serviceLogsPanel', serviceLogsPanel);
  };
  if (window.Alpine) register(); else document.addEventListener('alpine:init', register, {once: true});
})();
