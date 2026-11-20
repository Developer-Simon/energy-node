// Vorschlagsband: reine Auswahl und Texte, keine DOM-Abhaengigkeit.
// Das Backend schickt Code und Zahlen. Die Texte stehen im Katalog unter automations.learn.*,
// t(key, params) liefert sie. Ohne Katalogtext zeigt t den Schluessel.
(() => {
  'use strict';

  const CAT = 'automations.learn.';
  const SHAPES_WITH_LEGEND = ['day', 'events', 'scale'];

  // Die ersten max Vorschlaege sind sichtbar, der Rest wird als Zahl gezaehlt.
  function arrange(list, max) {
    const items = Array.isArray(list) ? list : [];
    const limit = Math.max(0, Math.floor(Number(max) || 0));
    const visible = items.slice(0, limit);
    return { visible, rest: items.length - visible.length };
  }

  const pad2 = (n) => (n < 10 ? '0' : '') + n;

  // Minuten ab Mitternacht als HH:MM, ueber den Tag hinaus umlaufend.
  function clockOf(minutes) {
    const total = ((Math.round(Number(minutes) || 0) % 1440) + 1440) % 1440;
    return pad2(Math.floor(total / 60)) + ':' + pad2(total % 60);
  }

  // Parameter fuer den Katalog: Zeiten als Uhrzeit, Anteile als ganze Prozent.
  function paramsOf(s) {
    const src = (s && s.summary && s.summary.params) || {};
    const out = Object.assign({}, src);
    if ('window_from' in src) out.window_from = clockOf(src.window_from);
    if ('window_to' in src) out.window_to = clockOf(src.window_to);
    if ('share' in src) out.share = Math.round(Number(src.share) * 100);
    return out;
  }

  function codeOf(s) {
    return (s && s.summary && s.summary.code) || '';
  }

  function title(s, t) {
    return t(CAT + codeOf(s) + '.title', paramsOf(s));
  }

  function line(s, t) {
    return t(CAT + codeOf(s) + '.line', paramsOf(s));
  }

  // Die Legende hangt an der Form der Grafik. Ohne bekannte Form gibt es keine.
  function legend(s, t) {
    const shape = s && s.evidence && s.evidence.shape;
    if (SHAPES_WITH_LEGEND.indexOf(shape) < 0) return '';
    return t(CAT + 'legend.' + shape, {});
  }

  window.AutomationSuggestions = { arrange, title, line, legend };
})();
