// Reine Rechnung fuer das Zielband des Assistenten. Keine DOM-Ausgabe.
// Fehler je Minute: Istwert minus Median der Nachbarminuten, gerundet auf 1 W.
// Das Gummiband kommt aus automation-sheet.js, hier nur durchgereicht.
(() => {
  const WINDOW = 5;
  const LIMITS = Object.freeze({
    target: Object.freeze({ min: -50, max: 150 }),
    tol: Object.freeze({ min: 5, max: 60 }),
    axis: Object.freeze({ min: -120, max: 160 }),
  });
  const REDUCED = '(prefers-reduced-motion: reduce)';

  const median = (values) => {
    const s = values.slice().sort((a, b) => a - b);
    const m = s.length >> 1;
    return s.length % 2 ? s[m] : (s[m - 1] + s[m]) / 2;
  };

  // rows: eine Zeile je Minute, { ts, avg }, sortiert wie geliefert.
  // Unter 60 Zeilen gibt es keine verlaessliche Basis, dann leer.
  function errorsFromRows(rows) {
    if (!rows || rows.length < 60) return [];
    const avgs = rows.map((r) => r.avg);
    return avgs.map((v, i) => {
      const win = avgs.slice(Math.max(0, i - WINDOW), Math.min(avgs.length, i + WINDOW + 1));
      return Math.round(v - median(win)) || 0;
    });
  }

  // Energie je Minute: W * 60 s, in kWh (3.6e6 J je kWh).
  function bandStats(errors, target, tol) {
    if (!errors || errors.length === 0) return null;
    let fed = 0;
    let drawn = 0;
    let moves = 0;
    for (const e of errors) {
      const n = target + e;
      fed += Math.max(0, -n);
      drawn += Math.max(0, n);
      if (Math.abs(e) > tol) moves += 1;
    }
    const perMinute = 60 / 3.6e6;
    return {
      fedKwh: fed * perMinute,
      drawnKwh: drawn * perMinute,
      movesPerHour: moves / (errors.length / 60),
    };
  }

  // Zaehlt die Fehler in Klassen von -160 bis +160 W. Werte ausserhalb fallen weg.
  function histogram(errors, binW = 5) {
    const count = Math.round(320 / binW);
    const bins = Array.from({ length: count }, (_, i) => ({ offset: -160 + i * binW, count: 0 }));
    for (const e of errors) {
      const i = Math.floor((e + 160) / binW);
      if (i >= 0 && i < count) bins[i].count += 1;
    }
    return bins;
  }

  function snap(v, step = 5) {
    return Math.round(v / step) * step;
  }

  // Gleiche Formel wie im Blatt, dort nachgeschlagen statt kopiert.
  function rubber(over, dim, c) {
    return window.__automationSheet.rubber(over, dim, c);
  }

  // Kritisch gedaempfte Feder (R:646-661). Startet vom aktuellen Wert und behaelt
  // seine Geschwindigkeit beim Umzielen. Mit reduzierter Bewegung springt der Wert.
  const reduced = () => Boolean(window.matchMedia && window.matchMedia(REDUCED).matches);
  function spring(get, set, { response = 0.35 } = {}) {
    let target = get();
    let vel = 0;
    let raf = 0;
    let last = 0;
    const w = (2 * Math.PI) / response;
    const tick = (t) => {
      const dt = Math.min(0.032, (t - last) / 1000 || 0.016);
      last = t;
      const x = get();
      const acc = -w * w * (x - target) - 2 * w * vel;
      vel += acc * dt;
      const nx = x + vel * dt;
      if (Math.abs(nx - target) < 0.05 && Math.abs(vel) < 0.5) {
        set(target);
        vel = 0;
        raf = 0;
        return;
      }
      set(nx);
      raf = requestAnimationFrame(tick);
    };
    return {
      to(v, v0) {
        target = v;
        if (v0 !== undefined) vel = v0;
        if (reduced()) {
          cancelAnimationFrame(raf);
          raf = 0;
          vel = 0;
          set(v);
          return;
        }
        if (!raf) {
          last = performance.now();
          raf = requestAnimationFrame(tick);
        }
      },
      stop() {
        cancelAnimationFrame(raf);
        raf = 0;
        vel = 0;
      },
    };
  }

  window.__automationBand = { errorsFromRows, bandStats, histogram, snap, rubber, spring, LIMITS };
})();
