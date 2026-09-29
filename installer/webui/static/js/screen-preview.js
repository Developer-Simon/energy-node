// Bildschirm Vorschau (Vorlage Aktualisieren) - der Bildschirm, den Plan D
// unveraendert ins Dashboard einbaut. Er spricht nur /api/* und veraendert
// bis "Aktualisieren" nichts.
(function () {
  'use strict';

  var WHEEL_COMPONENTS = window.Services.WHEEL_COMPONENTS;
  // dashboard traegt die Bundle-Version (make_bundle.sh) - sie steht in der
  // Versionsleiste, nicht in der Liste.
  var PACKAGE = 'dashboard';
  var PLAIN = ['bootstrap', 'services'];

  function plain(version) {
    return window.Format.plainVersion(version);
  }

  var PreviewModel = {
    WHEEL_COMPONENTS: WHEEL_COMPONENTS,

    // serviceVersions: je gewaehltem Dienst seine eigene Version aus dem
    // Plan (von/nach) - sie ersetzt die Sammelzeile "services". Ein fehlendes
    // "von" ist neu, ausser der Plan nennt die Version unbekannt (ein
    // installiertes Manifest von vor den Dienstversionen).
    serviceVersions: function (plan, manifest, shell) {
      var byId = {};
      ((manifest && manifest.steps) || []).forEach(function (step) { byId[step.id] = step; });
      var rows = [];
      ((plan && plan.steps) || []).forEach(function (step) {
        var entry = byId[step.id];
        if (!entry || !entry.service_id || step.state === 'deselected' || !step.to) {
          return;
        }
        var from = step.from ? plain(step.from) : null;
        var to = plain(step.to);
        rows.push({
          key: 'service-' + step.id, changed: from !== to,
          label: window.Services.serviceName(entry, shell), em: '',
          from: from !== null ? from : shell.t(step.restart === 'unknown' ? 'component.unknown' : 'component.new'),
          to: to,
        });
      });
      return rows;
    },

    components: function (plan, shell, manifest) {
      var all = (plan && plan.components) || {};
      var services = PreviewModel.serviceVersions(plan, manifest, shell);
      var rank = function (name) {
        if (PLAIN.indexOf(name) >= 0) {
          return PLAIN.indexOf(name);
        }
        return WHEEL_COMPONENTS.indexOf(name) >= 0 ? 10 + WHEEL_COMPONENTS.indexOf(name) : 100;
      };
      var names = Object.keys(all).filter(function (name) { return name !== PACKAGE; }).sort(function (a, b) {
        return rank(a) - rank(b) || (a < b ? -1 : a > b ? 1 : 0);
      });
      var rows = [];
      var unchangedDependencies = 0;
      names.forEach(function (name) {
        if (name === 'services' && services.length) {
          rows.push.apply(rows, services);
          return;
        }
        var delta = all[name];
        var from = delta.from === null || delta.from === undefined ? null : plain(delta.from);
        var to = plain(delta.to);
        var changed = from !== to;
        var kind = PLAIN.indexOf(name) >= 0 ? 'plain' : WHEEL_COMPONENTS.indexOf(name) >= 0 ? 'wheel' : 'dependency';
        if (kind === 'dependency' && !changed) {
          unchangedDependencies += 1;
          return;
        }
        rows.push({
          key: name, changed: changed,
          label: shell.t(kind === 'plain' ? 'component.' + name : 'component.' + kind),
          em: kind === 'plain' ? '' : name,
          from: from === null ? shell.t('component.new') : from,
          to: to,
        });
      });
      if (unchangedDependencies) {
        rows.push({ key: 'rest', changed: false, label: shell.t('component.rest'), em: '', from: '', to: shell.t('component.unchanged') });
      }
      return rows;
    },

    // restart: die Units, die neu starten, je mit ihrem Grund. Ein Dienst
    // startet nur neu, wenn der Plan ihm einen Grund gibt (step.restart,
    // restart_rule.py) oder "Alle neu starten" an ist. Der Schritt, der die
    // Dienst-Station anfuehrt (60), hat keine Unit im Manifest - er ist das
    // Dashboard und laeuft wie bisher immer mit.
    restart: function (plan, manifest, shell, restartAll) {
      var group = window.Services.runGroups(manifest, { steps: {} }, shell).filter(function (g) { return g.subs; })[0];
      var core = group ? group.ids[0] : '';
      var rows = [];
      var seen = {};
      ((plan && plan.steps) || []).forEach(function (step) {
        if (step.state !== 'pending') {
          return;
        }
        var unit = step.unit || (step.id === core ? window.Services.DASHBOARD_UNIT : '');
        var isService = !!step.unit;
        if (!unit || seen[unit] || (isService && !restartAll && !step.restart)) {
          return;
        }
        seen[unit] = true;
        var reason = isService ? step.restart || 'all' : 'package';
        rows.push({ unit: unit, reason: shell.t('preview.restart.reason.' + reason) });
      });
      return rows;
    },

    kept: function (plan, manifest, shell) {
      var serviceIds = ((manifest && manifest.steps) || []).filter(function (s) { return s.service_id; }).map(function (s) { return s.id; });
      return ((plan && plan.steps) || [])
        .filter(function (step) { return step.state === 'done' && serviceIds.indexOf(step.id) < 0; })
        .map(function (step) { return window.Services.stepName(step.id, shell); })
        .join(' · ');
    },
  };

  window.PreviewModel = PreviewModel;
  window.Screens = window.Screens || {};
  window.Screens.screenPreview = function screenPreview() {
    return {
      plan: null,
      manifest: null,
      busy: false,
      restartAll: false,

      get shell() {
        return window.Installer.shell;
      },

      get selection() {
        return this.shell.shared.selection;
      },

      init() {
        return this.load();
      },

      async load() {
        this.busy = true;
        this.shell.error = null;
        this.shell.progress = 'preview.progress.loading';
        var shared = this.shell.shared;
        try {
          var results = await Promise.all([
            shared.manifest ? Promise.resolve(shared.manifest) : window.Api.get('/api/manifest'),
            window.Api.get('/api/selection'),
            window.Api.get('/api/plan'),
          ]);
          this.manifest = shared.manifest = results[0];
          shared.selection = results[1];
          this.plan = shared.plan = results[2];
          if (!shared.selectionAtEntry) {
            shared.selectionAtEntry = JSON.parse(JSON.stringify(results[1]));
          }
        } catch (err) {
          this.shell.fail(err);
        } finally {
          this.busy = false;
          this.shell.progress = null;
        }
      },

      get fromVersion() {
        var delta = this.plan && this.plan.components && this.plan.components.dashboard;
        return delta && delta.from ? plain(delta.from) : '';
      },

      get toVersion() {
        return this.plan ? plain(this.plan.bundle_version) : '';
      },

      get selectionChanged() {
        var before = this.shell.shared.selectionAtEntry;
        if (!before || !this.selection) {
          return false;
        }
        var on = function (steps) {
          return Object.keys(steps || {}).filter(function (id) { return steps[id] === true; }).sort().join(',');
        };
        return on(before.steps) !== on(this.selection.steps);
      },

      get note() {
        if (!this.manifest) {
          return '';
        }
        return this.manifest.arch + ' · ' + this.shell.t(this.selectionChanged ? 'preview.selection.changed' : 'preview.selection.unchanged');
      },

      get components() {
        return this.plan ? PreviewModel.components(this.plan, this.shell, this.manifest) : [];
      },

      get restart() {
        return this.plan && this.manifest ? PreviewModel.restart(this.plan, this.manifest, this.shell, this.restartAll) : [];
      },

      get keepNames() {
        return this.plan && this.manifest ? PreviewModel.kept(this.plan, this.manifest, this.shell) : '';
      },

      get serviceParts() {
        if (!this.manifest || !this.selection) {
          return [];
        }
        var parts = [];
        window.Services.toggles(this.manifest, this.selection, this.shell).forEach(function (row) {
          parts.push({
            key: row.key, type: 'svc', cls: 'svc' + (row.on ? '' : ' off'),
            name: row.name, on: row.on, isNew: row.kind !== 'devices' && !row.known, chips: null,
          });
          if (row.chips) {
            parts.push({ key: row.key + '-chips', type: 'chips', cls: 'chips', chips: row.chips });
          }
        });
        return parts;
      },

      get canStart() {
        return !!this.plan && !this.busy;
      },

      get showReuse() {
        return !!this.shell.bootstrap && this.shell.bootstrap.host === 'installer';
      },

      editServices() {
        this.shell.shared.servicesOnly = true;
        this.shell.go('configure');
      },

      async start() {
        if (!this.canStart) {
          return;
        }
        this.busy = true;
        this.shell.error = null;
        try {
          var body = { mode: 'redeploy' };
          if (this.restartAll) {
            body.restart_all = true;
          }
          var response = await window.Api.post('/api/run', body);
          this.shell.shared.selectionAtEntry = null;
          this.shell.startRun(response, { mode: 'redeploy' });
        } catch (err) {
          this.shell.fail(err);
        } finally {
          this.busy = false;
        }
      },

      async cancel() {
        var shared = this.shell.shared;
        if (this.selectionChanged) {
          try {
            shared.selection = await window.Api.put('/api/selection', { steps: shared.selectionAtEntry.steps });
          } catch (err) {
            this.shell.fail(err);
            return;
          }
        }
        shared.selectionAtEntry = null;
        if (this.shell.bootstrap && this.shell.bootstrap.needs_connection) {
          this.shell.back('connect');
          return;
        }
        // Im Dashboard (Plan D) gibt es keinen "Verbindung"-Bildschirm, zu
        // dem needs_connection sonst zurueckfuehrt - ohne diesen Zweig
        // laedt Abbrechen hier nur die Vorschau neu. Faellt die Vorschau
        // selbst schon (z.B. MANIFEST_UNREADABLE, weil noch kein Bundle
        // bereitliegt), wiederholte das denselben Fehler statt den Nutzer
        // je wieder herauszulassen.
        if (this.shell.bootstrap && this.shell.bootstrap.host === 'dashboard') {
          this.shell.backToDashboard();
          return;
        }
        await this.load();
      },
    };
  };
})();
