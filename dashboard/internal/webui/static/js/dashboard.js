(() => {
  // Prefixes an absolute in-app path with the reverse-proxy base path that
  // base.html publishes before any deferred script runs. Empty (and thus a
  // no-op) on direct access, so every generated URL stays byte-identical.
  const withBase = path => `${window.__DASHBOARD_BASE_PATH__ || ''}${path}`;
  // Falls back to the key when i18n.js is absent (unit tests load this file
  // alone), so a missing runtime shows a visible gap instead of throwing.
  const t = (key, params) => (window.I18n ? window.I18n.t(key, params) : key);
  const tn = (key, n, params) => (window.I18n ? window.I18n.tn(key, n, params) : key);
  const apiError = (body, fallbackKey) => (window.I18n ? window.I18n.error(body, fallbackKey) : (body && body.message) || fallbackKey || 'common.request_failed');

  // Welche SSE-Themen ein Kartentyp braucht. Die Namen sind zugleich die
  // Zweige im Rumpf (siehe events.go). Ein Typ ohne Eintrag braucht keins:
  // die Verlaufskachel liest den Browser-Speicher selbst.
  const LIVE_TOPICS_BY_KIND = {
    energy_flow: ['energy'], energy_band: ['energy'], energy_ring: ['energy'],
    energy_board: ['energy'], energy_day: ['energy'], energy_schema: ['energy'],
    energy_status: ['energy'], battery_status: ['energy'],
    entity_value: ['entities'], entity_group: ['entities'],
    diagnostics: ['diagnostics'],
    energy_stats: ['energy_stats'],
    device: ['devices', 'entities'],
  };

  // Der Bedarf eines Panels als sortierter, mit Komma verbundener String.
  // Ein verdecktes Panel abonniert nichts, darum zaehlt nur das aktive. Ohne
  // Themen kommen weiter Versionswechsel, an denen Automationen, Device Map
  // und Energie-Anlage ihren Takt haben.
  const liveTopicsFor = (panel, panelId) => {
    const topics = new Set();
    if (panel && panelId === 'overview-panel') {
      for (const item of panel.querySelectorAll('[data-layout-item-kind]')) {
        for (const topic of LIVE_TOPICS_BY_KIND[item.dataset.layoutItemKind] || []) topics.add(topic);
      }
    } else if (panel && panelId === 'devices-panel' && panel.querySelector('#devices-live')) {
      topics.add('entities');
      topics.add('devices');
    } else if (panelId === 'stats-panel') {
      topics.add('energy_stats');
    }
    return [...topics].sort().join(',');
  };

  // Welche Geraete das aktive Panel zeigt, fuer ?devices=. :auto heisst: die
  // Kachel waehlt ihre Zeilen automatisch und braucht rows. Steht ein Geraet
  // mehrfach im Raster, gewinnt :auto. Der Geraete-Tab zeigt alle (*).
  const liveDevicesFor = (panel, panelId) => {
    if (!panel) return '';
    if (panelId === 'devices-panel') {
      const live = panel.querySelector('#devices-live');
      if (!live) return '';
      return live.dataset.deviceViewMode === 'control' ? '*' : '*:auto';
    }
    if (panelId !== 'overview-panel') return '';
    const auto = new Map();
    for (const item of panel.querySelectorAll('[data-layout-item-kind="device"]')) {
      const id = item.dataset.layoutItemRef;
      if (!id) continue;
      const isAuto = item.dataset.display === 'compact' && item.dataset.compactConfigured !== 'true';
      auto.set(id, Boolean(auto.get(id)) || isAuto);
    }
    return [...auto.keys()].sort().map(id => (auto.get(id) ? `${id}:auto` : id)).join(',');
  };

  const NAV_MODE_KEY = 'dashboard.navMode';
  const readNavMode = () => {
    try { return window.localStorage.getItem(NAV_MODE_KEY) === 'menu' ? 'menu' : 'more'; } catch (error) { return 'more'; }
  };

  // Wie viele Tabs (Breiten in px) in der Zeile bleiben: alle, wenn sie passen,
  // sonst so viele, wie neben den Knopf "Weitere" passen. Gleiche Logik steht in
  // settings.page.js fuer die Unter-Tabs.
  const visibleTabCount = (widths, available, moreWidth, gap) => {
    const total = widths.reduce((sum, width) => sum + width, 0) + gap * Math.max(widths.length - 1, 0);
    if (total <= available) return widths.length;
    let used = moreWidth;
    let count = 0;
    for (const width of widths) {
      used += width + gap;
      if (used > available) break;
      count += 1;
    }
    return count;
  };

  const dashboardShell = () => ({
    activePanel: 'overview-panel',
    activePage: '',
    // Nur die Mobilansicht liest diesen Zustand: oberhalb 700px greift
    // keine CSS-Regel darauf zu, die Tab-Leiste rendert wie eh und je.
    navOpen: false,

    // Hauptmenue bei wenig Platz: "more" (eine Zeile, Rest unter "Weitere") oder
    // "menu" (Aufklappmenue unter 700px). Gilt pro Browser (localStorage, Schluessel
    // dashboard.navMode), geschaltet wird auf Einstellungen > Darstellung.
    navMode: readNavMode(),
    // Tabs, die nicht in die Zeile passen, fuer die Liste "Weitere":
    // {key, label, el, active}.
    overflowNav: [],
    navMoreOpen: false,

    initNav() {
      const nav = document.getElementById('dashboard-tabs');
      if (!nav) return;
      const refit = () => this.$nextTick(() => this.fitNav());
      window.addEventListener('nav-mode-changed', (event) => {
        this.navMode = event.detail && event.detail.mode === 'menu' ? 'menu' : 'more';
        refit();
      });
      this.$watch('hiddenPages', refit);
      this.$watch('extraPages', refit);
      if (typeof ResizeObserver === 'function') new ResizeObserver(refit).observe(nav);
      if (typeof MutationObserver === 'function') new MutationObserver(refit).observe(nav, {childList: true, subtree: true, characterData: true});
      document.fonts?.ready?.then(refit);
      refit();
    },

    // Misst bei allen Tabs in der Zeile und blendet aus, was nicht passt. Laeuft in
    // einem Frame, die Zwischenstufe wird nie gemalt. Ein verborgenes Panel hat
    // keine Breite, dann kommt der ResizeObserver spaeter nochmal.
    fitNav() {
      const nav = document.getElementById('dashboard-tabs');
      if (!nav) return;
      const more = nav.querySelector('.nav-more');
      const tabs = [...nav.querySelectorAll('button.tab')];
      tabs.forEach((tab) => tab.classList.remove('is-overflow'));
      if (this.navMode !== 'more' || nav.clientWidth === 0) {
        this.overflowNav = [];
        if (more) more.hidden = true;
        return;
      }
      const shown = tabs.filter((tab) => getComputedStyle(tab).display !== 'none');
      more.hidden = false;
      const style = getComputedStyle(nav);
      const available = nav.clientWidth - (parseFloat(style.paddingLeft) || 0) - (parseFloat(style.paddingRight) || 0);
      const gap = parseFloat(style.columnGap) || 0;
      const keep = visibleTabCount(shown.map((tab) => tab.offsetWidth), available, more.offsetWidth, gap);
      const rest = shown.slice(keep);
      rest.forEach((tab) => tab.classList.add('is-overflow'));
      this.overflowNav = rest.map((tab, index) => ({
        key: tab.id || tab.dataset.pageId || `tab-${index}`,
        label: tab.textContent.trim(),
        el: tab,
        active: tab.classList.contains('active'),
      }));
      more.hidden = rest.length === 0;
      if (rest.length === 0) this.navMoreOpen = false;
    },

    // Welcher Eintrag aktiv ist, steht in den Klassen der Tabs (Alpine setzt sie
    // nach dem Wechsel), darum liest das hier nach $nextTick neu.
    syncNavActive() {
      this.overflowNav = this.overflowNav.map((item) => ({...item, active: item.el.classList.contains('active')}));
    },

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
    // Die Themen, mit denen liveSource geoeffnet ist. null heisst: noch nie
    // verbunden.
    liveTopics: null,
    liveDevices: '',

    // Tabs, die die volle Fensterbreite nutzen duerfen. Serverseitig ins
    // data-Attribut geschrieben, damit die Breite schon beim ersten Anstrich
    // stimmt - ein nachgeladener GET wuerde sichtbar springen.
    widePanels: [],

    // Im Editor mit "+ Seite" angelegte, noch nicht gespeicherte Seiten.
    // base.html rendert daraus zusaetzliche .tab.is-page-Knoepfe (x-for).
    // Nach echtem Speichern kommen die Seiten serverseitig und die Liste
    // wird beim naechsten Anstrich leer.
    // Enthaelt Objekte mit {id, name}.
    extraPages: [],

    // Server-gerenderte Seiten-Tabs, die der Editor inzwischen umbenannt oder
    // geloescht hat. Enthaelt Seiten-IDs.
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
      this.syncLiveTopics();
      window.dispatchEvent(new CustomEvent('dashboard-panel-changed', {detail: {panel}}));
    },

    async setActivePage(id) {
      // Aendert die aktive Layout-Seite und zeigt die Uebersicht an. Ein
      // Seitenwechsel wirft den Editor-Inhalt ebenso um wie ein Tab-Wechsel -
      // darum derselbe Waechter.
      const editor = window.__layoutEditor__;
      if (editor && editor.unsaved && id !== this.activePage && !(await editor.confirmLeave('tab'))) return;
      this.activePage = id;
      this.setActivePanel('overview-panel');
      // Die Seite steckt im server-gerenderten Fragment, nicht in einer
      // CSS-Klasse: ohne diesen Anstoss bliebe nach dem Tab-Klick genau
      // dieselbe Seite stehen. devicesPanel() hoert darauf und laedt
      // #overview-live mit dem neuen page= nach.
      window.dispatchEvent(new CustomEvent('layout-page-changed', {detail: {page: id}}));
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
        if (!response.ok) throw new Error(t('panel.load_failed', {code: response.status}));
        const html = await response.text();
        if (this.activePanel !== panel) return;
        await this.loadAsset(container.dataset.panelCss, 'style');
        await this.loadAsset(container.dataset.panelScript, 'script');
        container.outerHTML = html;
        const loaded = document.getElementById(panel);
        if (loaded) {
          loaded.dataset.panelLoaded = 'true';
          this.syncLiveTopics();
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

    // Accepts a comma-separated list so dependent scripts (e.g. flatpickr
    // before its German locale, which registers itself on window.flatpickr)
    // load one after another instead of racing.
    loadAsset(source, type) {
      const sources = (source || '').split(',').map(part => part.trim()).filter(Boolean);
      return sources.reduce((chain, url) => chain.then(() => this.loadSingleAsset(url, type)), Promise.resolve());
    },

    // No withBase() here either: source is a data-panel-script/-css entry and
    // therefore already prefixed by the template.
    loadSingleAsset(source, type) {
      if ((type === 'style' ? this.loadedStyles : this.loadedScripts).has(source)) return Promise.resolve();
      // Kartenskripte (EnergyCardScripts) stehen als feste <script src> in
      // base.html und kennt loadedScripts nicht. Gleiche URL heisst gleiche
      // Datei - ein zweites Ausfuehren von ApexCharts waere nur teuer.
      if (type === 'script' && [...document.scripts].some(script => script.getAttribute('src') === source)) {
        this.loadedScripts.add(source);
        return Promise.resolve();
      }
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
        asset.onerror = () => reject(new Error(t('panel.asset_load_failed', {url: source})));
        document.head.append(asset);
      });
    },

    // Die Editor-Abhaengigkeiten kommen erst beim ersten Druck auf Editieren.
    // overview-panel ist das einzige Panel ohne data-panel-src - es steht
    // inline in der Seite, und ohne diese Bremse zahlte jeder Aufruf der
    // Uebersicht Editor-Code und Choices mit. Das Promise wird gemerkt, damit
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
      // Speichere die server-seitigen Namen der Basis-Seiten, um Umbenennungen zu erkennen.
      this._serverPageNames = new Map(
        [...document.querySelectorAll('.tab.is-page')]
          .map(tab => [tab.dataset.pageId, tab.dataset.pageName])
      );
      // Ohne Vorgabe rendert der Server die erste Seite (activePage() in
      // webui.go). Damit der zugehoerige Tab von Anfang an als aktiv erscheint
      // - und der erste Klick auf ihn nicht wirkungslos aussieht - steht sie
      // hier auch im Zustand.
      if (!this.activePage && this._basePages.length) this.activePage = this._basePages[0];
      document.addEventListener('layout-pages-changed', event => {
        const pages = event.detail?.pages || [];
        const pageIds = pages.map(p => p.id);
        // Eine server-seite ist "geaendert", wenn ihre ID fehlt ODER ihr Name
        // sich vom server-namen unterscheidet.
        const changedServerPages = pages.filter(page =>
          this._basePages.includes(page.id) && this._serverPageNames.get(page.id) !== page.name
        );
        // Neue Seiten: im Editor angelegt, nicht auf dem Server.
        const newPages = pages.filter(page => page.id && !this._basePages.includes(page.id));
        // extraPages: neue Seiten PLUS umbenannte server-seiten.
        this.extraPages = [...newPages, ...changedServerPages];
        // hiddenPages: server-seiten, die nicht in der Editor-Liste stehen ODER
        // die server-seiten, die umbenannt wurden (zeigt den alten Namen).
        this.hiddenPages = this._basePages.filter(id =>
          !pageIds.includes(id) || changedServerPages.some(p => p.id === id)
        );
        // Der Editor sagt, welche Seite gemeint ist. Ohne Angabe bleibt die
        // alte Vermutung "die zuletzt hinzugekommene".
        if (event.detail?.active) this.activePage = event.detail.active;
        else if (this.extraPages.length) this.activePage = this.extraPages[this.extraPages.length - 1].id;
      });

      // Die Device Map bittet die Huelle, einen Tab zu oeffnen (und dort
      // Rollenzeilen hervorzuheben). Die Energie-Seite kann noch nicht
      // geladen sein, darum liegt die Anfrage zusaetzlich auf window.
      window.addEventListener('dashboard-open-panel', async event => {
        const detail = event.detail || {};
        if (detail.energyFocus) window.__energyFocusRequest__ = detail.energyFocus;
        if (detail.configFocus) window.__configFocusRequest__ = detail.configFocus;
        if (detail.automationFocus) window.__automationFocusRequest__ = detail.automationFocus;
        if (detail.statsFocus) window.__statsFocusRequest__ = detail.statsFocus;
        if (detail.deviceFocus) window.__deviceFocusRequest__ = detail.deviceFocus;
        await this.setActivePanel(detail.panel);
        if (detail.energyFocus) window.dispatchEvent(new CustomEvent('energy-focus-rows', {detail: {ids: detail.energyFocus}}));
        if (detail.configFocus) window.dispatchEvent(new CustomEvent('config-focus-item', {detail: detail.configFocus}));
        if (detail.automationFocus) window.dispatchEvent(new CustomEvent('automation-focus-rule', {detail: {id: detail.automationFocus}}));
        if (detail.deviceFocus) window.dispatchEvent(new CustomEvent('devices-focus-device', {detail: {id: detail.deviceFocus}}));
        if (detail.statsFocus && document.querySelector('#stats-panel .stats-page')) {
          // Ist die Seite schon geladen, nimmt sie das Ereignis. Sonst liest
          // stats.page.js die Anfrage beim Start von window.
          delete window.__statsFocusRequest__;
          window.dispatchEvent(new CustomEvent('energy-stats-focus', {detail: detail.statsFocus}));
        }
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
      // Ein Fragment-Tausch kann Kartentypen bringen oder nehmen (Seite
      // gewechselt, Layout gespeichert). Der Bedarf wird danach neu gelesen,
      // verbunden wird nur bei einer echten Aenderung.
      document.addEventListener('htmx:afterSettle', () => this.syncLiveTopics());
      this.syncLiveTopics();
    },

    // Oeffnet den Strom fuer genau diese Themen. Ein neuer Bedarf heisst
    // neu verbinden statt Stream-ID plus POST: so haelt der Server keinen
    // Zustand je Strom, und der volle Rumpf beim Verbinden faellt nur bei
    // Interaktion an.
    connectLive(topics, devices = '') {
      if (this.liveSource) this.liveSource.close();
      this.liveTopics = topics;
      this.liveDevices = devices;
      const query = `topics=${encodeURIComponent(topics)}${devices ? `&devices=${encodeURIComponent(devices)}` : ''}`;
      this.liveSource = new EventSource(withBase(`/api/v1/events?${query}`));
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

    // Liest den Bedarf des aktiven Panels. Im Rueckfall (kein EventSource
    // oder Strom gescheitert) bleibt es beim 30-Sekunden-Takt.
    syncLiveTopics() {
      if (!window.EventSource || this.liveFallback) return;
      const panel = document.getElementById(this.activePanel);
      const topics = liveTopicsFor(panel, this.activePanel);
      const devices = topics.split(',').includes('devices') ? liveDevicesFor(panel, this.activePanel) : '';
      if (this.liveSource && topics === this.liveTopics && devices === this.liveDevices) return;
      this.connectLive(topics, devices);
    },

    destroy() {
      if (this.liveSource) this.liveSource.close();
      if (this.liveFallback) window.clearInterval(this.liveFallback);
    },
  });

  const requestJSON = async (url, options) => {
    // withBase() is the single chokepoint for every URL literal below.
    const response = await fetch(withBase(url), options);
    const body = await response.json().catch(() => ({}));
    if (!response.ok) throw new Error(apiError(body));
    return body;
  };
  // Laedt ein Skript genau einmal nach (Erkennung ueber die exakte URL).
  const loadScript = src => new Promise((resolve, reject) => {
    if (!src) {
      reject(new Error(t('panel.asset_load_failed', {url: ''})));
      return;
    }
    if (document.querySelector(`script[src="${src}"]`)) {
      resolve();
      return;
    }
    const tag = document.createElement('script');
    tag.src = src;
    tag.onload = resolve;
    // Ein gescheitertes Tag muss weg, sonst hielte der naechste Versuch das
    // Skript fuer geladen.
    tag.onerror = () => {
      tag.remove();
      reject(new Error(t('panel.asset_load_failed', {url: src})));
    };
    document.head.append(tag);
  });
  const newID = prefix => `${prefix}-${Date.now()}-${Math.random().toString(16).slice(2)}`;

  const renderLocalTimestamps = root => {
    root.querySelectorAll('[data-local-timestamp]').forEach(node => {
      const timestamp = new Date(node.dateTime);
      if (Number.isNaN(timestamp.getTime())) return;
      node.textContent = window.I18n.formatDateTime(timestamp);
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
    if (absoluteDifference < 1) return t('time.just_now');
    const units = absoluteDifference < 60 ? ['seconds', 1]
      : absoluteDifference < 3600 ? ['minutes', 60]
      : absoluteDifference < 86400 ? ['hours', 3600]
      : ['days', 86400];
    const amount = Math.round(absoluteDifference / units[1]);
    // i18n-keys: time.short.past.seconds, time.short.past.minutes, time.short.past.hours, time.short.past.days
    // i18n-keys: time.short.future.seconds, time.short.future.minutes, time.short.future.hours, time.short.future.days
    const direction = difference < 0 ? 'past' : 'future';
    return t(`time.short.${direction}.${units[0]}`, {n: window.I18n ? window.I18n.formatNumber(amount) : amount});
  };

  const renderRelativeTimestamps = root => {
    root.querySelectorAll('[data-relative-timestamp]').forEach(node => {
      const timestamp = timestampValueToMilliseconds(node.dataset.relativeTimestamp);
      if (timestamp === null) return;
      node.textContent = formatRelativeTimestamp(timestamp);
      node.title = window.I18n.formatDateTime(timestamp);
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

  // Auf schmalen Fenstern hängt ein Hilfe-Tooltip unter seinem Knopf und soll
  // die Fensterbreite mit festem Rand nutzen (base.css). Wo der Knopf steht,
  // weiß nur das Skript: es setzt den Versatz nach links und die Breite als
  // CSS-Variablen, sobald der Knopf Fokus oder den Zeiger bekommt.
  const FIELD_HELP_EDGE = 20;
  const placeFieldHelp = event => {
    const help = event.target && event.target.closest ? event.target.closest('.field-help') : null;
    if (!help || !window.matchMedia || !window.matchMedia('(max-width: 700px)').matches) return;
    const viewport = document.documentElement.clientWidth;
    help.style.setProperty('--help-tip-left', `${FIELD_HELP_EDGE - help.getBoundingClientRect().left}px`);
    help.style.setProperty('--help-tip-width', `${viewport - 2 * FIELD_HELP_EDGE}px`);
  };
  document.addEventListener('focusin', placeFieldHelp);
  document.addEventListener('pointerover', placeFieldHelp);

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
      if (this.loading && !this.status) return t('status.loading');
      if (this.error && !this.status) return t('status.unavailable');
      return this.status?.status === 'degraded' ? t('status.degraded') : t('status.ok');
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
      if (days) return t('status.uptime.days_hours', {days, hours});
      if (hours) return t('status.uptime.hours_minutes', {hours, minutes});
      return t('status.uptime.minutes', {minutes});
    },

    formatTemp(v) { return (v === null || v === undefined) ? '-' : `${window.I18n.formatNumber(v, 1)} °C`; },
    formatPct(v) { return (v === null || v === undefined) ? '-' : `${Math.round(v)} %`; },

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

  const devicesPanel = () => {
    const panel = {
      ...window.deviceTileMixin(),
      deviceViewModeChanged: null,
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
      // Letzter bekannter Wert je Entity, aus den vollen und Delta-Ereignissen
      // gesammelt. Eine Zeile, die device-structure.js neu baut, malt daraus
      // ihren Wert, auch wenn er in einem frueheren Delta kam.
      liveValues: {},
      // Deklariert, damit Alpines mergeProxies() die Zuweisung nicht in den
      // aeusseren Bereich (dashboardShell) durchreicht - dort ueberlebte die
      // Kette jeden Fragment-Tausch.
      _liveQueue: null,
      // Anfragen je Kachel: Schluessel -> Strukturwunsch, der waehrend der
      // laufenden Anfrage kam (null = keiner). Angelegt in init().
      _tileRequests: null,
      csrfToken: '',
      canDeleteDiscovery: false,
      canEditPrefs: false,
      // Untergeraete des Geraete-Modals: canEditEnergy nach edit_energy, subDevices
      // und subEdgeProps aus /api/v1/device/map, subForm der offene Dialog.
      canEditEnergy: false,
      // Das Geraete-Modal (device-modal.js) laedt erst beim ersten Oeffnen
      // eines Geraets. Bis dahin rendert sein Markup nicht (x-if).
      deviceModalReady: false,
      _deviceModalLoad: null,
      // Gehoert zum Modal, steht aber hier, damit init() die Zuweisung von
      // attachDeviceModal() nicht in den aeusseren Bereich durchreicht.
      clockTimer: null,



      async loadSession() {
        try {
          const session = await requestJSON('/api/v1/auth/session');
          this.csrfToken = session.csrf_token || '';
          this.canDeleteDiscovery = session.delete_device_discovery === true;
          this.canEditPrefs = session.edit_layout !== false;
          this.canEditEnergy = session.edit_energy !== false;
        } catch (error) {
          this.csrfToken = '';
          this.canDeleteDiscovery = false;
          // Without a session API the instance runs unauthenticated, and the
          // server checks edit_energy on every write anyway.
          this.canEditEnergy = true;
          // Ohne Sitzungs-API laeuft die Instanz ohne Authentifizierung - dann
          // laesst auch requireLayoutMutation den Schreibzugriff durch.
          this.canEditPrefs = true;
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

      async unignoreDevice(deviceId) {
        if (!deviceId) return;
        try {
          await requestJSON(`/api/v1/devices/${encodeURIComponent(deviceId)}/unignore`, this.mutationOptions());
          await this.refreshAfterMutation();
        } catch (error) {
          this.$store.toasts.push(error.message, 'critical');
        }
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

      // Laedt device-modal.js einmal, haengt das Mixin in diese Komponente ein
      // und wartet, bis Alpine das Modal-Markup gerendert hat.
      ensureDeviceModal() {
        if (this.deviceModalReady) return Promise.resolve();
        if (!this._deviceModalLoad) {
          this._deviceModalLoad = this.$nextTick()
            .then(() => (window.deviceModalMixin ? null : loadScript(this.$refs.deviceModalTemplate?.dataset.script)))
            .then(() => {
              this.installDeviceModal();
              this.deviceModalReady = true;
              return this.$nextTick();
            })
            .then(() => this.attachDeviceModal())
            .catch(error => {
              this._deviceModalLoad = null;
              throw error;
            });
        }
        return this._deviceModalLoad;
      },

      // Getter und Methoden des Mixins landen auf dem rohen Datenobjekt, das
      // Alpine reaktiv umhuellt - so sind sie wie eigene Felder erreichbar.
      installDeviceModal() {
        if (Object.prototype.hasOwnProperty.call(panel, 'openDevice')) return;
        Object.defineProperties(panel, Object.getOwnPropertyDescriptors(window.deviceModalMixin()));
      },

      async selectDevice(deviceId) {
        if (!deviceId) return;
        try {
          await this.ensureDeviceModal();
        } catch (error) {
          this.$store.toasts.push(error.message, 'critical');
          return;
        }
        await this.openDevice(deviceId);
      },


      // Die sieben Kartentypen, deren Zahlen aus dem Energie-Schnappschuss
      // Welcher Zweig des SSE-Ereignisses welchen Kartentyp bedient. Ein
      // Kartentyp, der hier nicht steht, holt seine Zahlen weiter aus
      // serverseitig gerendertem HTML - fuer ihn bleibt der Tausch noetig.
      //
      // Es reicht nicht, dass alle Karten push-faehig sind: der Push muss auch
      // die Zweige mitbringen. Faellt einer aus (Marshalling-Fehler im
      // Server, aeltere Fassung), ist der Tausch das Richtige - lieber ein
      // teurer Neuaufbau als eine Karte, die stumm alte Zahlen zeigt.
      //
      // Geraetekacheln bedient der Zweig devices. Ob device-structure.js alle
      // einspielen konnte, sagt devicesApplied.
      liveGridPushCovers(detail, devicesApplied = true) {
        const branchForKind = {
          energy_flow: 'energy', energy_band: 'energy', energy_ring: 'energy',
          energy_board: 'energy', energy_day: 'energy', energy_schema: 'energy',
          energy_status: 'energy',
          battery_status: 'energy',
          entity_value: 'entities', entity_group: 'entities',
          diagnostics: 'diagnostics',
          device: 'devices',
          // Die Statistik-Kachel laedt ihre Zahlen selbst. Der Zweig traegt
          // nur die Revision, energy-stats-card.js entscheidet, ob sie neu
          // laedt.
          energy_stats: 'energy_stats',
        };
        // Kacheln, die sich selbst versorgen: die Verlaufskachel liest den
        // Browser-Speicher in ihrem eigenen Takt (history-view-card.js) und
        // braucht keinen Push-Zweig. Ohne diesen Eintrag erzwaenge eine
        // einzige Verlaufskachel den teuren Tausch bei jedem Tick.
        const selfServed = new Set(['history_view']);
        const items = [...this.$root.querySelectorAll('[data-layout-item-kind]')];
        // Leeres Raster heisst hier "nicht die Uebersicht" (das Geraete-Panel
        // benutzt dieselbe Komponente) oder "noch nichts geladen". Beide Male
        // ist der Tausch das Richtige.
        if (items.length === 0) return false;
        const covered = items.every(item => {
          if (selfServed.has(item.dataset.layoutItemKind)) return true;
          const branch = branchForKind[item.dataset.layoutItemKind];
          return branch !== undefined && detail?.[branch] !== undefined && detail[branch] !== null;
        });
        return covered && devicesApplied;
      },

      // Beide Panels teilen sich diese Komponente. Der Geraete-Tab braucht
      // die Werte und dass device-structure.js jede Karte einspielen konnte.
      livePushCovers(detail, devicesApplied) {
        if (this.$root.querySelector('#devices-live')) return Boolean(detail?.entities) && devicesApplied;
        return this.liveGridPushCovers(detail, devicesApplied);
      },

      // Holt genau eine Kachel neu, wenn sich ihre Struktur geaendert hat. Je
      // Kachel laeuft hoechstens eine Anfrage. htmx.ajax ersetzt den Traeger
      // per outerHTML, Alpine initialisiert die neue Kachel selbst. Haengt der
      // Traeger danach noch (404, Netzfehler), faellt es auf den Tausch des
      // ganzen Rasters zurueck.
      async replaceTile(host, wanted) {
        const itemId = host.dataset.layoutItemId;
        const key = itemId || host.dataset.deviceId;
        if (!key || !window.htmx) return;
        this._tileRequests = this._tileRequests || new Map();
        // Laeuft fuer diese Kachel schon eine Anfrage, merkt sich dieser Aufruf
        // den gewuenschten Stand. Nach deren Ende wird er einmal nachgeholt.
        if (this._tileRequests.has(key)) {
          this._tileRequests.set(key, wanted ?? host.dataset.structure);
          return;
        }
        this._tileRequests.set(key, null);
        let pending = null;
        const page = window.__dashboardShell__?.activePage || '';
        const url = itemId
          ? `/?fragment=layout-item&page=${encodeURIComponent(page)}&item=${encodeURIComponent(itemId)}`
          : `/?fragment=device-card&id=${encodeURIComponent(key)}&mode=${encodeURIComponent(this.viewMode)}`;
        // Die Hoehenstuetze gilt nur fuer diese Kachel. Das neue Element
        // bringt seine eigene Hoehe mit.
        host.style.minHeight = `${host.getBoundingClientRect().height}px`;
        try {
          await window.htmx.ajax('GET', withBase(url), {target: host, swap: 'outerHTML'});
        } catch (error) {
          // Unten entscheidet isConnected.
        } finally {
          pending = this._tileRequests.get(key);
          this._tileRequests.delete(key);
        }
        if (host.isConnected) {
          host.style.minHeight = '';
          this.refreshLiveFragment();
          return;
        }
        // Der Traeger wurde ersetzt. Weicht die neue Kachel vom waehrend der
        // Anfrage gemerkten Wunsch ab, holt genau diese Kachel einmal nach.
        if (pending === null) return;
        const fresh = [...this.$root.querySelectorAll(itemId ? '[data-layout-item-id]' : '[data-device-id]')]
          .find(el => (itemId ? el.dataset.layoutItemId : el.dataset.deviceId) === key);
        if (fresh && fresh.dataset.structure !== pending) this.replaceTile(fresh, pending);
      },

      init() {
        // registryUpdated ist bereits das Feld, an dem destroy() den Zuhoerer
        // wieder abmeldet - also der vorhandene Marker fuer "schon
        // eingerichtet". Ohne den Waechter kann derselbe Zuhoerer zweimal am
        // window haengen und jedes registry-updated zwei Fragmente ausloesen.
        if (this.registryUpdated) return;
        this.viewMode = this.$root.dataset.deviceViewMode === 'control' ? 'control' : 'compact';
        this._tileRequests = new Map();
        this.loadSession();
        this.configTileSettingChanged = () => this.refreshLiveFragment();
        this.deviceViewModeChanged = event => {
          const mode = event.detail?.mode === 'control' ? 'control' : 'compact';
          if (mode === this.viewMode) return;
          this.viewMode = mode;
          this.refreshLiveFragment(mode);
        };
        // The device map asks for a device in this tab (openDeviceInDevicesTab).
        // The shell leaves the request on window for a panel that starts later.
        // The overview runs the same component with its own hidden dialog, so
        // only the devices tab may react. A modal opened in a hidden panel
        // blocks every click on the page.
        const isDevicesTab = () => this.$root && this.$root.id === 'devices-panel';
        this.devicesFocusDevice = event => {
          if (!isDevicesTab()) return;
          const id = event.detail && event.detail.id;
          window.__deviceFocusRequest__ = null;
          if (id) this.selectDevice(id);
        };
        window.addEventListener('devices-focus-device', this.devicesFocusDevice);
        const pendingDevice = window.__deviceFocusRequest__;
        if (pendingDevice && isDevicesTab()) {
          window.__deviceFocusRequest__ = null;
          this.selectDevice(pendingDevice);
        }
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
          // Wert-Cache fuer Zeilen, die device-structure.js neu baut: der Wert
          // einer neu gewaehlten Zeile kam meist in einem frueheren Delta. Er
          // wird auch bei offenem Modal fortgeschrieben, sonst ist er beim
          // Schliessen veraltet.
          if (detail?.entities) {
            const delta = Boolean(detail.entities_delta);
            this.liveValues = delta ? {...this.liveValues, ...detail.entities} : {...detail.entities};
          }
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
          if (detail?.energy_stats) window.energyStatsCards?.applyRevision(this.$root, detail.energy_stats);
          // Der outerHTML-Tausch kostet gemessen 62 % der CPU-Zeit dieses
          // Tabs. Er faellt weg, sobald der Push jede Karte im Raster bedient
          // und die Struktur seit dem Rendern steht.
          const devicesApplied = window.deviceStructure
            ? window.deviceStructure.apply(this.$root, detail, {values: this.liveValues, replace: (host, structure) => this.replaceTile(host, structure)})
            : false;
          if (!this.livePushCovers(detail, devicesApplied)) this.refreshLiveFragment();
        };
        this.layoutSaved = () => this.refreshLiveFragment();
        this.pageChanged = () => this.refreshLiveFragment();
        document.addEventListener('config-tile-setting-changed', this.configTileSettingChanged);
        document.addEventListener('device-view-mode-changed', this.deviceViewModeChanged);
        window.addEventListener('registry-updated', this.registryUpdated);
        window.addEventListener('layout-saved', this.layoutSaved);
        window.addEventListener('layout-page-changed', this.pageChanged);
      },

      destroy() {
        if (this.configTileSettingChanged) document.removeEventListener('config-tile-setting-changed', this.configTileSettingChanged);
        if (this.deviceViewModeChanged) document.removeEventListener('device-view-mode-changed', this.deviceViewModeChanged);
        if (this.registryUpdated) window.removeEventListener('registry-updated', this.registryUpdated);
        if (this.devicesFocusDevice) window.removeEventListener('devices-focus-device', this.devicesFocusDevice);
        if (this.layoutSaved) window.removeEventListener('layout-saved', this.layoutSaved);
        if (this.pageChanged) window.removeEventListener('layout-page-changed', this.pageChanged);
        if (this.deviceModalReady) this.detachDeviceModal();
      },
    };
    return panel;
  };

  // Rangfolgen fuer die Diagnose: hoeher = dringender. Gruppen, Karten und
  // die Liste sortieren alle nach derselben Skala, damit das Dringendste
  // ueberall oben steht.
  const DIAGNOSTIC_SEVERITY_RANK = {critical: 3, warning: 2, info: 1};
  const DEVICE_HEALTH_RANK = {critical: 4, unhealthy: 3, degraded: 2, unknown: 1, healthy: 0};

  const diagnosticsPanel = () => ({
    warnings: [],
    healthScores: [],
    // deviceCount/entityCount statt der ganzen Geraeteliste: das Panel hat
    // aus den 448 KB von /api/v1/discovery ohnehin nur diese beiden Zahlen
    // gelesen. Die Vollansicht bleibt unter /api/v1/discovery bestehen.
    discovery: {deviceCount: 0, entityCount: 0, discovery_errors: [], duplicate_ids: {}},
    severity: 'all',
    device: 'all',
    // 'grouped' fasst Meldungen nach Ursache zusammen (Ursache und Handlung
    // nur einmal), 'list' ist die flache, sortierbare Tabelle.
    view: 'grouped',
    // Auf/Zu je Gruppe, sobald der Nutzer sie einmal umgeschaltet hat. Ohne
    // Eintrag gilt der Standard aus groupOpenByDefault().
    openGroups: {},
    sortBy: 'severity',
    sortDirection: 'desc',
    loading: false,
    loaded: false,
    checkedAt: null,
    discoveryError: '',

    async load() {
      this.loading = true;
      this.discoveryError = '';
      try {
        const [warnings, healthScores, discovery] = await Promise.allSettled([
          requestJSON('/api/v1/diagnostics'),
          requestJSON('/api/v1/diagnostics/health'),
          requestJSON('/api/v1/discovery/summary'),
        ]);
        if (warnings.status === 'rejected') throw warnings.reason;
        this.warnings = Array.isArray(warnings.value) ? warnings.value : [];
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
        this.checkedAt = new Date();
      } catch (error) {
        this.$store.toasts.push(error.message, 'critical');
        this.warnings = [];
        this.healthScores = [];
        this.discovery = {deviceCount: 0, entityCount: 0, discovery_errors: [], duplicate_ids: {}};
      } finally {
        this.loading = false;
        this.loaded = true;
      }
    },

    get duplicateIDs() {
      return Object.entries(this.discovery.duplicate_ids || {}).sort(([left], [right]) => window.I18n.compare(left, right));
    },

    get discoveryHasIssues() {
      return Boolean(this.discoveryError) || this.discovery.discovery_errors.length > 0 || this.duplicateIDs.length > 0;
    },

    // Die Karten zeigen immer alle Geraete, das schlechteste zuerst. Der
    // Geraetefilter markiert nur eine Karte, statt die anderen zu verstecken,
    // damit man von dort aus direkt zum naechsten Geraet springen kann.
    get sortedHealthScores() {
      return [...this.healthScores].sort((left, right) =>
        (DEVICE_HEALTH_RANK[right.status] ?? 1) - (DEVICE_HEALTH_RANK[left.status] ?? 1) ||
        (Number(left.score) || 0) - (Number(right.score) || 0) ||
        window.I18n.compare(left.device_id, right.device_id)
      );
    },

    selectDevice(deviceID) {
      this.device = this.device === deviceID ? 'all' : deviceID;
    },

    healthStatusLabel(status) {
      // i18n-keys: diagnostics.health.healthy, diagnostics.health.degraded, diagnostics.health.unhealthy, diagnostics.health.critical, diagnostics.health.unknown
      const known = ['healthy', 'degraded', 'unhealthy', 'critical', 'unknown'];
      if (known.includes(status)) return t(`diagnostics.health.${status}`);
      return status || t('diagnostics.health.unknown');
    },

    // Signal von heute nur mit Uhrzeit, aelteres mit Datum. Ohne Signal ein
    // Satz statt eines nackten Strichs.
    formatHealthTime(value, now = new Date()) {
      const date = value ? new Date(value) : null;
      if (!date || Number.isNaN(date.getTime()) || date.getUTCFullYear() <= 1) return t('diagnostics.device.never_seen');
      if (date.toDateString() === now.toDateString()) return window.I18n.formatTime(date);
      return window.I18n.formatDateTime(date);
    },

    severityLabel(severity) {
      // i18n-keys: diagnostics.severity.critical, diagnostics.severity.warning, diagnostics.severity.info
      const key = `diagnostics.severity.${severity}`;
      return window.I18n.has(key) ? t(key) : (severity || '');
    },

    // Meldungen nach Geraetefilter, aber noch ohne Schweregradfilter: daraus
    // zaehlt der Schweregrad-Umschalter, damit seine Zahlen zeigen, was ein
    // Klick bringen wuerde.
    get deviceWarnings() {
      if (this.device === 'all') return this.warnings;
      return this.warnings.filter(item => item.device_id === this.device);
    },

    severityCount(severity) {
      if (severity === 'all') return this.deviceWarnings.length;
      return this.deviceWarnings.filter(item => item.severity === severity).length;
    },

    get headline() {
      // i18n-keys: diagnostics.headline.critical.one, diagnostics.headline.critical.other, diagnostics.headline.warnings.one, diagnostics.headline.warnings.other, diagnostics.headline.info.one, diagnostics.headline.info.other
      if (!this.loaded) return t('diagnostics.headline.checking');
      if (this.warnings.length === 0) return t('diagnostics.headline.ok');
      const count = severity => this.warnings.filter(item => item.severity === severity).length;
      const parts = [];
      for (const [severity, key] of [['critical', 'critical'], ['warning', 'warnings'], ['info', 'info']]) {
        const n = count(severity);
        if (n > 0) parts.push(tn(`diagnostics.headline.${key}`, n));
      }
      const devices = new Set(this.warnings.map(item => item.device_id).filter(Boolean)).size;
      return t('diagnostics.headline.summary', {counts: parts.join(', '), devices: tn('diagnostics.headline.devices', devices)});
    },

    get headlineTone() {
      if (this.warnings.some(item => item.severity === 'critical')) return 'bad';
      if (this.warnings.length > 0) return 'warn';
      return this.loaded ? 'ok' : 'idle';
    },

    get checkedAtLabel() {
      return this.checkedAt ? t('diagnostics.checked_at', {time: window.I18n.formatTime(this.checkedAt)}) : '';
    },

    get filteredWarnings() {
      const result = this.deviceWarnings.filter(item => this.severity === 'all' || item.severity === this.severity);
      result.sort((left, right) => {
        let comparison;
        if (this.sortBy === 'severity') {
          comparison = (DIAGNOSTIC_SEVERITY_RANK[left.severity] || 0) - (DIAGNOSTIC_SEVERITY_RANK[right.severity] || 0);
        } else {
          comparison = window.I18n.compare(left[this.sortBy] || '', right[this.sortBy] || '');
        }
        return this.sortDirection === 'asc' ? comparison : -comparison;
      });
      return result;
    },

    // Eine Gruppe je Ursache (Warning.key, sonst rule_id): Ursache und
    // Handlung stehen einmal, die betroffenen Entitaeten haengen je Geraet
    // darunter. Dringendste Gruppe zuerst, bei Gleichstand die groessere.
    get warningGroups() {
      const groups = new Map();
      for (const item of this.filteredWarnings) {
        const id = item.key || item.rule_id || '';
        let group = groups.get(id);
        if (!group) {
          group = {id, sample: item, severity: item.severity, count: 0, devices: new Map()};
          groups.set(id, group);
        }
        group.count += 1;
        if ((DIAGNOSTIC_SEVERITY_RANK[item.severity] || 0) > (DIAGNOSTIC_SEVERITY_RANK[group.severity] || 0)) group.severity = item.severity;
        const deviceID = item.device_id || '';
        if (!group.devices.has(deviceID)) group.devices.set(deviceID, []);
        if (item.entity_id) group.devices.get(deviceID).push(item.entity_id);
      }
      return [...groups.values()]
        .map(group => ({
          id: group.id,
          severity: group.severity,
          count: group.count,
          title: this.ruleTitle(group.sample),
          message: this.warningText(group.sample, 'message'),
          hint: this.warningText(group.sample, 'hint'),
          devices: [...group.devices.entries()]
            .map(([deviceID, entities]) => ({deviceID, entities: [...new Set(entities)].sort(window.I18n.compare)}))
            .sort((left, right) => window.I18n.compare(left.deviceID, right.deviceID)),
        }))
        .sort((left, right) =>
          (DIAGNOSTIC_SEVERITY_RANK[right.severity] || 0) - (DIAGNOSTIC_SEVERITY_RANK[left.severity] || 0) ||
          right.count - left.count ||
          window.I18n.compare(left.title, right.title)
        );
    },

    // Kritische Gruppen stehen offen, ebenso eine einzelne Gruppe: dann gibt
    // es nichts zu ueberblicken und der Klick zum Aufklappen waere nur Arbeit.
    groupOpenByDefault(group) {
      return group.severity === 'critical' || this.warningGroups.length === 1;
    },

    isGroupOpen(group) {
      return Object.hasOwn(this.openGroups, group.id) ? this.openGroups[group.id] : this.groupOpenByDefault(group);
    },

    rememberGroup(group, open) {
      if (open === this.isGroupOpen(group)) return;
      this.openGroups[group.id] = open;
    },

    sort(field) {
      if (this.sortBy === field) {
        this.sortDirection = this.sortDirection === 'asc' ? 'desc' : 'asc';
        return;
      }
      this.sortBy = field;
      this.sortDirection = field === 'severity' ? 'desc' : 'asc';
    },

    sortState(field) {
      if (this.sortBy !== field) return 'none';
      return this.sortDirection === 'asc' ? 'ascending' : 'descending';
    },

    sortAria(field) {
      // i18n-keys: diagnostics.sort.severity, diagnostics.sort.severity_asc, diagnostics.sort.severity_desc, diagnostics.sort.device, diagnostics.sort.device_asc, diagnostics.sort.device_desc, diagnostics.sort.entity, diagnostics.sort.entity_asc, diagnostics.sort.entity_desc, diagnostics.sort.rule, diagnostics.sort.rule_asc, diagnostics.sort.rule_desc
      const columnMap = {severity: 'severity', device_id: 'device', entity_id: 'entity', rule_id: 'rule'};
      const column = columnMap[field];
      if (this.sortBy !== field) return t(`diagnostics.sort.${column}`);
      const direction = this.sortDirection === 'asc' ? 'asc' : 'desc';
      return t(`diagnostics.sort.${column}_${direction}`);
    },

    // Kurzer Name der Ursache fuer Gruppenkopf und Listenzeile. Ohne
    // Katalogtext bleibt die rohe rule_id stehen, damit neue Regeln aus dem
    // Backend sichtbar sind, bevor ihr Text nachgezogen ist.
    // i18n-keys: diagnostics.rule.duplicate_unique_id.title, diagnostics.rule.discovery_invalid_json.title, diagnostics.rule.discovery_mismatch.title, diagnostics.rule.discovery_not_retained.title, diagnostics.rule.missing_topic.title, diagnostics.rule.missing_availability.title, diagnostics.rule.missing_unit.title, diagnostics.rule.no_state_update.title, diagnostics.rule.offline.title, diagnostics.rule.empty_state_payload.title, diagnostics.rule.invalid_discovery_payload.title, diagnostics.rule.ignored_device_discovery_stale.title, diagnostics.rule.configured_device_missing.title
    ruleTitle(item) {
      const key = item && item.key ? `diagnostics.rule.${item.key}.title` : '';
      if (key && window.I18n && window.I18n.has(key)) return t(key);
      return (item && item.rule_id) || '';
    },

    // i18n-keys: diagnostics.rule.duplicate_unique_id.message, diagnostics.rule.duplicate_unique_id.hint, diagnostics.rule.discovery_invalid_json.message, diagnostics.rule.discovery_invalid_json.hint, diagnostics.rule.discovery_mismatch.message, diagnostics.rule.discovery_mismatch.hint, diagnostics.rule.discovery_not_retained.message, diagnostics.rule.discovery_not_retained.hint, diagnostics.rule.missing_topic.message, diagnostics.rule.missing_topic.hint, diagnostics.rule.missing_availability.message, diagnostics.rule.missing_availability.hint, diagnostics.rule.missing_unit.message, diagnostics.rule.missing_unit.hint, diagnostics.rule.no_state_update.message, diagnostics.rule.no_state_update.hint, diagnostics.rule.offline.message, diagnostics.rule.offline.hint, diagnostics.rule.empty_state_payload.message, diagnostics.rule.empty_state_payload.hint, diagnostics.rule.invalid_discovery_payload.message, diagnostics.rule.invalid_discovery_payload.hint, diagnostics.rule.ignored_device_discovery_stale.message, diagnostics.rule.ignored_device_discovery_stale.hint, diagnostics.rule.configured_device_missing.message, diagnostics.rule.configured_device_missing.hint
    warningText(item, part) {
      const key = item && item.key ? `diagnostics.rule.${item.key}.${part}` : '';
      if (key && window.I18n && window.I18n.has(key)) return t(key);
      return (item && item[part]) || '';
    },
  });

  document.addEventListener('alpine:init', () => {
    dashboardShell.liveTopicsFor = liveTopicsFor;
    dashboardShell.liveDevicesFor = liveDevicesFor;
    Alpine.data('dashboardShell', dashboardShell);
    Alpine.data('runtimeStatusPanel', runtimeStatusPanel);
    Alpine.data('diagnosticsPanel', diagnosticsPanel);
    Alpine.data('devicesPanel', devicesPanel);
  });
})();
