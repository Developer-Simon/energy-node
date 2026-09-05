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

  const defaultForm = () => ({
    enabled: false,
    host: '',
    port: 1883,
    client_id: '',
    username: '',
    tls: false,
    tls_insecure: false,
    keepalive_seconds: 30,
    clean_session: true,
    discovery_prefix: 'homeassistant',
    connect_timeout_seconds: 10,
    publish_energy_device: true,
  });

  const mqttPanel = () => ({
    form: defaultForm(),
    password: '',
    passwordConfigured: false,
    source: '',
    status: null,
    csrfToken: '',
    canConfigure: false,
    loading: false,
    statusLoading: false,
    busy: false,
    testResult: null,

    async load() {
      this.loading = true;
      try {
        const [config, session] = await Promise.all([
          requestJSON('/api/v1/mqtt'),
          requestJSON('/api/v1/auth/session').catch(() => null),
        ]);
        this.applyConfig(config);
        this.csrfToken = (session && session.csrf_token) || '';
        this.canConfigure = Boolean(session && session.mqtt_config) && window.location.protocol === 'https:';
        await this.loadStatus();
      } catch (error) {
        this.$store.toasts.push(error.message, 'critical');
      } finally {
        this.loading = false;
      }
    },

    applyConfig(config) {
      this.form = {
        enabled: Boolean(config.enabled),
        host: config.host || '',
        port: config.port || 1883,
        client_id: config.client_id || '',
        username: config.username || '',
        tls: Boolean(config.tls),
        tls_insecure: Boolean(config.tls_insecure),
        keepalive_seconds: config.keepalive_seconds || 30,
        clean_session: config.clean_session !== false,
        discovery_prefix: config.discovery_prefix || 'homeassistant',
        connect_timeout_seconds: config.connect_timeout_seconds || 10,
        publish_energy_device: config.publish_energy_device !== false,
      };
      this.source = config.source || '';
      this.passwordConfigured = Boolean(config.password_configured);
    },

    sourceLabel(source) {
      if (source === 'settings') return 'Dashboard-Einstellungen';
      if (source === 'config') return 'Zentrale Konfigurationsdatei';
      if (!source) return '-';
      return 'unbekannt';
    },

    formatTime(value) {
      return value ? new Date(value).toLocaleString() : '-';
    },

    async loadStatus() {
      this.statusLoading = true;
      try {
        this.status = await requestJSON('/api/v1/mqtt/status');
      } catch (error) {
        // Status is a best-effort display; keep whatever was shown before.
      } finally {
        this.statusLoading = false;
      }
    },

    get valid() {
      return Boolean(this.form.host) && this.form.port >= 1 && this.form.port <= 65535 &&
        Boolean(this.form.client_id) && Boolean(this.form.discovery_prefix) &&
        this.form.keepalive_seconds >= 5 && this.form.keepalive_seconds <= 300 &&
        this.form.connect_timeout_seconds >= 1 && this.form.connect_timeout_seconds <= 60;
    },

    async savePasswordIfChanged() {
      if (!this.password) return;
      const result = await requestJSON('/api/v1/mqtt/credentials', {
        method: 'POST',
        headers: {'Content-Type': 'application/json', 'X-CSRF-Token': this.csrfToken},
        body: JSON.stringify({password: this.password}),
      });
      this.passwordConfigured = Boolean(result.password_configured);
      this.password = '';
    },

    async save(reconnect) {
      if (!this.canConfigure || !this.valid || this.busy) return;
      this.busy = reconnect ? 'reconnect' : 'save';
      this.testResult = null;
      try {
        await this.savePasswordIfChanged();
        await requestJSON('/api/v1/mqtt', {
          method: 'PUT',
          headers: {'Content-Type': 'application/json', 'X-CSRF-Token': this.csrfToken},
          body: JSON.stringify(this.form),
        });
        if (reconnect) {
          const result = await requestJSON('/api/v1/mqtt/reconnect', {
            method: 'POST',
            headers: {'X-CSRF-Token': this.csrfToken},
          });
          this.status = result.status || this.status;
          if (result.ok) {
            this.$store.toasts.push('Gespeichert und neu verbunden.');
          } else {
            this.$store.toasts.push(`Neu verbinden fehlgeschlagen: ${result.error || ''}`, 'critical');
          }
        } else {
          this.$store.toasts.push('Gespeichert.');
        }
        await this.loadStatus();
      } catch (error) {
        this.$store.toasts.push(error.message, 'critical');
      } finally {
        this.busy = false;
      }
    },

    // saveEnergyDevice persists just form.publish_energy_device via its own
    // endpoint, so the switch works whether or not the dashboard-managed
    // connection is enabled and without a full, valid broker config. It is
    // fired straight from the toggle's change event.
    async saveEnergyDevice() {
      if (!this.canConfigure) return;
      const desired = this.form.publish_energy_device;
      try {
        await requestJSON('/api/v1/mqtt/energy-device', {
          method: 'PUT',
          headers: {'Content-Type': 'application/json', 'X-CSRF-Token': this.csrfToken},
          body: JSON.stringify({publish_energy_device: desired}),
        });
        this.$store.toasts.push(desired
          ? 'Energiewerte werden beim nächsten Verbinden als Home-Assistant-Gerät angeboten.'
          : 'Das Home-Assistant-Energie-Gerät wird beim nächsten Verbinden entfernt.');
      } catch (error) {
        this.form.publish_energy_device = !desired;
        this.$store.toasts.push(error.message, 'critical');
      }
    },

    async test() {
      if (!this.canConfigure || !this.valid || this.busy) return;
      this.busy = 'test';
      this.testResult = null;
      try {
        this.testResult = await requestJSON('/api/v1/mqtt/test', {
          method: 'POST',
          headers: {'Content-Type': 'application/json', 'X-CSRF-Token': this.csrfToken},
          body: JSON.stringify({...this.form, password: this.password}),
        });
      } catch (error) {
        this.$store.toasts.push(error.message, 'critical');
      } finally {
        this.busy = false;
      }
    },
  });

  const defaultBridgeForm = () => ({
    enabled: false,
    name: '',
    address: '',
    port: 1883,
    remote_client_id: '',
    remote_username: '',
    topics: [],
    try_private: true,
    start_type_auto: true,
    restart_timeout: 30,
    keepalive_seconds: 60,
    cleansession: false,
  });

  const mqttBridgePanel = () => ({
    form: defaultBridgeForm(),
    password: '',
    passwordConfigured: false,
    configured: false,
    addressWarning: '',
    preview: '',
    status: null,
    csrfToken: '',
    canConfigure: false,
    canApply: false,
    canRestart: false,
    loading: false,
    statusLoading: false,
    busy: false,

    async load() {
      this.loading = true;
      try {
        const session = await requestJSON('/api/v1/auth/session').catch(() => null);
        this.csrfToken = (session && session.csrf_token) || '';
        const https = window.location.protocol === 'https:';
        this.canConfigure = Boolean(session && session.mqtt_config) && https;
        this.canApply = this.canConfigure && Boolean(session && session.system_actions);
        this.canRestart = Boolean(session && session.system_actions) && https;
        const config = await requestJSON('/api/v1/mqtt/bridge').catch(() => null);
        if (config) this.applyConfig(config);
        await this.loadStatus();
      } catch (error) {
        this.$store.toasts.push(error.message, 'critical');
      } finally {
        this.loading = false;
      }
    },

    applyConfig(config) {
      this.configured = Boolean(config.configured);
      this.form = {
        enabled: Boolean(config.enabled),
        name: config.name || '',
        address: config.address || '',
        port: config.port || 1883,
        remote_client_id: config.remote_client_id || '',
        remote_username: config.remote_username || '',
        topics: (config.topics || []).map((topic) => ({
          pattern: topic.pattern || '',
          direction: topic.direction || 'out',
          qos: topic.qos || 0,
          comment: topic.comment || '',
        })),
        try_private: config.try_private !== false,
        start_type_auto: config.start_type_auto !== false,
        restart_timeout: config.restart_timeout || 30,
        keepalive_seconds: config.keepalive_seconds || 60,
        cleansession: Boolean(config.cleansession),
      };
      this.passwordConfigured = Boolean(config.password_configured);
      this.addressWarning = config.address_warning || '';
      this.preview = config.preview || '';
    },

    async loadStatus() {
      this.statusLoading = true;
      try {
        this.status = await requestJSON('/api/v1/mqtt/bridge/status');
      } catch (error) {
        // Status is a best-effort display; keep whatever was shown before.
      } finally {
        this.statusLoading = false;
      }
    },

    bridgeConnectionLabel() {
      if (!this.status || !this.status.bridge || !this.status.bridge.configured) return 'nicht konfiguriert';
      return this.status.bridge.connected ? 'verbunden' : 'getrennt';
    },

    driftLabel() {
      const drift = this.status && this.status.drift;
      if (!drift || !drift.known) return 'unbekannt';
      return drift.matches ? 'ja' : 'nein, abweichend';
    },

    lastApplyLabel() {
      const record = this.status && this.status.last_apply;
      if (!record) return '-';
      const when = this.formatTime(record.at);
      const who = record.user ? ` von ${record.user}` : '';
      return record.ok ? `Erfolgreich${who}, ${when}` : `Fehlgeschlagen${who}, ${when}: ${record.error || ''}`;
    },

    formatTime(value) {
      return value ? new Date(value).toLocaleString() : '-';
    },

    addTopic() {
      if (this.form.topics.length >= 32) return;
      this.form.topics.push({pattern: '', direction: 'out', qos: 0, comment: ''});
    },

    removeTopic(index) {
      this.form.topics.splice(index, 1);
    },

    get valid() {
      return Boolean(this.form.name) && Boolean(this.form.address) && Boolean(this.form.remote_client_id) &&
        this.form.port >= 1 && this.form.port <= 65535 &&
        this.form.topics.length >= 1 && this.form.topics.length <= 32 &&
        this.form.topics.every((topic) => Boolean(topic.pattern) && ['in', 'out', 'both'].includes(topic.direction) && topic.qos >= 0 && topic.qos <= 2) &&
        this.form.restart_timeout >= 5 && this.form.restart_timeout <= 600 &&
        this.form.keepalive_seconds >= 10 && this.form.keepalive_seconds <= 300;
    },

    async savePasswordIfChanged() {
      if (!this.password) return;
      const result = await requestJSON('/api/v1/mqtt/bridge/credentials', {
        method: 'POST',
        headers: {'Content-Type': 'application/json', 'X-CSRF-Token': this.csrfToken},
        body: JSON.stringify({password: this.password}),
      });
      this.passwordConfigured = Boolean(result.password_configured);
      this.password = '';
    },

    // saveInternal does the actual PUT/credentials round trip without the
    // busy-flag/validity guard save() and apply() each need with their own
    // busy value ('save' vs 'apply') - apply() calls this directly so it is
    // not blocked by save()'s own "already busy" guard.
    async saveInternal() {
      await this.savePasswordIfChanged();
      const result = await requestJSON('/api/v1/mqtt/bridge', {
        method: 'PUT',
        headers: {'Content-Type': 'application/json', 'X-CSRF-Token': this.csrfToken},
        body: JSON.stringify(this.form),
      });
      this.applyConfig(result);
    },

    async save() {
      if (!this.canConfigure || !this.valid || this.busy) return;
      this.busy = 'save';
      try {
        await this.saveInternal();
        this.$store.toasts.push('Gespeichert.');
        await this.loadStatus();
      } catch (error) {
        this.$store.toasts.push(error.message, 'critical');
      } finally {
        this.busy = false;
      }
    },

    async apply() {
      if (!this.canApply || !this.valid || this.busy) return;
      const confirmed = await this.$store.modal.confirm({
        title: 'Bridge anwenden und Mosquitto neu starten?',
        body: 'Die Live-Anzeige setzt kurz aus.',
        confirmLabel: 'Anwenden und neu starten',
        danger: true,
      });
      if (!confirmed) return;
      this.busy = 'apply';
      try {
        await this.saveInternal();
        await requestJSON('/api/v1/mqtt/bridge/apply', {
          method: 'POST',
          headers: {'Content-Type': 'application/json', 'X-CSRF-Token': this.csrfToken},
          body: JSON.stringify({confirm: true}),
        });
        this.$store.toasts.push('Bridge angewendet, Mosquitto wurde neu gestartet.');
        await this.loadStatus();
      } catch (error) {
        this.$store.toasts.push(error.message, 'critical');
      } finally {
        this.busy = false;
      }
    },

    async restartOnly() {
      if (!this.canRestart || this.busy) return;
      const confirmed = await this.$store.modal.confirm({
        title: 'Mosquitto jetzt neu starten?',
        body: 'Die Live-Anzeige setzt kurz aus.',
        confirmLabel: 'Neu starten',
        danger: true,
      });
      if (!confirmed) return;
      this.busy = 'restart';
      try {
        await requestJSON('/api/v1/mqtt/bridge/restart', {
          method: 'POST',
          headers: {'X-CSRF-Token': this.csrfToken},
        });
        this.$store.toasts.push('Mosquitto wurde neu gestartet.');
        await this.loadStatus();
      } catch (error) {
        this.$store.toasts.push(error.message, 'critical');
      } finally {
        this.busy = false;
      }
    },
  });

  const register = () => {
    if (!window.Alpine) return;
    Alpine.data('mqttPanel', mqttPanel);
    Alpine.data('mqttBridgePanel', mqttBridgePanel);
  };
  if (window.Alpine) register(); else document.addEventListener('alpine:init', register, {once: true});
})();
