// Bildschirme des Mediums: Geraet (Schritt 1) und Einstellungen (Schritt 2).
// Laufwerk, Schreiben und Fertig folgen auf eigenen Bildschirmen.
(function () {
  'use strict';

  var MEDIA_KEY = 'energy-node-installer.media';
  var DOCS_URL = 'https://developer-simon.github.io/energy-node/installer.html';
  var NAME_DELAY = 400;
  var DRIVE_POLL = 2000;
  var WRITE_POLL = 500;
  var DONE_CLOCK = 1000;
  var DONE_POLL = 5000;
  var SLOW_AFTER = 10 * 60 * 1000;
  var STEP_IDS = ['download', 'checksum', 'rights', 'write', 'verify', 'customize', 'eject'];

  window.Screens = window.Screens || {};

  // screenMediaDevice: Schritt 1. Prueft beim Eintritt den Rechner und den
  // Imager, zeigt bei Fehlern eine Karte statt der Liste und merkt sich die Wahl.
  window.Screens.screenMediaDevice = function screenMediaDevice() {
    return {
      // state: '' laedt, 'unsupported', 'missing' (Imager fehlt oder ist zu
      // alt, imagerKey ist 'missing' oder 'old'), 'failed' oder 'ready'.
      state: '',
      imagerKey: '',
      links: [],
      devices: [],
      groups: [],
      picked: '',

      get shell() {
        return window.Installer.shell;
      },

      init() {
        this.picked = (this.shell.shared.media && this.shell.shared.media.device) || '';
        return this.load();
      },

      async load() {
        this.state = '';
        var status;
        try {
          status = await window.Api.get('/api/media/status');
        } catch (err) {
          this.state = 'failed';
          this.shell.fail(err);
          return;
        }
        this.links = status.links || [];
        if (!status.supported) {
          this.state = 'unsupported';
          return;
        }
        if (!status.imager.found || status.imager.too_old) {
          this.imagerKey = status.imager.found ? 'old' : 'missing';
          this.state = 'missing';
          return;
        }
        try {
          var catalog = await window.Api.get('/api/media/catalog');
        } catch (err) {
          this.state = 'failed';
          this.shell.fail(err);
          return;
        }
        this.devices = catalog.devices || [];
        this.groups = window.MediaModel.groups(this.devices);
        this.state = 'ready';
      },

      recheck() {
        return this.load();
      },

      get device() {
        var id = this.picked;
        return this.devices.find(function (item) {
          return item.id === id;
        }) || null;
      },

      get canContinue() {
        return !!(this.device && this.device.image);
      },

      // summary: was das gewaehlte Geraet laden wuerde. offline, wenn die
      // Liste der Images fuer dieses Geraet nicht da ist.
      get summary() {
        var device = this.device;
        if (!device) {
          return null;
        }
        if (!device.image) {
          return { offline: true };
        }
        return {
          offline: false,
          name: device.image.name,
          pkg: this.shell.t('media.result.package', { arch: device.image.arch }),
          size: window.Format.megabytes(device.image.download_size, this.shell.lang),
          cached: device.image.cached,
        };
      },

      // tagFor: das Etikett einer Geraetezeile. Nur fehlende Tests tragen eins.
      tagFor(device) {
        return device && device.experimental ? this.shell.t('media.tag.experimental') : '';
      },

      pick(id) {
        this.picked = id;
      },

      openLink(link) {
        this.shell.openExternal(link.url);
      },

      openDocs() {
        this.shell.openExternal(DOCS_URL);
      },

      next() {
        if (!this.canContinue) {
          return;
        }
        this.shell.shared.media.device = this.picked;
        this.shell.go('media-settings');
      },
    };
  };

  // screenMediaSettings: Schritt 2. Vorbelegt aus den Vorgaben des Rechners und
  // dem, was sich der Installer gemerkt hat. Passwoerter werden nie gemerkt.
  window.Screens.screenMediaSettings = function screenMediaSettings() {
    return {
      form: window.MediaModel.defaultSettings({}, ''),
      timezones: [],
      keyboards: [],
      defaults: {},
      // nameState: '' vor der Pruefung, 'invalid', 'checking', 'ok', 'taken'
      // oder 'failed' (die Anfrage ist gescheitert, der Name bleibt erlaubt).
      nameState: '',
      suggestion: '',
      nameTimer: null,
      nameSeq: 0,
      // wifiState: '' ohne Pruefung, 'checking', 'match', 'mismatch', 'unknown'
      // oder 'failed'. Nur match und mismatch zeigen etwas an.
      wifiState: '',
      wifiTimer: null,
      wifiSeq: 0,

      get shell() {
        return window.Installer.shell;
      },

      async init() {
        var media = this.shell.shared.media || {};
        var defaults = {};
        var group = '';
        try {
          var catalog = await window.Api.get('/api/media/catalog');
          defaults = catalog.defaults || {};
          this.defaults = defaults;
          this.timezones = defaults.timezones || [];
          this.keyboards = defaults.keyboards || [];
          var device = (catalog.devices || []).find(function (item) {
            return item.id === media.device;
          });
          group = device ? device.group : '';
        } catch (err) {
          this.shell.fail(err);
        }

        var form = window.MediaModel.defaultSettings(defaults, group);
        var remembered = this.remembered();
        var self = this;
        Object.keys(window.MediaModel.remembered(form)).forEach(function (key) {
          if (remembered && Object.prototype.hasOwnProperty.call(remembered, key)) {
            // Ein leerer gemerkter WLAN-Name ersetzt nicht den erkannten.
            if (key === 'ssid' && remembered[key] === '') {
              return;
            }
            form[key] = remembered[key];
          }
        });
        // Was in dieser Sitzung schon eingegeben war, gilt vor den Vorgaben.
        if (media.settings) {
          Object.assign(form, media.settings);
        }
        this.form = form;
        // Das Land folgt immer der Zeitzone, auch nach gemerkten Werten.
        var country = window.MediaModel.countryFor(form.timezone, defaults);
        if (country) {
          form.country = country;
        }

        if (this.$watch) {
          this.$watch('form.hostname', function () {
            self.onNameInput();
          });
          this.$watch('form.timezone', function (timezone) {
            var country = window.MediaModel.countryFor(timezone, self.defaults);
            if (country) {
              self.form.country = country;
            }
          });
          this.$watch('form.ssid', function () {
            self.onWifiInput();
          });
          this.$watch('form.wifiPassword', function () {
            self.onWifiInput();
          });
          this.$watch('form.wifiOn', function () {
            self.onWifiInput();
          });
        }
        await this.checkName();
      },

      remembered() {
        try {
          var raw = window.localStorage.getItem(MEDIA_KEY);
          return raw ? JSON.parse(raw) : null;
        } catch (err) {
          return null;
        }
      },

      // onNameInput: beim Tippen sofort lokal pruefen, die Anfrage ans Netz
      // erst nach einer Pause.
      onNameInput() {
        clearTimeout(this.nameTimer);
        if (!window.MediaModel.validHostname(this.form.hostname)) {
          this.checkName();
          return;
        }
        this.nameSeq++;
        this.nameState = 'checking';
        var self = this;
        this.nameTimer = setTimeout(function () {
          self.checkName();
        }, NAME_DELAY);
      },

      // checkName: lokal ungueltig ist ohne Anfrage ungueltig. Eine Antwort zu
      // einem alten Namen wird verworfen.
      async checkName() {
        var name = this.form.hostname;
        var seq = ++this.nameSeq;
        this.suggestion = '';
        if (!window.MediaModel.validHostname(name)) {
          this.nameState = 'invalid';
          return;
        }
        this.nameState = 'checking';
        var result;
        try {
          result = await window.Api.get('/api/media/name?name=' + encodeURIComponent(name));
        } catch (err) {
          if (seq === this.nameSeq) {
            this.nameState = 'failed';
          }
          return;
        }
        if (seq !== this.nameSeq) {
          return;
        }
        if (result.taken) {
          this.nameState = 'taken';
          this.suggestion = result.suggestion || '';
        } else {
          this.nameState = result.valid ? 'ok' : 'invalid';
        }
      },

      async useSuggestion() {
        if (!this.suggestion) {
          return;
        }
        this.form.hostname = this.suggestion;
        await this.checkName();
      },

      pickNetwork(ssid) {
        this.form.ssid = ssid;
      },

      // onWifiInput: die Pruefung erst nach einer Pause, wie beim Namen.
      onWifiInput() {
        clearTimeout(this.wifiTimer);
        this.wifiSeq++;
        this.wifiState = '';
        var self = this;
        this.wifiTimer = setTimeout(function () {
          self.checkWifi();
        }, NAME_DELAY);
      },

      // checkWifi: fragt, ob dieser Rechner das Netz mit genau diesem Passwort
      // kennt. Das gespeicherte Passwort bleibt auf dem Rechner, zurueck kommt
      // nur das Ergebnis. Eine Antwort zu alten Eingaben wird verworfen.
      async checkWifi() {
        var seq = ++this.wifiSeq;
        if (!this.form.wifiOn || !this.form.ssid || !this.form.wifiPassword) {
          this.wifiState = '';
          return;
        }
        this.wifiState = 'checking';
        var result;
        try {
          result = await window.Api.post('/api/media/wifi-check', { ssid: this.form.ssid, password: this.form.wifiPassword });
        } catch (err) {
          if (seq === this.wifiSeq) {
            this.wifiState = 'failed';
          }
          return;
        }
        if (seq !== this.wifiSeq) {
          return;
        }
        this.wifiState = result.result || 'unknown';
      },

      get passwordShort() {
        return this.form.emergency && this.form.password !== '' && this.form.password.length < 8;
      },

      get passwordMismatch() {
        return this.form.emergency && this.form.password2 !== '' && this.form.password !== this.form.password2;
      },

      // countryKnown: das Land kommt aus der Zeitzone oder der Sprache. Nur
      // ohne beides fragt das Formular danach.
      get countryKnown() {
        return window.MediaModel.countryFor(this.form.timezone, this.defaults) !== '';
      },

      get fiveGhzOnly() {
        var media = this.shell.shared.media || {};
        return window.MediaModel.fiveGhzOnly(media.device, this.form.ssid, this.defaults);
      },

      get nearby() {
        return window.MediaModel.nearbyNetworks(this.defaults, this.form.ssid);
      },

      get canContinue() {
        return window.MediaModel.settingsComplete(this.form);
      },

      back() {
        this.shell.back('media-device');
      },

      next() {
        if (!this.canContinue) {
          return;
        }
        try {
          window.localStorage.setItem(MEDIA_KEY, JSON.stringify(window.MediaModel.remembered(this.form)));
        } catch (err) {
          // ohne Speicher bleibt es bei dieser Sitzung
        }
        this.shell.shared.media.settings = Object.assign({}, this.form);
        this.shell.go('media-drive');
      },
    };
  };

  // screenMediaDrive: Schritt 3. Holt die Laufwerke, solange der Bildschirm
  // offen ist, und hebt eine Auswahl auf, sobald ihr Laufwerk verschwindet.
  window.Screens.screenMediaDrive = function screenMediaDrive() {
    return {
      drives: [],
      showInternal: false,
      // limit: die Groesse, die das Image beim Entpacken braucht; kleinere
      // Laufwerke sind ausgegraut. isPi steuert den Kasten zur Aufteilung.
      limit: 0,
      isPi: false,
      selected: null,
      checked: false,
      typed: '',
      timer: null,
      seq: 0,

      get shell() {
        return window.Installer.shell;
      },

      async init() {
        var media = this.shell.shared.media || {};
        this.selected = media.drive ? media.drive.path : null;
        var catalog;
        try {
          catalog = await window.Api.get('/api/media/catalog');
        } catch (err) {
          this.shell.fail(err);
          return;
        }
        var device = (catalog.devices || []).find(function (item) {
          return item.id === media.device;
        });
        this.limit = device && device.image ? device.image.extract_size : 0;
        this.isPi = !!device && device.group === 'pi';
        this.timer = window.setInterval(() => this.load(), DRIVE_POLL);
        return this.load();
      },

      // load: holt die Liste. Antwortet eine altere Anfrage nach einer neueren,
      // zaehlt nur die neuere.
      async load() {
        if (this.shell.screen !== 'media-drive') {
          this.destroy();
          return;
        }
        var seq = ++this.seq;
        var drives;
        try {
          drives = await window.Api.get('/api/media/drives' + (this.showInternal ? '?internal=1' : ''));
        } catch (err) {
          if (seq === this.seq) {
            this.shell.fail(err);
          }
          return;
        }
        if (seq !== this.seq) {
          return;
        }
        this.drives = drives || [];
        if (this.selected && !this.selectedDrive) {
          this.clearSelection();
        } else {
          this.syncShared();
        }
      },

      // setInternal: der Schalter fuer interne Laufwerke. Die Liste wird gleich
      // neu geholt, mit oder ohne internal=1.
      setInternal(on) {
        this.showInternal = !!on;
        return this.load();
      },

      get visible() {
        var showInternal = this.showInternal;
        return this.drives.filter(function (item) {
          return showInternal || item.removable;
        });
      },

      get selectedDrive() {
        var path = this.selected;
        return this.visible.find(function (item) {
          return item.path === path;
        }) || null;
      },

      get canErase() {
        var drive = this.selectedDrive;
        return !!drive && this.fits(drive) && window.MediaModel.confirmed(drive, this.checked, this.typed);
      },

      get showLayout() {
        return this.isPi && !!this.selectedDrive;
      },

      fits(drive) {
        return !this.limit || drive.size >= this.limit;
      },

      labelFor(drive) {
        return drive.removable ? this.shell.t('media.drive.removable') : this.shell.t('media.drive.internal');
      },

      sizeText(drive) {
        return window.Format.gigabytes(drive.size / (1024 * 1024), this.shell.lang);
      },

      driveName(drive) {
        return drive.model || drive.path;
      },

      partitionsText(drive) {
        return this.shell.tn('media.warn.partitions', drive.partitions.length, { n: drive.partitions.length });
      },

      partitionText(part) {
        var used = window.Format.megabytes(part.used, this.shell.lang);
        return [part.label || part.name, part.fstype, this.shell.t('media.warn.used', { size: used })].filter(Boolean).join(', ');
      },

      pick(path) {
        var drive = this.visible.find(function (item) {
          return item.path === path;
        });
        if (!drive || !this.fits(drive)) {
          return;
        }
        if (this.selected !== path) {
          this.selected = path;
          this.checked = false;
          this.typed = '';
        }
        this.syncShared();
      },

      clearSelection() {
        this.selected = null;
        this.checked = false;
        this.typed = '';
        this.shell.shared.media.drive = null;
        this.shell.shared.media.confirm = '';
      },

      // syncShared: die Auswahl steht im Gedaechtnis der Sitzung, damit das
      // Schreiben das Laufwerk kennt. Die Bestaetigung kommt erst mit erase().
      syncShared() {
        var drive = this.selectedDrive;
        this.shell.shared.media.drive = drive ? Object.assign({}, drive) : null;
      },

      back() {
        this.destroy();
        this.shell.back('media-settings');
      },

      erase() {
        if (!this.canErase) {
          return;
        }
        var drive = this.selectedDrive;
        this.shell.shared.media.drive = Object.assign({}, drive);
        this.shell.shared.media.confirm = window.MediaModel.confirmationFor(drive);
        // Platzhalter fuer P3.4: heute nur die Karte, spaeter Stick oder Platte.
        this.shell.shared.media.target = 'card';
        this.destroy();
        this.shell.go('media-write');
      },

      destroy() {
        if (this.timer) {
          window.clearInterval(this.timer);
          this.timer = null;
        }
      },
    };
  };

  // screenMediaWrite: Schritt 4. Startet den Auftrag beim Eintritt und fragt
  // ihn alle 500 ms ab. Ende: Fertig. Fehler oder Abbruch: Banner, dann
  // Wiederholen oder Zurueck.
  window.Screens.screenMediaWrite = function screenMediaWrite() {
    return {
      job: null,
      running: false,
      failed: false,
      timer: null,

      get shell() {
        return window.Installer.shell;
      },

      init() {
        var media = this.shell.shared.media || {};
        if (!media.drive || !media.settings) {
          this.shell.go('media-device');
          return;
        }
        return this.start();
      },

      // rows: die Schritte des Auftrags, bis er antwortet alle wartend.
      get rows() {
        var rows = window.MediaModel.stepRows(this.job);
        if (rows.length) {
          return rows;
        }
        return STEP_IDS.map(function (id) {
          return { id: id, state: 'pending', labelKey: 'media.step.' + id };
        });
      },

      get percent() {
        return this.job ? Math.max(0, Math.min(100, this.job.percent || 0)) : 0;
      },

      get remainingText() {
        var seconds = window.MediaModel.remaining(this.job);
        if (seconds === null) {
          return '';
        }
        return this.shell.t('media.write.remaining', {
          time: window.Format.elapsed(seconds * 1000),
          rate: window.Format.megabytes(this.job.rate, this.shell.lang),
        });
      },

      get log() {
        return this.job && this.job.log ? this.job.log : [];
      },

      // start: POST /api/media/write. Erst nach der Annahme gilt der Lauf als
      // laufend und die Jobabfrage beginnt.
      async start() {
        var media = this.shell.shared.media;
        this.failed = false;
        this.job = null;
        try {
          await window.Api.post('/api/media/write', {
            device: media.device,
            drive: media.drive.path,
            confirm: window.MediaModel.confirmationFor(media.drive),
            settings: window.MediaModel.toRequest(media.settings),
          });
        } catch (err) {
          this.failed = true;
          this.shell.fail(err);
          return;
        }
        this.shell.mutating = true;
        this.running = true;
        this.timer = window.setInterval(() => this.poll(), WRITE_POLL);
        return this.poll();
      },

      async poll() {
        var job;
        try {
          job = await window.Api.get('/api/media/job');
        } catch (err) {
          if (this.running) {
            this.finish();
            this.failed = true;
            this.shell.fail(err);
          }
          return;
        }
        if (!this.running) {
          return;
        }
        this.job = job;
        if (job.state === 'idle' || job.state === 'running') {
          return;
        }
        this.finish();
        if (job.state === 'done') {
          this.shell.shared.media.target = job.target;
          this.shell.go('media-done');
          return;
        }
        this.failed = true;
        var error = job.error || { code: job.state === 'cancelled' ? 'MEDIA_CANCELLED' : 'MEDIA_WRITE_FAILED', detail: '' };
        this.shell.fail({ code: error.code, detail: error.detail });
      },

      // finish beendet Abfrage und Sperre. Jeder Ausgang geht hier durch.
      finish() {
        this.stop();
        this.shell.mutating = false;
      },

      stop() {
        this.running = false;
        if (this.timer) {
          window.clearInterval(this.timer);
          this.timer = null;
        }
      },

      cancel() {
        return window.Api.post('/api/media/cancel', {}).catch((err) => {
          this.shell.fail(err);
        });
      },

      retry() {
        this.shell.error = null;
        return this.start();
      },

      back() {
        this.destroy();
        this.shell.back('media-drive');
      },

      save() {
        this.shell.saveFile('energy-node-media.log', this.log.join('\n'));
      },

      destroy() {
        this.stop();
      },
    };
  };

  // screenMediaDone: Schritt 5. Wartet, bis der fertige Rechner antwortet,
  // zeigt nach zehn Minuten die Karte mit der Adresseingabe und uebergibt an
  // die Einrichtung.
  window.Screens.screenMediaDone = function screenMediaDone() {
    return {
      host: '',
      user: '',
      keyPath: '',
      ip: '',
      reachable: false,
      keyMatches: false,
      startedAt: 0,
      now: 0,
      slow: false,
      clockTimer: null,
      pollTimer: null,
      seq: 0,

      get shell() {
        return window.Installer.shell;
      },

      init() {
        var target = this.shell.shared.media.target;
        if (!target) {
          this.shell.go('media-device');
          return;
        }
        this.host = target.host || '';
        this.user = target.user || '';
        this.keyPath = target.key_path || '';
        this.startedAt = Date.now();
        this.now = this.startedAt;
        this.clockTimer = window.setInterval(() => this.tick(), DONE_CLOCK);
        this.pollTimer = window.setInterval(() => this.check(), DONE_POLL);
        return this.check();
      },

      // address: nach der Karte zaehlt die eingetippte IP-Adresse, sonst der Name.
      get address() {
        return this.ip.trim() || this.host;
      },

      get found() {
        return this.reachable && this.keyMatches;
      },

      get foreign() {
        return this.reachable && !this.keyMatches;
      },

      get waiting() {
        return !this.reachable;
      },

      get elapsed() {
        return window.Format.elapsed(this.now - this.startedAt);
      },

      tick() {
        this.now = Date.now();
        this.slow = this.now - this.startedAt >= SLOW_AFTER;
      },

      // check: fragt den Rechner ab. Eine Antwort zu einer alten Adresse zaehlt nicht.
      async check() {
        var seq = ++this.seq;
        var address = this.address;
        var result;
        try {
          result = await window.Api.get('/api/media/reachable?host=' + encodeURIComponent(address));
        } catch (err) {
          return;
        }
        if (seq !== this.seq) {
          return;
        }
        this.reachable = !!result.reachable;
        this.keyMatches = !!result.key_matches;
      },

      toSetup() {
        var address = this.address;
        this.destroy();
        this.shell.openSetup({ host: address, user: this.user, keyPath: this.keyPath });
      },

      another() {
        this.destroy();
        var media = this.shell.shared.media;
        media.drive = null;
        media.confirm = '';
        media.target = null;
        this.shell.go('media-device');
      },

      destroy() {
        if (this.clockTimer) {
          window.clearInterval(this.clockTimer);
          this.clockTimer = null;
        }
        if (this.pollTimer) {
          window.clearInterval(this.pollTimer);
          this.pollTimer = null;
        }
      },
    };
  };
})();
