// Zwei Alpine-Stores fuer alle Nutzermeldungen des Dashboards: 'toasts' fuer
// Ergebnisse und Fehler, 'modal' fuer Bestaetigungen.
//
// Bewusst ohne jeden DOM-Zugriff: jsdom 25 implementiert <dialog> nicht
// (showModal/close sind undefined), ein Store der showModal() selbst riefe
// waere in der node --test-Suite nicht pruefbar. Der Store haelt nur
// modal.open als Wahrheitswert; eine x-effect-Zeile in base.html uebersetzt
// ihn in showModal()/close().
(() => {
  const t = (key, params) => (window.I18n ? window.I18n.t(key, params) : key);
  // Jede Schwere bleibt doppelt so lange stehen wie die vorige. Fehler laufen
  // ebenfalls ab: wer weggeschaut hat, findet sie ueber die Haarlinie am
  // unteren Rand zumindest noch eine halbe Minute vor.
  const DISMISS_MS = { info: 8000, warning: 16000, critical: 32000 };
  // Nach dem Pausieren (Hover, Fokus, Wischen) bleibt mindestens so viel
  // Restzeit, sonst verschwindet ein Toast direkt unter der Maus.
  const MIN_REMAINING_MS = 1500;
  // So lange laeuft die Austrittsanimation (base.css), bevor der Eintrag
  // wirklich aus der Liste faellt. Etwas laenger als die laengste Transition.
  const EXIT_MS = 520;
  const MAX_VISIBLE_TOASTS = 5;
  const SEVERITIES = ['info', 'warning', 'critical'];

  // Nur eine Aktion mit run-Funktion zaehlt. Ohne Label gilt das Standard-Label.
  function normalizeAction(action) {
    if (!action || typeof action.run !== 'function') return null;
    const label = typeof action.label === 'string' && action.label.trim() !== ''
      ? action.label
      : t('common.undo');
    return { label, run: action.run };
  }

  const toasts = {
    items: [],
    // id -> Timer-Handle fuer das automatische Ausblenden.
    _timers: {},
    // id -> Timer-Handle fuer das Entfernen nach der Austrittsanimation.
    _exits: {},
    // id -> Set der Gruende (hover, focus, drag), aus denen der Ablauf steht.
    _holds: {},
    _nextId: 1,

    push(message, severity = 'info', options = {}) {
      if (typeof message !== 'string' || message.trim() === '') return null;
      const level = SEVERITIES.includes(severity) ? severity : 'info';
      const action = normalizeAction(options && options.action);
      // Entdopplung: runtimeStatusPanel pollt alle 30 s und schreibt bei
      // Ausfall jedes Mal dieselbe Fehlermeldung - ohne Zusammenfassung
      // liefe die Liste in den 5er-Deckel und verdraengte alles andere.
      // Ein Toast, der gerade geht, zaehlt nicht mehr: die Meldung bekommt
      // dann einen frischen Eintrag. Ein Toast mit Aktion geht nie in einen
      // anderen ueber, auch nicht umgekehrt: die Aktion gehoert zu genau
      // diesem Ereignis.
      const existing = this.items.find((item) => (
        item.state === 'open' && item.message === message && item.severity === level
        && !item.action && !action
      ));
      if (existing) {
        existing.count += 1;
        // run zaehlt Neustarts des Ablaufs: die Haarlinie im Template haengt
        // daran und beginnt dadurch wieder von vorn.
        existing.run += 1;
        // Eine wiederholte Meldung ist noch aktuell: Ablauf neu starten.
        this._arm(existing.id);
        return existing.id;
      }
      while (this.items.length >= MAX_VISIBLE_TOASTS) {
        this._remove(this.items[0].id);
      }
      // Ein hochgezaehlter Zaehler, kein Zeitstempel: zwei Toasts in derselben
      // Millisekunde bekaemen sonst denselben x-for-Schluessel.
      const id = this._nextId++;
      this.items.push({
        id,
        message,
        severity: level,
        count: 1,
        run: 0,
        // open | leaving | swiped. Die letzten beiden steuern die
        // Austrittsanimation, der Eintrag bleibt dafuer noch in items.
        state: 'open',
        paused: false,
        durationMs: DISMISS_MS[level],
        action,
      });
      this._arm(id);
      return id;
    },

    // Fuehrt die Aktion des Toasts einmal aus und schliesst ihn. Nach dem
    // Schliessen ist der Eintrag im Zustand leaving, ein zweiter Aufruf
    // findet also nichts Offenes mehr.
    runAction(id) {
      const item = this.items.find((entry) => entry.id === id);
      if (!item || !item.action || item.state !== 'open') return;
      const { run } = item.action;
      this.dismiss(id);
      run();
    },

    // via: 'swipe' fuer das Wegwischen, sonst faehrt der Toast den Eintrittsweg
    // zurueck. Doppelte Aufrufe (Timer + Klick im selben Frame) sind harmlos.
    dismiss(id, via) {
      const item = this.items.find((entry) => entry.id === id);
      if (!item || item.state !== 'open') return;
      this._disarm(id);
      item.state = via === 'swipe' ? 'swiped' : 'leaving';
      this._exits[id] = setTimeout(() => this._remove(id), EXIT_MS);
    },

    clear() {
      Object.keys(this._timers).forEach((key) => this._disarm(Number(key)));
      Object.keys(this._exits).forEach((key) => clearTimeout(this._exits[key]));
      this._exits = {};
      this.items = [];
    },

    // Der Ablauf steht, solange mindestens ein Grund aktiv ist. Beim ersten
    // Grund wird die Restzeit festgehalten, beim letzten laeuft sie weiter.
    hold(id, reason) {
      const item = this.items.find((entry) => entry.id === id);
      if (!item || item.state !== 'open') return;
      const reasons = this._holds[id] || (this._holds[id] = new Set());
      const wasRunning = reasons.size === 0;
      reasons.add(reason);
      if (!wasRunning) return;
      this._disarm(id);
      item.remainingMs = Math.max(0, item.remainingMs - (Date.now() - item.armedAt));
      item.paused = true;
    },

    release(id, reason) {
      const item = this.items.find((entry) => entry.id === id);
      const reasons = this._holds[id];
      if (!item || !reasons || !reasons.delete(reason) || reasons.size > 0) return;
      delete this._holds[id];
      if (item.state !== 'open') return;
      item.paused = false;
      this._schedule(item, Math.max(item.remainingMs, MIN_REMAINING_MS));
    },

    _arm(id) {
      const item = this.items.find((entry) => entry.id === id);
      if (!item) return;
      this._disarm(id);
      // Eine Wiederholung unter der Maus laeuft erst nach dem Verlassen los.
      item.remainingMs = item.durationMs;
      item.paused = this._holds[id] !== undefined && this._holds[id].size > 0;
      if (!item.paused) this._schedule(item, item.durationMs);
    },

    _schedule(item, ms) {
      item.remainingMs = ms;
      item.armedAt = Date.now();
      this._timers[item.id] = setTimeout(() => this.dismiss(item.id), ms);
    },

    _disarm(id) {
      if (this._timers[id] === undefined) return;
      clearTimeout(this._timers[id]);
      delete this._timers[id];
    },

    _remove(id) {
      this._disarm(id);
      clearTimeout(this._exits[id]);
      delete this._exits[id];
      delete this._holds[id];
      this.items = this.items.filter((item) => item.id !== id);
    },
  };

  const modal = {
    open: false,
    title: '',
    body: '',
    confirmLabel: '',
    cancelLabel: '',
    danger: false,
    _resolve: null,

    confirm(options = {}) {
      // Kann nur durch einen Bug entstehen - alle Aufrufer stehen in einem
      // await innerhalb eines Klick-Handlers. Still false zurueckzugeben
      // waere gefaehrlich, deshalb die Warnung.
      if (this.open) {
        console.warn('notify: confirm() bei bereits offenem Modal - wird mit false beantwortet'); // i18n-ignore: debug message
        return Promise.resolve(false);
      }
      this.title = options.title || '';
      this.body = options.body || '';
      this.confirmLabel = options.confirmLabel || t('common.confirm');
      this.cancelLabel = options.cancelLabel || t('common.cancel');
      this.danger = options.danger === true;
      this.open = true;
      return new Promise((resolve) => { this._resolve = resolve; });
    },

    // Wird von allen vier Ausgaengen gerufen: Bestaetigen (true), Abbrechen,
    // Escape (cancel-Event) und Backdrop-Klick (jeweils false). Escape
    // schliesst den Dialog nativ, bevor der Handler laeuft - deshalb ist ein
    // zweiter Aufruf zum selben Vorgang ausdruecklich harmlos.
    resolve(value) {
      const done = this._resolve;
      this._resolve = null;
      this.open = false;
      if (done) done(value === true);
    },
  };

  document.addEventListener('alpine:init', () => {
    window.Alpine.store('toasts', toasts);
    window.Alpine.store('modal', modal);
  });

  window.__notifyStores = { toasts, modal };
})();
