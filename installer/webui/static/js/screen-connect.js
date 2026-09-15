// Bildschirm Verbindung (Vorlage Main): Adresse, Benutzer, Anmeldeart, der
// Fingerabdruck beim ersten Kontakt und das Angebot, ein Schluesselpaar zu
// hinterlegen.
(function () {
  'use strict';

  var HOST_KEY = 'energy-node-installer.host';

  window.Screens = window.Screens || {};
  window.Screens.screenConnect = function screenConnect() {
    return {
      host: '',
      user: '',
      kind: 'password',
      secret: '',
      keyPath: '',
      makeKey: true,
      // fingerprint ist gesetzt, solange ein unbekannter Host-Key auf
      // Bestaetigung wartet. Ein geaenderter Key setzt ihn nie.
      fingerprint: '',
      busy: false,

      get shell() {
        return window.Installer.shell;
      },

      init() {
        try {
          this.host = window.localStorage.getItem(HOST_KEY) || '';
        } catch (err) {
          this.host = '';
        }
      },

      get canConnect() {
        if (this.busy || this.fingerprint || !this.host || !this.user) {
          return false;
        }
        return this.kind === 'password' ? this.secret !== '' : this.keyPath !== '';
      },

      get hint() {
        return this.fingerprint ? this.shell.t('connect.hint.fingerprint') : '';
      },

      get packageLabel() {
        var bootstrap = this.shell.bootstrap || {};
        return this.shell.t('connect.package.bundled', {
          version: window.Format.plainVersion(bootstrap.bundle_version),
          arch: bootstrap.bundle_arch || '',
        });
      },

      connect() {
        return this.canConnect ? this.attempt('') : Promise.resolve();
      },

      confirmFingerprint() {
        return this.fingerprint && !this.busy ? this.attempt(this.fingerprint) : Promise.resolve();
      },

      async attempt(acceptFingerprint) {
        this.busy = true;
        this.shell.error = null;
        try {
          var result = await window.Api.post('/api/connect', {
            host: this.host,
            user: this.user,
            kind: this.kind,
            secret: this.kind === 'password' ? this.secret : '',
            key_path: this.kind === 'key' ? this.keyPath : '',
            accept_fingerprint: acceptFingerprint,
          });
          this.fingerprint = '';
          try {
            window.localStorage.setItem(HOST_KEY, this.host);
          } catch (err) {
            // ohne gemerkte Adresse laesst sich trotzdem arbeiten
          }
          var keypairError = null;
          if (this.makeKey && this.kind === 'password') {
            try {
              await window.Api.post('/api/keypair', {});
            } catch (err) {
              keypairError = err;
            }
          }
          this.secret = '';
          this.shell.afterConnect({ host: result.host || this.host, user: result.user || this.user });
          if (keypairError) {
            // erst nach dem Wechsel: go() raeumt das Banner sonst gleich weg
            this.shell.fail(keypairError);
          }
        } catch (err) {
          if (err.code === 'HOSTKEY_UNKNOWN' && err.detail) {
            this.fingerprint = err.detail;
          } else {
            this.fingerprint = '';
            this.shell.fail(err);
          }
        } finally {
          this.busy = false;
        }
      },

      reset() {
        this.fingerprint = '';
        this.secret = '';
        this.shell.error = null;
      },
    };
  };
})();
