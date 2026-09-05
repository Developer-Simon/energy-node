// Test-Doubles fuer die beiden Alpine-Stores aus notify.js. Panel-Tests
// erzeugen ihre Komponente ueber factory() und haben deshalb kein $store -
// attachStores() haengt eines an und liefert die Doubles zum Pruefen zurueck.
//
// Der Toast-Double entdoppelt bewusst NICHT: ein Test, der zweimal dieselbe
// Meldung erwartet, soll zwei Eintraege sehen. Die Entdopplung selbst ist in
// notify.test.mjs geprueft.

export function fakeToastStore() {
  return {
    items: [],
    push(message, severity = 'info') {
      if (typeof message !== 'string' || message.trim() === '') return null;
      const id = this.items.length + 1;
      this.items.push({ id, message, severity, count: 1 });
      return id;
    },
    dismiss(id) { this.items = this.items.filter((item) => item.id !== id); },
    clear() { this.items = []; },
    get messages() { return this.items.map((item) => item.message); },
    get criticals() { return this.items.filter((item) => item.severity === 'critical').map((item) => item.message); },
    // Die zuletzt gepushte Meldung, optional auf einen Schweregrad gefiltert.
    last(severity) {
      const matching = severity ? this.items.filter((item) => item.severity === severity) : this.items;
      return matching.length ? matching[matching.length - 1].message : '';
    },
  };
}

// answer ist die Antwort des Nutzers: true = bestaetigt, false = abgebrochen.
// calls sammelt die uebergebenen Optionen, damit Tests Titel/danger pruefen.
export function fakeModalStore({ answer = true } = {}) {
  return {
    open: false,
    calls: [],
    answer,
    async confirm(options = {}) {
      this.calls.push(options);
      return this.answer;
    },
  };
}

export function attachStores(component, { toasts = fakeToastStore(), modal = fakeModalStore() } = {}) {
  component.$store = { toasts, modal };
  return { toasts, modal };
}
