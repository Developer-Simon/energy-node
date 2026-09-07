(() => {
  // Prefixes an absolute in-app path with the reverse-proxy base path that
  // base.html publishes before any deferred script runs. Empty (and thus a
  // no-op) on direct access, so every generated URL stays byte-identical.
  const withBase = path => `${window.__DASHBOARD_BASE_PATH__ || ''}${path}`;

  const dashboardShell = () => ({
    activePanel: 'overview-panel',
    activePage: '',
    // Nur die Mobilansicht liest diesen Zustand: oberhalb 700px greift
    // keine CSS-Regel darauf zu, die Tab-Leiste rendert wie eh und je.
    navOpen: false,

    // Aus dem DOM gelesen statt dupliziert: eine Tabelle panel-id -> Text
    // muesste bei jeder Tab-Umbenennung in base.html nachgezogen werden.
    get activeTabLabel() {
      const tab = document.querySelector(`[role="tab"][aria-controls="${this.activePanel}"]`);
      return tab ? tab.textContent.trim() : '';
    },
    expandedEntities: {},
    panelRequests: {},
    editorAssetsPromise: null,
    panelErrors: {},
    loadedScripts: new Set(),
    loadedStyles: new Set(),
    liveSource: null,
    liveFallback: null,

    // Tabs, die die volle Fensterbreite nutzen duerfen. Serverseitig ins
    // data-Attribut geschrieben, damit die Breite schon beim ersten Anstrich
    // stimmt - ein nachgeladener GET wuerde sichtbar springen.
    widePanels: [],

    // Im Editor mit "+ Seite" angelegte, noch nicht gespeicherte Seiten.
    // base.html rendert daraus zusaetzliche .tab.is-page-Knoepfe (x-for).
    // Nach echtem Speichern kommen die Seiten serverseitig und die Liste
    // wird beim naechsten Anstrich leer.
    extraPages: [],

    // Server-gerenderte Seiten-Tabs, die der Editor inzwischen umbenannt oder
    // geloescht hat.
    hiddenPages: [],
    // Genau ein Besitzer fuer das Uebersichts-Fragment: der
    // registry-updated-Zuhoerer in devicesPanel(). Der laedt ueber
    // refreshLiveFragment(), das sein Ziel aus $root.id ableitet und
    // view_mode an die URL haengt - beides fehlte dem Aufruf, der frueher
    // hier stand. Gemessen liefen dadurch zwei *verschiedene* Fragmente in
    // dieselbe Zielstelle, und die Reihenfolge der Antworten entschied.
    publishRegistryUpdate(detail = {}) {
      window.dispatchEvent(new CustomEvent('registry-updated', {detail}));
    },

    async setActivePanel(panel) {
      // Verlaesst der Nutzer die Uebersicht mit offenem Bearbeitungsmodus und
      // ungespeicherten Aenderungen, fragt erst der Waechter des Editors nach
      // (Themen-<dialog>, kein window.confirm). Sagt er ab, bleibt alles stehen.
      const editor = window.__layoutEditor__;
      if (editor && this.activePanel === 'overview-panel' && panel !== 'overview-panel'
          && editor.unsaved && !(await editor.confirmLeave('tab'))) return;
      this.navOpen = false;
      this.activePanel = panel;
      this.applyPageWidth();
      this.loadPanel(panel);
      window.dispatchEvent(new CustomEvent('dashboard-panel-changed', {detail: {panel}}));
    },

    async setActivePage(name) {
      // Aendert die aktive Layout-Seite und zeigt die Uebersicht an. Ein
      // Seitenwechsel wirft den Editor-Inhalt ebenso um wie ein Tab-Wechsel -
      // darum derselbe Waechter.
      const editor = window.__layoutEditor__;
      if (editor && editor.unsaved && name !== this.activePage && !(await editor.confirmLeave('tab'))) return;
      this.activePage = name;
      this.setActivePanel('overview-panel');
      // Die Seite steckt im server-gerenderten Fragment, nicht in einer
      // CSS-Klasse: ohne diesen Anstoss bliebe nach dem Tab-Klick genau
      // dieselbe Seite stehen. devicesPanel() hoert darauf und laedt
      // #overview-live mit dem neuen page= nach.
      window.dispatchEvent(new CustomEvent('layout-page-changed', {detail: {page: name}}));
    },

    initWidePanels() {
      this.widePanels = (document.body.dataset.widePanels || '').split(',').filter(Boolean);
      this.applyPageWidth();
      // Die Einstellungsseite speichert ohne Neuladen; ohne das Ereignis
      // wuerde die Breite erst beim naechsten Seitenaufruf folgen.
      document.addEventListener('wide-panels-changed', event => {
        this.widePanels = event.detail?.panels || [];
        this.applyPageWidth();
      });
    },

    // Der Tab-Schluessel ist die Panel-ID ohne das Suffix -panel.
    applyPageWidth() {
      const key = String(this.activePanel || '').replace(/-panel$/, '');
      document.body.style.setProperty('--page-max', this.widePanels.includes(key) ? 'none' : '1180px');
    },

    async loadPanel(panel) {
      const container = document.getElementById(panel);
      if (!container || container.dataset.panelLoaded === 'true' || this.panelRequests[panel]) return;
      let source = container.dataset.panelSrc;
      if (!source) return;
      if (panel === 'config-panel') {
        const configName = new URLSearchParams(window.location.search).get('config');
        if (configName) source += `&config=${encodeURIComponent(configName)}`;
      }
      const request = new AbortController();
      this.panelRequests[panel] = request;
      delete this.panelErrors[panel];
      try {
        // No dashboardPath() here: source comes from data-panel-src, which the
        // template already emitted with the base prefix. Rule of thumb -
        // template URLs arrive prefixed, JS literals prefix themselves.
        const response = await fetch(source, {headers: {'X-Requested-With': 'XMLHttpRequest'}, signal: request.signal});
        if (!response.ok) throw new Error(`Panel konnte nicht geladen werden (${response.status})`);
        const html = await response.text();
        if (this.activePanel !== panel) return;
        await this.loadAsset(container.dataset.panelCss, 'style');
        await this.loadAsset(container.dataset.panelScript, 'script');
        container.outerHTML = html;
        const loaded = document.getElementById(panel);
        if (loaded) {
          loaded.dataset.panelLoaded = 'true';
        }
      } catch (error) {
        if (error.name !== 'AbortError') {
          this.panelErrors[panel] = error.message;
          const message = container.querySelector('[data-panel-loading]');
          if (message) message.textContent = error.message;
        }
      } finally {
        delete this.panelRequests[panel];
      }
    },

    // Accepts a comma-separated list so dependent scripts (e.g. Popper before
    // Tippy, which reads window.Popper at load time) load one after another
    // instead of racing.
    loadAsset(source, type) {
      const sources = (source || '').split(',').map(part => part.trim()).filter(Boolean);
      return sources.reduce((chain, url) => chain.then(() => this.loadSingleAsset(url, type)), Promise.resolve());
    },

    // No withBase() here either: source is a data-panel-script/-css entry and
    // therefore already prefixed by the template.
    loadSingleAsset(source, type) {
      if ((type === 'style' ? this.loadedStyles : this.loadedScripts).has(source)) return Promise.resolve();
      return new Promise((resolve, reject) => {
        const asset = document.createElement(type === 'style' ? 'link' : 'script');
        if (type === 'style') {
          asset.rel = 'stylesheet';
          asset.href = source;
        } else {
          asset.src = source;
          asset.defer = true;
        }
        asset.onload = () => {
          (type === 'style' ? this.loadedStyles : this.loadedScripts).add(source);
          resolve();
        };
        asset.onerror = () => reject(new Error(`Asset konnte nicht geladen werden: ${source}`));
        document.head.append(asset);
      });
    },

    // Die Editor-Abhaengigkeiten kommen erst beim ersten Druck auf Editieren.
    // overview-panel ist das einzige Panel ohne data-panel-src - es steht
    // inline in der Seite, und ohne diese Bremse zahlte jeder Aufruf der
    // Uebersicht Gridstack und Choices mit. Das Promise wird gemerkt, damit
    // ein zweiter Klick nichts nachlaedt; bei Fehlschlag wird es verworfen,
    // sonst bliebe der Editor nach einem Netzausfall dauerhaft tot.
    editorAssetsReady() {
      if (!this.editorAssetsPromise) {
        const panel = document.getElementById('overview-panel');
        this.editorAssetsPromise = Promise.all([
          this.loadAsset(panel?.dataset.editorScript, 'script'),
          this.loadAsset(panel?.dataset.editorCss, 'style'),
        ]).catch(error => { this.editorAssetsPromise = null; throw error; });
      }
      return this.editorAssetsPromise;
    },

    init() {
      // Die serverseitig gerenderten Layout-Seiten-Tabs. Neue Seiten aus dem
      // Editor (layout-pages-changed) werden dagegen abgeglichen, damit nur
      // die wirklich neuen als extraPages erscheinen.
      this._basePages = [...document.querySelectorAll('.tab.is-page')].map(tab => tab.dataset.pageId).filter(Boolean);
      // Ohne Vorgabe rendert der Server die erste Seite (activePage() in
      // webui.go). Damit der zugehoerige Tab von Anfang an als aktiv erscheint
      // - und der erste Klick auf ihn nicht wirkungslos aussieht - steht sie
      // hier auch im Zustand.
      if (!this.activePage && this._basePages.length) this.activePage = this._basePages[0];
      document.addEventListener('layout-pages-changed', event => {
        const names = event.detail?.pages || [];
        this.extraPages = names.filter(name => name && !this._basePages.includes(name));
        // Umbenennen und Loeschen: ein server-gerenderter Tab, dessen Name in
        // der gemeldeten Liste fehlt, gehoert nicht mehr in die Leiste. Ohne
        // das stuenden nach einem Umbenennen alter und neuer Name nebeneinander.
        this.hiddenPages = this._basePages.filter(name => !names.includes(name));
        // Der Editor sagt, welche Seite gemeint ist. Ohne Angabe bleibt die
        // alte Vermutung "die zuletzt hinzugekommene".
        if (event.detail?.active) this.activePage = event.detail.active;
        else if (this.extraPages.length) this.activePage = this.extraPages[this.extraPages.length - 1];
      });

      // Die Huelle muss auch ohne EventSource erreichbar sein (der Waechter
      // des Editors greift ueber window.__dashboardShell__ zu), darum vor dem
      // Fallback-Return.
      window.__dashboardShell__ = this;

      const requestedPanel = new URLSearchParams(window.location.search).get('panel');
      const panelMap = {devices: 'devices-panel', history: 'history-panel', diagnostics: 'diagnostics-panel', config: 'config-panel', energy: 'energy-panel', devicemap: 'devicemap-panel', settings: 'settings-panel', automations: 'automations-panel'};
      if (panelMap[requestedPanel]) this.activePanel = panelMap[requestedPanel];
      this.loadPanel(this.activePanel);
      if (!window.EventSource) {
        this.liveFallback = window.setInterval(() => this.publishRegistryUpdate({source: 'fallback'}), 30000);
        return;
      }
      this.liveSource = new EventSource(withBase('/api/v1/events'));
      this.liveSource.addEventListener('registry', event => {
        let detail = {};
        try {
          detail = JSON.parse(event.data || '{}');
        } catch (error) {
          detail = {source: 'event-stream'};
        }
        this.publishRegistryUpdate(detail);
      });
      this.liveSource.onerror = () => {
        if (this.liveSource) this.liveSource.close();
        this.liveSource = null;
        if (!this.liveFallback) this.liveFallback = window.setInterval(() => this.publishRegistryUpdate({source: 'fallback'}), 30000);
      };
    },

    destroy() {
      if (this.liveSource) this.liveSource.close();
      if (this.liveFallback) window.clearInterval(this.liveFallback);
    },
  });

  const requestJSON = async (url, options) => {
    // withBase() is the single chokepoint for every URL literal below.
    const response = await fetch(withBase(url), options);
    const body = await response.json();
    if (!response.ok) throw new Error(body.message || 'Anfrage fehlgeschlagen');
    return body;
  };
  const newID = prefix => `${prefix}-${Date.now()}-${Math.random().toString(16).slice(2)}`;

  const renderLocalTimestamps = root => {
    root.querySelectorAll('[data-local-timestamp]').forEach(node => {
      const timestamp = new Date(node.dateTime);
      if (Number.isNaN(timestamp.getTime())) return;
      node.textContent = timestamp.toLocaleString();
    });
  };

  const timestampValueToMilliseconds = value => {
    const numeric = Number(String(value).trim());
    if (Number.isFinite(numeric)) return Math.abs(numeric) < 1e12 ? numeric * 1000 : numeric;
    const timestamp = Date.parse(value);
    return Number.isNaN(timestamp) ? null : timestamp;
  };

  const formatRelativeTimestamp = timestamp => {
    const difference = Math.round((timestamp - Date.now()) / 1000);
    const absoluteDifference = Math.abs(difference);
    if (absoluteDifference < 1) return 'gerade eben';
    const units = absoluteDifference < 60 ? ['s', 1]
      : absoluteDifference < 3600 ? ['min', 60]
      : absoluteDifference < 86400 ? ['h', 3600]
      : ['T', 86400];
    const amount = Math.round(absoluteDifference / units[1]);
    return difference < 0 ? `vor ${amount}${units[0]}` : `in ${amount}${units[0]}`;
  };

  const renderRelativeTimestamps = root => {
    root.querySelectorAll('[data-relative-timestamp]').forEach(node => {
      const timestamp = timestampValueToMilliseconds(node.dataset.relativeTimestamp);
      if (timestamp === null) return;
      node.textContent = formatRelativeTimestamp(timestamp);
      node.title = new Date(timestamp).toLocaleString();
    });
  };

  const renderTimestamps = root => {
    renderLocalTimestamps(root);
    renderRelativeTimestamps(root);
  };

  document.addEventListener('DOMContentLoaded', () => renderTimestamps(document));
  document.addEventListener('htmx:afterSwap', event => renderTimestamps(event.target));
  setInterval(() => {
    if (document.visibilityState === 'visible') renderRelativeTimestamps(document);
  }, 1000);
  document.addEventListener('visibilitychange', () => {
    if (document.visibilityState === 'visible') renderRelativeTimestamps(document);
  });

  const runtimeStatusPanel = () => ({
    status: null,
    enabled: true,
    items: ['mqtt', 'storage', 'uptime', 'version'],
    loading: false,
    error: '',
    timer: null,
    settingChanged: null,
    itemsChanged: null,

    get statusLabel() {
      if (this.loading && !this.status) return 'Lade Status ...';
      if (this.error && !this.status) return 'Status nicht verfügbar';
      return this.status?.status === 'degraded' ? 'Beeinträchtigt' : 'Betriebsbereit';
    },

    get statusClass() {
      if (this.error && !this.status) return 'runtime-status-error';
      return this.status?.status === 'degraded' ? 'runtime-status-degraded' : 'runtime-status-ok';
    },

    formatUptime(seconds) {
      const value = Number(seconds);
      if (!Number.isFinite(value)) return '-';
      const days = Math.floor(value / 86400);
      const hours = Math.floor((value % 86400) / 3600);
      const minutes = Math.floor((value % 3600) / 60);
      if (days) return `${days} T ${hours} h`;
      if (hours) return `${hours} h ${minutes} min`;
      return `${minutes} min`;
    },

    async load() {
      this.loading = true;
      this.error = '';
      try {
        this.status = await requestJSON('/api/v1/health');
      } catch (error) {
        // Das Feld bleibt: statusLabel/statusClass leiten das Badge daraus ab.
        // Der 30-s-Poll wuerde ohne die Entdopplung im Toast-Store die Liste
        // fluten - der Zaehler fasst ihn zu einem hochzaehlenden Toast zusammen.
        this.error = error.message;
        this.$store.toasts.push(error.message, 'critical');
      } finally {
        this.loading = false;
      }
    },

    init() {
      // Alpine ruft init() selbst; ein zusaetzliches x-init="init()" im
      // Template waere ein zweiter Aufruf und damit ein zweites
      // 30-Sekunden-Intervall. Auch ohne diese Ursache kann ein
      // Fragment-Nachladen oder ein Panel-Wechsel doppelt einsteigen.
      if (this.timer) return;
      this.enabled = this.$root.dataset.runtimeStatusEnabled !== 'false';
      this.items = (this.$root.dataset.statusBarItems || '').split(',').filter(Boolean);
      this.settingChanged = event => {
        this.enabled = Boolean(event.detail.enabled);
        if (!this.enabled) {
          if (this.timer) window.clearInterval(this.timer);
          this.timer = null;
          this.status = null;
          this.error = '';
          return;
        }
        this.load();
        if (!this.timer) this.timer = window.setInterval(() => this.load(), 30000);
      };
      this.itemsChanged = event => {
        this.items = event.detail?.items || [];
      };
      document.addEventListener('runtime-status-setting-changed', this.settingChanged);
      document.addEventListener('status-bar-items-changed', this.itemsChanged);
      if (this.enabled) {
        this.load();
        this.timer = window.setInterval(() => this.load(), 30000);
      }
    },

    destroy() {
      if (this.timer) window.clearInterval(this.timer);
      if (this.settingChanged) document.removeEventListener('runtime-status-setting-changed', this.settingChanged);
      if (this.itemsChanged) document.removeEventListener('status-bar-items-changed', this.itemsChanged);
    },
  });

  const devicesPanel = () => ({
    ...window.deviceTileMixin(),
    tooltipInstances: [],
    settingChanged: null,
    deviceViewModeChanged: null,
    contentChanged: null,
    registryUpdated: null,
    dialogClosed: null,
    viewMode: 'compact',
    viewModeSaving: false,
    viewModeMessage: '',
    selectedDeviceId: '',
    // Steht ein Geraete-Modal offen, verdeckt es das Raster. Bis es
    // schliesst, ruht das zeilenweise Nachziehen der Kacheln; dieser Marker
    // merkt sich, dass beim Schliessen einmal nachgeholt werden muss.
    livePatchStale: false,
    deviceDetail: null,
    detailLoading: false,
    detailError: '',
    reloadLoading: false,
    detailRequestToken: 0,
    // Deklariert, damit Alpines mergeProxies() die Zuweisung nicht in den
    // aeusseren Bereich (dashboardShell) durchreicht - dort ueberlebte die
    // Kette jeden Fragment-Tausch.
    _liveQueue: null,
    activeDeviceTab: 'measurements',
    mergedTab: false,
    // Erfahrungswert, kein Naturgesetz: ab dieser verfuegbaren Hoehe der
    // device-modal-scrollarea (Sidebar+Switcher zusammen, siehe
    // observeMergeThreshold) legen sich Messwerte und Steuerung zu einem Tab
    // zusammen. Kalibriert in Task 8 dieses Plans gegen echte Geraete.
    mergeHeightThreshold: 520,
    mergeObserver: null,
    // Zuletzt gemessene Scrollflaechen-Hoehe - damit setDeviceTab() beim
    // Wechsel auf Messwerte/Steuerung den Merge-Zustand nachziehen kann,
    // ohne auf das naechste ResizeObserver-Ereignis zu warten.
    availableHeight: 0,
    // true, sobald das Modal schmaler als die 860px-Container-Schwelle ist
    // (Akkordeon statt Tableiste). Rein aus JS beobachtet, weil die
    // Aktivitaets-Sektion in "Verwaltung & Analyse" nur im Akkordeon zeigt.
    isAccordion: false,
    // Sekundentakt fuer die relative "aktualisiert vor X"-Anzeige.
    nowTick: Date.now(),
    clockTimer: null,
    csrfToken: '',
    canDeleteDiscovery: false,
    deletePreview: null,
    deletePreviewDeviceId: '',
    deletePreviewLoading: false,
    deletePreviewError: '',
    deleteLoading: false,

    async loadSession() {
      try {
        const session = await requestJSON('/api/v1/auth/session');
        this.csrfToken = session.csrf_token || '';
        this.canDeleteDiscovery = session.delete_device_discovery === true;
      } catch (error) {
        this.csrfToken = '';
        this.canDeleteDiscovery = false;
      }
    },

    mutationOptions(body) {
      const options = {method: 'POST', headers: {'X-CSRF-Token': this.csrfToken}};
      if (body !== undefined) {
        options.headers['Content-Type'] = 'application/json';
        options.body = JSON.stringify(body);
      }
      return options;
    },

    async refreshAfterMutation() {
      await this.refreshLiveFragment();
      window.dispatchEvent(new CustomEvent('registry-updated', {detail: {source: 'device-action'}}));
    },

    async ignoreDevice(deviceId) {
      if (!deviceId) return;
      const confirmed = await this.$store.modal.confirm({
        title: 'Gerät wirklich ignorieren?',
        confirmLabel: 'Ignorieren',
        danger: true,
      });
      if (!confirmed) return;
      try {
        await requestJSON(`/api/v1/devices/${encodeURIComponent(deviceId)}/ignore`, this.mutationOptions());
        this.closeDeviceDetail();
        await this.refreshAfterMutation();
      } catch (error) {
        this.$store.toasts.push(error.message, 'critical');
      }
    },

    async unignoreDevice(deviceId) {
      if (!deviceId) return;
      try {
        await requestJSON(`/api/v1/devices/${encodeURIComponent(deviceId)}/unignore`, this.mutationOptions());
        await this.refreshAfterMutation();
      } catch (error) {
        this.$store.toasts.push(error.message, 'critical');
      }
    },

    async openDiscoveryDelete(deviceId) {
      if (!deviceId || !this.canDeleteDiscovery) return;
      this.deletePreviewDeviceId = deviceId;
      this.deletePreview = null;
      this.deletePreviewError = '';
      this.deletePreviewLoading = true;
      this.$nextTick(() => {
        const dialog = this.$refs.discoveryDeleteDialog;
        if (dialog && !dialog.open) dialog.showModal();
      });
      try {
        this.deletePreview = await requestJSON(`/api/v1/devices/${encodeURIComponent(deviceId)}/discovery-delete/preview`);
      } catch (error) {
        this.deletePreviewError = error.message;
      } finally {
        this.deletePreviewLoading = false;
      }
    },

    closeDiscoveryDelete() {
      if (this.$refs.discoveryDeleteDialog?.open) this.$refs.discoveryDeleteDialog.close();
      this.deletePreview = null;
      this.deletePreviewDeviceId = '';
      this.deletePreviewError = '';
    },

    async confirmDiscoveryDelete() {
      if (!this.deletePreviewDeviceId || !this.deletePreview || this.deleteLoading) return;
      const confirmed = await this.$store.modal.confirm({
        title: 'Discovery und alle aufgeführten retained Topics endgültig löschen?',
        confirmLabel: 'Endgültig löschen',
        danger: true,
      });
      if (!confirmed) return;
      this.deleteLoading = true;
      try {
        await requestJSON(`/api/v1/devices/${encodeURIComponent(this.deletePreviewDeviceId)}/discovery-delete`, this.mutationOptions({confirm: true}));
        this.closeDiscoveryDelete();
        await this.refreshAfterMutation();
      } catch (error) {
        this.deletePreviewError = error.message;
      } finally {
        this.deleteLoading = false;
      }
    },

    formatDetailTime(value) {
      if (!value) return '-';
      const date = new Date(value);
      return Number.isNaN(date.getTime()) ? '-' : date.toLocaleString();
    },

    // Kurzform "vor Xs/min/h" fuer die Kurzinfo an den Modal-Trigger. now
    // ist ein optionaler zweiter Parameter, damit der Test ohne echte Uhr
    // arbeitet - im Betrieb bleibt er weg und faellt auf new Date() zurueck.
    relativeTime(value, now = new Date()) {
      const date = value instanceof Date ? value : new Date(value);
      if (Number.isNaN(date.getTime())) return '';
      const seconds = Math.max(0, Math.round((now.getTime() - date.getTime()) / 1000));
      if (seconds < 60) return `vor ${seconds} s`;
      if (seconds < 3600) return `vor ${Math.round(seconds / 60)} min`;
      return `vor ${Math.round(seconds / 3600)} h`;
    },

    get sortedWarnings() {
      const severityRank = {critical: 3, warning: 2, info: 1};
      return [...(this.deviceDetail?.warnings || [])].sort((left, right) =>
        (severityRank[right.severity] || 0) - (severityRank[left.severity] || 0)
      );
    },

    get groupedWarnings() {
      const groups = new Map();
      for (const warning of this.sortedWarnings) {
        const key = `${warning.rule_id}\u0000${warning.message}\u0000${warning.hint}`;
        let group = groups.get(key);
        if (!group) {
          group = {...warning, affectedEntityIds: [], count: 0};
          groups.set(key, group);
        }
        group.count += 1;
        if (warning.entity_id && !group.affectedEntityIds.includes(warning.entity_id)) {
          group.affectedEntityIds.push(warning.entity_id);
        }
      }
      return [...groups.values()];
    },

    entityLabel(entityID) {
      const entity = (this.deviceDetail?.entities || []).find(item => item.unique_id === entityID);
      if (!entity) return entityID;
      return `${entity.name || entity.object_id} (${entityID})`;
    },

    // entityDisplayName strips a leading device-name prefix from the entity
    // name for display within a single device's detail modal, where the
    // device name is already shown in the dialog header and repeating it on
    // every entity just wastes space. Falls back to the full name whenever
    // it doesn't start with the device name, since not every MQTT bridge
    // prefixes entity names with the device name.
    entityDisplayName(entity, fallbackKey = 'object_id') {
      const name = (entity?.name || '').trim();
      const fallback = entity?.[fallbackKey] || entity?.unique_id || '';
      if (!name) return fallback;
      const deviceName = (this.deviceDetail?.name || '').trim();
      if (!deviceName || !name.toLowerCase().startsWith(deviceName.toLowerCase())) return name;
      const rest = name.slice(deviceName.length).trim();
      return rest || name;
    },

    get recentMessages() {
      return (this.deviceDetail?.entities || [])
        .filter(entity => entity.last_message)
        .map(entity => ({unique_id: entity.unique_id, ...entity.last_message}))
        .sort((left, right) => new Date(right.at) - new Date(left.at));
    },

    formatStateValue(value) {
      if (value === null || value === undefined) return '–';
      if (typeof value === 'object') return JSON.stringify(value);
      return String(value);
    },

    // Eine empfangene MQTT-Nachricht wird als Liste einzelner Zustaende
    // gezeigt (wie ein Set-Vorgang im Design, nur ohne "ok"): ein
    // JSON-Objekt-Payload zerfaellt in je eine Zeile pro Schluessel, ein
    // skalares Payload bleibt eine Zeile unter dem Entitaets- bzw.
    // Topic-Namen. Kein Payload -> ein "(leer)"-Hinweis, damit die Zeile
    // nicht verschwindet.
    messageStates(message) {
      if (!message) return [];
      const raw = message.payload;
      if (raw === undefined || raw === null || String(raw).trim() === '') {
        return [{label: message.topic || 'MQTT', value: '(leer)'}];
      }
      let parsed = null;
      try {
        parsed = JSON.parse(raw);
      } catch (error) {
        parsed = null;
      }
      if (parsed && typeof parsed === 'object' && !Array.isArray(parsed)) {
        const entries = Object.entries(parsed);
        if (entries.length) return entries.map(([key, value]) => ({label: key, value: this.formatStateValue(value)}));
      }
      const entity = (this.deviceDetail?.entities || []).find(item => item.unique_id === message.unique_id);
      const label = entity ? this.entityDisplayName(entity) : (message.topic || 'MQTT');
      return [{label, value: String(raw)}];
    },

    entityCategory(entity) {
      const category = String(entity.entity_category || '').toLowerCase();
      if (category === 'diagnostic') return 'diagnostics';
      if (category === 'config') return 'configuration';
      const component = String(entity.component || '').toLowerCase();
      if (entity.commandable || ['button', 'number', 'select', 'text'].includes(component)) return 'controls';
      const text = `${entity.name || ''} ${entity.object_id || ''} ${entity.device_class || ''}`.toLowerCase();
      if (entity.default_hidden || /diagnos|diagnostic|error|fault|rssi|signal|uptime|firmware|update|version|status/.test(text)) return 'diagnostics';
      if (/config|configuration|setting|einstellung|option|mode|modus/.test(text)) return 'configuration';
      return 'measurements';
    },

    get deviceAvailability() {
      const entities = this.deviceDetail?.entities || [];
      const withAvailability = entities.filter(entity => entity.has_availability);
      if (!withAvailability.length) return 'unknown';
      return withAvailability.some(entity => entity.available) ? 'online' : 'offline';
    },

    get measurementEntities() {
      return (this.deviceDetail?.entities || []).filter(entity => this.entityCategory(entity) === 'measurements');
    },

    get controlEntities() {
      return (this.deviceDetail?.entities || []).filter(entity => this.entityCategory(entity) === 'controls');
    },

    get configDiagEntities() {
      return (this.deviceDetail?.entities || []).filter(entity => ['configuration', 'diagnostics'].includes(this.entityCategory(entity)));
    },

    // Der "Konfiguration & Diagnose"-Tab teilt sich in zwei Abschnitte:
    // Konfigurations-Entitaeten sind haeufig schaltbar (number/select/switch
    // mit entity_category=config) und bekommen deshalb die Steuer-Kachel;
    // Diagnose-Entitaeten bleiben schreibgeschuetzte Wert-Kacheln.
    get configEntities() {
      return (this.deviceDetail?.entities || []).filter(entity => this.entityCategory(entity) === 'configuration');
    },
    get diagnosticEntities() {
      return (this.deviceDetail?.entities || []).filter(entity => this.entityCategory(entity) === 'diagnostics');
    },

    // Zusammenlegen von Messwerte + Steuerung ist nur sinnvoll, wenn beide
    // Seiten ueberhaupt etwas zu zeigen haben - sonst bliebe eine leere
    // Untersektion stehen und der "Steuerung"-Tab waere gar nicht da.
    get canMerge() {
      return this.measurementEntities.length > 0 && this.controlEntities.length > 0;
    },

    // "aktualisiert vor X" - rechtsbuendige, sekundengenaue Relativzeit
    // (nowTick tickt im Sekundentakt, siehe init()).
    get updatedRelative() {
      const at = this.deviceDetail?.last_updated;
      if (!at) return '';
      const relative = this.relativeTime(at, this.nowTick ? new Date(this.nowTick) : new Date());
      return relative ? `aktualisiert ${relative}` : '';
    },

    get measurementsTeaser() {
      const first = this.measurementEntities[0];
      if (!first || !first.has_value) return '';
      return `${first.value}${first.unit_of_measurement ? ` ${first.unit_of_measurement}` : ''}`;
    },

    get controlsTeaser() {
      const first = this.controlEntities[0];
      if (!first) return '';
      if (!first.has_availability) return 'Zustand unbekannt';
      return first.has_value ? first.value : (first.available ? 'Online' : 'Offline');
    },

    get configDiagTeaser() {
      if (!this.configDiagEntities.length) return '';
      const staleDiagnostic = this.configDiagEntities.some(entity => this.entityCategory(entity) === 'diagnostics' && entity.stale);
      return `${this.configDiagEntities.length}${staleDiagnostic ? ' · ⚠' : ''}`;
    },

    managementTeaser(now = new Date()) {
      const warningCount = this.groupedWarnings.length;
      const latest = [...(this.deviceDetail?.command_actions || []).map(a => a.at), ...this.recentMessages.map(m => m.at)]
        .filter(Boolean)
        .sort((left, right) => new Date(right) - new Date(left))[0];
      const parts = [];
      if (warningCount) parts.push(`⚠ ${warningCount}`);
      if (latest) parts.push(this.relativeTime(latest, now));
      return parts.join(' · ');
    },

    setDeviceTab(key) {
      this.activeDeviceTab = key;
      // Merge/Split nur anfassen, wenn der Nutzer gerade Messwerte/Steuerung
      // waehlt - so springt der Tab-Satz bei allen anderen Tabs nicht mehr um.
      if (['measurements', 'controls', 'measurementsControls'].includes(key)) {
        this.applyMergeThreshold(this.availableHeight);
      }
      if (this.$nextTick) this.$nextTick(() => this.positionUnderline());
    },

    // Der Auswahlstrich wird ueber gemessene Rechtecke positioniert (nicht
    // ueber offsetLeft): das absolute Element haengt sonst an seiner
    // Flex-Static-Position und sitzt versetzt statt mittig unter dem Tab.
    positionUnderline() {
      if (!this.$refs) return;
      const underline = this.$refs.deviceModalUnderline;
      const switcher = this.$refs.deviceModalSwitcher;
      if (!underline || !switcher || typeof switcher.getBoundingClientRect !== 'function') return;
      const active = switcher.querySelector('.device-modal-trigger.active');
      if (!active || getComputedStyle(underline).display === 'none') return;
      const base = switcher.getBoundingClientRect();
      const rect = active.getBoundingClientRect();
      underline.style.width = `${rect.width}px`;
      underline.style.transform = `translateX(${rect.left - base.left}px)`;
      underline.style.top = `${rect.bottom - base.top}px`;
    },

    // Beim Oeffnen eines Geraets ist immer derselbe Tab dran, unabhaengig
    // vom Ausloeser (Kachel oder Kompakt-Karte): der erste Entitaets-Tab,
    // der etwas zu zeigen hat. "Verwaltung & Analyse" existiert immer und
    // ist deshalb der Boden dieser Kette.
    defaultDeviceTab() {
      if (this.measurementEntities.length) return this.mergedTab ? 'measurementsControls' : 'measurements';
      if (this.controlEntities.length) return this.mergedTab ? 'measurementsControls' : 'controls';
      if (this.configDiagEntities.length) return 'configDiag';
      return 'management';
    },

    applyMergeThreshold(height) {
      this.availableHeight = height;
      // Nur umschalten, solange Messwerte/Steuerung (oder ihre Fusion) aktiv
      // sind. Sitzt der Nutzer z. B. auf "Verwaltung & Analyse" und dort
      // entsteht durch Inhalt eine Scrollbar, bleibt der Merge-Zustand
      // einfach stehen - kein Herumspringen des Tab-Satzes.
      if (!['measurements', 'controls', 'measurementsControls'].includes(this.activeDeviceTab)) return;
      const shouldMerge = height >= this.mergeHeightThreshold && this.canMerge;
      if (shouldMerge === this.mergedTab) return;
      this.mergedTab = shouldMerge;
      if (shouldMerge) {
        this.activeDeviceTab = 'measurementsControls';
      } else {
        this.activeDeviceTab = this.measurementEntities.length ? 'measurements' : 'controls';
      }
      if (this.$nextTick) this.$nextTick(() => this.positionUnderline());
    },

    observeMergeThreshold() {
      if (typeof ResizeObserver === 'undefined') return;
      const scrollarea = this.$refs.deviceModalScrollarea;
      const dialog = this.$refs.deviceModalDialog;
      if (!scrollarea && !dialog) return;
      this.mergeObserver = new ResizeObserver(entries => {
        for (const entry of entries) {
          if (entry.target === dialog) {
            this.isAccordion = entry.contentRect.width > 0 && entry.contentRect.width < 860;
          } else {
            this.applyMergeThreshold(entry.contentRect.height);
          }
        }
        this.positionUnderline();
      });
      if (scrollarea) this.mergeObserver.observe(scrollarea);
      if (dialog) this.mergeObserver.observe(dialog);
    },

    // Schaltet die Dichte nur fuer die laufende Sitzung um - kein PUT auf
    // /api/v1/settings. Der gespeicherte Standard kommt allein aus
    // Einstellungen -> Darstellung; nach einem Reload seedet init() den
    // viewMode wieder aus data-device-view-mode.
    async setViewMode(mode) {
      if (!['control', 'compact'].includes(mode) || mode === this.viewMode || this.viewModeSaving) return;
      const previousMode = this.viewMode;
      this.viewMode = mode;
      this.viewModeSaving = true;
      this.viewModeMessage = '';
      try {
        await this.refreshLiveFragment(mode);
      } catch (error) {
        this.viewMode = previousMode;
        this.viewModeMessage = error.message;
      } finally {
        this.viewModeSaving = false;
      }
    },

    // Ein Fragment-Tausch nach dem anderen. Zwei schnelle Klicks auf zwei
    // Seiten-Tabs schickten sonst zwei GETs los, und wessen Antwort zuletzt
    // eintraf, entschied - nicht wer zuletzt geklickt hat. Seit die Aufrufe in
    // einer Kette haengen, liest jeder erst beim Start seine Seite aus
    // dashboardShell.activePage und der letzte Klick gewinnt immer.
    refreshLiveFragment(mode = this.viewMode) {
      // Laeuft nichts, startet der Tausch sofort - bis zum htmx-Aufruf synchron,
      // wie vor der Kette. Nur wer auf einen laufenden trifft, wartet.
      const next = this._liveQueue
        ? this._liveQueue.then(() => this.runLiveFragment(mode), () => this.runLiveFragment(mode))
        : this.runLiveFragment(mode);
      this._liveQueue = next;
      next.catch(() => {}).then(() => { if (this._liveQueue === next) this._liveQueue = null; });
      return next;
    },

    async runLiveFragment(mode = this.viewMode) {
      if (document.visibilityState !== 'visible') return;
      const queryMode = ['control', 'compact'].includes(mode) ? `&view_mode=${encodeURIComponent(mode)}` : '';
      const liveId = `${this.$root.id.replace(/-panel$/, '')}-live`;
      if (!window.htmx || !document.getElementById(liveId)) return;
      // Die Uebersicht rendert genau eine Layout-Seite (activePage in
      // webui.go). Ohne page= liefe jede Auffrischung auf die erste Seite
      // zurueck - und ein Klick auf einen Seiten-Tab zeigte weiter dieselbe.
      const activePage = liveId === 'overview-live' ? (window.__dashboardShell__?.activePage || '') : '';
      const queryPage = activePage ? `&page=${encodeURIComponent(activePage)}` : '';
      // Die Karten in #overview-live bekommen ihre Hoehe erst per JavaScript:
      // direkt nach dem outerHTML-Tausch sind sie zusammen rund 880px flacher
      // als fertig (gemessen faellt die Dokumenthoehe 4344px -> 3461px). Der
      // Browser klemmt scrollTop dann auf das kleinere Seitenmaximum und
      // behaelt den geklemmten Wert, wenn der Inhalt zurueckkommt - die
      // Scrollposition ist weg.
      //
      // $root bleibt beim Tausch stehen (ersetzt wird nur sein Kind
      // #overview-live). Haelt es seine Hoehe, faellt die Dokumenthoehe nie
      // und es gibt nichts zu klemmen. Bis 2026-08 stand hier stattdessen
      // eine Reparatur per window.scrollTo *nach* dem Tausch; die liess den
      // Sprung sichtbar, verwarf jedes Scrollen waehrend der Anfrage und
      // brach auf dem Handy den Touch-Schwung ab.
      const anchor = this.$root;
      anchor.style.minHeight = `${anchor.getBoundingClientRect().height}px`;
      try {
        await window.htmx.ajax('GET', withBase(`/?fragment=${liveId}${queryMode}${queryPage}`), {target: `#${liveId}`, swap: 'outerHTML'});
      } finally {
        // Nachgemessen: wenn htmx.ajax aufloest, stehen die Karten wieder auf
        // voller Hoehe (4344px bei der Freigabe, unveraendert danach). Die
        // Stuetze darf also sofort weg - kein requestAnimationFrame noetig.
        anchor.style.minHeight = '';
      }
    },

    async selectDevice(deviceId) {
      if (!deviceId) return;
      const requestToken = ++this.detailRequestToken;
      this.selectedDeviceId = deviceId;
      this.deviceDetail = null;
      this.detailError = '';
      this.detailLoading = true;
      // Jedes Geraet startet getrennt; der ResizeObserver legt gleich wieder
      // zusammen, falls die Hoehe reicht.
      this.mergedTab = false;
      this.$nextTick(() => {
        const dialog = this.$refs.deviceModalDialog;
        if (dialog && !dialog.open) dialog.showModal();
        if (dialog && typeof dialog.clientWidth === 'number' && dialog.clientWidth > 0) {
          this.isAccordion = dialog.clientWidth < 860;
        }
      });
      try {
        await this.loadDeviceDetail(deviceId, requestToken);
        // Erstauswahl des Tabs nur beim Oeffnen (nicht bei jedem Live-Refresh).
        if (requestToken === this.detailRequestToken && this.selectedDeviceId === deviceId) {
          this.activeDeviceTab = this.defaultDeviceTab();
        }
      } catch (error) {
        if (requestToken !== this.detailRequestToken || this.selectedDeviceId !== deviceId) return;
        this.detailError = error.message;
      } finally {
        if (requestToken === this.detailRequestToken) this.detailLoading = false;
      }
    },

    // Laedt bzw. frischt die Detaildaten auf. Der aktive Tab wird hier
    // bewusst NICHT gesetzt - sonst wuerde jede Live-Wert-Aktualisierung
    // (SSE registry-updated -> refreshSelectedDevice) den Nutzer zurueck auf
    // "Messwerte" werfen. Die Erstauswahl passiert einmalig in selectDevice.
    async loadDeviceDetail(deviceId, requestToken = this.detailRequestToken) {
      const detail = await requestJSON(`/api/v1/devices/${encodeURIComponent(deviceId)}`);
      if (requestToken !== this.detailRequestToken || this.selectedDeviceId !== deviceId) return;
      this.deviceDetail = detail;
      if (this.$nextTick) this.$nextTick(() => this.positionUnderline());
    },

    async refreshSelectedDevice() {
      const deviceId = this.selectedDeviceId;
      if (!deviceId || document.visibilityState !== 'visible') return;
      const requestToken = ++this.detailRequestToken;
      try {
        await this.loadDeviceDetail(deviceId, requestToken);
        this.detailError = '';
      } catch (error) {
        if (requestToken !== this.detailRequestToken || this.selectedDeviceId !== deviceId) return;
        this.detailError = error.message;
      } finally {
        if (this.selectedDeviceId === deviceId) this.detailLoading = false;
      }
    },

    closeDeviceDetail() {
      this.detailRequestToken += 1;
      if (this.$refs.deviceModalDialog?.open) this.$refs.deviceModalDialog.close();
      this.selectedDeviceId = '';
      this.deviceDetail = null;
      this.detailError = '';
      if (this.livePatchStale) {
        this.livePatchStale = false;
        this.refreshLiveFragment();
      }
    },

    async reloadDevice() {
      if (!this.selectedDeviceId || this.reloadLoading) return;
      this.reloadLoading = true;
      this.detailError = '';
      try {
        const result = await requestJSON(`/api/v1/devices/${encodeURIComponent(this.selectedDeviceId)}/reload`, {method: 'POST'});
        await this.selectDevice(this.selectedDeviceId);
        this.$store.toasts.push(`Discovery-Registry für ${result.scope === 'all_devices' ? 'alle Geräte' : 'das Gerät'} neu geladen.`);
        await this.refreshLiveFragment();
      } catch (error) {
        this.detailError = error.message;
      } finally {
        this.reloadLoading = false;
      }
    },

    // Die sieben Kartentypen, deren Zahlen aus dem Energie-Schnappschuss
    // Welcher Zweig des SSE-Ereignisses welchen Kartentyp bedient. Ein
    // Kartentyp, der hier nicht steht, holt seine Zahlen weiter aus
    // serverseitig gerendertem HTML - fuer ihn bleibt der Tausch noetig.
    // Stand 2026-08 ist das nur noch "entity". "device" ist seit dem
    // Struktur-Fingerabdruck dabei - der JS-Renderer und ?view=index (B1a),
    // die Schritt 3 der Spec dafuer vorgesehen hatte, sind damit hinfaellig.
    //
    // Es reicht nicht, dass alle Karten push-faehig sind: der Push muss auch
    // die Zweige mitbringen. Faellt einer aus (Marshalling-Fehler im
    // Server, aeltere Fassung), ist der Tausch das Richtige - lieber ein
    // teurer Neuaufbau als eine Karte, die stumm alte Zahlen zeigt.
    liveGridPushCovers(detail) {
      const branchForKind = {
        energy_flow: 'energy', energy_band: 'energy', energy_ring: 'energy',
        energy_board: 'energy', energy_day: 'energy', energy_schema: 'energy',
        energy_status: 'energy',
        battery_status: 'energy',
        entity_value: 'entities', entity_group: 'entities',
        diagnostics: 'diagnostics',
        device: 'entities',
      };
      const items = [...this.$root.querySelectorAll('[data-layout-item-kind]')];
      // Leeres Raster heisst hier "nicht die Uebersicht" (das Geraete-Panel
      // benutzt dieselbe Komponente) oder "noch nichts geladen". Beide Male
      // ist der Tausch das Richtige.
      if (items.length === 0) return false;
      const covered = items.every(item => {
        const branch = branchForKind[item.dataset.layoutItemKind];
        return branch !== undefined && detail?.[branch] !== undefined && detail[branch] !== null;
      });
      if (!covered) return false;
      // Eine Geraetekachel baut ihre Entitaetenliste aus der Registry, nicht
      // aus dem Layout. Fuer sie reicht "der Zweig ist da" nicht - eine neu
      // entdeckte Entitaet bekaeme sonst nie ihre Zeile.
      //
      // Die kompakte Kachel braucht ein zweites Ja: welche bis zu drei
      // Zeilen sie zeigt, waehlt priorityEntities() anhand der Werte aus
      // (webui.CompactStructureFingerprint) - eine Auswahl, durch deren
      // Raster der geteilte Fingerabdruck faellt. Dieselbe Pruefung wie
      // compactCardsPushCovers() im Geraete-Tab, nur auf #overview-live.
      const live = this.$root.querySelector('#overview-live');
      const compactCells = items.filter(item =>
        item.dataset.layoutItemKind === 'device' && item.dataset.display === 'compact');
      if (compactCells.length) {
        // Discovery-Wechsel tauscht immer - dieselbe Untergrenze wie fuer die
        // Detailkachel.
        if (!this.structureUnchanged(detail, live)) return false;
        // Eine kompakte Kachel *ohne* feste Zeilenauswahl haengt an
        // priorityEntities: dafuer bleibt der wertabhaengige
        // CompactStructureFingerprint der Waechter (structure_compact).
        if (compactCells.some(cell => cell.dataset.compactConfigured !== 'true')) {
          return Boolean(detail.structure_compact)
            && detail.structure_compact === live?.dataset?.structureCompact;
        }
        // Sind alle kompakten Kacheln konfiguriert, stehen ihre Zeilen fest.
        // Dann reicht "Discovery unveraendert" plus "Geraete-Ampel
        // unveraendert" (structure_availability); ein reiner Messwert wird von
        // compact-card-values.js in place nachgezogen, ohne Fragment-Tausch.
        return Boolean(detail.structure_availability)
          && detail.structure_availability === live?.dataset?.structureAvailability;
      }
      if (items.some(item => item.dataset.layoutItemKind === 'device')) {
        return this.structureUnchanged(detail, live);
      }
      return true;
    },

    // Der Fingerabdruck aus registry.StructureFingerprint deckt Geraete-ID,
    // Entity-ID, Name, Component, device_class, Einheit und Commandable ab -
    // also alles, woraus die Templates Zeilen, Icons und Regler machen.
    // Steht er, hat sich nur ein Messwert bewegt und das Nachziehen genuegt.
    // Ein leerer Wert ist kein Treffer: ein aelterer Server oder ein
    // Marshalling-Fehler soll den Tausch ausloesen, nicht unterdruecken.
    structureUnchanged(detail, live) {
      return Boolean(detail?.structure) && detail.structure === live?.dataset?.structure;
    },

    // Das Gate der Steuerungs-Ansicht des Geraete-Tabs.
    deviceTilesPushCovers(detail) {
      if (this.viewMode !== 'control') return false;
      if (!detail?.entities) return false;
      return this.structureUnchanged(detail, this.$root.querySelector('#devices-live'));
    },

    // Das Gate der Kompakt-Ansicht. Sie ist nachziehbar, solange sowohl der
    // geteilte Fingerabdruck steht (Discovery unveraendert) als auch der
    // Kompakt-Fingerabdruck (webui.CompactStructureFingerprint): letzterer
    // deckt die wertabhaengige Zeilenauswahl je Karte und die Geraete-Ampel
    // ab - beides faellt durch das Raster des geteilten Fingerabdrucks.
    compactCardsPushCovers(detail) {
      if (this.viewMode !== 'compact') return false;
      if (!detail?.entities) return false;
      const live = this.$root.querySelector('#devices-live');
      return this.structureUnchanged(detail, live)
        && Boolean(detail.structure_compact)
        && detail.structure_compact === live?.dataset?.structureCompact;
    },

    // Beide Panels teilen sich diese Komponente; welches Gate gilt, sagt das
    // Fragment im DOM - im Geraete-Tab zusaetzlich die aktive Ansicht.
    livePushCovers(detail) {
      if (this.$root.querySelector('#devices-live')) {
        return this.viewMode === 'control'
          ? this.deviceTilesPushCovers(detail)
          : this.compactCardsPushCovers(detail);
      }
      return this.liveGridPushCovers(detail);
    },

    init() {
      // registryUpdated ist bereits das Feld, an dem destroy() den Zuhoerer
      // wieder abmeldet - also der vorhandene Marker fuer "schon
      // eingerichtet". Ohne den Waechter kann derselbe Zuhoerer zweimal am
      // window haengen und jedes registry-updated zwei Fragmente ausloesen.
      if (this.registryUpdated) return;
      this.viewMode = this.$root.dataset.deviceViewMode === 'control' ? 'control' : 'compact';
      this.loadSession();
      this.settingChanged = event => this.setTooltipsEnabled(event.detail.enabled);
      this.configTileSettingChanged = () => this.refreshLiveFragment();
      this.deviceViewModeChanged = event => {
        const mode = event.detail?.mode === 'control' ? 'control' : 'compact';
        if (mode === this.viewMode) return;
        this.viewMode = mode;
        this.refreshLiveFragment(mode);
      };
      this.registryUpdated = event => {
        if (!this.$root.classList.contains('active')) return;
        const detail = event?.detail;
        // Das SSE-Ereignis traegt drei Zweige mit (eventCache in
        // httpapi.go): den Energie-Schnappschuss fuer die Federn der sieben
        // Energiegrafiken, die Entitaetswerte und die
        // Diagnose-Zusammenfassung. Alle drei entstehen aus einem einzigen
        // reg.Snapshot() je Registry-Aenderung - statt einem Aggregat-Lauf
        // je offenem Tab.
        if (detail?.energy) window.EnergyPresentation?.publish(detail.energy);
        // Verdeckt ein offenes Modal das Raster, faellt das Nachziehen der
        // (unsichtbaren) Kacheln und ein etwaiger Fragment-Tausch weg -
        // refreshSelectedDevice() haelt das Modal selbst aktuell. Beim
        // Schliessen holt closeDeviceDetail() das Raster in einem Zug nach.
        if (this.selectedDeviceId) {
          this.livePatchStale = true;
          this.refreshSelectedDevice();
          return;
        }
        if (detail?.entities) {
          const delta = Boolean(detail.entities_delta);
          // Zwei Nachzieh-Module, eine Abbildung von Statustexten: die
          // Uebersichtskarten und die Geraetekacheln koennen im selben
          // Raster stehen, ihre Rueckgaben ueberschneiden sich nicht.
          const messages = {
            ...(window.overviewValues?.applyEntityValues(this.$root, detail.entities, delta) || {}),
            ...(window.deviceTileValues?.applyDeviceTiles(this.$root, detail.entities, delta) || {}),
          };
          for (const [id, message] of Object.entries(messages)) this.commandStates[id] = message;
          // Die Kompakt-Karten des Geraete-Tabs: read-only, also keine
          // Statustexte - der Aufruf steht fuer sich, nicht im messages-Spread.
          window.compactCardValues?.applyCompactCards(this.$root, detail.entities, delta);
        }
        if (detail?.diagnostics) window.overviewValues?.applyDiagnostics(this.$root, detail.diagnostics);
        // Der outerHTML-Tausch kostet gemessen 62 % der CPU-Zeit dieses
        // Tabs. Er faellt weg, sobald der Push jede Karte im Raster bedient
        // und die Struktur seit dem Rendern steht.
        if (!this.livePushCovers(detail)) this.refreshLiveFragment();
        this.refreshSelectedDevice();
      };
      this.contentChanged = event => {
        if (event.target.id === 'devices-live') this.initTooltips();
      };
      this.dialogClosed = event => {
        if (event.target === this.$refs.deviceModalDialog && this.selectedDeviceId) this.closeDeviceDetail();
      };
      this.layoutSaved = () => this.refreshLiveFragment();
      this.pageChanged = () => this.refreshLiveFragment();
      document.addEventListener('discovery-tooltips-setting-changed', this.settingChanged);
      document.addEventListener('config-tile-setting-changed', this.configTileSettingChanged);
      document.addEventListener('device-view-mode-changed', this.deviceViewModeChanged);
      window.addEventListener('registry-updated', this.registryUpdated);
      window.addEventListener('layout-saved', this.layoutSaved);
      window.addEventListener('layout-page-changed', this.pageChanged);
      document.addEventListener('htmx:afterSwap', this.contentChanged);
      this.$refs.deviceModalDialog?.addEventListener('close', this.dialogClosed);
      this.initTooltips();
      this.observeMergeThreshold();
      // Sekundentakt fuer die relative "aktualisiert vor X"-Anzeige im
      // Modal. Laeuft nur weiter, solange ein Geraet offen ist.
      this.clockTimer = window.setInterval(() => {
        if (this.deviceDetail && document.visibilityState === 'visible') this.nowTick = Date.now();
      }, 1000);
    },

    initTooltips() {
      if (typeof tippy !== 'function' || this.$root.dataset.discoveryTooltipsEnabled !== 'true') return;
      this.destroyTooltips();
      this.tooltipInstances = [...this.$root.querySelectorAll('[data-discovery-tooltip-target]')]
        .map(trigger => {
          const template = document.getElementById(trigger.dataset.discoveryTooltipTarget);
          if (!template) return null;
          return tippy(trigger, {
            allowHTML: true,
            content: template.innerHTML,
            interactive: true,
            // 760 ist fast die doppelte Breite eines Handy-Bildschirms.
            // Am Desktop gewinnt weiterhin 760.
            maxWidth: Math.min(760, window.innerWidth - 32),
            placement: 'top-start',
          });
        })
        .filter(Boolean);
    },

    setTooltipsEnabled(enabled) {
      this.$root.dataset.discoveryTooltipsEnabled = String(enabled);
      this.destroyTooltips();
      if (enabled) this.initTooltips();
    },

    destroyTooltips() {
      this.tooltipInstances.forEach(instance => instance.destroy());
      this.tooltipInstances = [];
    },

    destroy() {
      if (this.settingChanged) document.removeEventListener('discovery-tooltips-setting-changed', this.settingChanged);
      if (this.configTileSettingChanged) document.removeEventListener('config-tile-setting-changed', this.configTileSettingChanged);
      if (this.deviceViewModeChanged) document.removeEventListener('device-view-mode-changed', this.deviceViewModeChanged);
      if (this.registryUpdated) window.removeEventListener('registry-updated', this.registryUpdated);
      if (this.layoutSaved) window.removeEventListener('layout-saved', this.layoutSaved);
      if (this.pageChanged) window.removeEventListener('layout-page-changed', this.pageChanged);
      if (this.contentChanged) document.removeEventListener('htmx:afterSwap', this.contentChanged);
      if (this.dialogClosed) this.$refs.deviceModalDialog?.removeEventListener('close', this.dialogClosed);
      this.destroyTooltips();
      this.mergeObserver?.disconnect();
    },
  });

  const diagnosticsPanel = () => ({
    warnings: [],
    ruleCatalog: [],
    healthScores: [],
    // deviceCount/entityCount statt der ganzen Geraeteliste: das Panel hat
    // aus den 448 KB von /api/v1/discovery ohnehin nur diese beiden Zahlen
    // gelesen. Die Vollansicht bleibt unter /api/v1/discovery bestehen.
    discovery: {deviceCount: 0, entityCount: 0, discovery_errors: [], duplicate_ids: {}},
    severity: 'all',
    rule: 'all',
    device: 'all',
    sortBy: 'severity',
    sortDirection: 'desc',
    loading: false,
    discoveryError: '',

    async load() {
      this.loading = true;
      this.discoveryError = '';
      try {
        const [warnings, rules, healthScores, discovery] = await Promise.allSettled([
          requestJSON('/api/v1/diagnostics'),
          requestJSON('/api/v1/diagnostics/rules'),
          requestJSON('/api/v1/diagnostics/health'),
          requestJSON('/api/v1/discovery/summary'),
        ]);
        if (warnings.status === 'rejected') throw warnings.reason;
        this.warnings = Array.isArray(warnings.value) ? warnings.value : [];
        this.ruleCatalog = rules.status === 'fulfilled' && Array.isArray(rules.value) ? rules.value : [];
        this.healthScores = healthScores.status === 'fulfilled' && Array.isArray(healthScores.value) ? healthScores.value : [];
        if (discovery.status === 'fulfilled') {
          this.discovery = {
            deviceCount: Number(discovery.value?.device_count) || 0,
            entityCount: Number(discovery.value?.entity_count) || 0,
            discovery_errors: Array.isArray(discovery.value?.discovery_errors) ? discovery.value.discovery_errors : [],
            duplicate_ids: discovery.value?.duplicate_ids && typeof discovery.value.duplicate_ids === 'object' ? discovery.value.duplicate_ids : {},
          };
        } else {
          this.discoveryError = discovery.reason.message;
          this.discovery = {deviceCount: 0, entityCount: 0, discovery_errors: [], duplicate_ids: {}};
        }
      } catch (error) {
        this.$store.toasts.push(error.message, 'critical');
        this.warnings = [];
        this.ruleCatalog = [];
        this.healthScores = [];
        this.discovery = {deviceCount: 0, entityCount: 0, discovery_errors: [], duplicate_ids: {}};
      } finally {
        this.loading = false;
      }
    },

    get severities() {
      return [...new Set(this.warnings.map(item => item.severity).filter(Boolean))].sort();
    },

    get rules() {
      return [...new Set([
        ...this.ruleCatalog,
        ...this.warnings.map(item => item.rule_id).filter(Boolean),
      ])].sort();
    },

    get devices() {
      return [...new Set([
        ...this.healthScores.map(item => item.device_id).filter(Boolean),
        ...this.warnings.map(item => item.device_id).filter(Boolean),
      ])].sort();
    },

    get duplicateIDs() {
      return Object.entries(this.discovery.duplicate_ids || {}).sort(([left], [right]) => left.localeCompare(right));
    },

    get filteredHealthScores() {
      if (this.device === 'all') return this.healthScores;
      return this.healthScores.filter(item => item.device_id === this.device);
    },

    healthStatusLabel(status) {
      return {
        healthy: 'Gesund',
        degraded: 'Beeinträchtigt',
        unhealthy: 'Ungesund',
        critical: 'Kritisch',
        unknown: 'Unbekannt',
      }[status] || status || 'Unbekannt';
    },

    formatHealthTime(value) {
      if (!value) return '-';
      const date = new Date(value);
      if (Number.isNaN(date.getTime()) || date.getUTCFullYear() <= 1) return '-';
      return date.toLocaleString('de-DE');
    },

    get filteredWarnings() {
      const severityRank = {critical: 3, warning: 2, info: 1};
      const result = this.warnings.filter(item =>
        (this.severity === 'all' || item.severity === this.severity) &&
        (this.rule === 'all' || item.rule_id === this.rule) &&
        (this.device === 'all' || item.device_id === this.device)
      );
      result.sort((left, right) => {
        let comparison;
        if (this.sortBy === 'severity') {
          comparison = (severityRank[left.severity] || 0) - (severityRank[right.severity] || 0);
        } else {
          comparison = String(left[this.sortBy] || '').localeCompare(String(right[this.sortBy] || ''));
        }
        return this.sortDirection === 'asc' ? comparison : -comparison;
      });
      return result;
    },

    sort(field) {
      if (this.sortBy === field) {
        this.sortDirection = this.sortDirection === 'asc' ? 'desc' : 'asc';
        return;
      }
      this.sortBy = field;
      this.sortDirection = field === 'severity' ? 'desc' : 'asc';
    },

    sortLabel(field) {
      if (this.sortBy !== field) return '';
      return this.sortDirection === 'asc' ? ' aufsteigend' : ' absteigend';
    },
  });

  const prefersReducedMotion = () =>
    typeof window.matchMedia === 'function' && window.matchMedia('(prefers-reduced-motion: reduce)').matches;

  // x-roll: laesst eine per x-text gesetzte Zahl wie ein Zaehler zum neuen
  // Wert laufen, statt hart umzuspringen (apple-design: sanfter Uebergang
  // statt Teleport). Reiner Text-Tween ueber requestAnimationFrame - es
  // gibt keinen CSS-Weg, angezeigten Text zu interpolieren. Waehrend des
  // eigenen Schreibens wird der MutationObserver getrennt, damit er nicht
  // rekursiv ausloest. Interruptierbar: ein neuer Zielwert startet von der
  // zuletzt *gezeigten* Zahl, nicht vom alten Ziel. Nicht-numerische Werte
  // ("Ja", "-") werden sofort gesetzt.
  const registerRollDirective = Alpine => {
    Alpine.directive('roll', (el, meta, { cleanup }) => {
      const observeOptions = {childList: true, characterData: true, subtree: true};
      const parse = value => {
        const raw = String(value).trim().replace(',', '.');
        if (!/^-?\d*\.?\d+$/.test(raw)) return null;
        const number = Number(raw);
        return Number.isFinite(number) ? number : null;
      };
      let committed = parse(el.textContent);
      let displayed = committed;
      let frame = 0;
      const observer = new MutationObserver(() => {
        const text = el.textContent;
        const target = parse(text);
        if (prefersReducedMotion() || committed === null || target === null || target === committed) {
          committed = target;
          displayed = target;
          return;
        }
        const from = displayed === null ? committed : displayed;
        const decimals = (text.split('.')[1] || '').length;
        const startedAt = performance.now();
        const duration = 300;
        cancelAnimationFrame(frame);
        const write = value => {
          observer.disconnect();
          el.textContent = value;
          observer.observe(el, observeOptions);
        };
        const step = now => {
          const progress = Math.min(1, (now - startedAt) / duration);
          const eased = 1 - Math.pow(1 - progress, 3);
          if (progress < 1) {
            const value = from + (target - from) * eased;
            displayed = value;
            write(decimals ? value.toFixed(decimals) : String(Math.round(value)));
            frame = requestAnimationFrame(step);
          } else {
            write(text);
            committed = target;
            displayed = target;
          }
        };
        frame = requestAnimationFrame(step);
      });
      observer.observe(el, observeOptions);
      cleanup(() => {
        cancelAnimationFrame(frame);
        observer.disconnect();
      });
    });
  };

  // x-flip: laesst die Eintraege einer kurzen Liste an ihre neue Position
  // gleiten (FLIP), statt hart umzuspringen, wenn oben ein Eintrag
  // dazukommt. Neue Eintraege blenden von oben ein. Nur Transform/Opacity,
  // <=240ms ease-out, bei prefers-reduced-motion komplett aus.
  const registerFlipDirective = Alpine => {
    Alpine.directive('flip', (el, {expression}, { cleanup }) => {
      if (prefersReducedMotion()) return;
      const selector = expression || '.device-modal-log-entry';
      const easing = 'cubic-bezier(0.23, 1, 0.32, 1)';
      const rects = new Map();
      const items = () => [...el.querySelectorAll(selector)];
      const snapshot = () => {
        rects.clear();
        for (const item of items()) rects.set(item, item.getBoundingClientRect());
      };
      // Die Erstbefuellung durch x-for wird still aufgenommen; erst spaetere
      // Aenderungen (neuer Eintrag oben) werden animiert.
      let ready = false;
      requestAnimationFrame(() => { snapshot(); ready = true; });
      const observer = new MutationObserver(() => {
        if (!ready || el.offsetParent === null) {
          snapshot();
          return;
        }
        for (const item of items()) {
          const previous = rects.get(item);
          const next = item.getBoundingClientRect();
          if (!previous) {
            item.animate(
              [{opacity: 0, transform: 'translateY(-10px)'}, {opacity: 1, transform: 'none'}],
              {duration: 240, easing},
            );
          } else if (previous.top !== next.top) {
            item.animate(
              [{transform: `translateY(${previous.top - next.top}px)`}, {transform: 'none'}],
              {duration: 240, easing},
            );
          }
        }
        snapshot();
      });
      observer.observe(el, {childList: true, subtree: true});
      cleanup(() => observer.disconnect());
    });
  };

  document.addEventListener('alpine:init', () => {
    Alpine.data('dashboardShell', dashboardShell);
    Alpine.data('runtimeStatusPanel', runtimeStatusPanel);
    Alpine.data('diagnosticsPanel', diagnosticsPanel);
    Alpine.data('devicesPanel', devicesPanel);
    registerRollDirective(Alpine);
    registerFlipDirective(Alpine);
  });
})();
