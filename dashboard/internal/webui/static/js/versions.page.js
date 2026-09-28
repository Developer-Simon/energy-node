(() => {
  const t = (key, params) => (window.I18n ? window.I18n.t(key, params) : key);
  const apiError = (body, fallbackKey) => (window.I18n ? window.I18n.error(body, fallbackKey) : (body && body.message) || fallbackKey || 'common.request_failed');

  const requestJSON = async (url) => {
    // The single chokepoint for every URL literal in this file: behind a
    // reverse-proxy subpath base.html puts the prefix into
    // __DASHBOARD_BASE_PATH__; on direct access it is empty.
    const response = await fetch(`${window.__DASHBOARD_BASE_PATH__ || ''}${url}`);
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

  const kindTitle = (kind) => t(kind === 'other' ? 'settings.versions.kind_other' : `settings.versions.kind.${kind}`);

  const versionsPanel = () => ({
    loading: false,
    error: '',
    bundle: null,
    running: {dashboard: ''},
    components: [],
    hasChangelog: false,
    open: {},
    releases: {},
    releaseErrors: {},

    async load() {
      this.loading = true;
      this.error = '';
      try {
        const body = await requestJSON('/api/v1/versions');
        this.bundle = body.bundle || null;
        this.running = body.running || {dashboard: ''};
        this.components = body.components || [];
        this.hasChangelog = Boolean(body.has_changelog);
      } catch (error) {
        this.error = error.message;
      } finally {
        this.loading = false;
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
        const body = await requestJSON(`/api/v1/changelog?component=${encodeURIComponent(id)}`);
        const component = (body.components || []).find((entry) => entry.id === id);
        this.releases = {...this.releases, [id]: component ? component.releases : []};
      } catch (error) {
        this.releaseErrors = {...this.releaseErrors, [id]: error.message};
      }
    },

    // "scope: text", the scope is its own field in the changelog.
    entryText(entry) {
      return entry.scope ? `${entry.scope}: ${entry.text}` : entry.text;
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
