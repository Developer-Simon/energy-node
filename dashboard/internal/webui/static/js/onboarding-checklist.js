// Einstellungsseite Checkliste: Einrichten (nur Admins) und Neu erkunden
// (alle). Liest und schreibt denselben Store wie die Leiste. Jeder Punkt
// springt mit "Zeig es mir" in die Leiste ab genau diesem Punkt.
//
// Deep Link: ?panel=settings&page=checklist&mode=setup|explore&item=<id>
// waehlt den Modus und hebt den Punkt kurz hervor. Die Texte kommen aus dem
// Katalog (onboarding.checklist.*), die Schluessel der Schritte ueber I18n.t.
(() => {
  const SUB_KEY = {
    done: 'onboarding.checklist.sub_done',
    installer: 'onboarding.checklist.sub_installer',
    later: 'onboarding.checklist.sub_later',
  };
  const ACTION_KEY = {
    open: 'onboarding.checklist.action_show',
    later: 'onboarding.checklist.action_later',
    done: 'onboarding.checklist.action_change',
    installer: 'onboarding.checklist.action_change',
  };
  const NEWS_ACTION_KEY = {
    text: 'onboarding.checklist.action_read',
    seen: 'onboarding.checklist.action_again',
    new: 'onboarding.checklist.action_show',
    // Veraltet: der Knopf bleibt sichtbar, ist aber ausgegraut.
    stale: 'onboarding.checklist.action_show',
  };
  const FOCUS_MS = 1600;
  // Schalter der aelteren Versionen in Neu erkunden, gilt fuer die Sitzung.
  const OLDER_KEY = 'ob-explore-older';

  const t = (key, params) => window.I18n.t(key, params);
  const reducedMotion = () => Boolean(window.obMotion?.reducedMotion?.());
  // Ohne Zugriff auf den Sitzungsspeicher bleibt die Wahl bei geschlossen.
  const olderStored = () => {
    try { return window.sessionStorage.getItem(OLDER_KEY) === '1'; } catch { return false; }
  };
  const rememberOlder = (open) => {
    try { window.sessionStorage.setItem(OLDER_KEY, open ? '1' : '0'); } catch { /* siehe olderStored */ }
  };

  const obChecklist = () => ({
    reading: {},
    olderOpen: olderStored(),

    // Deep Link: Modus aus der Adresse, danach der Punkt.
    init() {
      const params = new URLSearchParams(window.location.search);
      const mode = params.get('mode');
      if (mode === 'setup' || mode === 'explore') this.s.checklistMode = mode;
      const item = params.get('item');
      if (item) this.focusItem(item);
    },

    // Wartet, bis der Punkt sichtbar ist (Seite oder Modus offen), scrollt
    // zu ihm und markiert ihn kurz (Klasse is-target, siehe onboarding.css).
    focusItem(id) {
      if (!window.obOnboarding) return;
      window.obOnboarding.waitFor(() => [...this.$root.querySelectorAll('[data-item]')]
        .find((el) => el.dataset.item === id)).then((el) => {
        if (!el) return;
        el.scrollIntoView?.({ block: 'center', behavior: reducedMotion() ? 'auto' : 'smooth' });
        el.classList.add('is-target');
        window.setTimeout(() => el.classList.remove('is-target'), FOCUS_MS);
      });
    },

    get s() { return this.$store.onboarding; },
    get mode() { return this.s.isAdmin ? this.s.checklistMode : 'explore'; },
    get counts() { return this.s.setupCounts; },
    get openCount() { return this.s.setupItems.filter((i) => ['open', 'later'].includes(this.s.stateOf(i.id))).length; },
    get firstId() { return this.s.setupItems[0]?.id; },
    get summaryNote() {
      if (!this.s.setupDone) return t('onboarding.checklist.note_pending');
      if (!this.openCount) return t('onboarding.checklist.note_done');
      if (this.openCount === 1) return t('onboarding.checklist.note_open_one');
      return t('onboarding.checklist.note_open_many', { n: this.openCount });
    },
    // Eine Karte je Bereich, wie die Eyebrows der Leiste. Was es auch im
    // Installer gibt, sagt die Zeile selbst.
    get groups() {
      const items = [...window.obSteps.installerOnly, ...this.s.setupItems];
      return window.obSteps.areas.map((area) => ({
        id: area.id,
        title: t(area.label),
        note: area.id === 'devices' ? t('onboarding.checklist.devices_note') : '',
        items: items.filter((item) => item.area === area.id),
      }));
    },
    get versions() { return this.s.exploreVersions; },
    // Karte oben in Neu erkunden: die aktuelle Version bestimmt, ob es Touren gibt.
    // Veraltete Touren zaehlen nicht, sie laufen nie.
    get exploreHasTour() {
      const current = this.versions.find((v) => v.current);
      return Boolean(current?.items.some((item) => !item.stale && this.s.tourSteps(item).length));
    },
    get exploreUnseen() { return this.s.unseenCount; },
    get exploreNote() {
      if (!this.exploreHasTour) return t('onboarding.checklist.explore_note_none');
      if (this.exploreUnseen) return t('onboarding.checklist.explore_note_unseen', { n: this.exploreUnseen });
      return t('onboarding.checklist.explore_note_all_seen');
    },
    // Tour starten, solange etwas ungesehen ist, sonst alle Touren noch einmal.
    exploreStart() { this.run('explore', null, { all: this.exploreUnseen === 0 }); },
    // Karte nach Minor (0.12). Ein altes dreiteiliges Format (0.12.0) zeigt ohne die 0.
    versionLabel(version) { return t('onboarding.checklist.version', { version: version.version.replace(/^(\d+\.\d+)\.0$/, '$1') }); },
    versionEyebrow(version) { return t(version.current ? 'onboarding.checklist.since_visit' : 'onboarding.checklist.older'); },
    // Die Versionsseite zeigt den Changelog des neuesten Release der Karte (?release=, siehe versions.page.js).
    releaseHref(version) {
      return `${window.__DASHBOARD_BASE_PATH__ || ''}/?panel=settings&page=versions&release=${encodeURIComponent(version.release || version.version)}`;
    },

    // Aeltere Karten: ohne ungesehene Tour bleiben sie zu, der Schalter zeigt sie.
    hasUnseenTour(version) {
      return version.items.some((item) => !item.stale && !this.s.seen[item.id] && this.s.tourSteps(item).length);
    },
    shownByDefault(version) { return version.current || this.hasUnseenTour(version); },
    get shownVersions() { return this.olderOpen ? this.versions : this.versions.filter((v) => this.shownByDefault(v)); },
    get olderHidden() { return this.versions.filter((v) => !this.shownByDefault(v)).length; },
    // Der Schalter steht, wenn es aeltere Karten gibt, die er zeigen oder wieder ausblenden kann.
    get olderToggleShown() { return this.versions.some((v) => !v.current) && (this.olderOpen || this.olderHidden > 0); },
    get olderLabel() { return t(this.olderOpen ? 'onboarding.checklist.older_hide' : 'onboarding.checklist.older_show', { n: this.olderHidden }); },
    toggleOlder() {
      this.olderOpen = !this.olderOpen;
      rememberOlder(this.olderOpen);
    },

    isInstallerOnly(item) { return window.obSteps.installerOnly.some((i) => i.id === item.id); },
    stateOf(item) { return this.s.stateOf(item.id); },
    titleOf(item) { return t(item.title); },
    subOf(item) {
      const state = this.stateOf(item);
      if (SUB_KEY[state]) return t(SUB_KEY[state]);
      if (item.sub) return t(item.sub);
      return t(item.installer ? 'onboarding.checklist.sub_installer_also' : 'onboarding.checklist.sub_here_only');
    },
    actionOf(item) {
      if (this.isInstallerOnly(item)) return '';
      const key = ACTION_KEY[this.stateOf(item)];
      return key ? t(key) : '';
    },
    act(item) {
      const state = this.stateOf(item);
      this.run(state === 'open' || state === 'later' ? 'setup' : 'change', item.id);
    },
    run(mode, from = null, options = {}) {
      this.s.queue = [];
      this.s.start(mode, from, options);
    },

    // Zustand eines Highlights: text, other-device, seen oder new.
    newsState(item) { return window.obModel.exploreState(item, this.s.seen, this.s.ctx()); },
    newsSub(item) {
      const state = this.newsState(item);
      if (state === 'other-device') return t(this.s.ctx().device === 'phone' ? 'onboarding.checklist.only_desktop' : 'onboarding.checklist.only_phone');
      if (state === 'seen') return t('onboarding.checklist.news_seen');
      if (state === 'new') return t('onboarding.checklist.news_new');
      if (state === 'stale') return t('onboarding.checklist.news_stale');
      return t(item.tour === false ? 'onboarding.checklist.news_no_tour' : 'onboarding.checklist.news_no_tour_rights');
    },
    isStale(item) { return this.newsState(item) === 'stale'; },
    // Ohne Eintrag (other-device) gibt es keinen Knopf: die Seite ist am
    // anderen Geraet zu zeigen.
    newsAction(item) {
      const key = NEWS_ACTION_KEY[this.newsState(item)];
      return key ? t(key) : '';
    },
    isReading(item) { return Boolean(this.reading[item.id]); },
    // Der Knopf des Highlights: Text lesen, eine Tour starten. Veraltet startet nichts.
    newsAct(item) {
      const state = this.newsState(item);
      if (state === 'other-device' || state === 'stale') return;
      if (state === 'text') {
        this.toggleReading(item);
        return;
      }
      this.run('explore', item.id);
    },
    // Details aufklappen. Ein Highlight nur mit Text gilt dabei als gesehen.
    toggleReading(item) {
      this.reading = { ...this.reading, [item.id]: !this.reading[item.id] };
      if (this.newsState(item) === 'text') this.s.save(item.id);
    },
  });

  const register = () => window.Alpine.data('obChecklist', obChecklist);
  // Panel-Skripte laden nach dem Start von Alpine, dann sofort registrieren.
  if (window.Alpine) register(); else document.addEventListener('alpine:init', register, { once: true });
})();
