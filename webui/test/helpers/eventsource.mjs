// EventSource-Attrappe: merkt sich jede geoeffnete Quelle; der Test loest
// Oeffnen, Ereignisse und Fehler aus.
export function installEventSource(window) {
  const sources = [];
  class FakeEventSource {
    constructor(url) {
      this.url = url;
      this.listeners = {};
      this.closed = false;
      this.onopen = null;
      this.onerror = null;
      sources.push(this);
    }
    addEventListener(type, fn) {
      (this.listeners[type] ||= []).push(fn);
    }
    close() {
      this.closed = true;
    }
    open() {
      if (this.onopen) this.onopen({});
    }
    emit(type, seq, data) {
      for (const fn of this.listeners[type] || []) {
        fn({ type, lastEventId: String(seq), data: JSON.stringify(data) });
      }
    }
    fail() {
      if (this.onerror) this.onerror({});
    }
  }
  window.EventSource = FakeEventSource;
  return sources;
}

// manualTimers: setTimeout ohne Uhr - der Test ruft run() auf.
export function manualTimers() {
  const pending = [];
  return {
    pending,
    setTimeout: (fn, delay) => { pending.push({ fn, delay }); return pending.length; },
    clearTimeout: (id) => { if (pending[id - 1]) pending[id - 1].fn = () => {}; },
    run: () => { const next = pending.shift(); if (next) next.fn(); return next; },
  };
}
