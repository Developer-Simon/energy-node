// Bildschirm Paket vorbereiten: loest die gewaehlte Paketquelle fuer die
// Architektur des Geraets auf, uebertraegt das Paket und prueft es dort. Er
// laeuft ueber denselben Lauf-Weg wie die Schritte (POST /api/run, SSE), zeigt
// aber nur das Log des Pseudo-Schritts "package".
(function () {
  'use strict';

  var MAX_LINES = 500;

  window.Screens = window.Screens || {};
  window.Screens.screenPrepare = function screenPrepare() {
    return {
      lines: [],
      // working | done | failed
      state: 'working',
      unsigned: false,
      runId: null,
      // armed: erst ab unserem eigenen run-started zaehlen Ereignisse. Der
      // Strom beginnt bei seq 0 und traegt auch Aelteres.
      armed: false,
      stream: null,

      get shell() {
        return window.Installer.shell;
      },

      async init() {
        var self = this;
        try {
          var started = await window.Api.post('/api/run', { mode: 'prepare' });
          this.runId = started.run_id;
          this.stream = window.Events.open({
            since: 0,
            onEvent: function (type, data) { self.onEvent(type, data); },
          });
        } catch (err) {
          this.state = 'failed';
          this.shell.fail(err);
        }
      },

      onEvent(type, data) {
        if (type === 'run-started') {
          this.armed = data.run_id === this.runId;
          return;
        }
        if (!this.armed) {
          return;
        }
        if (type === 'log' && data.step_id === 'package') {
          var text = data.key ? window.I18n.t(data.key, data.args) : data.line;
          this.lines.push(text);
          if (this.lines.length > MAX_LINES) {
            this.lines.shift();
          }
        } else if (type === 'run-finished' && data.run_id === this.runId) {
          this.finish(data);
        }
      },

      async finish(data) {
        this.stop();
        if (!data.ok) {
          this.state = 'failed';
          this.shell.fail({ code: data.code, detail: data.detail });
          return;
        }
        try {
          var bootstrap = await window.Api.get('/api/bootstrap');
          this.shell.bootstrap = bootstrap;
          // Manifest und Auswahl gehoerten zum vorigen Paket.
          this.shell.shared.manifest = null;
          this.shell.shared.selection = null;
          var resolved = bootstrap.package && bootstrap.package.resolved;
          this.unsigned = !!(resolved && !resolved.signed);
          this.state = 'done';
          if (!this.unsigned) {
            this.next();
          }
        } catch (err) {
          this.state = 'failed';
          this.shell.fail(err);
        }
      },

      next() {
        this.shell.afterPrepare();
      },

      back() {
        this.stop();
        this.shell.back('connect');
      },

      async cancel() {
        try {
          await window.Api.post('/api/cancel', {});
        } catch (err) {
          this.shell.fail(err);
        }
      },

      stop() {
        if (this.stream) {
          this.stream.close();
          this.stream = null;
        }
      },

      destroy() {
        this.stop();
      },
    };
  };
})();
