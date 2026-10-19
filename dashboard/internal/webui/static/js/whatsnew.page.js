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

    // All entries of a component in one list, the ones with details first.
    // Array.prototype.sort is stable, so the order inside each group is kept.
    entriesOf(component) {
      const entries = (component.releases || []).flatMap((release) => release.entries || []);
      return entries.sort((a, b) => Number(Boolean(b.details)) - Number(Boolean(a.details)));
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

    // The detail text of an entry in the page language, English as fallback.
    detailText(entry) {
      const text = entry.details.text;
      return window.I18n ? window.I18n.localized(text, text.en) : text.en;
    },

    // Position and size of an image inside its frame. crop is [x, y, w, h] as
    // fractions of the image; without it, or with cropped false, the whole
    // image fills the frame.
    imageFrame(image, cropped) {
      const [x, y, w, h] = cropped && image.crop ? image.crop : [0, 0, 1, 1];
      return {
        aspect: `${w * image.width} / ${h * image.height}`,
        left: `${(-x / w) * 100}%`,
        top: `${(-y / h) * 100}%`,
        width: `${100 / w}%`,
        height: `${100 / h}%`,
      };
    },

    // Scrolls an opened row into view once the open animation has run.
    onToggle(event) {
      const row = event.target;
      if (!row.open) return;
      const reduced = Boolean(window.matchMedia && window.matchMedia('(prefers-reduced-motion: reduce)').matches);
      setTimeout(() => row.scrollIntoView({block: 'nearest', behavior: reduced ? 'auto' : 'smooth'}), 330);
    },
  });

  const register = () => {
    if (!window.Alpine) return;
    Alpine.data('whatsNewPanel', whatsNewPanel);
  };
  if (window.Alpine) register(); else document.addEventListener('alpine:init', register, {once: true});
})();
