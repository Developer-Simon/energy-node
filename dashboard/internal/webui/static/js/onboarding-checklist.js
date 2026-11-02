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
  // Highlight ohne Tour (wie exploreState): ohne Schritte oder mit tour: false.
  const noTour = (item) => item.tour === false || !(item.steps || []).length;
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
    // Alle Touren der Karte gesehen: der Knopf wird dann zum neutralen Wiederholen. Gleiche Auswahl wie unseenCount.
    catchUpDone(version) {
      const items = version.items.filter((item) => !item.stale && this.s.tourSteps(item).length);
      return items.length > 0 && items.every((item) => this.newsState(item) === 'seen');
    },
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

    // Drei Stufen, eine baut auf der anderen auf: Die Releases der beiden neuesten
    // Minors sind immer zu sehen. Weitere zeigt die aelteren Minors des Fensters,
    // erst danach zeigt Weiter zurueck die archivierten Releases. Weitere einklappen
    // klappt beide Stufen wieder ein.
    get visibleMinors() {
      const minors = [];
      for (const v of this.versions) {
        if (!v.archive && !minors.includes(v.minor)) minors.push(v.minor);
      }
      return minors.slice(0, 2);
    },
    shownByDefault(version) { return !version.archive && this.visibleMinors.includes(version.minor); },
    // Ohne aeltere Minors im Fenster gibt es keine erste Stufe, dann ist das Archiv direkt erreichbar.
    get archiveReachable() { return this.olderOpen || this.olderHidden === 0; },
    // Jede Karte bleibt im DOM und wird mit x-show gezeigt oder ausgeblendet, damit der Wechsel animiert.
    cardShown(version) {
      if (version.archive) return this.archiveOpen && this.archiveReachable;
      return this.olderOpen || this.shownByDefault(version);
    },
    get shownVersions() { return this.versions.filter((v) => this.cardShown(v)); },
    get olderHidden() { return this.versions.filter((v) => !v.archive && !this.shownByDefault(v)).length; },
    get archiveCount() { return this.versions.filter((v) => v.archive).length; },
    get archiveToggleShown() { return this.archiveCount > 0 && this.archiveReachable; },
    get archiveLabel() { return t(this.archiveOpen ? 'onboarding.checklist.archive_hide' : 'onboarding.checklist.archive_show', { n: this.archiveCount }); },
    toggleArchive() {
      this.archiveOpen = !this.archiveOpen;
      remember(ARCHIVE_KEY, this.archiveOpen);
    },
    // Gestaffelt beim Einblenden: 40 ms je Karte der Stufe, die gerade aufgeht, hoechstens 5 mal.
    // Verglichen wird der Schluessel (release): versions wird bei jedem Zugriff neu gebaut.
    enterDelay(version) {
      const tier = this.versions.filter((v) => !this.shownByDefault(v) && Boolean(v.archive) === Boolean(version.archive));
      const rank = Math.max(tier.map((v) => v.release).indexOf(version.release), 0);
      return `${Math.min(rank, 5) * 40}ms`;
    },
    get olderToggleShown() { return this.olderHidden > 0; },
    get olderLabel() { return t(this.olderOpen ? 'onboarding.checklist.older_hide' : 'onboarding.checklist.older_show', { n: this.olderHidden }); },
    // Weitere einklappen klappt auch das Archiv wieder ein.
    toggleOlder() {
      this.olderOpen = !this.olderOpen;
      remember(OLDER_KEY, this.olderOpen);
      if (!this.olderOpen && this.archiveOpen) {
        this.archiveOpen = false;
        remember(ARCHIVE_KEY, false);
      }
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
      // Ohne Tour und ohne Details ist der eine Satz die Unterzeile, in jedem Zustand.
      if (noTour(item) && !item.details) return t(item.text);
      if (noTour(item)) return t(item.tour === false ? 'onboarding.checklist.news_no_tour' : 'onboarding.checklist.news_no_tour_rights');
      if (state === 'seen') return t('onboarding.checklist.news_seen');
      if (state === 'new') return t('onboarding.checklist.news_new');
      return t(this.replacementStart(item) ? 'onboarding.checklist.news_replaced' : 'onboarding.checklist.news_stale');
    },
    isStale(item) { return this.newsState(item) === 'stale'; },
    // Ersatzschritt einer veralteten Tour: Highlight und Position des Schritts in
    // tourSteps (gerätegefiltert). Der Server zaehlt seinen Index unter den Schritten,
    // die der Nutzer gehen darf, das ist dieselbe Liste wie item.steps. Fehlt das
    // Ersatz-Item oder der Schritt fuer dieses Geraet, gibt es keinen.
    replacementStart(item) {
      const replacement = item.replacement;
      if (!replacement) return null;
      const ersatz = this.versions.flatMap((v) => v.items).find((i) => i.id === replacement.id);
      const step = ersatz?.steps[replacement.index];
      if (!step) return null;
      const index = this.s.tourSteps(ersatz).indexOf(step);
      return index < 0 ? null : { id: ersatz.id, index };
    },
    // Ohne Eintrag (other-device) gibt es keinen Knopf: die Seite ist am
    // anderen Geraet zu zeigen.
    newsAction(item) {
      if (this.isStale(item) && this.replacementStart(item)) return t('onboarding.checklist.action_replacement');
      // Ohne Tour: mit Details wird gelesen, ohne Details nur als gelesen markiert, danach gar nichts.
      if (noTour(item)) {
        if (item.details) return t('onboarding.checklist.action_read');
        return this.newsState(item) === 'seen' ? '' : t('onboarding.checklist.action_mark_read');
      }
      const key = NEWS_ACTION_KEY[this.newsState(item)];
      return key ? t(key) : '';
    },
    isReading(item) { return Boolean(this.reading[item.id]); },
    // Ohne Tour und ohne Details: kein Aufklappen, nur der Knopf zum Markieren.
    isMarkOnly(item) { return noTour(item) && !item.details; },
    // Der Knopf des Highlights: Text lesen, eine Tour starten. Veraltet startet nur
    // die Tour an ihrer neuen Stelle, sonst nichts.
    newsAct(item) {
      const state = this.newsState(item);
      if (state === 'other-device') return;
      if (noTour(item)) {
        if (item.details) this.toggleReading(item);
        else if (state === 'text') this.s.save(item.id);
        return;
      }
      if (state === 'stale') {
        const start = this.replacementStart(item);
        if (start) this.run('explore', start.id, { index: start.index });
        return;
      }
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
