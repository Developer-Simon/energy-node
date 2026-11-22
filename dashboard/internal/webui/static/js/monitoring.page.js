// Monitoring-Seite: Meldungen (Liste und Karte) und Lernwerte. Die Daten kommen
// aus GET /api/v1/monitoring, das Quittieren geht per POST /api/v1/monitoring/ack
// mit dem CSRF-Token der Sitzung. Die Texte baut window.MonitoringModel.
//
// i18n-keys: monitoring.filter.all, monitoring.filter.energy, monitoring.filter.learn,
// monitoring.filter.suggest, monitoring.source.energy, monitoring.source.chain,
// monitoring.source.learn, monitoring.source.suggest,
// monitoring.group.new, monitoring.group.known, monitoring.steps.title,
// monitoring.ack.button, monitoring.ack.done, monitoring.ack.undo, monitoring.ack.no_right,
// monitoring.badge.new, monitoring.learned.count
(() => {
  const t = (key, params) => (window.I18n ? window.I18n.t(key, params) : key);
  const base = () => window.__DASHBOARD_BASE_PATH__ || '';
  const esc = (s) => String(s == null ? '' : s).replace(/[&<>"']/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));
  const wide = () => Boolean(window.matchMedia) && window.matchMedia('(min-width: 721px)').matches;
  const reduced = () => Boolean(window.matchMedia) && window.matchMedia('(prefers-reduced-motion: reduce)').matches;
  const nowSeconds = () => Date.now() / 1000;

  const FILTERS = [
    { id: 'all', key: 'monitoring.filter.all' },
    { id: 'energy', key: 'monitoring.filter.energy' },
    { id: 'learn', key: 'monitoring.filter.learn' },
    { id: 'suggest', key: 'monitoring.filter.suggest' },
  ];
  const SOURCE_KEYS = {
    energy: 'monitoring.source.energy',
    chain: 'monitoring.source.chain',
    learn: 'monitoring.source.learn',
    suggest: 'monitoring.source.suggest',
  };
  const GROUP_KEYS = { new: 'monitoring.group.new', known: 'monitoring.group.known' };
  // Sprungziel der Schritte auf die Seite (Reiter) und das Panel, das den Reiter besitzt.
  const TARGET_PANELS = { efficiency: 'efficiency-panel', devicemap: 'devicemap-panel' };

  async function requestJSON(path, options) {
    const response = await fetch(base() + path, options || { credentials: 'same-origin', headers: { Accept: 'application/json' } });
    if (!response.ok) throw new Error(path + ' ' + response.status);
    return response.json();
  }

  function panelFor(target) {
    if (String(target || '').startsWith('energy')) return 'energy-panel';
    return TARGET_PANELS[target] || '';
  }

  function openTab(panel) {
    const tab = document.querySelector('[role="tab"][aria-controls="' + panel + '"]');
    if (tab) tab.click();
  }

  function monitoringPanel() {
    return {
      notices: [],
      newCount: 0,
      learned: { enabled: false, values: [] },
      loaded: false,
      failed: false,
      // Rollen aus der Sitzung; ohne Antwort gibt es keine Rechte (nichts quittieren).
      sessionRoles: [],
      csrfToken: '',
      view: 'notices',
      filter: 'all',
      pick: '',
      flashKey: '',

      init() {
        // Eine Anfrage, die vor dem Laden der Seite kam, liegt auf window.
        const request = window.__monitoringOpenRequest__;
        if (request) {
          delete window.__monitoringOpenRequest__;
          this.view = 'notices';
          this.filter = request.filter || 'all';
        }
        window.addEventListener('monitoring-open', (event) => {
          delete window.__monitoringOpenRequest__;
          this.view = 'notices';
          this.filter = (event.detail && event.detail.filter) || 'all';
          this.pick = '';
          openTab('monitoring-panel');
        });
        this.load();
      },

      async load() {
        try {
          const [body, session] = await Promise.all([
            requestJSON('/api/v1/monitoring'),
            requestJSON('/api/v1/auth/session').catch(() => null),
          ]);
          this.apply(body);
          this.sessionRoles = session && Array.isArray(session.roles) ? session.roles : [];
          this.csrfToken = (session && session.csrf_token) || '';
          this.failed = false;
        } catch (error) {
          this.failed = true;
        }
        this.loaded = true;
      },

      apply(body) {
        const doc = body || {};
        this.notices = Array.isArray(doc.notices) ? doc.notices : [];
        this.newCount = Number(doc.new_count) || 0;
        const learned = doc.learned || {};
        this.learned = { enabled: Boolean(learned.enabled), values: Array.isArray(learned.values) ? learned.values : [] };
      },

      notify(error) {
        if (this.$store && this.$store.toasts) this.$store.toasts.push(error.message, 'critical');
      },

      rights() {
        const roles = this.sessionRoles || [];
        return {
          view_energy: roles.includes('view_energy'),
          view_efficiency: roles.includes('view_efficiency'),
          view_device_map: roles.includes('view_device_map'),
          edit_energy: roles.includes('edit_energy'),
          view_automations: roles.includes('view_automations'),
        };
      },
      canAck() { return (this.sessionRoles || []).includes('edit_setup'); },

      sourceLabel(source) { return SOURCE_KEYS[source] ? t(SOURCE_KEYS[source]) : ''; },
      setView(view) {
        if (this.view === view) return;
        this.view = view;
        this.$nextTick(() => this.fadeIn(this.$el && this.$el.querySelector(view === 'learned' ? '.monitoring-learned' : '.monitoring-split'), 6));
      },
      setFilter(id) { this.filter = id; this.pick = ''; },

      filters() {
        return FILTERS.map((f) => ({
          id: f.id,
          label: t(f.key),
          count: window.MonitoringModel.filterNotices(this.notices, f.id).length,
        }));
      },
      list() {
        return window.MonitoringModel.sortNotices(window.MonitoringModel.filterNotices(this.notices, this.filter));
      },
      groups() {
        return window.MonitoringModel.groups(this.list()).map((g) => ({ id: g.id, label: t(GROUP_KEYS[g.id]), items: g.items }));
      },
      selected() {
        const list = this.list();
        if (!wide()) return list.find((item) => item.id === this.pick) || null;
        return list.find((item) => item.id === this.pick) || list[0] || null;
      },
      isSelected(item) { const s = this.selected(); return Boolean(s) && s.id === item.id; },

      // Auswahl: schmal klappt eine zweite Berührung die Zeile wieder zu.
      select(id) {
        const before = this.selected();
        if (!wide() && before && before.id === id) { this.pick = ''; return; }
        this.pick = id;
        if (wide() && (!before || before.id !== id) && this.$el) {
          this.$nextTick(() => this.fadeIn(this.$el.querySelector('.monitoring-detail-inner'), 4));
        }
      },

      // Ruhiger Wechsel: nur Deckkraft und ein paar Pixel, damit Lesen nie springt.
      fadeIn(el, dy) {
        if (!el || !el.animate) return;
        const frames = reduced()
          ? [{ opacity: 0 }, { opacity: 1 }]
          : [{ opacity: 0, transform: 'translateY(' + dy + 'px)' }, { opacity: 1, transform: 'none' }];
        el.animate(frames, { duration: reduced() ? 120 : 180, easing: 'cubic-bezier(0.23, 1, 0.32, 1)' });
      },

      // Quittieren: die Zeile zieht sichtbar in die andere Gruppe (FLIP).
      moveRows(change) {
        if (!this.$el) { change(); return; }
        const rows = () => [...this.$el.querySelectorAll('.monitoring-master [data-row]')];
        const first = new Map(rows().map((el) => [el.dataset.row, el.getBoundingClientRect().top]));
        change();
        this.$nextTick(() => {
          if (reduced()) return;
          rows().forEach((el) => {
            const top = first.get(el.dataset.row);
            if (top === undefined) return;
            const dy = top - el.getBoundingClientRect().top;
            if (Math.abs(dy) < 1) return;
            el.animate([{ transform: 'translateY(' + dy + 'px)' }, { transform: 'none' }],
              { duration: 260, easing: 'cubic-bezier(0.77, 0, 0.175, 1)' });
          });
        });
      },

      async setAck(id, ack) {
        if (!this.canAck()) return;
        let body;
        try {
          body = await requestJSON('/api/v1/monitoring/ack', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': this.csrfToken }, // i18n-ignore
            body: JSON.stringify({ id, ack }),
          });
        } catch (error) {
          this.notify(error);
          return;
        }
        if (!wide()) this.pick = id;
        this.moveRows(() => this.apply(body));
        window.dispatchEvent(new CustomEvent('monitoring-changed'));
      },
      ack(id) { return this.setAck(id, true); },
      unack(id) { return this.setAck(id, false); },

      async roleKeep(ids) {
        if (!this.rights().edit_energy || !ids.length) return;
        const roleNeeded = {};
        ids.forEach((id) => { roleNeeded[id] = false; });
        try {
          await requestJSON('/api/v1/energy/roles', {
            method: 'PATCH',
            headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': this.csrfToken }, // i18n-ignore
            body: JSON.stringify({ role_needed: roleNeeded }),
          });
        } catch (error) {
          this.notify(error);
          return;
        }
        await this.load();
        window.dispatchEvent(new CustomEvent('monitoring-changed'));
      },

      // Schritt mit Sprungziel: Energie, Effizienz, Gerätekarte oder Lernwerte.
      runStep(notice, index) {
        const step = this.stepsOf(notice)[index];
        if (!step || !step.target) return;
        if (step.target === 'learned') { this.goLearned(); return; }
        if (step.target === 'suggestion') { this.goSuggestion((step.ids || [])[0]); return; }
        if (step.target === 'action.role_keep') { this.roleKeep(step.ids); return; }
        const panel = panelFor(step.target);
        if (!panel) return;
        const detail = { panel };
        if (step.ids && step.ids.length) detail.focus = step.ids;
        if (step.chain) detail.chain = step.chain;
        // The energy page may not be mounted yet: it takes these requests in its load().
        if (panel === 'energy-panel') {
          window.__energyFocusRequest__ = detail.focus || null;
          window.__energyChainRequest__ = detail.chain || null;
        }
        window.dispatchEvent(new CustomEvent('monitoring-goto', { detail }));
        openTab(panel);
      },

      goLearned() {
        this.setView('learned');
        this.$nextTick(() => {
          const el = this.$el && this.$el.querySelector('.monitoring-learned');
          if (el && el.scrollIntoView) el.scrollIntoView({ block: 'start', behavior: reduced() ? 'auto' : 'smooth' });
        });
      },

      // Sprung zum aufgeklappten Vorschlag. Ist die Automationen-Seite noch nicht
      // geladen, nimmt ihr Band die Anfrage in load() entgegen.
      goSuggestion(id) {
        if (!id) return;
        window.__automationSuggestionRequest__ = id;
        window.dispatchEvent(new CustomEvent('automation-suggestion-open', { detail: { id } }));
        openTab('automations-panel');
      },

      learnedGroups() {
        return window.MonitoringModel.learnedGroups(this.learned.values, nowSeconds(), t);
      },
      countText(group) {
        return t('monitoring.learned.count', { have: group.have, total: group.total });
      },

      titleOf(notice) { return window.MonitoringModel.title(notice, t); },
      rowMeta(notice) {
        return [this.sourceLabel(notice.source), window.MonitoringModel.sinceText(notice.first_seen, nowSeconds(), t)].join(' · ');
      },

      stepsOf(notice) {
        return window.MonitoringModel.steps(notice, this.rights(), t);
      },

      // Karte einer Meldung als HTML. Ohne Recht Einrichten keine Quittier-Knöpfe.
      cardHtml(notice) {
        const model = window.MonitoringModel;
        const acked = Boolean(notice.ack);
        const severity = notice.severity === 'warn' ? 'warn' : 'info';
        const icon = severity === 'info' ? '#ico-info' : '#ico-warning';
        const since = model.sinceText(notice.first_seen, nowSeconds(), t);
        const facts = model.facts(notice, t);
        const factsHtml = facts.length
          ? '<dl class="monitoring-facts">' + facts.map(([k, v]) => '<dt>' + esc(k) + '</dt><dd>' + esc(v) + '</dd>').join('') + '</dl>'
          : '';
        let evidenceHtml = '';
        if (notice.evidence && window.AutomationEvidence && typeof window.AutomationEvidence.render === 'function') {
          evidenceHtml = '<div class="monitoring-evidence">' + window.AutomationEvidence.render(notice.evidence, t, {}) + '</div>';
        }
        const steps = this.stepsOf(notice);
        const stepsHtml = steps.length
          ? '<div class="monitoring-steps"><h4>' + esc(t('monitoring.steps.title')) + '</h4><ol>' + steps.map((s, index) => '<li><span>' + esc(s.text) + '</span>'
            + (s.target && s.label ? '<button type="button" class="monitoring-btn monitoring-btn-link" data-act="step" data-id="' + esc(notice.id) + '" data-index="' + index + '">' + esc(s.label) + '<svg aria-hidden="true"><use href="#ico-chevron"></use></svg></button>' : '')
            + '</li>').join('') + '</ol></div>'
          : '';
        const id = esc(notice.id);
        let foot = '';
        if (acked) {
          const at = new Date(Number(notice.ack.at) * 1000);
          const date = window.I18n.formatDate(at, { day: '2-digit', month: '2-digit', year: 'numeric' });
          foot += '<span class="monitoring-acked"><svg aria-hidden="true"><use href="#ico-check"></use></svg>' + esc(t('monitoring.ack.done', { date, user: notice.ack.user || '' })) + '</span>';
        }
        if (this.canAck()) {
          foot += acked
            ? '<button type="button" class="monitoring-btn monitoring-btn-quiet" data-act="unack" data-id="' + id + '">' + esc(t('monitoring.ack.undo')) + '</button>'
            : '<button type="button" class="monitoring-btn monitoring-btn-primary" data-act="ack" data-id="' + id + '"><svg aria-hidden="true"><use href="#ico-check"></use></svg>' + esc(t('monitoring.ack.button')) + '</button>';
        } else {
          foot += '<p class="monitoring-muted">' + esc(t('monitoring.ack.no_right')) + '</p>';
        }
        return '<article class="monitoring-card is-' + severity + (acked ? ' is-acked' : ' is-new') + '">'
          + '<header class="monitoring-card-head"><span class="monitoring-row-badge is-' + severity + '"><svg aria-hidden="true"><use href="' + icon + '"></use></svg></span>'
          + '<div class="monitoring-card-titles"><h4 class="monitoring-card-title">' + esc(model.title(notice, t)) + '</h4>'
          + '<p class="monitoring-card-meta"><span>' + esc(this.sourceLabel(notice.source)) + '</span><span>' + esc(since) + '</span>'
          + (acked ? '' : '<span class="monitoring-badge-new">' + esc(t('monitoring.badge.new')) + '</span>') + '</p></div></header>'
          + '<p class="monitoring-card-text">' + esc(model.text(notice, t)) + '</p>'
          + factsHtml + evidenceHtml + stepsHtml
          + '<footer class="monitoring-card-foot">' + foot + '</footer></article>';
      },

      onClick(event) {
        const btn = event.target.closest && event.target.closest('[data-act]');
        if (!btn || (this.$el && !this.$el.contains(btn))) return;
        const id = btn.dataset.id;
        if (btn.dataset.act === 'ack') this.ack(id);
        else if (btn.dataset.act === 'unack') this.unack(id);
        else if (btn.dataset.act === 'step') {
          const notice = this.notices.find((n) => n.id === id);
          if (notice) this.runStep(notice, Number(btn.dataset.index));
        }
      },
    };
  }

  const register = () => {
    if (window.Alpine) window.Alpine.data('monitoringPanel', monitoringPanel);
  };
  if (window.Alpine) register(); else document.addEventListener('alpine:init', register, { once: true });
})();
