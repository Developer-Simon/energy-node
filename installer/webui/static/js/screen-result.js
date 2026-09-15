// Bildschirm Ergebnis (Vorlage Ergebnis): gelungen, fehlgeschlagen oder
// abgebrochen - dieselben Bausteine, andere Inhalte.
(function () {
  'use strict';

  // Die zwei Schritte, deren Auswahl das Ergebnis veraendert: Tailscale bringt
  // den Punkt "Schluesselablauf", Caddy die HTTPS-Adresse und das Zertifikat.
  // 8080 ist der unverschluesselte Port des Dashboards (diag.fixedPortSteps).
  var TAILSCALE_STEP = '40';
  var CADDY_STEP = '70';
  var DASHBOARD_PORT = '8080';

  window.Screens = window.Screens || {};
  window.Screens.screenResult = function screenResult() {
    return {
      diagnose: null,

      get shell() {
        return window.Installer.shell;
      },

      get outcome() {
        return this.shell.shared.lastRun ||
          { ok: false, code: '', mode: '', only: '', steps: {}, groups: [], lastLines: [], logText: '', startedAt: 0, finishedAt: 0 };
      },

      async init() {
        if (!this.outcome.ok || this.outcome.mode !== 'install') {
          return;
        }
        try {
          this.diagnose = await window.Api.get('/api/diagnose');
        } catch (err) {
          // Die Zahl der aktiven Dienste ist eine Zugabe; ohne sie steht das
          // Ergebnis trotzdem.
          this.diagnose = null;
        }
      },

      get ok() {
        return !!this.outcome.ok;
      },

      get cancelled() {
        return this.outcome.code === 'RUN_CANCELLED';
      },

      get duration() {
        return window.Format.duration(this.outcome.finishedAt - this.outcome.startedAt);
      },

      get manifest() {
        return this.shell.shared.manifest || {};
      },

      selected(stepId) {
        return window.Services.isSelected({ id: stepId, optional: true }, this.shell.shared.selection);
      },

      label(stepId) {
        return window.Services.stepLabel(this.manifest, stepId, this.shell);
      },

      get heading() {
        if (this.ok) {
          return this.shell.t('result.ok.heading');
        }
        return this.shell.t(this.cancelled ? 'result.cancelled.heading' : 'result.fail.heading');
      },

      get summary() {
        var shell = this.shell;
        var outcome = this.outcome;
        if (this.cancelled) {
          return shell.t('result.cancelled.summary', { duration: this.duration });
        }
        if (!outcome.ok) {
          return shell.t('result.fail.summary', { step: this.label(outcome.stepId), duration: this.duration });
        }
        if (outcome.mode === 'repair') {
          return shell.t('result.repair.summary', { step: this.label(outcome.only), duration: this.duration });
        }
        var version = window.Format.plainVersion(this.manifest.bundle_version);
        if (outcome.mode === 'redeploy') {
          var plan = shell.shared.plan;
          var from = plan && plan.components && plan.components.bootstrap ? window.Format.plainVersion(plan.components.bootstrap.from) : '';
          return shell.t('result.redeploy.summary', {
            from: from || '?', to: version, restarted: shell.tn('result.restarted', this.restarted), duration: this.duration,
          });
        }
        if (!this.diagnose) {
          return shell.t('result.ok.summary_short', { duration: this.duration, version: version, arch: this.manifest.arch });
        }
        var count = this.activeCount;
        return shell.t('result.ok.summary', {
          active: count.active, selected: count.selected, duration: this.duration, version: version, arch: this.manifest.arch,
        });
      },

      get servicesGroup() {
        return (this.outcome.groups || []).filter(function (group) { return group.subs; })[0] || null;
      },

      get activeCount() {
        var group = this.servicesGroup;
        var units = (this.diagnose && this.diagnose.units) || {};
        var on = group ? group.subs.filter(function (sub) { return sub.on; }) : [];
        return { selected: on.length, active: on.filter(function (sub) { return units[sub.unit] === 'active'; }).length };
      },

      // restarted: verschiedene Units der Dienst-Station, deren Schritt ok
      // meldete. Schritt 60 hat keine Unit im Manifest - er ist das Dashboard.
      get restarted() {
        var group = this.servicesGroup;
        if (!group) {
          return 0;
        }
        var steps = this.outcome.steps || {};
        var manifestSteps = this.manifest.steps || [];
        var units = {};
        group.ids.forEach(function (id) {
          if (!steps[id] || steps[id].state !== 'ok') {
            return;
          }
          var step = manifestSteps.filter(function (s) { return s.id === id; })[0];
          units[step && step.unit ? step.unit : window.Services.DASHBOARD_UNIT] = true;
        });
        return Object.keys(units).length;
      },

      get urls() {
        var host = this.shell.shared.target.host || window.location.hostname;
        var http = { url: 'http://' + host + ':' + DASHBOARD_PORT, hint: this.shell.t('result.url.http'), lead: false };
        if (!this.selected(CADDY_STEP)) {
          http.lead = true;
          return [http];
        }
        return [{ url: 'https://' + host, hint: this.shell.t('result.url.https'), lead: true }, http];
      },

      get primaryUrl() {
        return this.urls[0].url;
      },

      get todos() {
        var t = this.shell.t.bind(this.shell);
        var outcome = this.outcome;
        var list = [];
        if (outcome.loginPending && outcome.loginUrl) {
          list.push({ key: 'login', title: t('result.todo.login.title'), text: outcome.loginUrl });
        }
        if (this.selected(TAILSCALE_STEP)) {
          list.push({ key: 'tailscale', title: t('result.todo.tailscale.title'), text: t('result.todo.tailscale.text') });
        }
        if (this.selected(CADDY_STEP)) {
          list.push({ key: 'caddy', title: t('result.todo.caddy.title'), text: t('result.todo.caddy.text') });
        }
        list.push({ key: 'devices', title: t('result.todo.devices.title'), text: t('result.todo.devices.text') });
        return list.map(function (item, index) { return Object.assign({ n: index + 1 }, item); });
      },

      get todoText() {
        var n = this.todos.length;
        var key = 'result.todo.count.' + n;
        var word = this.shell.t(key);
        return this.shell.tn('result.todo.text', n, { count: word === key ? String(n) : word });
      },

      get fault() {
        var t = this.shell.t.bind(this.shell);
        var code = this.outcome.code;
        var key = 'fault.' + code + '.remediation';
        var remediation = t(key);
        return { message: window.RunModel.faultText(code, t), remediation: remediation === key ? '' : remediation };
      },

      get lines() {
        return this.outcome.lastLines || [];
      },

      openUrl(url) {
        window.open(url, '_blank', 'noopener');
      },

      save() {
        window.Download.text('energy-node-protokoll-' + window.Download.stamp(new Date()) + '.txt', this.outcome.logText || '');
      },

      openDiagnose() {
        this.shell.openDiagnose();
      },

      retry() {
        var mode = this.outcome.mode;
        this.shell.back(mode === 'redeploy' ? 'preview' : mode === 'repair' ? 'diagnose' : 'configure');
      },
    };
  };
})();
