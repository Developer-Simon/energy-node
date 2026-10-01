// Bildschirm "Was ist neu": was das Paket gegenueber dem Installierten aendert,
// je Komponente, mit Breaking Changes oben und Filtern nach Art, Bereich und
// Text. Er liest nur /api/changelog und veraendert nichts - dieselbe Seite steht
// im Installer (ueber SSH) und im Dashboard (lokal). Die Rechnung selbst steckt
// in changelog-model.js.
(function () {
  'use strict';

  window.Screens = window.Screens || {};
  window.Screens.screenChangelog = function screenChangelog() {
    return {
      view: null,
      loading: true,
      missing: false,
      kind: '',
      scope: '',
      query: '',

      get shell() {
        return window.Installer.shell;
      },

      get model() {
        return window.ChangelogModel;
      },

      init() {
        return this.load();
      },

      async load() {
        this.loading = true;
        this.missing = false;
        this.shell.error = null;
        this.shell.progress = 'changelog.progress.loading';
        try {
          this.view = await window.Api.get('/api/changelog');
        } catch (err) {
          // Kein Changelog im Paket (404) oder Wirt kann es nicht (501) ist ein
          // Zustand, den der Bildschirm benennt - kein Fehlerbanner.
          if (err && (err.status === 404 || err.status === 501)) {
            this.missing = true;
          } else {
            this.shell.fail(err);
          }
        } finally {
          this.loading = false;
          this.shell.progress = null;
        }
      },

      get rows() {
        return this.model.slice(this.view);
      },

      get visible() {
        return this.model.filter(this.rows, { kind: this.kind, scope: this.scope, query: this.query });
      },

      get breaking() {
        return this.model.breaking(this.rows);
      },

      get kinds() {
        return this.model.kinds(this.rows);
      },

      get scopes() {
        return this.model.scopes(this.rows);
      },

      get hasRows() {
        return this.rows.length > 0;
      },

      get nothingMatches() {
        return this.hasRows && this.visible.length === 0;
      },

      get installedVersion() {
        var installed = this.view && this.view.installed && this.view.installed.dashboard;
        return installed ? window.Format.plainVersion(installed) : '';
      },

      get packageVersion() {
        return this.view ? window.Format.plainVersion(this.view.bundle_version) : '';
      },

      // "3 Neuerungen · 12 Korrekturen · 1 Breaking Change" - Nullen entfallen.
      get countLine() {
        var summary = this.model.summarize(this.rows);
        var shell = this.shell;
        var out = [];
        [['feat', 'changelog.count.feat'], ['fix', 'changelog.count.fix'], ['other', 'changelog.count.other'], ['breaking', 'changelog.count.breaking']].forEach(function (pair) {
          if (summary[pair[0]] > 0) {
            out.push(shell.tn(pair[1], summary[pair[0]]));
          }
        });
        return out.join(' · ');
      },

      kindLabel(kind) {
        var key = 'changelog.kind.' + kind;
        var text = this.shell.t(key);
        return text === key ? this.shell.t('changelog.kind.other') : text;
      },

      // Die Gruppenueberschrift kommt aus dem Katalog (die Sprache der
      // Oberflaeche); nur eine Art, die der Katalog nicht kennt, faellt auf die
      // englische Ueberschrift der Datei zurueck.
      groupLabel(group) {
        var key = 'changelog.group.' + group.type;
        var text = this.shell.t(key);
        return text === key ? group.label : text;
      },

      toggleKind(kind) {
        this.kind = this.kind === kind ? '' : kind;
      },

      back() {
        this.shell.back(this.shell.shared.changelogFrom || 'preview');
      },
    };
  };
})();
