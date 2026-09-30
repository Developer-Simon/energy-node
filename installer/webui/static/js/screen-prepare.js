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
      // lastErrorCode is the run-finished event's code when state is
      // 'failed' -- canForceFull reads it to decide whether to offer the
      // recommendation dialog, without shell.fail ever seeing this
      // particular code (mirrors screen-connect's own handling of
      // HOSTKEY_UNKNOWN: a dedicated inline recovery UI, not the generic
      // error banner).
      lastErrorCode: null,
      // armed: erst ab unserem eigenen run-started zaehlen Ereignisse. Der
      // Strom beginnt bei seq 0 und traegt auch Aelteres.
      armed: false,
      stream: null,

      get shell() {
        return window.Installer.shell;
      },

      async init() {
        await this.start({ mode: 'prepare', force_full_transfer: !!this.shell.shared.forceFullTransfer });
      },

      // start posts /api/run and opens the event stream; init() and
      // forceFull() both begin a run this same way, differing only in the
      // request body.
      async start(body) {
        var self = this;
        try {
          var started = await window.Api.post('/api/run', body);
          this.runId = started.run_id;
          this.armed = false;
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
          this.lastErrorCode = data.code || null;
          if (data.code === 'PACKAGE_VERIFY_FAILED_DELTA') {
            if (data.detail) {
              this.lines.push(data.detail);
            }
          } else {
            this.shell.fail({ code: data.code, detail: data.detail });
          }
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

      // canForceFull is true only right after a delta transfer's own verify
      // failure -- the one case where retrying makes sense without the
      // operator re-choosing anything else.
      get canForceFull() {
        return this.state === 'failed' && this.lastErrorCode === 'PACKAGE_VERIFY_FAILED_DELTA';
      },

      async forceFull() {
        this.state = 'working';
        this.lines = [];
        this.lastErrorCode = null;
        this.shell.error = null;
        await this.start({ mode: 'prepare', force_full_transfer: true });
      },

      // Ein Wirt, der sein Paket selbst besorgt, hat keinen Verbindungs-
      // bildschirm, zu dem es zurueckgehen koennte.
      get autoPrepare() {
        return !!(this.shell.bootstrap && this.shell.bootstrap.auto_prepare);
      },

      // Schlaegt das Laden fehl, kann der Nutzer mit einem Paket weitermachen,
      // das schon vorher im Kandidatenverzeichnis lag.
      get canUseExisting() {
        return this.autoPrepare && this.state === 'failed' && !!this.shell.bootstrap.bundle_version;
      },

      useExisting() {
        this.stop();
        this.shell.error = null;
        this.shell.afterPrepare();
      },

      back() {
        this.stop();
        if (this.autoPrepare) {
          this.shell.backToDashboard();
          return;
        }
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
