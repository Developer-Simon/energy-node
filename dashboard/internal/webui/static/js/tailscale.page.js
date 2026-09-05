(() => {
  const requestJSON = async (url, options) => {
    // The single chokepoint for every URL literal in this file: behind a
    // reverse-proxy subpath base.html puts the prefix into
    // __DASHBOARD_BASE_PATH__; on direct access it is empty.
    const response = await fetch(`${window.__DASHBOARD_BASE_PATH__ || ''}${url}`, options);
    const body = await response.json();
    if (!response.ok) {
      throw new Error(body.message || 'Anfrage fehlgeschlagen');
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
      {id: 1, label: 'Voraussetzungen prüfen'},
      {id: 2, label: 'Anmeldung starten'},
      {id: 3, label: 'Ergebnis prüfen'},
    ],
    stepperLabel: 'Tailscale-Schritte',
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
          this.$store.toasts.push('Anmeldung erfolgreich.');
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
          headers: {'Content-Type': 'application/json', 'X-CSRF-Token': this.csrfToken},
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
        title: 'Von diesem Tailnet abmelden?',
        confirmLabel: 'Abmelden',
        danger: true,
      });
      if (!confirmed) return;
      this.busy = 'logout';
      try {
        await requestJSON('/api/v1/tailscale/logout', {
          method: 'POST',
          headers: {'Content-Type': 'application/json', 'X-CSRF-Token': this.csrfToken},
          body: JSON.stringify({confirm: true}),
        });
        this.$store.toasts.push('Abgemeldet.');
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
          headers: {'X-CSRF-Token': this.csrfToken},
        });
        this.$store.toasts.push('Dienst wurde neu gestartet.');
        await this.loadStatus();
      } catch (error) {
        this.$store.toasts.push(error.message, 'critical');
      } finally {
        this.busy = false;
      }
    },

    formatTime(value) {
      return value ? new Date(value).toLocaleString() : '-';
    },

    lastActionLabel() {
      if (!this.lastAction) return '-';
      const when = this.formatTime(this.lastAction.at);
      const who = this.lastAction.user ? ` von ${this.lastAction.user}` : '';
      return this.lastAction.ok ? `${this.lastAction.action} erfolgreich${who}, ${when}` : `${this.lastAction.action} fehlgeschlagen${who}, ${when}: ${this.lastAction.error || ''}`;
    },
  });

  const register = () => {
    if (!window.Alpine) return;
    Alpine.data('tailscalePanel', tailscalePanel);
  };
  if (window.Alpine) register(); else document.addEventListener('alpine:init', register, {once: true});
})();
