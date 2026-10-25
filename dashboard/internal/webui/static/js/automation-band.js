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
  // Ohne Blatt-Modul greift ein harter Anschlag: kein Widerstand, der Wert bleibt an der Grenze.
  function rubber(over, dim, c) {
    const sheet = window.__automationSheet;
    return sheet && sheet.rubber ? sheet.rubber(over, dim, c) : 0;
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

  // DOM-Teil des Zielbands (Port von R:840-923). Alle Texte kommen aus opts.labels.
  // opts: { state: { target, tol }, tolEditable(), errors(), labels, onChange(state) }.
  const clamp = (v, lo, hi) => Math.min(hi, Math.max(lo, v));
  const esc = (s) => String(s).replace(/[&<>"]/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;' })[c]);
  const SVG_KEYS = { ArrowRight: 5, ArrowUp: 5, ArrowLeft: -5, ArrowDown: -5 };

  function mountBand(svg, opts) {
    const { state, labels } = opts;
    const tMin = LIMITS.target.min;
    const tMax = LIMITS.target.max;
    const tolMin = LIMITS.tol.min;
    const tolMax = LIMITS.tol.max;
    const AX = { min: LIMITS.axis.min, max: LIMITS.axis.max, W: 580, H: 150, top: 26, bot: 96 };

    // Zeichnet in CSS-Pixeln, damit Beschriftung und Griffe auf dem Telefon gleich gross bleiben.
    const fitAxis = () => {
      const w = Math.round(svg.getBoundingClientRect().width) || 580;
      AX.W = w;
      AX.H = w < 420 ? 170 : 150;
      AX.bot = w < 420 ? 112 : 96;
      svg.setAttribute('viewBox', `0 0 ${AX.W} ${AX.H}`);
    };
    const bx = (v) => ((v - AX.min) / (AX.max - AX.min)) * AX.W;
    const bv = (x) => AX.min + (x / AX.W) * (AX.max - AX.min);

    const draw = () => {
      const t = state.target;
      const tol = state.tol;
      const editable = Boolean(opts.tolEditable && opts.tolEditable());
      const errs = (opts.errors && opts.errors()) || [];
      const bins = errs.length ? histogram(errs) : [];
      const maxH = bins.reduce((m, b) => Math.max(m, b.count), 0);
      const hist = maxH > 0 ? bins.map((b) => {
        const v = t + b.offset;
        if (v < AX.min || v > AX.max) return '';
        const h = (b.count / maxH) * (AX.bot - AX.top - 6);
        return `<rect class="auto-band-bar" x="${bx(v)}" y="${AX.bot - h}" width="${(5 / (AX.max - AX.min)) * AX.W - 0.6}" height="${h}" fill="${v < 0 ? 'var(--warn)' : 'var(--info)'}" opacity="0.45"/>`;
      }).join('') : '';
      const l = bx(t - tol);
      const r = bx(t + tol);
      const z = bx(0);
      const mid = (AX.top + AX.bot) / 2;
      const sides = editable
        ? `<g class="auto-band-handle" data-drag="l"><rect x="${l - 10}" y="${AX.top - 8}" width="20" height="${AX.bot - AX.top + 16}" fill="transparent"/><rect x="${l - 3}" y="${mid - 14}" width="6" height="28" rx="3" fill="var(--accent)"/></g><g class="auto-band-handle" data-drag="r"><rect x="${r - 10}" y="${AX.top - 8}" width="20" height="${AX.bot - AX.top + 16}" fill="transparent"/><rect x="${r - 3}" y="${mid - 14}" width="6" height="28" rx="3" fill="var(--accent)"/></g>`
        : '';
      const label = editable ? labels.targetTol(Math.round(t), Math.round(tol)) : labels.target(Math.round(t));
      const ticks = AX.W < 420 ? [-100, 0, 100] : [-100, -50, 0, 50, 100, 150];

      svg.innerHTML = `<rect x="0" y="${AX.top}" width="${z}" height="${AX.bot - AX.top}" rx="8" fill="var(--warn-bg)"/><rect x="${z}" y="${AX.top}" width="${AX.W - z}" height="${AX.bot - AX.top}" rx="8" fill="var(--info-bg)"/>
      <text x="10" y="${AX.top + 16}" font-size="12" fill="var(--warn)" font-weight="600">${esc(labels.feedIn)}</text><text x="${AX.W - 10}" y="${AX.top + 16}" font-size="12" fill="var(--info)" font-weight="600" text-anchor="end">${esc(labels.import)}</text>
      ${hist}
      <line x1="${z}" x2="${z}" y1="${AX.top - 4}" y2="${AX.bot + 4}" stroke="var(--text-muted)" stroke-width="1.5"/>
      <g class="auto-band-handle" data-drag="move"><rect x="${l}" y="${AX.top - 8}" width="${r - l}" height="${AX.bot - AX.top + 16}" rx="10" fill="color-mix(in srgb, var(--accent) 22%, transparent)" stroke="var(--accent)" stroke-width="2"/>
        <line x1="${bx(t)}" x2="${bx(t)}" y1="${AX.top - 8}" y2="${AX.bot + 8}" stroke="var(--accent-line)" stroke-width="2" stroke-dasharray="3 4"/>
        <rect x="${bx(t) - 4}" y="${mid - 10}" width="8" height="20" rx="4" fill="var(--accent-line)"/></g>
      ${sides}
      <text x="${clamp(bx(t), 50, AX.W - 50)}" y="${AX.top - 12}" font-size="12.5" fill="var(--accent-line)" font-weight="700" text-anchor="middle">${esc(label)}</text>
      ${ticks.map((v) => `<text x="${bx(v)}" y="${AX.H - 30}" font-size="11" fill="var(--text-muted)" text-anchor="middle">${v > 0 ? '+' : ''}${v}</text>`).join('')}
      <text x="${AX.W / 2}" y="${AX.H - 10}" font-size="11" fill="var(--text-faint)" text-anchor="middle">${esc(AX.W < 420 ? labels.axisShort : labels.axis)}</text>`;
      svg.setAttribute('aria-valuenow', String(Math.round(t)));
      svg.setAttribute('aria-valuetext', labels.valueText(Math.round(t), Math.round(tol)));
    };

    // Jede Aenderung von Ziel oder Toleranz zeichnet neu und meldet sie an die Seite.
    const update = () => {
      draw();
      if (opts.onChange) opts.onChange(state);
    };
    const tSpring = spring(() => state.target, (v) => {
      state.target = v;
      update();
    });

    const svgX = (e) => {
      const r = svg.getBoundingClientRect();
      return (e.clientX - r.left) * (AX.W / r.width);
    };

    let drag = null;
    const endDrag = () => {
      if (!drag) return;
      svg.removeEventListener('pointermove', drag.move);
      svg.removeEventListener('pointerup', drag.up);
      svg.removeEventListener('pointercancel', drag.up);
      drag = null;
    };

    const onPointerDown = (e) => {
      endDrag();
      const h = e.target.closest('[data-drag]');
      const mode = h ? h.getAttribute('data-drag') : 'jump';
      tSpring.stop();
      if (mode === 'jump') {
        tSpring.to(snap(clamp(bv(svgX(e)), tMin, tMax)));
        return;
      }
      svg.setPointerCapture(e.pointerId);
      const grab = bv(svgX(e)) - state.target; // Greifpunkt im Fenster bleibt erhalten
      const hist = [];
      const move = (ev) => {
        const v = bv(svgX(ev));
        hist.push([ev.timeStamp, v]);
        if (hist.length > 5) hist.shift();
        if (mode === 'move') {
          const raw = v - grab;
          state.target = raw < tMin ? tMin + rubber(raw - tMin, 60) : raw > tMax ? tMax + rubber(raw - tMax, 60) : raw;
        } else {
          const raw = Math.abs(v - state.target);
          state.tol = raw < tolMin ? tolMin + rubber(raw - tolMin, 20) : raw > tolMax ? tolMax + rubber(raw - tolMax, 20) : raw;
        }
        update();
      };
      const up = () => {
        endDrag();
        if (mode === 'move') {
          // Geschwindigkeit des Loslassens projizieren, dann auf das 5-W-Raster federn.
          const first = hist[0];
          const last = hist[hist.length - 1];
          const vel = hist.length > 1 ? ((last[1] - first[1]) / Math.max(1, last[0] - first[0])) * 1000 : 0;
          const projected = state.target + (vel / 1000) * 0.99 / (1 - 0.99);
          tSpring.to(snap(clamp(projected, tMin, tMax)), vel);
        } else {
          state.tol = snap(clamp(state.tol, tolMin, tolMax));
          update();
        }
      };
      drag = { move, up };
      svg.addEventListener('pointermove', move);
      svg.addEventListener('pointerup', up);
      svg.addEventListener('pointercancel', up);
    };

    const onKeyDown = (e) => {
      const d = SVG_KEYS[e.key];
      if (d === undefined) return;
      e.preventDefault();
      tSpring.stop();
      state.target = snap(clamp(state.target + d * (e.shiftKey ? 4 : 1), tMin, tMax));
      update();
    };

    svg.setAttribute('role', 'slider');
    svg.setAttribute('tabindex', '0');
    svg.setAttribute('aria-valuemin', String(tMin));
    svg.setAttribute('aria-valuemax', String(tMax));
    svg.addEventListener('pointerdown', onPointerDown);
    svg.addEventListener('keydown', onKeyDown);

    const ro = typeof ResizeObserver !== 'undefined'
      ? new ResizeObserver(() => { fitAxis(); draw(); })
      : null;
    fitAxis();
    draw();
    if (ro) ro.observe(svg);

    return {
      redraw() {
        draw();
      },
      setTarget(value) {
        tSpring.to(clamp(value, tMin, tMax));
      },
      destroy() {
        if (ro) ro.disconnect();
        svg.removeEventListener('pointerdown', onPointerDown);
        svg.removeEventListener('keydown', onKeyDown);
        endDrag();
        tSpring.stop();
      },
    };
  }

  window.__automationBand = { errorsFromRows, bandStats, histogram, snap, rubber, spring, LIMITS, mountBand };
})();
