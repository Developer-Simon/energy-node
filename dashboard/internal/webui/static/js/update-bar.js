// The bar under the masthead: an available update, the progress of a running
// one, a dashboard restart in the middle of it, and how it ended. It replaces
// the masthead pill. Everything it shows comes from three GET endpoints:
// /api/v1/updates/progress (the updater's job directory, every session),
// /api/v1/updates/status (the cached GitHub check, check_updates role) and
// /api/v1/auth/session (update_settings decides install and details links).
(() => {
  const t = (key, params) => (window.I18n ? window.I18n.t(key, params) : key);
  const withBase = (p) => `${window.__DASHBOARD_BASE_PATH__ || ''}${p}`;
  const ACK_KEY = 'energy-node.update-bar.acknowledged';
  // A finished update stays news for a week; a browser that comes by later
  // does not need to be told about it any more.
  const OK_MAX_AGE_MS = 7 * 24 * 60 * 60 * 1000;
  const FAST_POLL_MS = 2000;
  const SLOW_POLL_MS = 30000;

  const readAck = () => {
    try { return window.localStorage.getItem(ACK_KEY) || ''; } catch (error) { return ''; }
  };
  const writeAck = (value) => {
    try { window.localStorage.setItem(ACK_KEY, value); } catch (error) { /* private mode: the bar shows again next load */ }
  };

  // A run is identified by run_id, then finished_at. A record with neither still
  // needs a non-empty key, or it could be shown but never dismissed.
  const ackKeyOf = (progress) => {
    if (!progress) return '';
    return progress.run_id || progress.finished_at || `${progress.version || ''}:${progress.state}`;
  };
  // An empty key never counts as acknowledged, so the comparison needs both.
  const isAcknowledged = (ackKey, acknowledged) => Boolean(ackKey) && ackKey === acknowledged;
  const percentOf = (progress) => (progress && progress.total > 0 ? (progress.done / progress.total) * 100 : 0);

  // "Step 60 · Title · CODE" from the parts that exist, so an empty title or
  // code leaves no doubled or trailing separator.
  const failStepDetail = (step, code) => [
    t('update_bar.fail.step', { step: step.id || '?' }),
    step.title || '',
    code || '',
  ].filter(Boolean).join(' · ');

  const runningDetail = (progress) => {
    const total = progress.total || 0;
    const current = Math.min((progress.done || 0) + 1, Math.max(total, 1));
    const title = progress.step && progress.step.title;
    return title
      ? t('update_bar.running.step', { current, total, title })
      : t('update_bar.running.step_untitled', { current, total });
  };

  // updateBarView decides what the bar shows. It is pure so the tests can walk
  // every state without timers or fetches.
  const updateBarView = ({ progress, lastProgress, status, canInstall, acknowledged, restarting, now }) => {
    const state = progress ? progress.state : 'idle';
    const last = lastProgress || progress;
    if (restarting && last && (last.state === 'running' || last.state === 'pending')) {
      return { kind: 'restarting', title: t('update_bar.running.title', { version: last.version }), detail: t('update_bar.restarting.detail'), percent: percentOf(last) };
    }
    if (state === 'pending') {
      return { kind: 'pending', title: t('update_bar.running.title', { version: progress.version }), detail: t('update_bar.pending.detail'), percent: 0 };
    }
    if (state === 'running') {
      return { kind: 'running', title: t('update_bar.running.title', { version: progress.version }), detail: runningDetail(progress), percent: percentOf(progress) };
    }
    const ackKey = ackKeyOf(progress);
    const fresh = progress && progress.finished_at && now - Date.parse(progress.finished_at) < OK_MAX_AGE_MS;
    if (state === 'ok' && fresh && !isAcknowledged(ackKey, acknowledged)) {
      return { kind: 'ok', ackKey, title: t('update_bar.ok.title', { version: progress.version }), detail: t('update_bar.ok.detail') };
    }
    if ((state === 'fail' || state === 'rejected') && !isAcknowledged(ackKey, acknowledged)) {
      const step = progress.step || {};
      const detail = state === 'rejected'
        ? t('update_bar.fail.rejected', { code: progress.code || '' })
        : failStepDetail(step, progress.code);
      return { kind: 'fail', ackKey, title: t('update_bar.fail.title', { version: progress.version }), detail };
    }
    const justInstalled = state === 'ok' && status && status.latest === progress.version;
    if (status && status.available && !justInstalled) {
      return {
        kind: 'available',
        title: t('update_bar.available.title', { version: status.latest }),
        detail: canInstall ? t('update_bar.available.installed', { version: status.current }) : t('update_bar.available.login_hint', { version: status.current }),
        notesUrl: status.notes_url || '',
        canInstall,
      };
    }
    return { kind: 'hidden' };
  };

  const getJSON = async (url) => {
    const response = await fetch(withBase(url), { headers: { Accept: 'application/json' } });
    if (!response.ok) {
      const error = new Error(`HTTP ${response.status}`);
      error.status = response.status;
      throw error;
    }
    return response.json();
  };

  const updateBar = () => ({
    progress: { state: 'idle' },
    lastProgress: null,
    status: null,
    canInstall: false,
    restarting: false,
    acknowledged: readAck(),
    timer: 0,
    loaded: false,

    get view() {
      return updateBarView({
        progress: this.progress, lastProgress: this.lastProgress, status: this.status,
        canInstall: this.canInstall, acknowledged: this.acknowledged, restarting: this.restarting, now: Date.now(),
      });
    },

    async init() {
      // x-init plus Alpine's own init() hook would run this twice.
      if (this.loaded) return;
      this.loaded = true;
      try {
        const session = await getJSON('/api/v1/auth/session');
        this.canInstall = Boolean(session && session.update_settings);
      } catch (error) {
        this.canInstall = false;
      }
      try {
        const status = await getJSON('/api/v1/updates/status');
        this.status = status && status.checked === false ? null : status;
      } catch (error) {
        // 403 without check_updates, or no cache yet: no "available" state.
        this.status = null;
      }
      await this.poll();
    },

    async poll() {
      window.clearTimeout(this.timer);
      try {
        const progress = await getJSON('/api/v1/updates/progress');
        if (progress.state === 'running' || progress.state === 'pending') this.lastProgress = progress;
        this.progress = progress;
        this.restarting = false;
      } catch (error) {
        // While a run is under way a failed poll is the dashboard restarting
        // itself (step 60), not an error worth a toast.
        const last = this.lastProgress;
        this.restarting = Boolean(last && (this.progress.state === 'running' || this.progress.state === 'pending' || this.restarting));
      }
      const busy = this.restarting || this.progress.state === 'running' || this.progress.state === 'pending';
      this.timer = window.setTimeout(() => this.poll(), busy ? FAST_POLL_MS : SLOW_POLL_MS);
    },

    acknowledge() {
      const key = ackKeyOf(this.progress);
      if (!key) return;
      writeAck(key);
      this.acknowledged = key;
    },

    // The dialog lives outside the bar (base.html), because the bar hides itself
    // once the message is acknowledged. It listens for this event.
    openWhatsNew() {
      this.acknowledge();
      window.dispatchEvent(new CustomEvent('update-whatsnew-open', { detail: { version: this.progress.version || '' } }));
    },

    redeployHref() {
      return withBase('/redeploy/');
    },

    destroy() {
      window.clearTimeout(this.timer);
    },
  });

  window.updateBarView = updateBarView;
  document.addEventListener('alpine:init', () => {
    window.Alpine.data('updateBar', updateBar);
  });
})();
