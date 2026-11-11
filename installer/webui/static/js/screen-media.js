// Bildschirme des Mediums: Geraet (Schritt 1) und Einstellungen (Schritt 2).
// Laufwerk, Schreiben und Fertig folgen auf eigenen Bildschirmen.
(function () {
  'use strict';

  var MEDIA_KEY = 'energy-node-installer.media';
  var DOCS_URL = 'https://developer-simon.github.io/energy-node/installer.html';
  var NAME_DELAY = 400;
  var DRIVE_POLL = 2000;

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
      // nameState: '' vor der Pruefung, 'invalid', 'checking', 'ok', 'taken'
      // oder 'failed' (die Anfrage ist gescheitert, der Name bleibt erlaubt).
      nameState: '',
      suggestion: '',
      nameTimer: null,
      nameSeq: 0,

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
            form[key] = remembered[key];
          }
        });
        // Was in dieser Sitzung schon eingegeben war, gilt vor den Vorgaben.
        if (media.settings) {
          Object.assign(form, media.settings);
        }
        this.form = form;

        if (this.$watch) {
          this.$watch('form.hostname', function () {
            self.onNameInput();
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

      get passwordShort() {
        return this.form.emergency && this.form.password !== '' && this.form.password.length < 8;
      },

      get passwordMismatch() {
        return this.form.emergency && this.form.password2 !== '' && this.form.password !== this.form.password2;
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
})();
