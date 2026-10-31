// Bildschirm Konfiguration (Vorlage Konfiguration). Der Knopf "Weiter" fuehrt
// zum Dashboard, dort beginnt mit "Installation starten" der Lauf. Im
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
          window.Services.dropUnmet(this.manifest, this.steps);
          // Ein allgemeines Bundle traegt kein Ziel: dann gilt der Benutzer,
          // mit dem die Verbindung besteht, und sein Heimatverzeichnis.
          var loginUser = (shared.target && shared.target.user) || '';
          this.targetUser = this.manifest.target_user || loginUser;
          this.targetBase = this.manifest.target_base || (this.targetUser ? '/home/' + this.targetUser : '');
          this.mqttUser = this.manifest.target_user || loginUser;
          // Zurueck vom Dashboard: die Eingaben von vorhin bleiben stehen.
          var kept = this.shell.installInput;
          if (kept) {
            this.steps = Object.assign({}, kept.steps);
            this.mqttPassword = kept.mqttPassword;
            this.adminPassword = kept.adminPassword;
          }
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
        window.Services.dropUnmet(this.manifest, steps);
      },

      toggleChip(chip) {
        this.steps[chip.id] = !chip.on;
        window.Services.dropUnmet(this.manifest, this.steps);
      },

      get canStart() {
        return !this.busy && !!this.manifest && !!this.mqttUser && !!this.mqttPassword &&
          !!this.adminPassword && !!this.targetUser && !!this.targetBase;
      },

      get canApply() {
        return !this.busy && !!this.manifest;
      },

      // start reicht die Eingaben an das Dashboard weiter, dort beginnt der
      // Lauf. Die Passwoerter verlassen diesen Bildschirm nur im Speicher der
      // Shell, nie in der Auswahl oder im Lauf selbst.
      start() {
        if (!this.canStart) {
          return;
        }
        this.shell.installInput = {
          steps: Object.assign({}, this.steps),
          mqttUser: this.mqttUser,
          mqttPassword: this.mqttPassword,
          adminPassword: this.adminPassword,
          targetUser: this.targetUser,
          targetBase: this.targetBase,
        };
        this.shell.go('dashboard');
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
        this.shell.installInput = null;
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
