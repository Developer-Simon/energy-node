(() => {
  const t = (key, params) => (window.I18n ? window.I18n.t(key, params) : key);

  const requestJSON = async (url, options) => {
    // The single chokepoint for every URL literal in this file: behind a
    // reverse-proxy subpath base.html puts the prefix into
    // __DASHBOARD_BASE_PATH__; on direct access it is empty.
    const response = await fetch(`${window.__DASHBOARD_BASE_PATH__ || ''}${url}`, options);
    const body = await response.json();
    if (!response.ok) {
      throw new Error(body.message || t('common.request_failed'));
    }
    return body;
  };

  const defaultStatus = () => ({
    installed: false,
    backend_state: '',
    auth_url: '',
    tailscale_ips: [],
    dns_name: '',
    online: false,
    key_expiry: '',
    tailnet: '',
    peer_count: 0,
    health: [],
  });

  const POLL_INTERVAL_MS = 2000;

  const tailscalePanel = () => ({
    steps: [
      {id: 1, label: 'tailscale.step1.title'},
      {id: 2, label: 'tailscale.step2.title'},
      {id: 3, label: 'tailscale.step3.title'},
    ],
    get stepperLabel() { return t('tailscale.steps_label'); },
    currentStep: 1,
    direction: 'forward',
    prereqs: {installed: false, version: '', service_active: '', service_enabled: ''},
    prereqsLoading: false,
    prereqsChecked: false,
    status: defaultStatus(),
    lastAction: null,
    loginStarted: false,
    csrfToken: '',
    canSystemActions: false,
    loading: false,
    statusLoading: false,
    busy: false,
    pollTimer: null,

    async init() {
      this.loading = true;
      try {
        const session = await requestJSON('/api/v1/auth/session').catch(() => null);
        this.csrfToken = (session && session.csrf_token) || '';
        this.canSystemActions = Boolean(session && session.system_actions) && window.location.protocol === 'https:';
        await Promise.all([this.checkPrereqs(), this.loadStatus()]);
      } catch (error) {
        this.$store.toasts.push(error.message, 'critical');
      } finally {
        this.loading = false;
      }
    },

    async checkPrereqs() {
      this.prereqsLoading = true;
      try {
        this.prereqs = await requestJSON('/api/v1/tailscale/prereqs');
        this.prereqsChecked = true;
      } catch (error) {
        this.$store.toasts.push(error.message, 'critical');
      } finally {
        this.prereqsLoading = false;
      }
    },

    async loadStatus() {
      this.statusLoading = true;
      try {
        const result = await requestJSON('/api/v1/tailscale/status');
        this.status = {...defaultStatus(), ...result.status};
        this.lastAction = result.last_action || null;
        if (this.currentStep === 2 && this.status.backend_state && this.status.backend_state !== 'NeedsLogin' && this.status.online) {
          this.stopPolling();
          this.goToStep(3);
          this.$store.toasts.push(t('tailscale.login_successful'));
        }
      } catch (error) {
        // Status is a best-effort display; keep whatever was shown before.
      } finally {
        this.statusLoading = false;
      }
    },

    startPolling() {
      this.stopPolling();
      this.pollTimer = window.setInterval(() => this.loadStatus(), POLL_INTERVAL_MS);
    },

    stopPolling() {
      if (this.pollTimer) {
        window.clearInterval(this.pollTimer);
        this.pollTimer = null;
      }
    },

    backToPrereqs() {
      this.stopPolling();
      this.goToStep(1);
    },

    goToStep(id) {
      this.direction = id >= this.currentStep ? 'forward' : 'back';
      this.currentStep = id;
    },

    async startLogin() {
      if (!this.canSystemActions || this.busy) return;
      this.busy = 'login';
      this.loginStarted = true;
      try {
        await requestJSON('/api/v1/tailscale/login', {
          method: 'POST',
          headers: {'Content-Type': 'application/json', 'X-CSRF-Token': this.csrfToken}, // i18n-ignore
          body: JSON.stringify({confirm: true}),
        });
        this.startPolling();
        await this.loadStatus();
      } catch (error) {
        this.$store.toasts.push(error.message, 'critical');
      } finally {
        this.busy = false;
      }
    },

    async logout() {
      if (!this.canSystemActions || this.busy) return;
      const confirmed = await this.$store.modal.confirm({
        title: t('tailscale.logout_confirm_title'),
        confirmLabel: t('tailscale.logout_confirm_button'),
        danger: true,
      });
      if (!confirmed) return;
      this.busy = 'logout';
      try {
        await requestJSON('/api/v1/tailscale/logout', {
          method: 'POST',
          headers: {'Content-Type': 'application/json', 'X-CSRF-Token': this.csrfToken}, // i18n-ignore
          body: JSON.stringify({confirm: true}),
        });
        this.$store.toasts.push(t('tailscale.logout_success'));
        await this.loadStatus();
      } catch (error) {
        this.$store.toasts.push(error.message, 'critical');
      } finally {
        this.busy = false;
      }
    },

    async restart() {
      if (!this.canSystemActions || this.busy) return;
      this.busy = 'restart';
      try {
        await requestJSON('/api/v1/tailscale/restart', {
          method: 'POST',
          headers: {'X-CSRF-Token': this.csrfToken}, // i18n-ignore
        });
        this.$store.toasts.push(t('tailscale.restart_success'));
        await this.loadStatus();
      } catch (error) {
        this.$store.toasts.push(error.message, 'critical');
      } finally {
        this.busy = false;
      }
    },

    formatTime(value) {
      return value ? window.I18n.formatDateTime(value) || '-' : '-';
    },

    lastActionLabel() {
      if (!this.lastAction) return '-';
      const when = this.formatTime(this.lastAction.at);
      const user = this.lastAction.user || '';
      if (this.lastAction.ok) {
        const key = user ? 'tailscale.last_action.succeeded_by' : 'tailscale.last_action.succeeded';
        return t(key, {action: this.lastAction.action, user, when});
      }
      const key = user ? 'tailscale.last_action.failed_by' : 'tailscale.last_action.failed';
      return t(key, {action: this.lastAction.action, user, when, error: this.lastAction.error || ''});
    },
  });

  const register = () => {
    if (!window.Alpine) return;
    Alpine.data('tailscalePanel', tailscalePanel);
  };
  if (window.Alpine) register(); else document.addEventListener('alpine:init', register, {once: true});
})();
