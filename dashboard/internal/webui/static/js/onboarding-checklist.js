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
  // Schalter der aelteren Versionen und der frueheren Versionen in Neu
  // erkunden, gelten fuer die Sitzung.
  const OLDER_KEY = 'ob-explore-older';
  const ARCHIVE_KEY = 'ob-explore-archive';

  const t = (key, params) => window.I18n.t(key, params);
  const reducedMotion = () => Boolean(window.obMotion?.reducedMotion?.());
  // Ohne Zugriff auf den Sitzungsspeicher bleibt die Wahl bei geschlossen.
  const stored = (key) => {
    try { return window.sessionStorage.getItem(key) === '1'; } catch { return false; }
  };
  const remember = (key, open) => {
    try { window.sessionStorage.setItem(key, open ? '1' : '0'); } catch { /* siehe stored */ }
  };

  const obChecklist = () => ({
    reading: {},
    olderOpen: stored(OLDER_KEY),
    archiveOpen: stored(ARCHIVE_KEY),

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
    // Tour nachholen: auf jeder aelteren Karte mit einer Tour fuer dieses Geraet.
    // Der Lauf startet beim ersten Highlight mit Tour, all nimmt jede Tour des Release mit.
    catchUpItem(version) { return version.items.find((item) => !item.stale && this.s.tourSteps(item).length) || null; },
    catchUpShown(version) { return !version.current && Boolean(this.catchUpItem(version)); },
    catchUp(version) {
      const item = this.catchUpItem(version);
      if (item) this.run('explore', item.id, { all: true });
    },
    // Karte je Release, ohne das v (v0.10.33 heisst Version 0.10.33, 0.12.0 bleibt 0.12.0).
    versionLabel(version) { return t('onboarding.checklist.version', { version: version.version.replace(/^v/, '') }); },
    // Unterzeile eines Bereichs: Komponente und Version, etwa "Bootstrap 0.2.6". Ohne Version nur der Name.
    sectionHead(section) {
      const version = (section.version || '').replace(/^v/, '');
      return version ? `${section.label} ${version}` : section.label;
    },
    versionEyebrow(version) {
      if (version.archive) return t('onboarding.checklist.archive');
      return t(version.current ? 'onboarding.checklist.since_visit' : 'onboarding.checklist.older');
    },
    // Die Versionsseite zeigt den Changelog des neuesten Release der Karte (?release=, siehe versions.page.js).
    releaseHref(version) {
      return `${window.__DASHBOARD_BASE_PATH__ || ''}/?panel=settings&page=versions&release=${encodeURIComponent(version.release || version.version)}`;
    },

    // Aeltere Karten: ohne ungesehene Tour bleiben sie zu, der Schalter zeigt sie.
    // Alle Releases des neuesten Minors bleiben sichtbar, nur aeltere Minors fallen weg.
    hasUnseenTour(version) {
      return version.items.some((item) => !item.stale && !this.s.seen[item.id] && this.s.tourSteps(item).length);
    },
    // Archivierte Karten zeigt nur der eigene Schalter, nie der Filter der aelteren.
    shownByDefault(version) { return !version.archive && (version.minor === this.versions[0]?.minor || this.hasUnseenTour(version)); },
    // Jede Karte bleibt im DOM und wird mit x-show gezeigt oder ausgeblendet, damit der Wechsel animiert.
    cardShown(version) { return version.archive ? this.archiveOpen : (this.olderOpen || this.shownByDefault(version)); },
    get shownVersions() { return this.versions.filter((v) => this.cardShown(v)); },
    get olderHidden() { return this.versions.filter((v) => !v.archive && !this.shownByDefault(v)).length; },
    get archiveCount() { return this.versions.filter((v) => v.archive).length; },
    get archiveToggleShown() { return this.archiveCount > 0; },
    get archiveLabel() { return t(this.archiveOpen ? 'onboarding.checklist.archive_hide' : 'onboarding.checklist.archive_show', { n: this.archiveCount }); },
    toggleArchive() {
      this.archiveOpen = !this.archiveOpen;
      remember(ARCHIVE_KEY, this.archiveOpen);
    },
    // Gestaffelt beim Einblenden: 40 ms je ausgeblendeter Karte, hoechstens 5 mal.
    // Verglichen wird der Schluessel (release): versions wird bei jedem Zugriff neu gebaut.
    enterDelay(version) {
      const rank = Math.max(this.versions.filter((v) => !this.shownByDefault(v)).map((v) => v.release).indexOf(version.release), 0);
      return `${Math.min(rank, 5) * 40}ms`;
    },
    // Der Schalter steht, wenn es Karten aelterer Minors gibt, die er zeigen oder wieder ausblenden kann.
    get olderToggleShown() { return this.versions.some((v) => !v.archive && v.minor !== this.versions[0]?.minor) && (this.olderOpen || this.olderHidden > 0); },
    get olderLabel() { return t(this.olderOpen ? 'onboarding.checklist.older_hide' : 'onboarding.checklist.older_show', { n: this.olderHidden }); },
    toggleOlder() {
      this.olderOpen = !this.olderOpen;
      remember(OLDER_KEY, this.olderOpen);
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
