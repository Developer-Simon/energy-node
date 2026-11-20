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

  const api = { arrange, title, line, legend };
  window.AutomationSuggestions = api;

  // Vorschlagsband oberhalb der Regeln. Laedt einmal beim Oeffnen der Seite. Die
  // Knoepfe haengen an canEdit (Recht automations) und csrf, beides setzt die Seite
  // per x-effect. Verwerfen, Spaeter und Rueckgaengig schicken POST an die Vorschlags-Route.
  const REQUEST_PATH = '/api/v1/automations/suggestions';
  const BAND_MAX = 3;
  const HINT_MS = 6000;
  const CHANGED = 'automation-findings-changed';
  const PERCENT_FACTS = ['direction_share', 'hit_rate'];

  const base = () => window.__DASHBOARD_BASE_PATH__ || '';

  // Wert eines Belegs: Ursache und Ja/Nein aus dem Katalog, Anteile als ganze Prozent.
  function valueOf(f, t) {
    const v = f.value;
    if (f.code === 'cause') return t(CAT + 'fact.cause.' + v, {});
    if (typeof v === 'boolean') return t(v ? 'common.yes' : 'common.no', {});
    if (PERCENT_FACTS.indexOf(f.code) >= 0) return t(CAT + 'value.percent', { value: Math.round(Number(v) * 100) });
    return String(v == null ? '' : v);
  }

  function autoLearnBand() {
    return {
      enabled: false,
      loaded: false,
      items: [],
      days: 0,
      devices: 0,
      all: false,
      open: '',
      canEdit: false,
      csrf: '',
      dropped: null,
      hintTimer: 0,
      get shown() { return this.loaded && this.enabled; },
      get count() { return this.items.length; },
      get arranged() { return arrange(this.items, this.all ? this.items.length : BAND_MAX); },
      get visible() { return this.arranged.visible; },
      get rest() { return this.arranged.rest; },
      // Der Knopf zeigt "+N" oder, aufgeklappt, "Weniger". Ohne Rest und zugeklappt entfaellt er.
      get moreShown() { return this.all || this.rest > 0; },
      get current() {
        const id = this.open;
        return this.items.find((s) => s.id === id) || null;
      },

      async load() {
        try {
          const response = await window.fetch(base() + REQUEST_PATH);
          const body = await response.json();
          if (!response.ok) throw new Error(String(response.status));
          this.enabled = Boolean(body && body.enabled);
          this.items = this.enabled && Array.isArray(body.suggestions) ? body.suggestions.slice() : [];
          this.days = Number(body && body.learning && body.learning.days) || 0;
          this.devices = Number(body && body.learning && body.learning.devices) || 0;
          this.loaded = true;
        } catch (error) {
          this.loaded = false;
        }
      },

      toggle(id) { this.open = this.open === id ? '' : id; },
      expandAll() {
        this.all = !this.all;
        if (!this.all && this.open && !this.visible.some((s) => s.id === this.open)) this.open = '';
      },

      titleOf(s, t) { return api.title(s, t); },
      lineOf(s, t) { return api.line(s, t); },
      legendOf(s, t) { return api.legend(s, t); },
      groupLabel(s, t) { return t(CAT + 'group.' + s.group, {}); },
      evidenceOf(s, t) { return window.AutomationEvidence ? window.AutomationEvidence.render(s.evidence, t) : ''; },
      countLabel(t) { return this.count === 1 ? t(CAT + 'band.count_one', {}) : t(CAT + 'band.count_many', { count: this.count }); },
      learningText(t) { return t(CAT + 'band.learning', { days: this.days, devices: this.devices }); },

      confidenceOf(s, t) {
        const sure = s.confidence === 'high';
        return { tone: sure ? 'ok' : 'wait', label: t(CAT + (sure ? 'confidence.sure' : 'confidence.estimate'), {}) };
      },
      seasonOf(s, t) {
        const season = s.season;
        if (!season) return t(CAT + 'season.all', {});
        return t(CAT + 'season', { from: season.from_week, to: season.to_week });
      },
      factsOf(s, t) {
        return (s.facts || []).map((f) => ({ label: t(CAT + 'fact.' + f.code, {}), value: valueOf(f, t) }));
      },

      // Der erste Befehl mit Vorlage oeffnet den Assistenten, sonst gibt es den Knopf nicht.
      fixOf(s) { return (s.apply || []).find((f) => f && f.template) || null; },
      actionsOf(s) {
        const edit = Boolean(this.canEdit) && Boolean(s);
        return { assistant: edit && Boolean(this.fixOf(s)), later: edit, dismiss: edit };
      },

      async send(s, action) {
        const response = await window.fetch(base() + REQUEST_PATH + '/' + encodeURIComponent(s.id) + '/' + action, {
          method: 'POST',
          headers: { 'X-CSRF-Token': this.csrf }, // i18n-ignore
        });
        return response.ok;
      },
      dismiss(s) { return this.drop(s, 'dismiss'); },
      later(s) { return this.drop(s, 'later'); },
      async drop(s, action) {
        const index = this.items.findIndex((x) => x.id === s.id);
        let ok = false;
        try { ok = await this.send(s, action); } catch (error) { ok = false; }
        if (!ok || index < 0) return;
        this.items = this.items.filter((x) => x.id !== s.id);
        if (this.open === s.id) this.open = '';
        this.dropped = { item: s, index };
        this.armHint();
        window.dispatchEvent(new window.CustomEvent(CHANGED));
      },
      armHint() {
        clearTimeout(this.hintTimer);
        this.hintTimer = window.setTimeout(() => { this.dropped = null; }, HINT_MS);
      },
      async undo() {
        const drop = this.dropped;
        if (!drop) return;
        let ok = false;
        try { ok = await this.send(drop.item, 'undo'); } catch (error) { ok = false; }
        if (!ok) return;
        clearTimeout(this.hintTimer);
        this.dropped = null;
        const items = this.items.slice();
        items.splice(Math.min(drop.index, items.length), 0, drop.item);
        this.items = items;
        window.dispatchEvent(new window.CustomEvent(CHANGED));
      },
    };
  }

  window.__autoLearnBand = autoLearnBand;
  const register = () => window.Alpine.data('autoLearnBand', autoLearnBand);
  if (window.Alpine) register(); else document.addEventListener('alpine:init', register, { once: true });
})();
