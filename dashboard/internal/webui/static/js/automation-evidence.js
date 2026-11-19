// Ist-Soll-Grafik fuer Regelbefunde. Reine Rechnung als HTML-String, keine DOM-Abhaengigkeit.
// Keine Texte in den SVG-Grafiken: Spurmarken und Achse stehen als HTML.
// Form und Feldnamen folgen dem Go-Evidence in dashboard/internal/autolearn/finding.go.
(() => {
  const esc = (value) => String(value == null ? '' : value)
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&#39;');

  const clamp = (v) => Math.max(0, Math.min(100, v));
  const pos = (v, from, to) => ((v - from) / (to - from)) * 100;
  const pct = (v, max) => clamp((v / max) * 100);

  // Ohne Max (z. B. unerfuellbare Regel) ergibt sich ein Massstab aus den Werten.
  function scaleMaxOf(ev) {
    if (ev.max > 0) return ev.max;
    const seen = Array.isArray(ev.seen) ? ev.seen[1] || 0 : 0;
    const top = Math.max(seen, ev.ist_value || 0, ev.soll_value || 0) * 1.2;
    return top > 0 ? top : 1;
  }

  function laneSvg(inner) {
    return '<svg class="auto-learn-lane-svg" viewBox="0 0 100 10" preserveAspectRatio="none" aria-hidden="true">' + inner + '</svg>';
  }

  function lane(label, body, soll) {
    return '<div class="auto-learn-lane' + (soll ? ' is-soll' : '') + '"><span class="auto-learn-lane-label">' + esc(label) + '</span>' + body + '</div>';
  }

  function spans(list, from, to) {
    return (list || []).map((s) => {
      const x = pos(s[0], from, to);
      const w = pos(s[1], from, to) - x;
      return '<rect class="auto-learn-soll" x="' + x + '" y="1.5" width="' + w + '" height="7" rx="0.8"/>';
    }).join('');
  }

  function marks(list, toX, cls) {
    return (list || []).map((v) => {
      const x = toX(v);
      return '<line class="' + cls + '" x1="' + x + '" x2="' + x + '" y1="0.5" y2="9.5" vector-effect="non-scaling-stroke"/>';
    }).join('');
  }

  function axis(labels) {
    return '<div class="auto-learn-axis"><span></span><div>' + labels.map((l) => '<span>' + esc(l) + '</span>').join('') + '</div></div>';
  }

  const pad2 = (n) => (n < 10 ? '0' : '') + n;
  const clock = (sec) => pad2(new Date(sec * 1000).getHours());

  function eventsShape(ev, t) {
    const from = ev.from;
    const to = ev.to;
    const toX = (v) => pos(v, from, to);
    const ticks = [from, Math.round((from + to) / 2), to].map(clock);
    return lane(t('automations.learn.lane.ist'), laneSvg(marks(ev.ist, toX, 'auto-learn-mark-event')))
      + lane(t('automations.learn.lane.soll'), laneSvg(spans(ev.soll_spans, from, to)), true)
      + axis(ticks);
  }

  function scaleShape(ev, t) {
    const max = scaleMaxOf(ev);
    const seenWidth = ev.seen ? pct(ev.seen[1] || 0, max) : 0;
    const toX = (v) => pct(v, max);
    const seen = '<rect class="auto-learn-ist" x="0" y="1.5" width="' + seenWidth + '" height="7" rx="0.8"/>';
    const threshold = ev.ist_value != null ? marks([ev.ist_value], toX, 'auto-learn-mark-threshold') : '';
    const soft = '<rect class="auto-learn-ist auto-learn-soft" x="0" y="1.5" width="' + seenWidth + '" height="7" rx="0.8"/>';
    const soll = ev.soll_value != null ? marks([ev.soll_value], toX, 'auto-learn-mark-soll auto-learn-mark-wide') : '';
    const unit = ev.unit ? ' ' + ev.unit : '';
    const ticks = [0, max / 2, max].map((v, i) => (i === 2 ? Math.round(v) + unit : String(Math.round(v))));
    return lane(t('automations.learn.lane.ist'), laneSvg(seen + threshold))
      + lane(t('automations.learn.lane.soll'), laneSvg(soft + soll), true)
      + axis(ticks);
  }

  function chips(names) {
    return (names || []).map((n) => '<span class="auto-learn-chip">' + esc(n) + '</span>').join('');
  }

  // Ist: Regelnamen als Chips. Ziel: ein Knoten, gestrichelt bei Missing.
  // Soll: die Soll-Namen, ohne Soll-Namen steht nur das Ziel auf der Spur.
  function linksShape(ev, t) {
    const hasSoll = Array.isArray(ev.soll_names) && ev.soll_names.length > 0;
    const targetNode = '<div class="auto-learn-target' + (ev.missing ? ' is-missing' : '') + '">' + esc(ev.target) + '</div>';
    const middle = ev.target && hasSoll ? targetNode : '';
    const sollBody = hasSoll ? chips(ev.soll_names) : (ev.target ? targetNode : '');
    return '<div class="auto-learn-lane"><span class="auto-learn-lane-label">' + esc(t('automations.learn.lane.ist')) + '</span><div class="auto-learn-chips">' + chips(ev.ist_names) + '</div></div>'
      + middle
      + '<div class="auto-learn-lane is-soll"><span class="auto-learn-lane-label">' + esc(t('automations.learn.lane.soll')) + '</span><div class="auto-learn-chips">' + sollBody + '</div></div>';
  }

  const SHAPES = { events: eventsShape, scale: scaleShape, links: linksShape };

  // render(evidence, t) -> HTML. t(key, params) liefert die Spurmarken.
  function render(evidence, t) {
    if (!evidence || !SHAPES[evidence.shape]) return '';
    const tr = typeof t === 'function' ? t : (key) => key;
    return '<div class="auto-learn-is auto-learn-shape-' + evidence.shape + '">' + SHAPES[evidence.shape](evidence, tr) + '</div>';
  }

  window.AutomationEvidence = { render };
})();
