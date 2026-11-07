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
      // packageArgs: die Argumente von PACKAGE_UNAVAILABLE (names und
      // repo_possible), nach dem Fehler gesetzt.
      packageArgs: null,
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
          if (data.code === 'PACKAGE_UNAVAILABLE') {
            // Der Betreiber entscheidet im Bildschirm, wie es weitergeht.
            this.packageArgs = data.args || {};
          } else if (data.code === 'PACKAGE_VERIFY_FAILED_DELTA' || data.code === 'PYTHON_ABI_MISMATCH') {
            if (data.detail) {
              this.lines.push(window.Format.verifyDetail(data.detail, function (key, args) { return window.I18n.t(key, args); }));
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

      // canAllowOnline: the node's Python does not match the wheels in the
      // package. The operator can let the node fetch its Python dependencies
      // from the internet instead, an explicit choice because those files are
      // not covered by the package signature.
      get canAllowOnline() {
        return this.state === 'failed' && this.lastErrorCode === 'PYTHON_ABI_MISMATCH';
      },

      // canChoosePackages: ein Paket, das das Gerät braucht, fehlt und ließ
      // sich nicht laden. Der Betreiber antwortet je Paket mit "repo" oder
      // "skip", der Lauf fragt so lange, bis alle beantwortet sind.
      get canChoosePackages() {
        return this.state === 'failed' && this.lastErrorCode === 'PACKAGE_UNAVAILABLE';
      },

      // packageRepoOffered: die Paketquelle des Geräts kann die Pakete
      // installieren, sonst bleibt nur das Weitermachen ohne sie.
      get packageRepoOffered() {
        return !!(this.packageArgs && this.packageArgs.repo_possible === 'true');
      },

      // packageNames: die fehlenden Pakete in Anzeigeform, "Caddy, Tailscale".
      get packageNames() {
        return this.packageKeys().map(function (name) {
          return name.charAt(0).toUpperCase() + name.slice(1);
        }).join(', ');
      },

      packageKeys() {
        var names = this.packageArgs && this.packageArgs.names;
        return names ? names.split(',').map(function (name) { return name.trim(); }) : [];
      },

      // packageNotice: der Text im Hinweis, je fehlendem Paket ein Satz mit
      // seinen Folgen, dazu der Hinweis auf die Paketquelle, wenn sie geht.
      get packageNotice() {
        var t = function (key, args) { return window.I18n.t(key, args); };
        var parts = [t('prepare.packageUnavailable', { names: this.packageNames })];
        if (this.packageRepoOffered) {
          parts.push(t('prepare.packageRepoHint'));
        }
        this.packageKeys().forEach(function (name) {
          parts.push(t('prepare.packageConsequence.' + name));
        });
        return parts.join(' ');
      },

      // choosePackages: die Antwort gilt für alle fehlenden Pakete. "repo"
      // installiert sie aus der Paketquelle des Geräts, "skip" lässt sie weg.
      async choosePackages(choice) {
        var choices = {};
        this.packageKeys().forEach(function (name) { choices[name] = choice; });
        this.state = 'working';
        this.lines = [];
        this.lastErrorCode = null;
        this.packageArgs = null;
        this.shell.error = null;
        await this.start({
          mode: 'prepare',
          force_full_transfer: !!this.shell.shared.forceFullTransfer,
          package_choices: choices,
        });
      },

      async allowOnline() {
        this.state = 'working';
        this.lines = [];
        this.lastErrorCode = null;
        this.shell.error = null;
        await this.start({
          mode: 'prepare',
          force_full_transfer: !!this.shell.shared.forceFullTransfer,
          allow_online_deps: true,
        });
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
