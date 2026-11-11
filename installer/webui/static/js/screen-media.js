// Bildschirme des Mediums: Geraet (Schritt 1) und Einstellungen (Schritt 2).
// Laufwerk, Schreiben und Fertig folgen auf eigenen Bildschirmen.
(function () {
  'use strict';

  var MEDIA_KEY = 'energy-node-installer.media';
  var DOCS_URL = 'https://developer-simon.github.io/energy-node/installer.html';
  var NAME_DELAY = 400;

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
})();
