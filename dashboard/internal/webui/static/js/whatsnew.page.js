// The highlights of the last update (GET /api/v1/changelog/highlights), shown
// in the dialog of base.html. The update bar opens it after a finished update,
// the versions page has a button for it.
(() => {
  const t = (key, params) => (window.I18n ? window.I18n.t(key, params) : key);
  const apiError = (body, fallbackKey) => (window.I18n ? window.I18n.error(body, fallbackKey) : (body && body.message) || fallbackKey || 'common.request_failed');
  const localized = (entry) => (window.I18n ? window.I18n.localized(entry.i18n, entry.text) : entry.text);

  const whatsNewPanel = () => ({
    loading: false,
    error: '',
    from: '',
    to: '',
    sincePrevious: false,
    components: [],
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

    // Loads freshly on every opening so the data is current, then shows the dialog.
    async openDialog(dialog) {
      await this.load();
      if (!dialog.open) dialog.showModal();
    },

    get title() {
      return this.sincePrevious ? t('settings.whatsnew.title', {version: this.to}) : t('settings.whatsnew.installed_title');
    },

    entryText(entry) {
      const text = localized(entry);
      return entry.pr ? `${text} (#${entry.pr})` : text;
    },

    // The versions page lists every change, so the link goes there and
    // starts in the "all" view (?all=1, read by versions.page.js).
    showAll(dialog) {
      if (dialog && dialog.open) dialog.close();
      this.navigate(`${window.__DASHBOARD_BASE_PATH__ || ''}/?panel=settings&page=versions&all=1`);
    },

    navigate(url) {
      window.location.assign(url);
    },
  });

  const register = () => {
    if (!window.Alpine) return;
    Alpine.data('whatsNewPanel', whatsNewPanel);
  };
  if (window.Alpine) register(); else document.addEventListener('alpine:init', register, {once: true});
})();
