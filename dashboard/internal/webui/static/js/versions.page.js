(() => {
  const t = (key, params) => (window.I18n ? window.I18n.t(key, params) : key);
  const apiError = (body, fallbackKey) => (window.I18n ? window.I18n.error(body, fallbackKey) : (body && body.message) || fallbackKey || 'common.request_failed');
  const localized = (entry) => (window.I18n ? window.I18n.localized(entry.i18n, entry.text) : entry.text);

  const requestJSON = async (url, options) => {
    // The single chokepoint for every URL literal in this file: behind a
    // reverse-proxy subpath base.html puts the prefix into
    // __DASHBOARD_BASE_PATH__; on direct access it is empty.
    const response = await fetch(`${window.__DASHBOARD_BASE_PATH__ || ''}${url}`, options);
    const body = await response.json().catch(() => ({}));
    if (!response.ok) {
      throw new Error(apiError(body));
    }
    return body;
  };

  // Order of the cards. A kind this list does not know (a new value in
  // components.json) is listed under "other" instead of vanishing.
  // i18n-keys: settings.versions.kind.app, settings.versions.kind.shared, settings.versions.kind.service, settings.versions.kind.library, settings.versions.kind.tool, settings.versions.kind.integration, settings.versions.kind_other
  const KIND_ORDER = ['app', 'shared', 'service', 'library', 'tool', 'integration'];

  // Change types of changelog.json (TYPE_BY_LABEL in make_changelog_json.py).
  // i18n-keys: settings.versions.change_type.feat, settings.versions.change_type.fix, settings.versions.change_type.perf, settings.versions.change_type.refactor, settings.versions.change_type.docs, settings.versions.change_type.test, settings.versions.change_type.style, settings.versions.change_type.chore, settings.versions.change_type.dev, settings.versions.change_type.build, settings.versions.change_type.ci, settings.versions.change_type_other
  const CHANGE_TYPES = ['feat', 'fix', 'perf', 'refactor', 'docs', 'test', 'style', 'chore', 'dev', 'build', 'ci'];

  // "v0.7.5-dev" and "v0.7.5" are the same version: only X.Y.Z counts when
  // checking whether the running dashboard differs from the package.
  const baseVersion = (version) => {
    const match = /^v?(\d+\.\d+\.\d+)/.exec(version || '');
    return match ? match[1] : '';
  };

  // changelog.json flags every entry as a highlight or a detail
  // (make_changelog_json.py). A file from before the flag carries none.
  const isFlagged = (releases) => releases.some((release) => (release.groups || [])
    .some((group) => (group.entries || []).some((entry) => 'highlight' in entry)));

  const onlyHighlights = (releases) => releases
    .map((release) => ({
      ...release,
      groups: (release.groups || [])
        .map((group) => ({...group, entries: (group.entries || []).filter((entry) => entry.highlight === true)}))
        .filter((group) => group.entries.length > 0),
    }))
    .filter((release) => release.groups.length > 0);

  // Bundle versions the changelog shows per setting value (changelog_history
  // in settings.go). 0 asks for every version the node has.
  const HISTORY_LIMITS = {recent: 5, extended: 20, all: 0};
  const historyValue = (value) => (value in HISTORY_LIMITS ? value : 'recent');

  // Text des Zustands eines Highlights in der Versionsliste. other-device
  // hat seinen eigenen Text (je nach Geraet, siehe highlightLabel).
  // i18n-keys: onboarding.checklist.news_new, onboarding.checklist.news_seen, settings.versions.highlight.text
  const HIGHLIGHT_LABEL_KEY = {new: 'onboarding.checklist.news_new', seen: 'onboarding.checklist.news_seen', stale: 'onboarding.checklist.news_stale', text: 'settings.versions.highlight.text'};

  // Der Eintrag des Highlights im Neu-erkunden-Stand des Stores, sonst null.
  const highlightItem = (store, entry) => {
    if (!entry || !entry.id) return null;
    for (const version of store.exploreVersions || []) {
      const item = version.items.find((candidate) => candidate.id === entry.id);
      if (item) return item;
    }
    return null;
  };

  const kindTitle = (kind) => t(kind === 'other' ? 'settings.versions.kind_other' : `settings.versions.kind.${kind}`);

  const versionsPanel = () => ({
    loading: false,
    error: '',
    bundle: null,
    running: {dashboard: ''},
    components: [],
    hasChangelog: false,
    // The bundle the node ran before its last version change, null when none is recorded.
    previous: null,
    open: {},
    releases: {},
    releaseErrors: {},
    // 'highlights' or 'all', switched at the top of the page for every component.
    mode: 'highlights',
    updateStatus: null,
    checkingForUpdates: false,
    canUpdateSettings: false,
    // Verlauf der Changelogs und Update-Pruefung gehoeren zur Einrichtung (edit_setup).
    canEditSetup: true,
    updateCheckDisabled: false,
    savingUpdateCheck: false,
    changelogHistory: 'recent',
    bundlesTotal: 0,
    bundlesShown: 0,
    savingChangelogHistory: false,
    // Version aus ?release=, bis der Dashboard-Changelog geladen ist.
    releaseTarget: '',

    // ?all=1 starts in the "all" view.
    // ?release=<version> (Link aus der Checkliste) oeffnet den Dashboard-Changelog
    // in der Ansicht "Alles" und scrollt zu diesem Release.
    init() {
      const params = new URLSearchParams(window.location.search);
      if (params.get('all') === '1') this.mode = 'all';
      this.releaseTarget = params.get('release') || '';
      if (this.releaseTarget) this.mode = 'all';
    },

    async load() {
      this.loading = true;
      this.error = '';
      try {
        const body = await requestJSON('/api/v1/versions');
        this.bundle = body.bundle || null;
        this.running = body.running || {dashboard: ''};
        this.components = body.components || [];
        this.hasChangelog = Boolean(body.has_changelog);
        this.previous = body.previous || null;
      } catch (error) {
        this.error = error.message;
      } finally {
        this.loading = false;
      }
      await this.openRelease();
      await this.loadUpdates();
    },

    // Oeffnet den Dashboard-Changelog und scrollt zum Release aus ?release=.
    // Die Eintraege stehen erst im DOM, wenn Alpine die Schleifen gerendert hat.
    async openRelease() {
      const version = this.releaseTarget;
      if (!version || !this.components.some((component) => component.id === 'dashboard')) return;
      this.releaseTarget = '';
      this.open = {...this.open, dashboard: true};
      await this.loadReleases('dashboard');
      if (this.$nextTick) await this.$nextTick();
      const target = Array.from(document.querySelectorAll('[data-release]')).find((el) => el.dataset.release === version);
      if (!target || !target.scrollIntoView) return;
      const reduced = Boolean(window.matchMedia && window.matchMedia('(prefers-reduced-motion: reduce)').matches);
      target.scrollIntoView({block: 'start', behavior: reduced ? 'auto' : 'smooth'});
    },

    // Zustand eines Highlights der Versionsliste (obModel.exploreState), leer ohne Eintrag.
    highlightState(entry) {
      const store = this.$store.onboarding;
      const item = highlightItem(store, entry);
      return item ? window.obModel.exploreState(item, store.seen, store.ctx()) : '';
    },

    // Der Text neben dem Highlight: Zustand, bei other-device das fehlende Geraet.
    highlightLabel(entry) {
      const state = this.highlightState(entry);
      if (state === 'other-device') {
        return t(this.$store.onboarding.ctx().device === 'phone' ? 'onboarding.checklist.only_desktop' : 'onboarding.checklist.only_phone');
      }
      const key = HIGHLIGHT_LABEL_KEY[state];
      return key ? t(key) : '';
    },

    // Neu und gesehen haben eine Tour: der Knopf startet sie. Veraltet zeigt den
    // Knopf ausgegraut (im Template). Jeder Zustand mit Eintrag hat den Link in
    // die Checkliste, nur ohne Tour gibt es keinen Knopf.
    highlightButtons(entry) {
      const state = this.highlightState(entry);
      if (!state) return {run: false, runText: '', checklist: false};
      const run = state === 'new' || state === 'seen' || state === 'stale';
      return {
        run,
        runText: run ? t(state === 'seen' ? 'onboarding.checklist.action_again' : 'onboarding.checklist.action_show') : '',
        checklist: true,
      };
    },

    // Startet die Tour des Highlights, wie die Checkliste: Warteschlange leeren, dann starten.
    showHighlight(entry) {
      if (this.highlightState(entry) === 'stale') return;
      const store = this.$store.onboarding;
      store.queue = [];
      store.start('explore', entry.id);
    },

    // Deep Link in die Checkliste, Modus Neu erkunden, optional auf einen Punkt.
    checklistHref(entry) {
      const base = `${window.__DASHBOARD_BASE_PATH__ || ''}/?panel=settings&page=checklist&mode=explore`;
      return entry && entry.id ? `${base}&item=${encodeURIComponent(entry.id)}` : base;
    },

    async loadUpdates() {
      try {
        const session = await requestJSON('/api/v1/auth/session');
        this.canUpdateSettings = Boolean(session.update_settings);
        this.canEditSetup = Boolean(session.edit_setup);
      } catch (error) {
        this.canUpdateSettings = false;
        this.canEditSetup = true;
      }
      try {
        const value = await requestJSON('/api/v1/settings');
        this.updateCheckDisabled = Boolean(value.update_check_disabled);
        this.changelogHistory = historyValue(value.changelog_history);
      } catch (error) {
        this.updateCheckDisabled = false;
        this.changelogHistory = 'recent';
      }
      try {
        const status = await requestJSON('/api/v1/updates/status');
        this.updateStatus = status.checked === false ? null : status;
      } catch (error) {
        this.updateStatus = null;
      }
    },

    // The switch applies at once, there is no save button on this page.
    // PUT /api/v1/settings replaces every field, so the current settings are
    // read first and only update_check_disabled changes. The settings form
    // hears the event and keeps its copy in step.
    async setUpdateCheck(enabled) {
      if (this.savingUpdateCheck) return;
      const previous = this.updateCheckDisabled;
      this.updateCheckDisabled = !enabled;
      this.savingUpdateCheck = true;
      try {
        const current = await requestJSON('/api/v1/settings');
        await requestJSON('/api/v1/settings', {
          method: 'PUT',
          headers: {'Content-Type': 'application/json'},
          body: JSON.stringify({...current, update_check_disabled: !enabled}),
        });
        document.dispatchEvent(new CustomEvent('update-check-setting-changed', {detail: {disabled: !enabled}}));
      } catch (error) {
        this.updateCheckDisabled = previous;
        this.$store.toasts.push(error.message, 'critical');
      } finally {
        this.savingUpdateCheck = false;
      }
    },

    // Same round trip as setUpdateCheck(): only changelog_history changes,
    // then every open component is loaded again with the new count.
    async setChangelogHistory(value) {
      if (this.savingChangelogHistory || !(value in HISTORY_LIMITS)) return;
      const previous = this.changelogHistory;
      this.changelogHistory = value;
      this.savingChangelogHistory = true;
      try {
        const current = await requestJSON('/api/v1/settings');
        await requestJSON('/api/v1/settings', {
          method: 'PUT',
          headers: {'Content-Type': 'application/json'},
          body: JSON.stringify({...current, changelog_history: value}),
        });
        document.dispatchEvent(new CustomEvent('changelog-history-setting-changed', {detail: {value}}));
        this.releases = {};
        const openIds = Object.keys(this.open).filter((id) => this.open[id]);
        await Promise.all(openIds.map((id) => this.loadReleases(id)));
      } catch (error) {
        this.changelogHistory = previous;
        this.$store.toasts.push(error.message, 'critical');
      } finally {
        this.savingChangelogHistory = false;
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

    get groups() {
      const byKind = new Map();
      for (const component of this.components) {
        const kind = KIND_ORDER.includes(component.kind) ? component.kind : 'other';
        if (!byKind.has(kind)) byKind.set(kind, []);
        byKind.get(kind).push(component);
      }
      return [...KIND_ORDER, 'other']
        .filter((kind) => byKind.has(kind))
        .map((kind) => ({kind, title: kindTitle(kind), components: byKind.get(kind)}));
    },

    get dashboardDiffers() {
      const packaged = this.components.find((component) => component.id === 'dashboard');
      const running = baseVersion(this.running && this.running.dashboard);
      const inBundle = baseVersion(packaged && packaged.version);
      return Boolean(running && inBundle && running !== inBundle);
    },

    get builtAt() {
      const raw = this.bundle && this.bundle.built_at;
      if (!raw) return '';
      const formatted = window.I18n ? window.I18n.formatDateTime(raw, {dateStyle: 'medium', timeStyle: 'short'}) : '';
      return formatted || raw;
    },

    async toggle(id) {
      this.open = {...this.open, [id]: !this.open[id]};
      if (this.open[id] && !this.releases[id]) {
        await this.loadReleases(id);
      }
    },

    async loadReleases(id) {
      try {
        const limit = HISTORY_LIMITS[historyValue(this.changelogHistory)];
        const body = await requestJSON(`/api/v1/changelog?component=${encodeURIComponent(id)}&bundles=${limit}`);
        const component = (body.components || []).find((entry) => entry.id === id);
        this.releases = {...this.releases, [id]: component ? component.releases : []};
        this.bundlesTotal = body.bundles_total || 0;
        this.bundlesShown = body.bundles_shown || 0;
      } catch (error) {
        this.releaseErrors = {...this.releaseErrors, [id]: error.message};
      }
    },

    // The releases of a component as the current view shows them. An
    // unflagged file is shown in full, there is nothing to pick from.
    shownReleases(id) {
      const releases = this.releases[id] || [];
      if (this.mode === 'all' || !isFlagged(releases)) return releases;
      return onlyHighlights(releases);
    },

    // True when the highlights view leaves a flagged component empty.
    noHighlights(id) {
      const releases = this.releases[id] || [];
      return this.mode === 'highlights' && releases.length > 0 && isFlagged(releases)
        && this.shownReleases(id).length === 0;
    },

    // "scope: text", the scope is its own field in the changelog. The text
    // is the translation in the page language when the entry has one.
    entryText(entry) {
      const text = localized(entry);
      return entry.scope ? `${entry.scope}: ${text}` : text;
    },

    // Release dates are plain YYYY-MM-DD; shown in the page language's format.
    releaseDate(date) {
      if (!date) return '';
      const formatted = window.I18n ? window.I18n.formatDate(`${date}T00:00:00`, {dateStyle: 'medium'}) : '';
      return formatted || date;
    },

    // A known change type gets the catalog title, anything else keeps the
    // heading the changelog itself carries.
    groupTitle(group) {
      if (group.type === 'other') return t('settings.versions.change_type_other');
      return CHANGE_TYPES.includes(group.type) ? t(`settings.versions.change_type.${group.type}`) : group.label;
    },
  });

  const register = () => {
    if (!window.Alpine) return;
    Alpine.data('versionsPanel', versionsPanel);
  };
  if (window.Alpine) register(); else document.addEventListener('alpine:init', register, {once: true});
})();
