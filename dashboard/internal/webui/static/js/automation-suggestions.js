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

  // Schluessel und Parameter eines Befunds, gemeinsam fuer Vorschlagsband und Regelmarken.
  // Der Dienst liefert den Anteil als Bruchteil, die Texte nennen Prozent.
  // Sonnenzeit: der Dienst nennt das Ereignis als Kennung und den Abstand mit Vorzeichen.
  // Ab 0 steht "nach", darunter "vor" mit dem Betrag, damit kein "-20 Minuten nach" entsteht.
  function resolveSummary(summary, t) {
    let code = (summary && summary.code) || '';
    const params = Object.assign({}, (summary && summary.params) || {});
    if (typeof params.share === 'number') params.share = Math.round(params.share * 100);
    // Ohne gemessenen Verbraucher bleibt der Name leer, der Text nennt dann den Verbraucher ohne Messung.
    if (code === 'baseload.drift' && !params.candidate) params.candidate = t('automations.learn.baseload.candidate.none');
    if (code === 'battery.empty_early' && !params.candidates) params.candidates = t('automations.learn.battery.candidate.none');
    if (/^switch_time\.sun_(on|off)$/.test(code)) {
      const offset = Number(params.offset_min) || 0;
      params.minutes = Math.abs(offset);
      params.event = t(`automations.sun_event.${params.event}`);
      delete params.offset_min;
      code = code + (offset < 0 ? '.before' : '.after');
    }
    // Jede Dezimalzahl wird mit einer Nachkommastelle in der Sprache der Seite gezeigt.
    Object.keys(params).forEach((key) => {
      const v = params[key];
      if (typeof v === 'number' && isFinite(v) && !Number.isInteger(v)) params[key] = roundedText(v);
    });
    return { code, params };
  }

  // Zahl mit hoechstens einer Nachkommastelle, im Dezimalformat der Seite (1,2 auf Deutsch).
  function roundedText(v) {
    const r = Math.round(v * 10) / 10;
    const format = window.I18n && window.I18n.formatNumber;
    if (!format) return String(r);
    return format(r, Number.isInteger(r) ? 0 : 1);
  }

  // Schluessel und Parameter des Bandes: zusaetzlich Zeiten als Uhrzeit.
  function bandOf(s, t) {
    const r = resolveSummary(s && s.summary, t);
    const src = r.params;
    if ('window_from' in src) src.window_from = clockOf(src.window_from);
    if ('window_to' in src) src.window_to = clockOf(src.window_to);
    return r;
  }

  function title(s, t) {
    const r = bandOf(s, t);
    return t(CAT + r.code + '.title', r.params);
  }

  function line(s, t) {
    const r = bandOf(s, t);
    return t(CAT + r.code + '.line', r.params);
  }

  // Erklaerungstext der Karte. Ohne Katalogtext (t liefert den Schluessel) bleibt er leer.
  function text(s, t) {
    const r = bandOf(s, t);
    const key = CAT + r.code + '.text';
    const out = t(key, r.params);
    return out === key ? '' : out;
  }

  // Die Legende hangt an der Form der Grafik. Ohne bekannte Form gibt es keine.
  function legend(s, t) {
    const shape = s && s.evidence && s.evidence.shape;
    if (SHAPES_WITH_LEGEND.indexOf(shape) < 0) return '';
    return t(CAT + 'legend.' + shape, {});
  }

  // Zahlen der Befunde als Zeilen aus Bezeichnung und Wert, wie im Vorschlagsband.
  const factRows = (facts, t) => (facts || []).map((f) => ({ label: t(CAT + 'fact.' + f.code, {}), value: valueOf(f, t) }));
  const api = { arrange, title, line, text, legend, resolveSummary, factRows };
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

  // Zahl eines Belegs mit Einheit. Minuten und Sekunden ganz, Sekunden ab 120 als Minuten
  // mit hoechstens einer Nachkommastelle, alle anderen Zahlen hoechstens eine Nachkommastelle.
  function numberOf(code, v, t) {
    if (code === 'spread_min') return t(CAT + 'value.minutes', { value: Math.round(v) });
    if (code === 'window_s' || code === 'limit_s') {
      const seconds = Math.round(v);
      if (seconds >= 120) return t(CAT + 'value.minutes', { value: Math.round((seconds / 60) * 10) / 10 });
      return t(CAT + 'value.seconds', { value: seconds });
    }
    if (code === 'min_runtime') return t(CAT + 'value.seconds', { value: Math.round(v) });
    if (code === 'per_hour_max') return t(CAT + 'value.per_hour', { value: Math.round(v) });
    return String(Math.round(v * 10) / 10);
  }

  // Wert eines Belegs: Ursache und Ja/Nein aus dem Katalog, Anteile als ganze Prozent.
  function valueOf(f, t) {
    const v = f.value;
    if (f.code === 'cause') return t(CAT + 'fact.cause.' + v, {});
    if (typeof v === 'boolean') return t(v ? 'common.yes' : 'common.no', {});
    if (PERCENT_FACTS.indexOf(f.code) >= 0) return t(CAT + 'value.percent', { value: Math.round(Number(v) * 100) });
    if (typeof v === 'number' && isFinite(v)) return numberOf(f.code, v, t);
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
      // Tagesgrafik: false zeigt den Ausschnitt, true den ganzen Tag. Wechselt mit jeder Karte.
      fullDay: false,
      // Erklaerung der Grafik (Ist und Soll) als Tooltip, per Tipp oder Klick. Schliesst mit jeder Karte.
      tipOpen: false,
      canEdit: false,
      csrf: '',
      // Alle Verwerfen seit dem letzten Ablauf des Hinweises, in der Reihenfolge des Verwerfens.
      dropped: [],
      hintTimer: 0,
      // Spitze der Karte: Mitte der gewaehlten Kachel in px ab der linken Kante der Kacheln.
      caret: '',
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
          this.openRequested();
        } catch (error) {
          this.loaded = false;
        }
      },

      toggle(id, tile = null) {
        this.open = this.open === id ? '' : id;
        this.fullDay = false;
        this.tipOpen = false;
        if (this.open && tile) this.placeCaret(tile);
      },
      // Sprung aus dem Monitoring: der Vorschlag klappt auf und rueckt ins Bild.
      openRequested() {
        const id = window.__automationSuggestionRequest__;
        if (!id || !this.items.some((s) => s.id === id)) return;
        window.__automationSuggestionRequest__ = null;
        if (!this.visible.some((s) => s.id === id)) this.all = true;
        this.open = id;
        this.fullDay = false;
        this.tipOpen = false;
        if (!this.$nextTick) return;
        this.$nextTick(() => {
          this.placeCaret();
          const tile = this.$root ? this.$root.querySelector('.auto-learn-tile[aria-expanded="true"]') : null;
          if (!tile || typeof tile.scrollIntoView !== 'function') return;
          const calm = typeof window.matchMedia === 'function' && window.matchMedia('(prefers-reduced-motion: reduce)').matches;
          tile.scrollIntoView({ block: 'center', behavior: calm ? 'auto' : 'smooth' });
        });
      },
      // Die Karte zeigt mit ihrer Spitze auf die Kachel, aus der sie kommt. Ohne Kachel
      // (Groessenwechsel, mehr Kacheln) gilt die gerade aufgeklappte.
      placeCaret(tile = null) {
        const el = tile || (this.$root ? this.$root.querySelector('.auto-learn-tile[aria-expanded="true"]') : null);
        const row = el && el.parentElement;
        if (!el || !row) return;
        const a = el.getBoundingClientRect();
        const b = row.getBoundingClientRect();
        this.caret = `${Math.round(a.left - b.left + a.width / 2)}px`;
      },
      toggleTip() { this.tipOpen = !this.tipOpen; },
      // Escape und Klick neben den Tooltip
      closeTip() { this.tipOpen = false; },
      expandAll() {
        this.all = !this.all;
        if (!this.all && this.open && !this.visible.some((s) => s.id === this.open)) { this.open = ''; this.tipOpen = false; }
        if (this.open && this.$nextTick) this.$nextTick(() => this.placeCaret());
      },

      titleOf(s, t) { return api.title(s, t); },
      lineOf(s, t) { return api.line(s, t); },
      textOf(s, t) { return api.text(s, t); },
      legendOf(s, t) { return api.legend(s, t); },
      groupLabel(s, t) { return t(CAT + 'group.' + s.group, {}); },
      evidenceOf(s, t) { return window.AutomationEvidence ? window.AutomationEvidence.render(s.evidence, t, { full: this.fullDay }) : ''; },
      zoomableOf(s) { return Boolean(window.AutomationEvidence && window.AutomationEvidence.zoomable(s.evidence)); },
      zoomLabelOf(t) { return t(CAT + (this.fullDay ? 'zoom.window' : 'zoom.full'), {}); },
      countLabel(t) { return this.count === 1 ? t(CAT + 'band.count_one', {}) : t(CAT + 'band.count_many', { count: this.count }); },
      learningText(t) { return t(CAT + 'band.learning', { days: this.days, devices: this.devices }); },

      confidenceOf(s, t) {
        const sure = s.confidence === 'high';
        return { tone: sure ? 'ok' : 'wait', label: t(CAT + (sure ? 'confidence.sure' : 'confidence.estimate'), {}) };
      },
      // Die Wochen gelten nur mit Zeitraum. Ein Befund nur mit Heizzeit zeigt die Heizzeit daneben.
      seasonOf(s, t) {
        const season = s.season;
        if (!season) return t(CAT + 'season.all', {});
        if (!season.from_week || !season.to_week) return '';
        return t(CAT + 'season', { from: season.from_week, to: season.to_week });
      },
      heatingOf(s, t) {
        const below = s.season ? s.season.heating_below_c : null;
        if (typeof below !== 'number' || !isFinite(below)) return '';
        return t(CAT + 'season.heating', { below: Math.round(below * 10) / 10 });
      },
      factsOf(s, t) {
        return (s.facts || []).map((f) => ({ label: t(CAT + 'fact.' + f.code, {}), value: valueOf(f, t) }));
      },

      // Der erste Befehl mit Vorlage oeffnet den Assistenten, sonst gibt es den Knopf nicht.
      fixOf(s) { return (s.apply || []).find((f) => f && f.template) || null; },
      // Ein Befehl mit Ops schreibt seine Pfade direkt in den Regelentwurf (applyFix der Seite).
      opFixOf(s) { return (s.apply || []).find((f) => f && !f.template && Array.isArray(f.ops) && f.ops.length > 0) || null; },
      applyLabelOf(s, t) {
        const fix = this.opFixOf(s);
        return fix ? t(CAT + 'fix.' + fix.code, {}) : '';
      },
      actionsOf(s) {
        const edit = Boolean(this.canEdit) && Boolean(s);
        return { assistant: edit && Boolean(this.fixOf(s)), apply: edit && Boolean(this.opFixOf(s)), later: edit, dismiss: edit };
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
        if (index < 0) return;
        if (!ok) {
          const text = window.I18n && window.I18n.t ? window.I18n.t(CAT + 'drop_failed') : CAT + 'drop_failed';
          this.$store.toasts.push(text, 'critical');
          return;
        }
        this.items = this.items.filter((x) => x.id !== s.id);
        if (this.open === s.id) { this.open = ''; this.tipOpen = false; }
        this.dropped = [...this.dropped, { item: s, index }];
        this.armHint();
        window.dispatchEvent(new window.CustomEvent(CHANGED));
      },
      armHint() {
        clearTimeout(this.hintTimer);
        this.hintTimer = window.setTimeout(() => { this.dropped = []; }, HINT_MS);
      },
      // Holt alle Verwerfen zurueck, das juengste zuerst: so kehrt jede Einfuegung an ihren Platz.
      // Schlaegt eines fehl, bleiben es und die aelteren im Hinweis.
      async undo() {
        const drops = this.dropped.slice();
        for (let n = drops.length - 1; n >= 0; n -= 1) {
          const drop = drops[n];
          let ok = false;
          try { ok = await this.send(drop.item, 'undo'); } catch (error) { ok = false; }
          if (!ok) break;
          this.dropped = this.dropped.filter((entry) => entry !== drop);
          const items = this.items.slice();
          items.splice(Math.min(drop.index, items.length), 0, drop.item);
          this.items = items;
          window.dispatchEvent(new window.CustomEvent(CHANGED));
        }
        if (this.dropped.length === 0) clearTimeout(this.hintTimer);
      },
    };
  }

  window.__autoLearnBand = autoLearnBand;
  const register = () => window.Alpine.data('autoLearnBand', autoLearnBand);
  if (window.Alpine) register(); else document.addEventListener('alpine:init', register, { once: true });
})();
