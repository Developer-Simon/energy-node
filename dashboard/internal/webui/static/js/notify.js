// Zwei Alpine-Stores fuer alle Nutzermeldungen des Dashboards: 'toasts' fuer
// Ergebnisse und Fehler, 'modal' fuer Bestaetigungen.
//
// Bewusst ohne jeden DOM-Zugriff: jsdom 25 implementiert <dialog> nicht
// (showModal/close sind undefined), ein Store der showModal() selbst riefe
// waere in der node --test-Suite nicht pruefbar. Der Store haelt nur
// modal.open als Wahrheitswert; eine x-effect-Zeile in base.html uebersetzt
// ihn in showModal()/close().
(() => {
  const AUTO_DISMISS_MS = 8000;
  const MAX_VISIBLE_TOASTS = 5;
  const SEVERITIES = ['info', 'warning', 'critical'];

  const toasts = {
    items: [],
    // id -> Timer-Handle. Nur info-Toasts stehen hier drin.
    _timers: {},
    _nextId: 1,

    push(message, severity = 'info') {
      if (typeof message !== 'string' || message.trim() === '') return null;
      const level = SEVERITIES.includes(severity) ? severity : 'info';
      // Entdopplung: runtimeStatusPanel pollt alle 30 s und schreibt bei
      // Ausfall jedes Mal dieselbe Fehlermeldung - ohne Zusammenfassung
      // liefe die Liste in den 5er-Deckel und verdraengte alles andere.
      const existing = this.items.find((item) => item.message === message && item.severity === level);
      if (existing) {
        existing.count += 1;
        // Eine wiederholte Meldung ist noch aktuell: Timer neu starten.
        if (level === 'info') this._arm(existing.id);
        return existing.id;
      }
      while (this.items.length >= MAX_VISIBLE_TOASTS) {
        this._disarm(this.items[0].id);
        this.items.shift();
      }
      // Ein hochgezaehlter Zaehler, kein Zeitstempel: zwei Toasts in derselben
      // Millisekunde bekaemen sonst denselben x-for-Schluessel.
      const id = this._nextId++;
      this.items.push({ id, message, severity: level, count: 1 });
      if (level === 'info') this._arm(id);
      return id;
    },

    dismiss(id) {
      this._disarm(id);
      this.items = this.items.filter((item) => item.id !== id);
    },

    clear() {
      Object.keys(this._timers).forEach((key) => this._disarm(Number(key)));
      this.items = [];
    },

    _arm(id) {
      this._disarm(id);
      this._timers[id] = setTimeout(() => this.dismiss(id), AUTO_DISMISS_MS);
    },

    _disarm(id) {
      if (this._timers[id] === undefined) return;
      clearTimeout(this._timers[id]);
      delete this._timers[id];
    },
  };

  const modal = {
    open: false,
    title: '',
    body: '',
    confirmLabel: 'Bestätigen',
    cancelLabel: 'Abbrechen',
    danger: false,
    _resolve: null,

    confirm(options = {}) {
      // Kann nur durch einen Bug entstehen - alle Aufrufer stehen in einem
      // await innerhalb eines Klick-Handlers. Still false zurueckzugeben
      // waere gefaehrlich, deshalb die Warnung.
      if (this.open) {
        console.warn('notify: confirm() bei bereits offenem Modal - wird mit false beantwortet');
        return Promise.resolve(false);
      }
      this.title = options.title || '';
      this.body = options.body || '';
      this.confirmLabel = options.confirmLabel || 'Bestätigen';
      this.cancelLabel = options.cancelLabel || 'Abbrechen';
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
