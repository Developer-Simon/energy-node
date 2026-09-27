(() => {
  const requestJSON = async (url, options) => {
    // The single chokepoint for every URL literal in this file: behind a
    // reverse-proxy subpath base.html puts the prefix into
    // __DASHBOARD_BASE_PATH__; on direct access it is empty.
    const response = await fetch(`${window.__DASHBOARD_BASE_PATH__ || ''}${url}`, options);
    const body = await response.json();
    if (!response.ok) {
      const error = new Error(body.message || t('common.request_failed'));
      error.toolOutput = body.tool_output || '';
      throw error;
    }
    return body;
  };

  const t = (key, params) => (window.I18n ? window.I18n.t(key, params) : key);
  const tn = (key, n, params) => (window.I18n ? window.I18n.tn(key, n, params) : key);
  const storageText = (key, params) => {
    // Format numeric parameters with number formatting before translation
    if (!params) return t(key, params);
    const formattedParams = {};
    for (const [k, v] of Object.entries(params)) {
      formattedParams[k] = typeof v === 'number' && window.I18n ? window.I18n.formatNumber(v) : v;
    }
    return t(key, formattedParams);
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
    showRuntimeStatus: true,
    deviceViewMode: 'compact',
    theme: 'mint',
    numberFormat: 'auto',
    numberGrouping: 'match',
    loadedNumberFormat: 'auto|match',
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
    updateCheckDisabled: false,
    languageSwitchHidden: false,
    // Die Sprache ist keine Knoten-Einstellung, sondern das Cookie dieses
    // Browsers (i18n.js). Die Kachel "Formatierung" zeigt sie trotzdem hier,
    // uebernommen wird sie wie alles auf der Seite erst mit "Speichern".
    uiLanguage: window.I18n ? window.I18n.lang : 'de',
    // Wird aus der IndexedDB nachgeladen; 6 ist die Zahl der Energie-Rollen
    // und damit der ehrliche Startwert, solange noch nichts aufgezeichnet ist.
    historySeriesCount: 6,
    historyBytesUsed: 0,
    historyEstimate: null,
    // Beschriftungen aus der Tab-Leiste (base.html), Schluessel ist die
    // Panel-ID ohne -panel. Labels are translation keys, translated in template.
    widePanelOptions: [
      {key: 'overview', label: 'nav.overview'},
      {key: 'devices', label: 'nav.devices'},
      {key: 'history', label: 'nav.history'},
      {key: 'config', label: 'nav.config'},
      {key: 'energy', label: 'nav.energy'},
      {key: 'layout', label: 'nav.layout'},
      {key: 'devicemap', label: 'nav.devicemap'},
      {key: 'diagnostics', label: 'nav.diagnostics'},
      {key: 'settings', label: 'nav.settings'},
      {key: 'automations', label: 'nav.automations'},
    ],
    statusBarItems: [],
    // Schluessel, wie sie runtimeStatusPanel() in dashboard.js interpretiert.
    // Labels are translation keys for status bar item names (technical names stay untranslated).
    statusBarItemOptions: [
      {key: 'mqtt', label: 'MQTT'},
      {key: 'cache', label: 'Cache'},
      {key: 'storage', label: 'Storage'},
      {key: 'uptime', label: 'Uptime'},
      {key: 'version', label: 'Dashboard-Version'},
      {key: 'cpu_temp', label: 'CPU-Temperatur'},
      {key: 'ram', label: 'RAM'},
      {key: 'undervoltage', label: 'Unterspannung'},
    ], // i18n-ignore: technical names (MQTT, Cache, Storage, Uptime, RAM, CPU-Temperatur, Unterspannung) stay as fallback for status bar items
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
        this.showRuntimeStatus = value.show_runtime_status !== false;
        this.deviceViewMode = value.device_view_mode === 'control' ? 'control' : 'compact';
        this.theme = ['mint', 'stromblau', 'signalgelb', 'tageslicht'].includes(value.theme) ? value.theme : 'mint';
        this.numberFormat = ['auto', 'comma', 'point'].includes(value.number_format) ? value.number_format : 'auto';
        this.numberGrouping = ['match', 'thin'].includes(value.number_grouping) ? value.number_grouping : 'match';
        this.loadedNumberFormat = `${this.numberFormat}|${this.numberGrouping}`;
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
        this.updateCheckDisabled = Boolean(value.update_check_disabled);
        this.languageSwitchHidden = Boolean(value.language_switch_hidden);
        this.uiLanguage = window.I18n ? window.I18n.lang : this.uiLanguage;
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
      const options = {removeItemButton: true, searchEnabled: true, shouldSort: false, placeholderValue: t('settings.choices.placeholder'), noChoicesText: t('settings.choices.no_results')};
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

    storageText(key, params, fallback) {
      // Format numeric parameters with number formatting before translation,
      // then translate the key. Used for storage health remaining/consumed fields
      // that come from the server with numeric params needing locale formatting.
      if (!params) return t(key, params);
      const formattedParams = {};
      for (const [k, v] of Object.entries(params)) {
        formattedParams[k] = typeof v === 'number' && window.I18n ? window.I18n.formatNumber(v) : v;
      }
      return t(key, formattedParams) || fallback;
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
        this.historyExchangeStatus = t('settings.history_exchange.not_active');
        return;
      }
      const status = window.HistoryExchange.status();
      if (!status.connected) {
        this.historyExchangeStatus = status.reason || t('settings.history_exchange.disconnected');
        return;
      }
      const others = status.peers.filter(peer => peer !== status.peerId).length;
      const peers = others === 1 ? t('settings.history_exchange.connected_peers_one') : t('settings.history_exchange.connected_peers', {others});
      this.historyExchangeStatus = status.addedRows
        ? t('settings.history_exchange.connected_rows_added', {peers, rows: status.addedRows})
        : t('settings.history_exchange.connected_rows_unchanged', {peers});
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
      return t('settings.history.volume_per_day', {bytes: this.formatBytes(this.historyBytesPerDay), series: this.historySeriesCount});
    },

    get historyBudgetText() {
      if (this.historyRetentionMode !== 'size') {
        return t('settings.history.budget_time', {hours: this.historyRetentionHours});
      }
      const perDay = this.historyBytesPerDay;
      if (!perDay) return t('settings.history.budget_size', {mb: this.historyBudgetMb, days: '?'});
      const days = Math.floor((Number(this.historyBudgetMb) * 1024 * 1024) / perDay);
      return t('settings.history.budget_size', {mb: this.historyBudgetMb, days});
    },

    get historyUsageText() {
      const own = t('settings.history.usage_own', {bytes: this.formatBytes(this.historyBytesUsed)});
      if (!this.historyEstimate) return own;
      // Der eigene Zaehler regelt, die Browser-Angabe ist die Gegenprobe:
      // sie zaehlt das ganze Origin und rundet grob.
      return t('settings.history.usage_browser', {own, usage: this.formatBytes(this.historyEstimate.usage), quota: this.formatBytes(this.historyEstimate.quota)});
    },

    formatBytes(bytes) {
      const value = Number(bytes) || 0;
      const n = window.I18n.formatNumber;
      if (value >= 1024 * 1024 * 1024) return `${n(value / (1024 * 1024 * 1024), 1)} GB`;
      if (value >= 1024 * 1024) return `${n(value / (1024 * 1024), 1)} MB`;
      if (value >= 1024) return `${n(value / 1024, 0)} kB`;
      return `${n(value, 0)} B`;
    },

    get storageHealthBadgeClass() {
      if (this.storageHealthLoading || !this.storageHealth) return 'storage-health-badge-unavailable';
      return this.storageHealth.available
        ? (this.storageHealth.mode === 'estimated' ? 'storage-health-badge-estimated' : 'storage-health-badge-measured')
        : 'storage-health-badge-unavailable';
    },

    get storageHealthBadgeLabel() {
      if (this.storageHealthLoading || !this.storageHealth) return t('storage_health.checking');
      if (!this.storageHealth.available) return t('storage_health.badge.unavailable');
      return this.storageHealth.mode === 'estimated' ? t('storage_health.badge.estimated') : t('storage_health.badge.measured');
    },

    formatStorageHealthTime(value) {
      return value ? window.I18n.formatDateTime(value) || '-' : '-';
    },

    formatStorageBytes(value) {
      const bytes = Number(value);
      if (!Number.isFinite(bytes)) return '-';
      const units = ['B', 'KB', 'MB', 'GB', 'TB'];
      let amount = bytes;
      let unit = 0;
      while (amount >= 1000 && unit < units.length - 1) { amount /= 1000; unit += 1; }
      return `${window.I18n.formatNumber(amount, unit > 1 ? 1 : 0)} ${units[unit]}`;
    },

    formatStorageDays(value) {
      const days = Number(value);
      return Number.isFinite(days) ? t('storage_health.days_format', {n: window.I18n.formatNumber(days, 1)}) : '-';
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
        show_runtime_status: Boolean(this.showRuntimeStatus),
        device_view_mode: this.deviceViewMode,
        theme: this.theme,
        number_format: this.numberFormat,
        number_grouping: this.numberGrouping,
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
        update_check_disabled: Boolean(this.updateCheckDisabled),
        language_switch_hidden: Boolean(this.languageSwitchHidden),
      };
    },

    // Kept as a method so tests can replace it, jsdom cannot reload.
    reloadPage() {
      window.location.reload();
    },

    revisionConfig() {
      return {
        basePath: '/api/v1/settings',
        current: () => this.payload(),
        reload: () => this.load(),
        label: t('settings.revisions.label'),
      };
    },

    async save() {
      if (!this.valid) {
        // critical, damit die Meldung stehen bleibt, waehrend der Nutzer die
        // Felder korrigiert - sie steht jetzt oben rechts, nicht am Formular.
        this.$store.toasts.push(t('settings.validation_error'), 'critical');
        return;
      }
      this.saving = true;
      try {
        await requestJSON('/api/v1/settings', {
          method: 'PUT',
          headers: {'Content-Type': 'application/json'},
          body: JSON.stringify(this.payload()),
        });
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
        document.dispatchEvent(new CustomEvent('language-switch-setting-changed', {detail: {visible: !this.languageSwitchHidden}}));
        this.$store.toasts.push(t('settings.saved'));
        // Erst nach dem erfolgreichen Speichern: der Sprachwechsel laedt neu,
        // ungespeicherte Aenderungen gingen sonst verloren.
        if (window.I18n && this.uiLanguage !== window.I18n.lang) {
          window.I18n.setLanguage(this.uiLanguage);
        } else if (`${this.numberFormat}|${this.numberGrouping}` !== this.loadedNumberFormat) {
          // Numbers are rendered by the server and by many scripts, a reload
          // applies a new format everywhere at once (like a language switch).
          this.reloadPage();
        }
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
    updateStatus: null,
    checkingForUpdates: false,

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
      try {
        const status = await requestJSON('/api/v1/updates/status');
        this.updateStatus = status.checked === false ? null : status;
      } catch (error) {
        this.updateStatus = null;
      }
    },

    async checkForUpdates() {
      if (this.checkingForUpdates) return;
      this.checkingForUpdates = true;
      try {
        this.updateStatus = await requestJSON('/api/v1/updates/check');
      } catch (error) {
        this.$store.toasts.push(error.message, 'critical');
      } finally {
        this.checkingForUpdates = false;
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
        title: t('settings.system_action.confirm_title', {action: label}),
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
        this.$store.toasts.push(t('settings.system_action.started', {action: label}));
      } catch (error) {
        this.$store.toasts.push(error.message, 'critical');
      } finally {
        this.busy = false;
      }
    },
  });

  const tinyTuyaPanel = () => ({
    steps: [
      {id: 1, label: 'settings.tuya.step.access'},
      {id: 2, label: 'settings.tuya.step.device'},
      {id: 3, label: 'settings.tuya.step.dps'},
      {id: 4, label: 'settings.tuya.step.takeover'},
    ],
    stepperLabel: 'TinyTuya-Schritte', // i18n-ignore: TinyTuya is a brand name
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
        if (!Array.isArray(this.devices) || this.devices.length === 0) throw new Error(t('settings.tuya.no_devices_found'));
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
          this.$store.toasts.push(t('settings.tuya.save_error', {error: result.reload_error || t('common.unknown_error')}), 'critical');
        } else {
          this.$store.toasts.push(t('settings.tuya.save_success'));
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
