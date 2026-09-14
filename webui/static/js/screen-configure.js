// Bildschirm Konfiguration (Vorlage Konfiguration). Ab hier wird das Geraet
// veraendert - der Lauf beginnt mit "Installation starten". Im
// Nur-Dienste-Modus ("Dienste aendern" aus der Vorschau) nur die Auswahl.
(function () {
  'use strict';

  window.Screens = window.Screens || {};
  window.Screens.screenConfigure = function screenConfigure() {
    return {
      manifest: null,
      steps: {},
      mqttUser: '',
      mqttPassword: '',
      adminPassword: '',
      showMqtt: false,
      showAdmin: false,
      targetUser: '',
      targetBase: '',
      busy: false,

      get shell() {
        return window.Installer.shell;
      },

      get servicesOnly() {
        return this.shell.shared.servicesOnly;
      },

      async init() {
        this.busy = true;
        try {
          var shared = this.shell.shared;
          var results = await Promise.all([
            shared.manifest ? Promise.resolve(shared.manifest) : window.Api.get('/api/manifest'),
            shared.selection ? Promise.resolve(shared.selection) : window.Api.get('/api/selection'),
          ]);
          shared.manifest = results[0];
          shared.selection = results[1];
          this.manifest = results[0];
          this.steps = Object.assign({}, results[1].steps);
          this.targetUser = this.manifest.target_user || '';
          this.targetBase = this.manifest.target_base || '';
          this.mqttUser = this.manifest.target_user || '';
        } catch (err) {
          this.shell.fail(err);
        } finally {
          this.busy = false;
        }
      },

      get rows() {
        return this.manifest ? window.Services.toggles(this.manifest, { steps: this.steps }, this.shell) : [];
      },

      toggle(row) {
        var next = !row.on;
        var steps = this.steps;
        row.ids.forEach(function (id) { steps[id] = next; });
      },

      toggleChip(chip) {
        this.steps[chip.id] = !chip.on;
      },

      get canStart() {
        return !this.busy && !!this.manifest && !!this.mqttUser && !!this.mqttPassword &&
          !!this.adminPassword && !!this.targetUser && !!this.targetBase;
      },

      get canApply() {
        return !this.busy && !!this.manifest;
      },

      async start() {
        if (!this.canStart) {
          return;
        }
        this.busy = true;
        this.shell.error = null;
        try {
          this.shell.shared.selection = await window.Api.put('/api/selection', { steps: this.steps });
          var response = await window.Api.post('/api/run', {
            mode: 'install',
            mqtt_user: this.mqttUser,
            mqtt_password: this.mqttPassword,
            admin_password: this.adminPassword,
            target_user: this.targetUser,
            target_base: this.targetBase,
          });
          // Die Passwoerter gingen genau einmal hinaus und bleiben nirgends.
          this.mqttPassword = '';
          this.adminPassword = '';
          this.shell.startRun(response, { mode: 'install' });
        } catch (err) {
          this.shell.fail(err);
        } finally {
          this.busy = false;
        }
      },

      async apply() {
        if (!this.canApply) {
          return;
        }
        this.busy = true;
        this.shell.error = null;
        try {
          this.shell.shared.selection = await window.Api.put('/api/selection', { steps: this.steps });
          this.shell.shared.servicesOnly = false;
          this.shell.back('preview');
        } catch (err) {
          this.shell.fail(err);
        } finally {
          this.busy = false;
        }
      },

      back() {
        if (this.servicesOnly) {
          this.shell.shared.servicesOnly = false;
          this.shell.back('preview');
          return;
        }
        this.shell.back('precheck');
      },
    };
  };
})();
