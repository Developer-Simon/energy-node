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
      packageKind: '',
      repoPath: '',
      packageFile: null,

      get shell() {
        return window.Installer.shell;
      },

      get packageInfo() {
        return (this.shell.bootstrap || {}).package || null;
      },

      init() {
        try {
          this.host = window.localStorage.getItem(HOST_KEY) || '';
        } catch (err) {
          this.host = '';
        }
        var info = this.packageInfo;
        if (info) {
          this.packageKind = info.bundled ? 'bundled' : 'github';
          this.repoPath = (info.repo && info.repo.path) || '';
        }
      },

      get canConnect() {
        if (this.busy || this.fingerprint || !this.host || !this.user) {
          return false;
        }
        if (!(this.kind === 'password' ? this.secret !== '' : this.keyPath !== '')) {
          return false;
        }
        if (this.packageInfo) {
          if (this.packageKind === 'file' && !this.packageFile) {
            return false;
          }
          if (this.packageKind === 'repo' && !this.repoPath) {
            return false;
          }
        }
        return true;
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

      selectPackage(kind) {
        if (kind === 'repo' && !(this.packageInfo && this.packageInfo.repo.available)) {
          return;
        }
        this.packageKind = kind;
      },

      async submitPackage() {
        if (!this.packageInfo) {
          return;
        }
        if (this.packageKind === 'file') {
          await window.Api.upload('/api/package/upload', this.packageFile);
          return;
        }
        await window.Api.put('/api/package', {
          kind: this.packageKind,
          path: this.packageKind === 'repo' ? this.repoPath : '',
        });
      },

      async attempt(acceptFingerprint) {
        this.busy = true;
        this.shell.error = null;
        this.shell.progress = 'connect.progress.connecting';
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
          await this.submitPackage();
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
          this.shell.progress = null;
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
