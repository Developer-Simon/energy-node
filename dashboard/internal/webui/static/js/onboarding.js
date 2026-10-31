// Begleitende Leiste fuer Ersteinrichtung, Neu erkunden und Begruessung.
// Ein Store ($store.onboarding) haelt Fortschritt und Warteschlange, die
// Komponente obBar() zeigt den aktuellen Schritt, oeffnet seinen Ort auf der
// echten Seite und richtet das Spotlight darauf. Am Handy ist die Leiste ein
// Glasblatt unten, das man per Wischen auf eine schmale Kante einklappt, am
// Desktop ein Panel rechts, das zur anderen Seite ausweicht, wenn es das Ziel
// verdecken wuerde. Nichts blockiert: jeder Schritt hat Spaeter, die Leiste
// hat einen Weg hinaus, die Seite bleibt bedienbar.
//
// Fortschritt liegt serverseitig (/api/v1/onboarding). Gelesen wird einmal
// beim Start, geschrieben bei jeder Aenderung. Fehler sind still: einmal
// wiederholen, dann aufgeben. Die Leiste bleibt dabei bedienbar und offen
// bleibt, was offen war.
(() => {
  const PHONE = '(max-width: 759px)';
  const PEEK_PX = 84;
  const SLOP_PX = 6;
  const HISTORY = 5;
  const FIND_TIMEOUT_MS = 4000;

  const PROGRESS = '/api/v1/onboarding';
  const STEP = '/api/v1/onboarding/step';
  const ME = '/api/v1/onboarding/me';
  const SETUP_COMPLETE = '/api/v1/onboarding/setup/complete';
  const SESSION = '/api/v1/auth/session';

  const t = (key, params) => window.I18n.t(key, params);

  const isPhone = () => window.matchMedia(PHONE).matches;
  const reduced = () => window.obMotion.reducedMotion();

  // --- Netz -----------------------------------------------------------------

  // Ein Aufruf gegen den Dashboard-Pfad (Reverse-Proxy-Praefix aus base.html).
  async function request(url, options = {}) {
    const response = await fetch(`${window.__DASHBOARD_BASE_PATH__ || ''}${url}`, options);
    if (!response.ok) throw new Error(`HTTP ${response.status}`);
    return response.json().catch(() => ({}));
  }

  // Hoechstens zwei Versuche. Danach null: die Leiste laeuft ohne diesen Wert weiter.
  async function attempt(fn) {
    for (let i = 0; i < 2; i += 1) {
      try { return await fn(); } catch { /* naechster Versuch */ }
    }
    return null;
  }

  // Liest die Sitzung. Sie liefert das CSRF-Token fuer Schreibzugriffe und die
  // Rechte, die canOf() daraus baut. null, wenn der Aufruf scheitert.
  let csrfToken = '';
  async function readSession() {
    const session = await attempt(() => request(SESSION));
    csrfToken = session?.csrf_token || '';
    return session;
  }

  // Schreibt mit CSRF-Token. Das Token kommt aus der Sitzung und wird beim
  // Fehlschlag verworfen, damit der zweite Versuch ein frisches holt.
  async function send(method, url, body) {
    const ok = await attempt(async () => {
      try {
        if (!csrfToken) await readSession();
        if (!csrfToken) throw new Error('csrf'); // i18n-ignore
        const headers = { 'X-CSRF-Token': csrfToken };
        if (body !== undefined) headers['Content-Type'] = 'application/json';
        await request(url, { method, headers, body: body === undefined ? undefined : JSON.stringify(body) });
        return true;
      } catch (error) {
        csrfToken = '';
        throw error;
      }
    });
    return ok === true;
  }

  // Wartet, bis find() ein sichtbares Element liefert (Panels laden ihr
  // Fragment erst beim ersten Oeffnen). null nach Ablauf: der Schritt
  // entfaellt, statt zu haengen.
  function waitFor(find, timeout = FIND_TIMEOUT_MS) {
    const started = performance.now();
    return new Promise((resolve) => {
      const poll = () => {
        let el = null;
        try { el = find(); } catch { el = null; }
        if (el && window.obSpotlight.visibleRect(el)) return resolve(el);
        if (performance.now() - started > timeout) return resolve(null);
        requestAnimationFrame(poll);
      };
      poll();
    });
  }

  // Wartet, bis ein Element nicht mehr waechst (Karte klappt auf,
  // Fragment rendert nach), hoechstens 900 ms.
  function settle(el, timeout = 900) {
    const started = performance.now();
    let last = '';
    let calm = 0;
    return new Promise((resolve) => {
      const check = () => {
        const r = el.getBoundingClientRect();
        const key = `${Math.round(r.width)}x${Math.round(r.height)}`;
        calm = key === last ? calm + 1 : 0;
        last = key;
        if (calm >= 4 || performance.now() - started > timeout) return resolve();
        requestAnimationFrame(check);
      };
      check();
    });
  }

  // Ein Selektor kann mehrfach treffen (versteckte Kopien in anderen
  // Ansichten). Das erste sichtbare Element gewinnt.
  const resolveTarget = (target) => {
    if (typeof target === 'function') return target();
    const all = [...document.querySelectorAll(target)];
    return all.find((el) => window.obSpotlight.visibleRect(el)) || all[0] || null;
  };

  // Oeffnet den Ort eines Schritts ueber dieselben Wege wie ?panel=&page=:
  // Tab anklicken, Einstellungsseite setzen, dann prepare() (Karte aufklappen).
  async function openPlace(step) {
    if (step.panel) {
      // Mehrere Tabs koennen dasselbe Panel steuern (Layout-Seiten, Liste
      // unter "Mehr"). Der sichtbare gewinnt.
      const tabs = [...document.querySelectorAll(`[role="tab"][aria-controls="${step.panel}"]`)];
      const tab = tabs.find((t) => t.getBoundingClientRect().width > 0) || tabs[0];
      if (!tab) return false;
      // Seiten-Tabs melden aria-selected fuer ihre Seite, auch wenn ein
      // anderes Panel offen ist. Massgeblich ist .active.
      if (!tab.classList.contains('active')) tab.click();
    }
    if (step.page) {
      const panel = await waitFor(() => document.querySelector('#settings-panel .settings-side-list')?.closest('#settings-panel'));
      if (!panel) return false;
      window.Alpine.$data(panel).activeSettingsPage = step.page;
    }
    // prepare oeffnet, was das Ziel verdeckt (Reiter, Karte, Akkordeon).
    if (step.prepare) await step.prepare();
    return true;
  }

  // Rechte der Sitzung (Rechtenamen aus auth). Die Steps kennen einen Namen,
  // den es als Recht nicht gibt: edit_users ist user_management. Sonst gewaehrt
  // nichts etwas, was die Sitzung nicht hat.
  const NEED_RIGHT = { edit_users: 'user_management' };
  const canOf = (rights) => ({ has: (need) => rights.includes(NEED_RIGHT[need] || need) });

  const store = {
    role: 'guest',
    loaded: false,
    setupDone: false,
    status: {},
    seen: {},
    welcomed: false,
    run: null,
    queue: [],
    checklistMode: 'setup',
    // TEMP bis Task 9, dann aus der API: welche Dienste der Installer eingerichtet hat.
    installed: ['tailscale'],
    // Rechte aus der Sitzung, leer solange sie nicht gelesen ist.
    rights: [],
    // Zaehlt jeden Start. Ein neuer Lauf bei offener Leiste aendert weder
    // active noch zwingend index, nur dieser Zaehler.
    runId: 0,
    active: false,
    steps: [],
    index: 0,
    finished: null,

    // Liest den Fortschritt des angemeldeten Kontos. Gelingt es nicht, bleibt
    // der Store leer und die Leiste startet nichts.
    async load() {
      const [progress, session] = await Promise.all([attempt(() => request(PROGRESS)), readSession()]);
      this.rights = Array.isArray(session?.roles) ? session.roles : [];
      if (!progress) return false;
      this.role = progress.role || 'guest';
      this.setupDone = Boolean(progress.setup?.done);
      this.status = progress.setup?.steps || {};
      this.seen = Object.fromEntries((progress.user?.seen || []).map((id) => [id, true]));
      this.welcomed = Boolean(progress.user?.welcomed);
      this.run = progress.user?.run || null;
      this.queue = progress.user?.queue || [];
      this.loaded = true;
      return true;
    },

    // Kontext fuer obModel: Rolle, Rechte, Geraet, Dienste und Uebersetzer.
    ctx() {
      return {
        role: this.role,
        can: canOf(this.rights),
        device: window.obModel.deviceOf(window.innerWidth),
        installed: this.installed,
        t,
      };
    },

    get isAdmin() { return this.role === 'admin'; },
    get showGuestNote() { return this.role === 'guest' && !this.setupDone; },
    get step() { return this.steps[this.index] || null; },
    get mode() { return this.run?.mode || null; },

    // Zustand eines Einrichtungspunkts fuer Leiste und Checkliste.
    stateOf(id) { return this.status[id] || 'open'; },
    get setupItems() {
      // Schritte zu abgewaehlten Diensten entfallen. Die Umsetzung nimmt
      // .InstalledServices, der Entwurf die Dienste der Demo.
      return window.obSteps.setup.filter((step) => !step.needs || this.installed.includes(step.needs));
    },
    get setupCounts() {
      const items = [...window.obSteps.installerOnly, ...this.setupItems];
      const done = items.filter((item) => ['done', 'installer'].includes(this.stateOf(item.id))).length;
      return { done, total: items.length };
    },
    get unseenCount() {
      return window.obModel.unseenCount(window.obSteps.explore, this.seen, this.ctx());
    },
    // Schritte eines Highlights, die fuer Rolle und Geraet gelten (wie in obModel).
    tourSteps(item) {
      if (item.tour === false || !item.steps) return [];
      return item.steps.filter((s) => window.obModel.stepAllowed(s, this.ctx()));
    },
    // Merkt ein Highlight als gesehen, ohne Tour (Checkliste), und schreibt es ans Konto.
    save(id) {
      if (!id) return Promise.resolve(false);
      this.seen = { ...this.seen, [id]: true };
      return send('PUT', ME, { seen_add: [id] });
    },

    // Baut die Schrittliste eines Laufs. from = Id, ab der es losgeht. Das
    // Modell liefert Katalogschluessel, hier werden Titel und Texte uebersetzt.
    build(mode, from) {
      const { setup, explore, welcome } = window.obSteps;
      const areas = window.obSteps.areas.map((a) => ({ ...a, label: t(a.label) }));
      const steps = window.obModel.buildSteps({
        mode, from, setup, explore, welcome, areas, seen: this.seen, status: this.status, ctx: this.ctx(),
      });
      return steps.map((s) => ({ ...s, title: s.title ? t(s.title) : '', text: s.text ? t(s.text) : '' }));
    },

    start(mode, from = null, { index = 0 } = {}) {
      const steps = this.build(mode, from);
      if (!steps.length) {
        // Ein Lauf ohne Schritte wird nicht bei jedem Laden wieder versucht.
        this.run = null;
        void send('PUT', ME, { run: null });
        return this.advanceQueue();
      }
      this.steps = steps;
      this.index = Math.min(index, steps.length - 1);
      this.run = { mode, from, index: this.index };
      this.finished = null;
      this.active = true;
      this.runId += 1;
      this.persistRun();
      return true;
    },
    // Warteschlange: nie zwei Leisten gleichzeitig. Admin erst Einrichten,
    // dann Neu erkunden.
    enqueue(...modes) {
      this.queue = [...this.queue, ...modes.filter((m) => !this.queue.includes(m))];
      if (!this.active) this.advanceQueue();
      void send('PUT', ME, { queue: this.queue });
    },
    advanceQueue() {
      let started = false;
      while (this.queue.length && !started) {
        const mode = this.queue.shift();
        started = this.start(mode) === true;
      }
      void send('PUT', ME, { queue: this.queue });
      return started;
    },

    // Schreibt den Lauf mit aktuellem Schritt, damit Neuladen oder Login
    // beim selben Schritt weitergehen. null beendet den Lauf.
    persistRun() {
      if (this.run) this.run = { ...this.run, index: this.index };
      const run = this.run ? { mode: this.run.mode, from: this.run.from || '', index: this.index } : null;
      void send('PUT', ME, { run });
    },

    // Schreibt den Schritt. Einrichtungsschritte gehen an den Installations-
    // Fortschritt (nur Admin), Neu erkunden und Begruessung an das Konto.
    markStep(state) {
      const step = this.step;
      if (!step) return Promise.resolve(false);
      if (this.mode === 'setup' || this.mode === 'change') {
        if (!this.isAdmin) return Promise.resolve(false);
        // Erledigt bleibt erledigt, auch wenn es spaeter vorgemerkt wird.
        if (this.stateOf(step.id) === 'done' && state !== 'done') return Promise.resolve(false);
        this.status = { ...this.status, [step.id]: state };
        return send('PUT', STEP, { id: step.id, state });
      }
      this.seen = { ...this.seen, [step.id]: true };
      return send('PUT', ME, { seen_add: [step.id] });
    },

    // Einrichtung beim Server als abgeschlossen melden (nur Admin).
    completeSetup() {
      if (!this.isAdmin) return Promise.resolve(false);
      return send('POST', SETUP_COMPLETE);
    },

    finish() {
      const mode = this.mode;
      if (mode === 'setup') {
        this.setupDone = true;
        void this.completeSetup();
        const items = this.setupItems;
        this.finished = {
          mode,
          title: t('onboarding.finish.setup.title'),
          text: t('onboarding.finish.setup.text', {
            done: items.filter((s) => this.stateOf(s.id) === 'done').length,
            later: items.filter((s) => this.stateOf(s.id) === 'later').length,
          }),
        };
      } else if (mode === 'explore') {
        this.finished = { mode, title: t('onboarding.finish.explore.title'), text: t('onboarding.finish.explore.text') };
      } else if (mode === 'welcome') {
        this.finished = { mode, title: t('onboarding.finish.welcome.title'), text: t('onboarding.finish.welcome.text') };
      } else {
        this.finished = null;
        this.close();
        return;
      }
      this.run = null;
      this.persistRun();
    },
    // Weg hinaus, ohne Rueckfrage: alles bleibt in der Checkliste erreichbar.
    skip() {
      const mode = this.mode;
      if (mode === 'setup') {
        this.setupDone = true;
        void this.completeSetup();
      }
      this.close();
      const key = mode === 'setup' ? 'onboarding.toast.setup_skipped' : 'onboarding.toast.tour_ended';
      window.Alpine.store('toasts')?.push(t(key), 'info');
    },
    close() {
      this.active = false;
      this.run = null;
      this.finished = null;
      this.steps = [];
      void send('PUT', ME, { run: null });
      setTimeout(() => this.advanceQueue(), 450);
    },
  };

  // Der Store wird beim Start einmal gelesen. obBar wartet darauf, bevor es
  // einen Lauf aus dem Fortschritt wieder aufnimmt.
  let ready = Promise.resolve(false);

  const obBar = () => ({
    spot: null,
    sheet: null,
    target: null,
    revealedAgain: false,
    collapsed: false,
    left: false,
    done: false,
    lost: false,
    direction: 1,
    drag: null,
    cleanup: [],

    init() {
      this.spot = window.obSpotlight.create(this.$refs.spot);
      this.sheet = window.obMotion.createSpring({
        response: 0.35,
        damping: 1,
        onUpdate: ({ y }) => { this.$refs.bar.style.transform = y ? `translate3d(0, ${y}px, 0)` : ''; },
      });
      this.$watch('$store.onboarding.active', (on) => { if (!on) this.hideAll(); });
      this.$watch('$store.onboarding.runId', () => { this.direction = 1; this.show(); });
      this.$watch('$store.onboarding.index', (next, prev) => {
        this.direction = next >= prev ? 1 : -1;
        if (this.$store.onboarding.active) {
          this.show();
          this.s.persistRun();
        }
      });
      this.$watch('$store.onboarding.finished', (fin) => { if (fin) { this.spot.hide(); this.setCollapsed(false); this.swapIn(); } });
      window.matchMedia(PHONE).addEventListener('change', () => { this.sheet.set({ y: 0 }); this.collapsed = false; this.left = false; });
      ready.then((loaded) => {
        if (!loaded) return;
        const s = this.$store.onboarding;
        if (s.run) {
          const { mode, from, index } = s.run;
          // Nach Neuladen oder Login: beim selben Schritt weiter.
          setTimeout(() => s.start(mode, from || null, { index }), 600);
        } else if (s.queue.length) {
          setTimeout(() => s.advanceQueue(), 600);
        }
      });
    },

    get s() { return this.$store.onboarding; },
    get step() { return this.s.step; },
    get last() { return this.s.index >= this.s.steps.length - 1; },
    get primaryLabel() { return this.last ? t('onboarding.bar.finish') : t('onboarding.bar.next'); },
    get checklistLabel() { return this.s.mode === 'explore' ? t('onboarding.bar.checklist_explore') : t('onboarding.bar.checklist_setup'); },
    get closeLabel() { return this.s.mode === 'setup' ? t('onboarding.bar.skip_setup') : t('onboarding.bar.end_tour'); },
    get dotTransform() { return `translateX(${(this.step?.dot || 0) * 12}px)`; },
    dotState(i) {
      const step = this.step;
      if (!step || (this.s.mode !== 'setup')) return '';
      const peer = this.s.steps.filter((x) => x.area === step.area)[i];
      return peer ? this.s.stateOf(peer.id) : '';
    },

    async show() {
      const step = this.step;
      if (!step) return;
      this.done = this.s.mode !== 'explore' && this.s.mode !== 'welcome' && this.s.stateOf(step.id) === 'done' && this.s.mode !== 'change';
      this.lost = false;
      this.revealedAgain = false;
      // Neuer Schritt: Blatt wieder aufziehen, damit der Text zu lesen ist.
      // Verdeckt es danach das Ziel, klappt avoid() es erneut ein.
      if (this.collapsed) this.setCollapsed(false);
      this.swapIn();
      this.detach();
      this.spot.hold();
      const placed = await openPlace(step);
      let el = placed ? await waitFor(() => resolveTarget(step.target)) : null;
      if (el) await settle(el);
      if (step !== this.step) return;
      // Waehrend des Wartens kann ein Live-Tausch das Element ersetzt haben.
      if (el && !el.isConnected) el = await waitFor(() => resolveTarget(step.target), 1500);
      if (step !== this.step) return;
      if (!el) {
        // Ziel fehlt (anderer Dienst, Recht, Layout): Schritt ueberspringen.
        this.spot.hide();
        if (!this.last) this.s.index += 1; else this.s.finish();
        return;
      }
      this.target = el;
      this.reveal(el);
      this.spot.show(el, {
        onLost: () => { this.lost = true; },
        onFound: () => { this.lost = false; },
        resolve: () => resolveTarget(step.target),
      });
      this.watchDone(el, step);
      this.avoidWhenStill(step);
    },

    // Ziel in den freien Bereich holen: am Handy oberhalb der Leiste.
    reveal(el) {
      const rect = el.getBoundingClientRect();
      const bar = this.$refs.bar.getBoundingClientRect();
      // Abstand zur Leiste inklusive Ring (8 px) und etwas Luft.
      const bottom = isPhone() ? window.innerHeight - bar.height - 28 : window.innerHeight - 16;
      const top = 72;
      if (rect.top >= top && rect.bottom <= bottom) return;
      el.style.scrollMarginTop = `${top}px`;
      el.style.scrollMarginBottom = isPhone() ? `${bar.height + 36}px` : '24px';
      el.scrollIntoView({ block: rect.height > bottom - top ? 'start' : 'nearest', behavior: reduced() ? 'auto' : 'smooth' });
    },

    // Erst entscheiden, wenn Scrollen und Feder zur Ruhe gekommen sind.
    avoidWhenStill(step) {
      let last = null;
      let calm = 0;
      const started = performance.now();
      const check = () => {
        if (step !== this.step) return;
        const r = this.spot.rect();
        const key = r ? `${Math.round(r.top)}:${Math.round(r.left)}` : '';
        calm = key === last ? calm + 1 : 0;
        last = key;
        if (calm >= 6 || performance.now() - started > 2000) return this.avoid();
        requestAnimationFrame(check);
      };
      requestAnimationFrame(check);
    },

    // Verdeckt die Leiste das Ziel, weicht sie aus: am Desktop zur anderen
    // Seite, am Handy klappt sie auf die Kante ein.
    avoid() {
      const target = this.spot.rect();
      if (!target) return;
      // Ein Live-Tausch kann das Ziel nach dem Scrollen an den Rand
      // geschoben haben (neues Element ohne Scroll-Marge): einmal nachholen.
      if (target.top < 48 && !this.revealedAgain) {
        this.revealedAgain = true;
        this.reveal(this.spot.element());
        this.avoidWhenStill(this.step);
        return;
      }
      const bar = this.$refs.bar.getBoundingClientRect();
      const overlaps = !(target.right < bar.left || target.left > bar.right || target.bottom < bar.top || target.top > bar.bottom);
      if (isPhone()) {
        if (overlaps && !this.collapsed) this.setCollapsed(true);
      } else if (overlaps) {
        this.left = !this.left;
      }
    },

    watchDone(el, step) {
      if (step.check !== 'change') return;
      // watch: groesserer Bereich als das Ziel (Rollen: Ziel ist die erste
      // Zeile, abgehakt wird bei jeder Zuordnung).
      const area = step.watch ? resolveTarget(step.watch) || el : el;
      const handler = () => {
        if (this.done) return;
        this.done = true;
        this.s.markStep('done');
        this.spot.done();
        navigator.vibrate?.(8);
      };
      area.addEventListener('change', handler, true);
      this.cleanup.push(() => area.removeEventListener('change', handler, true));
    },
    detach() { this.cleanup.splice(0).forEach((fn) => fn()); },

    // Inhalt wechselt: neuer Text gleitet in Laufrichtung herein.
    swapIn() {
      const body = this.$refs.body;
      if (!body?.animate) return;
      const from = reduced() ? { opacity: 0 } : { opacity: 0, transform: `translateX(${14 * this.direction}px)` };
      body.animate([from, { opacity: 1, transform: 'none' }], { duration: reduced() ? 160 : 240, easing: 'cubic-bezier(0.23, 1, 0.32, 1)' });
    },

    next() {
      if (this.s.finished) return this.s.close();
      this.s.markStep('done');
      if (this.last) return this.s.finish();
      this.s.index += 1;
    },
    later() {
      this.s.markStep('later');
      if (this.last) return this.s.finish();
      this.s.index += 1;
    },
    back() { if (this.s.index > 0) this.s.index -= 1; },
    again() { this.lost = false; this.show(); },
    checklist() {
      const mode = this.s.mode === 'setup' || this.s.mode === 'change' || this.s.finished?.mode === 'setup' ? 'setup' : 'explore';
      this.s.checklistMode = this.s.isAdmin ? mode : 'explore';
      if (this.s.finished) this.s.close();
      this.setCollapsed(true);
      openPlace({ panel: 'settings-panel', page: 'checklist' });
    },
    hideAll() {
      this.detach();
      this.spot.hide();
      this.collapsed = false;
      this.left = false;
      this.sheet.set({ y: 0 });
    },

    // --- Blatt am Handy: Ziehen mit 1:1-Verfolgung, Gummiband an beiden
    // Enden, beim Loslassen entscheidet der projizierte Endpunkt.
    collapsedY() { return Math.max(0, this.$refs.bar.offsetHeight - PEEK_PX); },
    setCollapsed(on, velocity = 0) {
      this.collapsed = on;
      if (!isPhone()) return;
      this.sheet.to({ y: on ? this.collapsedY() : 0 }, { velocity: { y: velocity } });
    },
    toggleCollapsed() { if (!this.drag?.moved) this.setCollapsed(!this.collapsed); },
    dragStart(event) {
      if (!isPhone() || event.button > 0) return;
      this.sheet.stop();
      const y = this.sheet.values?.y || 0;
      this.drag = { startY: event.clientY, base: y, y, moved: false, history: [{ v: event.clientY, t: event.timeStamp }] };
      event.currentTarget.setPointerCapture?.(event.pointerId);
    },
    dragMove(event) {
      const d = this.drag;
      if (!d) return;
      const dy = event.clientY - d.startY;
      if (!d.moved && Math.abs(dy) < SLOP_PX) return;
      d.moved = true;
      const max = this.collapsedY();
      const height = this.$refs.bar.offsetHeight || 400;
      let y = d.base + dy;
      if (y < 0) y = window.obMotion.rubberband(y, height);
      else if (y > max) y = max + window.obMotion.rubberband(y - max, height);
      d.y = y;
      d.history = [...d.history, { v: event.clientY, t: event.timeStamp }].slice(-HISTORY);
      this.sheet.set({ y });
    },
    dragEnd() {
      const d = this.drag;
      if (!d) return;
      setTimeout(() => { this.drag = null; }, 0);
      if (!d.moved) return;
      const velocity = window.obMotion.releaseVelocity(d.history);
      const landing = d.y + window.obMotion.project(velocity);
      this.setCollapsed(landing > this.collapsedY() / 2, velocity);
    },
  });

  document.addEventListener('alpine:init', () => {
    window.Alpine.store('onboarding', store);
    window.Alpine.data('obBar', obBar);
    ready = window.Alpine.store('onboarding').load();
  });

  // Marker am Einstellungs-Tab, solange es ungesehene Neuerungen gibt. Das
  // Element steht im Template, hier schaltet nur seine Sichtbarkeit.
  document.addEventListener('alpine:initialized', () => {
    const marker = document.querySelector('#tab-settings .ob-marker--tab');
    if (!marker) return;
    window.Alpine.effect(() => { marker.hidden = !window.Alpine.store('onboarding').unseenCount; });
  });

  window.obOnboarding = { openPlace, waitFor, resolveTarget };
})();
