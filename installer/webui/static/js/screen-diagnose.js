// Bildschirm Diagnose (Vorlage Diagnose): die Pruefliste aus diagnose.sh in
// drei Karten, zu jeder fehlgeschlagenen Pruefung "erneut ausfuehren".
(function () {
  'use strict';

  function optional(shell, key) {
    var text = shell.t(key);
    return text === key ? '' : text;
  }

  function stripService(name) {
    return String(name).replace(/\.service$/, '');
  }

  function levelOf(check) {
    if (check.ok) {
      return 'ok';
    }
    return check.severity === 'warn' ? 'warn' : 'bad';
  }

  // kindOf: die Art der Pruefung bestimmt Beschriftung und Klartext. Sie folgt
  // aus Gruppe und Gegenstand, nicht aus dem Namen.
  function kindOf(check) {
    var subject = check.subject || '';
    if (check.group === 'config') {
      return 'config';
    }
    // Shelly-Wake-Webhook (nur mit Opt-in, Schritt 35): Firewall-Regel und
    // Listener, der Port steht hinter dem Doppelpunkt.
    if (/^shelly-webhook-firewall:/.test(subject)) {
      return 'webhook-firewall';
    }
    if (/^shelly-webhook-listener:/.test(subject)) {
      return 'webhook-listener';
    }
    if (/\.service$/.test(subject)) {
      return 'unit';
    }
    if (/^\d+$/.test(subject)) {
      return 'port';
    }
    return subject === 'tailscale' ? 'tailscale' : 'other';
  }

  function portOf(check) {
    return String(check.subject || '').split(':')[1] || '';
  }

  var DiagnoseModel = {
    tally: function (view) {
      var out = { ok: 0, warn: 0, bad: 0 };
      ((view && view.checks) || []).forEach(function (check) {
        out[levelOf(check)] += 1;
      });
      return out;
    },

    retryLabel: function (stepId, manifest, shell) {
      var name = window.Services.stepLabel(manifest, stepId, shell);
      var found = window.Services.groupOf(window.Services.runGroups(manifest, { steps: {} }, shell), stepId);
      return found
        ? shell.t('diagnose.retry', { n: found.number, name: name })
        : shell.t('diagnose.retry_plain', { name: name });
    },

    cards: function (view, manifest, shell) {
      var t = shell.t.bind(shell);
      var byGroup = { services: [], system: [], config: [] };
      ((view && view.checks) || []).forEach(function (check) {
        (byGroup[check.group] || byGroup.system).push(check);
      });

      function row(parts, key, name, value, level) {
        var suffix = level === 'ok' ? '' : ' ' + level;
        parts.push({ key: key, type: 'r', cls: 'r', name: name, value: value, dot: 'd' + suffix, valueCls: 'r-v' + suffix });
      }

      function follow(parts, check, title, text, warnText) {
        var level = levelOf(check);
        if (level === 'ok') {
          return;
        }
        if (level === 'warn') {
          parts.push({ key: 'warn-' + check.name, type: 'warnbox', cls: 'warnbox', text: warnText || t('diagnose.warn.text') });
          return;
        }
        parts.push({
          key: 'fail-' + check.name, type: 'fail', cls: 'fail', chip: check.detail, title: title, text: text,
          retry: check.retry_step_id ? { stepId: check.retry_step_id, label: DiagnoseModel.retryLabel(check.retry_step_id, manifest, shell) } : null,
        });
      }

      function build(checks) {
        var parts = [];
        var ports = checks.filter(function (check) { return kindOf(check) === 'port'; });
        var portsShown = false;
        checks.forEach(function (check) {
          switch (kindOf(check)) {
            case 'port': {
              if (portsShown) {
                return;
              }
              portsShown = true;
              var closed = ports.filter(function (p) { return !p.ok; });
              var closedNames = closed.map(function (p) { return p.subject; }).join(' · ');
              row(parts, 'ports', t('diagnose.ports', { ports: ports.map(function (p) { return p.subject; }).join(' · ') }),
                closed.length ? t('diagnose.port.closed', { ports: closedNames }) : t('diagnose.port.open'),
                closed.length ? levelOf(closed[0]) : 'ok');
              if (closed.length) {
                follow(parts, closed[0], t('diagnose.fail.port.title'), t('diagnose.fail.port.text', { ports: closedNames }));
              }
              return;
            }
            case 'unit': {
              var name = check.group === 'services'
                ? stripService(check.subject)
                : optional(shell, 'unit.' + check.subject) || stripService(check.subject);
              row(parts, check.name, name, optional(shell, 'diagnose.unit.' + check.detail) || check.detail, levelOf(check));
              follow(parts, check, t('diagnose.fail.unit.title'), t('diagnose.fail.unit.text', { unit: check.subject }));
              return;
            }
            case 'webhook-firewall':
              row(parts, check.name, t('diagnose.webhook.firewall', { port: portOf(check) }),
                t(check.ok ? 'diagnose.webhook.firewall.allowed' : 'diagnose.webhook.firewall.missing'), levelOf(check));
              follow(parts, check, t('diagnose.fail.webhook.title'), t('diagnose.fail.webhook.text', { port: portOf(check) }));
              return;
            case 'webhook-listener':
              row(parts, check.name, t('diagnose.webhook.listener'),
                t(check.ok ? 'diagnose.webhook.listener.on' : 'diagnose.webhook.listener.off'), levelOf(check));
              follow(parts, check, '', '', t('diagnose.warn.webhook', { port: portOf(check) }));
              return;
            case 'tailscale':
              row(parts, check.name, t('diagnose.tailscale'), t(check.ok ? 'diagnose.tailscale.in' : 'diagnose.tailscale.out'), levelOf(check));
              follow(parts, check, t('diagnose.fail.tailscale.title'), t('diagnose.fail.tailscale.text'));
              return;
            case 'config':
              row(parts, check.name, check.subject || check.name, t(check.ok ? 'diagnose.config.present' : 'diagnose.config.missing'), levelOf(check));
              follow(parts, check, t('diagnose.fail.config.title'), t('diagnose.fail.config.text', { file: check.subject || check.name }));
              return;
            default:
              row(parts, check.name, check.subject || check.name, check.detail, levelOf(check));
              follow(parts, check, t('diagnose.fail.other.title'), check.detail);
          }
        });
        return parts;
      }

      return ['services', 'system', 'config']
        .filter(function (group) { return byGroup[group].length; })
        .map(function (group) {
          return { key: group, heading: t('diagnose.group.' + group), side: group === 'services' ? 'left' : 'right', parts: build(byGroup[group]) };
        });
    },

    // reportText: der gespeicherte Bericht ist ein Arbeitsdokument fuer den
    // Fehlerbericht - sprachneutral, eine Pruefung je Zeile.
    reportText: function (view, when) {
      var lines = ['energy-node diagnose ' + when.toISOString(), 'bundle ' + (view.bundle_version || ''), ''];
      (view.checks || []).forEach(function (check) {
        var mark = check.ok ? 'OK  ' : check.severity === 'warn' ? 'WARN' : 'FAIL';
        lines.push(mark + '  ' + check.name + '  ' + check.detail);
      });
      return lines.join('\n') + '\n';
    },
  };

  window.DiagnoseModel = DiagnoseModel;
  window.Screens = window.Screens || {};
  window.Screens.screenDiagnose = function screenDiagnose() {
    return {
      view: null,
      manifest: null,
      busy: false,
      checkedAt: 0,
      timer: null,

      get shell() {
        return window.Installer.shell;
      },

      async init() {
        var self = this;
        this.shell.bar.action = { label: this.shell.t('action.recheck'), run: function () { return self.load(); } };
        this.timer = window.setInterval(function () { self.updateBar(); }, 1000);
        await this.load();
      },

      async load() {
        this.busy = true;
        this.shell.error = null;
        this.shell.progress = 'diagnose.progress.loading';
        var shared = this.shell.shared;
        try {
          var results = await Promise.all([
            window.Api.get('/api/diagnose'),
            shared.manifest ? Promise.resolve(shared.manifest) : window.Api.get('/api/manifest'),
          ]);
          this.view = results[0];
          this.manifest = shared.manifest = results[1];
          this.checkedAt = Date.now();
        } catch (err) {
          this.shell.fail(err);
        } finally {
          this.busy = false;
          this.shell.progress = null;
          this.updateBar();
        }
      },

      updateBar() {
        var bar = this.shell.bar;
        var host = this.shell.shared.target.host || window.location.hostname;
        bar.lead = this.view
          ? this.shell.t('diagnose.bar.lead', { host: host, version: window.Format.plainVersion(this.view.bundle_version) })
          : '';
        bar.status = this.checkedAt
          ? this.shell.t('diagnose.bar.checked', { seconds: Math.max(0, Math.round((Date.now() - this.checkedAt) / 1000)) })
          : '';
      },

      destroy() {
        if (this.timer) {
          window.clearInterval(this.timer);
          this.timer = null;
        }
      },

      get cards() {
        return this.view && this.manifest ? DiagnoseModel.cards(this.view, this.manifest, this.shell) : [];
      },

      get leftCards() {
        return this.cards.filter(function (card) { return card.side === 'left'; });
      },

      get rightCards() {
        return this.cards.filter(function (card) { return card.side === 'right'; });
      },

      get tally() {
        return DiagnoseModel.tally(this.view);
      },

      async retry(part) {
        if (!part.retry || this.busy) {
          return;
        }
        this.busy = true;
        this.shell.error = null;
        try {
          var request = { mode: 'repair', only: part.retry.stepId };
          var response = await window.Api.post('/api/run', request);
          this.shell.startRun(response, request);
        } catch (err) {
          this.shell.fail(err);
        } finally {
          this.busy = false;
        }
      },

      save() {
        window.Download.text('energy-node-diagnose-' + window.Download.stamp(new Date()) + '.txt', DiagnoseModel.reportText(this.view, new Date()));
      },

      close() {
        var other = ((this.shell.bootstrap && this.shell.bootstrap.entry_points) || []).filter(function (entry) { return entry !== 'diagnose'; })[0];
        if (other) {
          this.shell.switchEntry(other);
        }
      },
    };
  };
})();
