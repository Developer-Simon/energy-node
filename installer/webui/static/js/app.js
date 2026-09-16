// Die Shell: Kopfleiste, Stepper, Einstiegspunkte, Bildschirmwechsel und das
// eine Fehlerbanner. Sie kennt keinen Bildschirm im Einzelnen - jede
// Bildschirmdatei haengt ihre Fabrik an window.Screens und erreicht die Shell
// ueber window.Installer.shell.
(function () {
  'use strict';

  window.Screens = window.Screens || {};
  window.Installer = window.Installer || {};

  // Plan C-II, Vertrag 5, als Tabellen.
  var FIRST_SCREEN = { install: 'precheck', redeploy: 'preview', diagnose: 'diagnose' };
  var FLOW = {
    install: ['connect', 'precheck', 'configure', 'run', 'result'],
    redeploy: ['connect', 'preview', 'run', 'result'],
  };
  // Auf diesen Bildschirmen ist noch nichts veraendert - nur dort steht der
  // Einstiegs-Umschalter.
  var HARMLESS = ['connect', 'precheck', 'preview', 'diagnose'];

  function emptyBar() {
    return { lead: '', sub: '', status: '', action: null };
  }

  function installerShell() {
    return {
      ready: false,
      bootstrap: null,
      entry: 'install',
      screen: 'connect',
      dir: 'forward',
      // '' statt einer echten Sprache: der erste Bildschirm haengt schon vor
      // init() am DOM (screen ist von Anfang an 'connect'), und t() haengt
      // sich per void this.lang an lang. Stuende hier schon 'en', waere die
      // Zuweisung in init() bei einer gemerkten Praeferenz 'en' ein No-op -
      // Alpine sieht keine Aenderung und zeichnet nie mit dem Katalog neu.
      lang: '',
      // mutating wird wahr, sobald ein Lauf angenommen wurde, und erst mit
      // dem Ergebnis wieder falsch. Es ist die eine Sperre fuer den Umschalter.
      mutating: false,
      connected: false,
      error: null,
      // progress ist der Uebersetzungsschluessel einer Zeile, waehrend ein
      // Bildschirm ohne sichtbare Rueckmeldung wartet (Verbindungsaufbau,
      // Vorpruefung) - an derselben Stelle wie error, aber blau statt rot.
      // Ein Fehler geht immer vor: fail() und error setzen es implizit weg.
      progress: null,
      // bar traegt, was ein Bildschirm in der Kopfleiste zeigt: lead hinter
      // dem Titel, sub vor dem Umschalter, status und action dahinter.
      bar: emptyBar(),
      shared: {
        target: { host: '', user: '' },
        manifest: null,
        selection: null,
        selectionAtEntry: null,
        servicesOnly: false,
        run: null,
        lastRun: null,
      },

      async init() {
        window.Installer.shell = this;
        var body = document.body;
        var basePath = body.getAttribute('data-base-path') || '';
        var token = body.getAttribute('data-token') || '';
        window.Api.configure({ basePath: basePath, token: token });
        try {
          this.bootstrap = await window.Api.get('/api/bootstrap');
          var wanted = this.bootstrap.language_fixed
            ? this.bootstrap.language
            : window.I18n.preferred(this.bootstrap.language);
          if ((this.bootstrap.languages || []).indexOf(wanted) < 0) {
            wanted = this.bootstrap.language;
          }
          await window.I18n.load(basePath, token, wanted);
          this.lang = wanted;
        } catch (err) {
          this.fail(err);
          this.ready = true;
          return;
        }
        this.entry = this.bootstrap.entry_points[0] || 'install';
        this.connected = !this.bootstrap.needs_connection;
        var hello = window.Events ? await window.Events.hello() : null;
        if (hello && hello.running) {
          // Ein Lauf ist unterwegs (Fenster neu geladen, Dashboard neu
          // gestartet): zurueck in die Ausfuehrung, sie holt ab seq 0 nach.
          this.connected = true;
          this.mutating = true;
          this.shared.run = { runId: hello.run_id, mode: '', only: '', resumed: true };
          this.screen = 'run';
        } else {
          this.screen = this.connected ? FIRST_SCREEN[this.entry] : 'connect';
        }
        this.ready = true;
      },

      // t liest zuerst lang: so haengt jeder Text an einer reaktiven
      // Abhaengigkeit und wird beim Sprachwechsel neu gerechnet.
      t(key, params) {
        void this.lang;
        return window.I18n.t(key, params);
      },

      tn(key, n, params) {
        void this.lang;
        return window.I18n.tn(key, n, params);
      },

      number(n) {
        void this.lang;
        return window.I18n.number(n);
      },

      get title() {
        return this.t('app.title.' + this.entry);
      },

      get chip() {
        if (this.screen === 'connect' || this.screen === 'diagnose' || !this.shared.target.host) {
          return '';
        }
        return this.shared.target.user + '@' + this.shared.target.host;
      },

      get errorText() {
        if (!this.error) {
          return '';
        }
        var key = 'error.' + this.error.code;
        var text = this.t(key);
        return text === key ? this.t('error.unknown', { code: this.error.code }) : text;
      },

      get progressText() {
        return this.progress ? this.t(this.progress) : '';
      },

      get showEntrySwitch() {
        return !this.mutating && !!this.bootstrap && this.bootstrap.entry_points.length > 1 &&
          HARMLESS.indexOf(this.screen) >= 0;
      },

      get showLanguageSwitch() {
        return this.screen === 'connect' && !!this.bootstrap && !this.bootstrap.language_fixed &&
          (this.bootstrap.languages || []).length > 1;
      },

      // stepperParts: Stationen und Verbinder abwechselnd, je mit fertiger
      // Klassenliste - ein <template x-for> darf nur ein Wurzelelement haben.
      get stepperParts() {
        if (!this.bootstrap || !FLOW[this.entry]) {
          return [];
        }
        var needsConnection = this.bootstrap.needs_connection;
        var flow = FLOW[this.entry].filter(function (id) {
          return id !== 'connect' || needsConnection;
        });
        var at = this.screen === 'configure' && this.entry === 'redeploy' ? 'preview' : this.screen;
        var position = flow.indexOf(at);
        if (position < 0) {
          return [];
        }
        var self = this;
        var parts = [];
        flow.forEach(function (id, index) {
          if (index > 0) {
            parts.push({ key: 'conn-' + index, item: false, cls: 'st-conn' + (index <= position ? ' filled' : '') });
          }
          var done = index < position;
          parts.push({
            key: id,
            item: true,
            done: done,
            n: index + 1,
            label: self.t('stepper.' + id),
            cls: 'st-item' + (done ? ' done' : index === position ? ' active' : ''),
          });
        });
        return parts;
      },

      navigate(screen, dir) {
        this.dir = dir;
        this.error = null;
        // progress raeumt sich verzoegert auf: setzte navigate() es hier
        // synchron auf null, faellt das in denselben Alpine-Durchlauf, in dem
        // Alpine das x-if des neuen Bildschirms (und damit dessen init()/
        // load(), das progress oft sofort neu setzt) synchron mitzieht -
        // progress kippt dann innerhalb eines einzigen Durchlaufs von wahr
        // auf null und zurueck auf wahr, und das Banner-x-if verpasst die
        // Aenderung (bleibt unsichtbar, obwohl progress am Ende wieder einen
        // Wert traegt). Der Mikrotask-Schub laeuft erst, nachdem dieser
        // Durchlauf fertig ist: hat der neue Bildschirm bis dahin selbst
        // einen neuen Wert gesetzt, ist stale veraltet und das Aufraeumen
        // unterbleibt - sonst greift es wie zuvor.
        var stale = this.progress;
        queueMicrotask(() => {
          if (this.progress === stale) {
            this.progress = null;
          }
        });
        this.bar = emptyBar();
        this.screen = screen;
      },

      go(screen) {
        this.navigate(screen, 'forward');
      },

      back(screen) {
        this.navigate(screen, 'back');
      },

      // Nur sinnvoll wenn bootstrap.host === 'dashboard': /redeploy/ ist ein
      // Unterpfad des Dashboards, das diese Oberflaeche mountet (siehe
      // dashboard/cmd/dashboard/redeploy.go). bootstrap.base_path nennt genau
      // diesen Mount-Pfad ("/redeploy") - alles davor in der tatsaechlichen
      // Browser-URL ist der Weg zurueck zum Dashboard, egal ob ein
      // Reverse-Proxy zusaetzlich einen eigenen Praefix voranstellt
      // (basepath.Middleware kennt hostapi.Options.BasePath selbst nicht,
      // aber die Browser-URL traegt jeden Praefix ohnehin schon). Als eigene,
      // reine Funktion herausgeloest, weil window.location.assign() selbst in
      // jsdom nicht ueberschreibbar ist (Location.prototype.assign ist
      // absichtlich non-configurable, wie im echten Browser) - so bleibt
      // wenigstens die Pfadberechnung pruefbar.
      dashboardRootPath() {
        var path = window.location.pathname;
        var marker = (this.bootstrap && this.bootstrap.base_path) || '/redeploy';
        var idx = path.indexOf(marker);
        return (idx >= 0 ? path.slice(0, idx) : '') + '/';
      },

      backToDashboard() {
        window.location.assign(this.dashboardRootPath());
      },

      switchEntry(name) {
        if (this.mutating || this.entry === name || !this.bootstrap || this.bootstrap.entry_points.indexOf(name) < 0) {
          return;
        }
        this.entry = name;
        this.shared.servicesOnly = false;
        this.go(this.connected ? FIRST_SCREEN[name] : 'connect');
      },

      openDiagnose() {
        if (this.entry === 'diagnose') {
          this.go('diagnose');
          return;
        }
        this.switchEntry('diagnose');
      },

      async switchLanguage(lang) {
        if (!this.bootstrap || this.bootstrap.language_fixed || this.lang === lang) {
          return;
        }
        var body = document.body;
        try {
          await window.I18n.load(body.getAttribute('data-base-path') || '', body.getAttribute('data-token') || '', lang);
          this.lang = lang;
        } catch (err) {
          this.fail(err);
        }
      },

      // afterConnect ruft der Verbindungsbildschirm, sobald die Verbindung
      // steht - die Shell entscheidet, wohin es dann geht.
      afterConnect(target) {
        this.connected = true;
        this.shared.target = { host: target.host || '', user: target.user || '' };
        this.go(FIRST_SCREEN[this.entry]);
      },

      fail(err) {
        this.progress = null;
        this.error = {
          code: err && err.code ? err.code : 'BACKEND_ERROR',
          detail: err && err.detail ? String(err.detail) : '',
        };
      },

      startRun(response, request) {
        this.mutating = true;
        this.shared.lastRun = null;
        this.shared.run = { runId: response.run_id, mode: request.mode, only: request.only || '', resumed: false };
        this.go('run');
      },

      finishRun(outcome) {
        this.mutating = false;
        this.shared.run = null;
        this.shared.lastRun = outcome;
        this.go('result');
      },
    };
  }

  // Registrierung erst in alpine:init: so ist gleich, in welcher Reihenfolge
  // die Bildschirmdateien laden - Hauptsache vor Alpine.
  document.addEventListener('alpine:init', function () {
    window.Alpine.data('installerShell', installerShell);
    Object.keys(window.Screens).forEach(function (name) {
      window.Alpine.data(name, window.Screens[name]);
    });
  });
})();
