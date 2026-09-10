(() => {
  const requestJSON = async (url, options) => {
    // The single chokepoint for every URL literal in this file: behind a
    // reverse-proxy subpath base.html puts the prefix into
    // __DASHBOARD_BASE_PATH__; on direct access it is empty.
    const response = await fetch(`${window.__DASHBOARD_BASE_PATH__ || ''}${url}`, options);
    const body = await response.json();
    if (!response.ok) {
      const error = new Error(body.message || 'Anfrage fehlgeschlagen');
      error.toolOutput = body.tool_output || '';
      throw error;
    }
    return body;
  };

  // Schrittweite und Grenzen der Stepper auf den Einstellungs-Tabs (Allgemein
  // und Verlaeufe). Die Grenzen decken sich mit den min/max der Formularfelder
  // und mit dem, was valid() unten prueft - der Stepper kann also nie einen
  // Wert erzeugen, den das Speichern ablehnt.
  const STEP_FIELDS = {
    healthScoreThreshold: {min: 1, max: 600, step: 1},
    sweepIntervalSeconds: {min: 30, max: 3600, step: 30},
    liveUpdateIntervalSeconds: {min: 1, max: 60, step: 1},
    historySampleIntervalSeconds: {min: 5, max: 3600, step: 5},
    historyRawWindowHours: {min: 1, max: 168, step: 1},
    historyMinuteWindowDays: {min: 1, max: 365, step: 1},
    historyRetentionHours: {min: 1, max: 8760, step: 1},
    historyBudgetMb: {min: 16, max: 8192, step: 16},
  };

  const settingsPanel = () => ({
    healthScoreThreshold: 3,
    sweepIntervalSeconds: 300,
    showDiscoveryTooltips: true,
    showRuntimeStatus: true,
    deviceViewMode: 'compact',
    theme: 'mint',
    showConfigEntitiesOnTile: false,
    showDiagnosticEntitiesOnTile: false,
    liveUpdateIntervalSeconds: 3,
    canTuneLiveUpdates: false,
    widePanels: [],
    historySampleIntervalSeconds: 10,
    historyRetentionMode: 'time',
    historyRetentionHours: 6,
    historyBudgetMb: 512,
    historyRawWindowHours: 24,
    historyMinuteWindowDays: 7,
    historyExtraEntities: [],
    historyExchangeDisabled: false,
    historyExchangeStatus: '',
    historyViews: [],
    // Wird aus der IndexedDB nachgeladen; 6 ist die Zahl der Energie-Rollen
    // und damit der ehrliche Startwert, solange noch nichts aufgezeichnet ist.
    historySeriesCount: 6,
    historyBytesUsed: 0,
    historyEstimate: null,
    // Beschriftungen aus der Tab-Leiste (base.html), Schluessel ist die
    // Panel-ID ohne -panel.
    widePanelOptions: [
      {key: 'overview', label: 'Übersicht'},
      {key: 'devices', label: 'Geräte'},
      {key: 'history', label: 'Verläufe'},
      {key: 'config', label: 'Konfiguration'},
      {key: 'energy', label: 'Energie'},
      {key: 'layout', label: 'Layout'},
      {key: 'devicemap', label: 'Device-Map'},
      {key: 'diagnostics', label: 'Diagnose'},
      {key: 'settings', label: 'Einstellungen'},
      {key: 'automations', label: 'Automationen'},
    ],
    statusBarItems: [],
    // Schluessel, wie sie runtimeStatusPanel() in dashboard.js interpretiert.
    statusBarItemOptions: [
      {key: 'mqtt', label: 'MQTT'},
      {key: 'cache', label: 'Cache'},
      {key: 'storage', label: 'Storage'},
      {key: 'uptime', label: 'Uptime'},
      {key: 'version', label: 'Dashboard-Version'},
    ],
    loading: false,
    saving: false,
    storageHealth: null,
    storageHealthLoading: false,
    storageHealthError: '',
    widePanelsChoices: null,
    statusBarItemsChoices: null,

    async load() {
      this.loading = true;
      try {
        const [value, session] = await Promise.all([requestJSON('/api/v1/settings'), requestJSON('/api/v1/auth/session').catch(() => null), this.loadStorageHealth()]);
        this.healthScoreThreshold = value.health_score_threshold;
        this.sweepIntervalSeconds = value.sweep_interval_seconds;
        this.showDiscoveryTooltips = value.show_discovery_tooltips;
        this.showRuntimeStatus = value.show_runtime_status !== false;
        this.deviceViewMode = value.device_view_mode === 'control' ? 'control' : 'compact';
        this.theme = ['mint', 'stromblau', 'signalgelb', 'tageslicht'].includes(value.theme) ? value.theme : 'mint';
        this.showConfigEntitiesOnTile = Boolean(value.show_config_entities_on_tile);
        this.showDiagnosticEntitiesOnTile = Boolean(value.show_diagnostic_entities_on_tile);
        this.liveUpdateIntervalSeconds = value.live_update_interval_seconds || 3;
        this.widePanels = Array.isArray(value.wide_panels) ? [...value.wide_panels] : [];
        this.statusBarItems = Array.isArray(value.status_bar_items) ? [...value.status_bar_items] : [];
        this.historySampleIntervalSeconds = value.history_sample_interval_seconds || 10;
        this.historyRetentionMode = value.history_retention_mode === 'size' ? 'size' : 'time';
        this.historyRetentionHours = value.history_retention_hours || 6;
        this.historyBudgetMb = value.history_budget_mb || 512;
        this.historyRawWindowHours = value.history_raw_window_hours || 24;
        this.historyMinuteWindowDays = value.history_minute_window_days || 7;
        this.historyExtraEntities = Array.isArray(value.history_extra_entities) ? [...value.history_extra_entities] : [];
        this.historyExchangeDisabled = Boolean(value.history_exchange_disabled);
        this.historyViews = Array.isArray(value.history_views) ? [...value.history_views] : [];
        this.loadHistoryUsage();
        this.canTuneLiveUpdates = Boolean(session && session.tune_live_updates);
        this.setChoicesSelection(this.widePanelsChoices, this.widePanels);
        this.setChoicesSelection(this.statusBarItemsChoices, this.statusBarItems);
      } catch (error) {
        this.$store.toasts.push(error.message, 'critical');
      } finally {
        this.loading = false;
      }
    },

    // Choices.js liest die Auswahl nur beim Erstellen aus dem <select>; spaetere
    // Aenderungen an widePanels/statusBarItems (initiales load(), Revision
    // wiederherstellen) muessen daher explizit ueber die API nachgezogen werden.
    setChoicesSelection(instance, values) {
      if (!instance) return;
      instance.removeActiveItems();
      if (values.length) instance.setChoiceByValue(values);
    },

    // Choices.js feuert bei Auswahlaenderungen ein eigenes "change" mit
    // detail={value: ...}; Alpines x-model liest bei CustomEvents mit
    // gesetztem detail dieses Feld direkt als neuen Modellwert (fuer
    // Web-Components gedacht) und ueberschreibt widePanels/statusBarItems damit
    // mit einem einzelnen Objekt statt einem Array. Deshalb kein x-model auf
    // diesen <select>-Elementen, sondern x-on:change ruft dies hier auf und
    // fragt Choices direkt nach dem aktuellen Auswahlstand.
    syncFromChoices(instance, options) {
      const selected = instance.getValue(true);
      return options.map(option => option.key).filter(key => selected.includes(key));
    },

    initChoices() {
      if (!window.Choices) return;
      const options = {removeItemButton: true, searchEnabled: true, shouldSort: false, placeholderValue: 'Auswählen ...', noChoicesText: 'Keine Auswahl mehr verfügbar'};
      if (this.$refs.widePanelsSelect && !this.widePanelsChoices) {
        this.widePanelsChoices = new Choices(this.$refs.widePanelsSelect, options);
        this.setChoicesSelection(this.widePanelsChoices, this.widePanels);
      }
      if (this.$refs.statusBarItemsSelect && !this.statusBarItemsChoices) {
        this.statusBarItemsChoices = new Choices(this.$refs.statusBarItemsSelect, options);
        this.setChoicesSelection(this.statusBarItemsChoices, this.statusBarItems);
      }
    },

    async loadStorageHealth() {
      this.storageHealthLoading = true;
      this.storageHealthError = '';
      try {
        this.storageHealth = await requestJSON('/api/v1/health/storage');
      } catch (error) {
        this.storageHealth = null;
        this.storageHealthError = error.message;
      } finally {
        this.storageHealthLoading = false;
      }
    },

    // Liest den tatsaechlichen Stand aus der Browser-Historie. Bewusst ohne
    // await im Aufrufer: die Seite soll nicht auf die IndexedDB warten, die
    // Zahlen tragen sich nach.
    async loadHistoryUsage() {
      if (!window.HistoryStore) return;
      try {
        const [series, bytes, estimate] = await Promise.all([
          window.HistoryStore.seriesNames(),
          window.HistoryStore.bytesUsed(),
          window.HistoryStore.estimate(),
        ]);
        if (series.length) this.historySeriesCount = series.length;
        this.historyBytesUsed = bytes;
        this.historyEstimate = estimate;
      } catch (error) {
        // Kein Toast: eine nicht lesbare Historie ist hier eine fehlende
        // Zusatzinformation, kein Fehler der Einstellungsseite.
        this.historyEstimate = null;
      }
    },

    // Der Austausch ist absichtlich sichtbar: der Nutzer soll erkennen,
    // woher Messwerte stammen, die er selbst nicht aufgezeichnet hat.
    refreshExchangeStatus() {
      if (!window.HistoryExchange) {
        this.historyExchangeStatus = 'Der Austausch ist in diesem Tab nicht aktiv.';
        return;
      }
      const status = window.HistoryExchange.status();
      if (!status.connected) {
        this.historyExchangeStatus = status.reason || 'Nicht verbunden.';
        return;
      }
      const others = status.peers.filter(peer => peer !== status.peerId).length;
      const geraete = others === 1 ? '1 weiteres Gerät' : `${others} weitere Geräte`;
      this.historyExchangeStatus = status.addedRows
        ? `Verbunden, ${geraete}. ${status.addedRows} Messwerte ergänzt.`
        : `Verbunden, ${geraete}. Noch nichts ergänzt.`;
    },

    // Ein Klick auf - / + der Stepper. dir ist +1 oder -1; der Wert bleibt
    // in den Feldgrenzen aus STEP_FIELDS.
    stepField(name, dir) {
      const cfg = STEP_FIELDS[name];
      if (!cfg) return;
      const current = Number(this[name]);
      const base = Number.isFinite(current) ? current : cfg.min;
      const next = base + (dir < 0 ? -cfg.step : cfg.step);
      this[name] = Math.min(cfg.max, Math.max(cfg.min, next));
    },

    // Gedrueckt halten: der erste Schritt kommt ueber x-on:click (auch per
    // Tastatur), hier startet nur der Wiederholeinsatz - nach 400 ms alle
    // 80 ms ein weiterer Schritt, wie eine gehaltene Taste.
    startRepeat(name, dir) {
      this.releaseStep();
      this._holdTimer = setTimeout(() => {
        this._holdInterval = setInterval(() => this.stepField(name, dir), 80);
      }, 400);
    },

    releaseStep() {
      clearTimeout(this._holdTimer);
      clearInterval(this._holdInterval);
      this._holdTimer = null;
      this._holdInterval = null;
    },

    get historyBytesPerDay() {
      const rollup = window.HistoryRollup;
      const store = window.HistoryStore;
      if (!rollup) return 0;
      const estimate = rollup.estimateBytesPerDay({
        intervalSeconds: Number(this.historySampleIntervalSeconds) || 10,
        seriesCount: Number(this.historySeriesCount) || 1,
        rawBytes: store ? store.ROW_BYTES_RAW : 120,
        rollupBytes: store ? store.ROW_BYTES_ROLLUP : 160,
      });
      return estimate.raw + estimate.minute + estimate.fiveMinute;
    },

    get historyVolumeText() {
      return `${this.formatBytes(this.historyBytesPerDay)}/Tag bei ${this.historySeriesCount} Serien`;
    },

    get historyBudgetText() {
      if (this.historyRetentionMode !== 'size') {
        return `Aufbewahrung ${this.historyRetentionHours} Stunden`;
      }
      const perDay = this.historyBytesPerDay;
      if (!perDay) return `Budget ${this.historyBudgetMb} MB`;
      const days = Math.floor((Number(this.historyBudgetMb) * 1024 * 1024) / perDay);
      return `Budget ${this.historyBudgetMb} MB reicht für rund ${days} Tage`;
    },

    get historyUsageText() {
      const own = `belegt ${this.formatBytes(this.historyBytesUsed)}`;
      if (!this.historyEstimate) return own;
      // Der eigene Zaehler regelt, die Browser-Angabe ist die Gegenprobe:
      // sie zaehlt das ganze Origin und rundet grob.
      return `${own} · Browser meldet ${this.formatBytes(this.historyEstimate.usage)} für diese Seite, Kontingent ${this.formatBytes(this.historyEstimate.quota)}`;
    },

    formatBytes(bytes) {
      const value = Number(bytes) || 0;
      if (value >= 1024 * 1024 * 1024) return `${(value / (1024 * 1024 * 1024)).toFixed(1)} GB`;
      if (value >= 1024 * 1024) return `${(value / (1024 * 1024)).toFixed(1)} MB`;
      if (value >= 1024) return `${(value / 1024).toFixed(0)} kB`;
      return `${value} B`;
    },

    get storageHealthBadgeClass() {
      if (this.storageHealthLoading || !this.storageHealth) return 'storage-health-badge-unavailable';
      return this.storageHealth.available
        ? (this.storageHealth.mode === 'estimated' ? 'storage-health-badge-estimated' : 'storage-health-badge-measured')
        : 'storage-health-badge-unavailable';
    },

    get storageHealthBadgeLabel() {
      if (this.storageHealthLoading || !this.storageHealth) return 'Wird geprüft';
      if (!this.storageHealth.available) return 'Nicht verfügbar';
      return this.storageHealth.mode === 'estimated' ? 'Geschätzt' : 'Gemessen';
    },

    formatStorageHealthTime(value) {
      return value ? new Date(value).toLocaleString() : '-';
    },

    formatStorageBytes(value) {
      const bytes = Number(value);
      if (!Number.isFinite(bytes)) return '-';
      const units = ['B', 'KB', 'MB', 'GB', 'TB'];
      let amount = bytes;
      let unit = 0;
      while (amount >= 1000 && unit < units.length - 1) { amount /= 1000; unit += 1; }
      return `${amount.toFixed(unit > 1 ? 1 : 0)} ${units[unit]}`;
    },

    formatStorageDays(value) {
      const days = Number(value);
      return Number.isFinite(days) ? `${days.toFixed(1)} Tage` : '-';
    },

    get valid() {
      return this.healthScoreThreshold >= 1 && this.sweepIntervalSeconds >= 1 &&
        this.liveUpdateIntervalSeconds >= 1 && this.liveUpdateIntervalSeconds <= 60 &&
        this.historySampleIntervalSeconds >= 5 && this.historySampleIntervalSeconds <= 3600 &&
        this.historyRetentionHours >= 1 && this.historyBudgetMb >= 16;
    },

    payload() {
      return {
        health_score_threshold: Number(this.healthScoreThreshold),
        sweep_interval_seconds: Number(this.sweepIntervalSeconds),
        show_discovery_tooltips: Boolean(this.showDiscoveryTooltips),
        show_runtime_status: Boolean(this.showRuntimeStatus),
        device_view_mode: this.deviceViewMode,
        theme: this.theme,
        show_config_entities_on_tile: Boolean(this.showConfigEntitiesOnTile),
        show_diagnostic_entities_on_tile: Boolean(this.showDiagnosticEntitiesOnTile),
        live_update_interval_seconds: Number(this.liveUpdateIntervalSeconds),
        wide_panels: [...this.widePanels],
        status_bar_items: [...this.statusBarItems],
        history_sample_interval_seconds: Number(this.historySampleIntervalSeconds),
        history_retention_mode: this.historyRetentionMode,
        history_retention_hours: Number(this.historyRetentionHours),
        history_budget_mb: Number(this.historyBudgetMb),
        history_raw_window_hours: Number(this.historyRawWindowHours),
        history_minute_window_days: Number(this.historyMinuteWindowDays),
        history_extra_entities: [...this.historyExtraEntities],
        history_exchange_disabled: Boolean(this.historyExchangeDisabled),
        history_views: [...this.historyViews],
      };
    },

    revisionConfig() {
      return {
        basePath: '/api/v1/settings',
        current: () => this.payload(),
        reload: () => this.load(),
        label: 'Revisionen der Einstellungen',
      };
    },

    async save() {
      if (!this.valid) {
        // critical, damit die Meldung stehen bleibt, waehrend der Nutzer die
        // Felder korrigiert - sie steht jetzt oben rechts, nicht am Formular.
        this.$store.toasts.push('Health-Schwellwert und Sweep-Intervall müssen mindestens 1 sein, das Live-Update-Intervall zwischen 1 und 60 Sekunden, die Verlaufs-Abtastrate zwischen 5 und 3600 Sekunden liegen.', 'critical');
        return;
      }
      this.saving = true;
      try {
        await requestJSON('/api/v1/settings', {
          method: 'PUT',
          headers: {'Content-Type': 'application/json'},
          body: JSON.stringify(this.payload()),
        });
        document.dispatchEvent(new CustomEvent('discovery-tooltips-setting-changed', {detail: {enabled: this.showDiscoveryTooltips}}));
        document.dispatchEvent(new CustomEvent('runtime-status-setting-changed', {detail: {enabled: this.showRuntimeStatus}}));
        document.dispatchEvent(new CustomEvent('device-view-mode-changed', {detail: {mode: this.deviceViewMode}}));
        document.documentElement.setAttribute('data-theme', this.theme);
        document.dispatchEvent(new CustomEvent('dashboard-theme-changed', {detail: {theme: this.theme}}));
        document.dispatchEvent(new CustomEvent('config-tile-setting-changed', {detail: {enabled: this.showConfigEntitiesOnTile}}));
        document.dispatchEvent(new CustomEvent('wide-panels-changed', {detail: {panels: [...this.widePanels]}}));
        document.dispatchEvent(new CustomEvent('status-bar-items-changed', {detail: {items: [...this.statusBarItems]}}));
        document.dispatchEvent(new CustomEvent('history-settings-changed', {detail: {
          intervalSeconds: Number(this.historySampleIntervalSeconds),
          retentionMode: this.historyRetentionMode,
          retentionHours: Number(this.historyRetentionHours),
          budgetMB: Number(this.historyBudgetMb),
          rawWindowHours: Number(this.historyRawWindowHours),
          minuteWindowDays: Number(this.historyMinuteWindowDays),
          extraEntities: [...this.historyExtraEntities],
          exchangeDisabled: Boolean(this.historyExchangeDisabled),
        }}));
        this.$store.toasts.push('Einstellungen gespeichert.');
      } catch (error) {
        this.$store.toasts.push(error.message, 'critical');
      } finally {
        this.saving = false;
      }
    },
  });

  const settingsPage = () => ({
    activeSettingsPage: 'general',
  });

  const systemPanel = () => ({
    username: '',
    canSystemActions: false,
    csrfToken: '',
    version: '',
    servicesVersion: '',
    loading: false,
    busy: false,

    async load() {
      this.loading = true;
      try {
        const session = await requestJSON('/api/v1/auth/session');
        this.username = session.username || '';
        this.canSystemActions = Boolean(session.system_actions);
        this.csrfToken = session.csrf_token || '';
      } catch (error) {
        this.$store.toasts.push(error.message, 'critical');
      } finally {
        this.loading = false;
      }
      try {
        const health = await requestJSON('/api/v1/health');
        this.version = health.version || '';
        this.servicesVersion = health.services_version || '';
      } catch (error) {
        this.version = '';
        this.servicesVersion = '';
      }
    },

    async logout() {
      this.loading = true;
      try {
        await requestJSON('/api/v1/auth/logout', {method: 'POST'});
        window.location.assign(`${window.__DASHBOARD_BASE_PATH__ || ''}/`);
      } catch (error) {
        this.$store.toasts.push(error.message, 'critical');
        this.loading = false;
      }
    },

    async run(action, label) {
      if (!this.canSystemActions || this.busy) return;
      const confirmed = await this.$store.modal.confirm({
        title: `${label} wirklich ausführen?`,
        confirmLabel: label,
        danger: true,
      });
      if (!confirmed) return;
      this.busy = true;
      try {
        await requestJSON(`/api/v1/system/${action}`, {
          method: 'POST',
          headers: {'X-CSRF-Token': this.csrfToken},
        });
        this.$store.toasts.push(`${label} wurde gestartet.`);
      } catch (error) {
        this.$store.toasts.push(error.message, 'critical');
      } finally {
        this.busy = false;
      }
    },
  });

  const tinyTuyaPanel = () => ({
    steps: [
      {id: 1, label: 'Zugang'},
      {id: 2, label: 'Gerät'},
      {id: 3, label: 'DPS'},
      {id: 4, label: 'Übernahme'},
    ],
    stepperLabel: 'TinyTuya-Schritte',
    currentStep: 1,
    direction: 'forward',
    region: 'eu',
    accessID: '',
    accessSecret: '',
    savedCredentials: false,
    savedAccessIDHint: '',
    saveCredentials: false,
    devices: [],
    selectedDeviceID: '',
    device: {device_id: '', name: '', local_key: '', ip: '', version: 3.3},
    dps: [],
    rawStatus: '',
    switchDP: '',
    config: {id: '', name: '', device_type: 'valve'},
    loading: false,
    toolOutput: '',

    async init() {
      try {
        const value = await requestJSON('/api/v1/tiny-tuya/credentials');
        this.savedCredentials = Boolean(value.configured);
        if (value.region) this.region = value.region;
        this.savedAccessIDHint = value.access_id_hint || '';
      } catch (error) {
        this.savedCredentials = false;
      }
    },

    get credentialsValid() {
      if (!this.region) return false;
      // A typed secret means explicit credentials, so an Access ID is required
      // too. Without a secret we rely on the saved credentials, regardless of
      // whatever the Access ID field currently holds (autofill or a leftover
      // value from an earlier query in this session).
      return this.accessSecret ? Boolean(this.accessID) : this.savedCredentials;
    },

    get deviceValid() {
      return Boolean(this.device.device_id && this.device.local_key && this.device.ip && Number(this.device.version) >= 3);
    },

    get configValid() {
      return Boolean(this.config.id && this.config.name && this.config.device_type && this.switchDP);
    },

    goToStep(id) {
      this.direction = id >= this.currentStep ? 'forward' : 'back';
      this.currentStep = id;
    },

    selectDevice() {
      const selected = this.devices.find(device => device.device_id === this.selectedDeviceID);
      if (!selected) return;
      this.device = {
        device_id: selected.device_id || '',
        name: selected.name || '',
        local_key: selected.local_key || '',
        ip: selected.ip || '',
        version: Number(selected.version || 3.3),
      };
      this.config.id = selected.device_id || '';
      this.config.name = selected.name || selected.device_id || '';
    },

    async loadDevices() {
      this.loading = true;
      this.toolOutput = '';
      try {
        this.devices = await requestJSON('/api/v1/tiny-tuya/devices', {
          method: 'POST',
          headers: {'Content-Type': 'application/json'},
          // Only send the Access ID alongside an explicitly typed secret;
          // otherwise let the backend fall back to the saved credentials.
          body: JSON.stringify({
            region: this.region,
            access_id: this.accessSecret ? this.accessID : '',
            access_secret: this.accessSecret,
          }),
        });
        if (!Array.isArray(this.devices) || this.devices.length === 0) throw new Error('Keine Tuya-Geräte gefunden.');
        if (this.saveCredentials && this.accessSecret) {
          const saved = await requestJSON('/api/v1/tiny-tuya/credentials', {
            method: 'POST',
            headers: {'Content-Type': 'application/json'},
            body: JSON.stringify({region: this.region, access_id: this.accessID, access_secret: this.accessSecret}),
          });
          this.savedCredentials = Boolean(saved.configured);
          this.savedAccessIDHint = saved.access_id_hint || '';
          this.accessSecret = '';
        }
        this.selectedDeviceID = this.devices[0].device_id || '';
        this.selectDevice();
        this.goToStep(2);
      } catch (error) {
        this.$store.toasts.push(error.message, 'critical');
        this.toolOutput = error.toolOutput || error.message;
      } finally {
        this.loading = false;
      }
    },

    async testConnection() {
      this.loading = true;
      this.toolOutput = '';
      try {
        const result = await requestJSON('/api/v1/tiny-tuya/status', {
          method: 'POST',
          headers: {'Content-Type': 'application/json'},
          body: JSON.stringify(this.device),
        });
        this.dps = Array.isArray(result.dps) ? result.dps : [];
        this.rawStatus = JSON.stringify(result.raw || result, null, 2);
        this.switchDP = this.dps.some(item => String(item.id) === '1') ? '1' : (this.dps[0]?.id || '');
        this.goToStep(3);
      } catch (error) {
        this.$store.toasts.push(error.message, 'critical');
        this.toolOutput = error.toolOutput || error.message;
      } finally {
        this.loading = false;
      }
    },

    async saveDevice() {
      this.loading = true;
      this.toolOutput = '';
      try {
        const result = await requestJSON('/api/v1/tiny-tuya/configure', {
          method: 'POST',
          headers: {'Content-Type': 'application/json'},
          body: JSON.stringify({
            ...this.device,
            ...this.config,
            datapoints: {switch: String(this.switchDP)},
          }),
        });
        if (result.reload_failed) {
          this.$store.toasts.push(`Tuya-Gerät gespeichert, aber der Dienst konnte nicht neu geladen werden: ${result.reload_error || 'unbekannter Fehler'}`, 'critical');
        } else {
          this.$store.toasts.push('Tuya-Gerät übernommen und Dienst neu geladen.');
        }
        this.accessSecret = '';
        this.device.local_key = '';
        this.goToStep(1);
      } catch (error) {
        this.$store.toasts.push(error.message, 'critical');
        this.toolOutput = error.toolOutput || error.message;
      } finally {
        this.loading = false;
      }
    },
  });

  const register = () => {
    if (!window.Alpine) return;
    Alpine.data('settingsPage', settingsPage);
    Alpine.data('settingsPanel', settingsPanel);
    Alpine.data('systemPanel', systemPanel);
    Alpine.data('tinyTuyaPanel', tinyTuyaPanel);
  };
  if (window.Alpine) register(); else document.addEventListener('alpine:init', register, {once: true});
})();
