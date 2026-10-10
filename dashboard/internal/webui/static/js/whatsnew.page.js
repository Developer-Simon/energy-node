// The highlights of the last update (GET /api/v1/changelog/highlights). The
// update bar opens it after a finished update; the versions page links to it.
(() => {
  const t = (key, params) => (window.I18n ? window.I18n.t(key, params) : key);
  const apiError = (body, fallbackKey) => (window.I18n ? window.I18n.error(body, fallbackKey) : (body && body.message) || fallbackKey || 'common.request_failed');

  const whatsNewPanel = () => ({
    loading: false,
    error: '',
    from: '',
    to: '',
    sincePrevious: false,
    components: [],
    // Loads once, the first time the sub page is shown. A flag instead of
    // checking the data: an empty answer must not trigger another request.
    requested: false,

    async load() {
      this.loading = true;
      this.error = '';
      try {
        const response = await fetch(`${window.__DASHBOARD_BASE_PATH__ || ''}/api/v1/changelog/highlights`);
        const body = await response.json().catch(() => ({}));
        if (!response.ok) throw new Error(apiError(body));
        this.from = body.from || '';
        this.to = body.to || '';
        this.sincePrevious = Boolean(body.since_previous);
        this.components = body.components || [];
      } catch (error) {
        this.error = error.message;
        this.components = [];
      } finally {
        this.loading = false;
      }
    },

    ensureLoaded() {
      if (this.requested) return;
      this.requested = true;
      this.load();
    },

    get title() {
      return this.sincePrevious ? t('settings.whatsnew.title', {version: this.to}) : t('settings.whatsnew.installed_title');
    },

    entryText(entry) {
      return entry.pr ? `${entry.text} (#${entry.pr})` : entry.text;
    },

    // The versions page lists every change, so the link goes there and
    // switches its view to "all". activeSettingsPage is not declared here:
    // the write reaches the settingsPage() scope this panel sits in.
    showAll() {
      this.activeSettingsPage = 'versions';
      window.dispatchEvent(new CustomEvent('versions-show-all'));
    },

    overviewHref() {
      return `${window.__DASHBOARD_BASE_PATH__ || ''}/`;
    },
  });

  const register = () => {
    if (!window.Alpine) return;
    Alpine.data('whatsNewPanel', whatsNewPanel);
  };
  if (window.Alpine) register(); else document.addEventListener('alpine:init', register, {once: true});
})();
