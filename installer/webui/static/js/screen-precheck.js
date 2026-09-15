// Bildschirm Vorpruefung (Vorlage Vorpruefung). Bis hierher wurde auf dem Node
// nichts veraendert - preflight.sh liest nur, und dieser Bildschirm zeigt nur.
(function () {
  'use strict';

  // Warnungen und Befunde, die eine eigene Zeile der Vorlage haben. Jeder
  // andere Code bekommt eine allgemeine Zeile.
  var ROW_WARNINGS = ['DISK_LOW', 'NO_INTERNET', 'SUDO_PASSWORD_REQUIRED', 'TIMEZONE_UTC'];
  var ROW_BLOCKING = ['ARCH_MISMATCH', 'PYTHON_ABI_MISMATCH'];

  // Die Bibliotheken, die scripts/build/lib/wheels.sh als eigene Wheels baut
  // (dieselbe Liste wie PreviewModel.WHEEL_COMPONENTS - ein Test haelt beide
  // gegen das Skript). dashboard/bootstrap sind die Kernbestandteile hinter
  // der "Dashboard"-Zeile, alles andere gehoert zur "Python-Dienste"-Zeile.
  var WHEEL_COMPONENTS = ['energy_node_common', 'battery_soc_core'];
  var CORE_COMPONENTS = ['dashboard', 'bootstrap'];

  // componentItem beschriftet eine Komponente fuer die aufklappbare
  // Versionsliste: bekannte Namen (Dashboard, Bootstrap, Dienste) bekommen
  // ihre eigene Uebersetzung, alles andere zeigt die generische Art (Wheel /
  // Abhaengigkeit) mit dem Rohnamen daneben - wie in der Vorschau (PreviewModel).
  function componentItem(name, version, shell) {
    var key = 'component.' + name;
    var text = shell.t(key);
    if (text !== key) {
      return { label: text, em: '', value: window.Format.plainVersion(version) };
    }
    var kind = WHEEL_COMPONENTS.indexOf(name) >= 0 ? 'wheel' : 'dependency';
    return { label: shell.t('component.' + kind), em: name, value: window.Format.plainVersion(version) };
  }

  function includes(list, code) {
    return (list || []).indexOf(code) >= 0;
  }

  function row(id, title, state, detail, em, remedy) {
    return { id: id, title: title, state: state, detail: detail || '', em: em || '', remedy: remedy || '' };
  }

  var PrecheckModel = {
    rows: function (report, manifest, shell) {
      var t = shell.t.bind(shell);
      var lang = shell.lang;
      var warnings = report.warnings || [];
      var blocking = report.blocking || [];
      var rows = [];

      var bits = window.Format.bits(report.arch);
      var archKey = (report.arch_ok ? 'precheck.arch.fits' : 'precheck.arch.mismatch') + (bits ? '' : '_plain');
      rows.push(row('arch', t('precheck.arch.title'), report.arch_ok ? 'ok' : 'bad',
        t(archKey, { arch: report.arch, bits: bits }), manifest.arch,
        report.arch_ok ? '' : t('fault.ARCH_MISMATCH.remediation')));

      var os = report.os_pretty_name || (report.os_id + ' ' + report.os_version_id).trim();
      rows.push(row('os', t('precheck.os.title'), report.python_abi_ok ? 'ok' : 'bad',
        report.python_abi_ok
          ? t('precheck.os.detail', { os: os, python: report.python_version })
          : t('precheck.os.abi_mismatch', { os: os, python: report.python_version, abi: manifest.python_abi }),
        '', report.python_abi_ok ? '' : t('fault.PYTHON_ABI_MISMATCH.remediation')));

      var low = includes(warnings, 'DISK_LOW');
      var free = window.Format.gigabytes(report.disk_free_mb, lang);
      var diskKey = 'precheck.disk.' + (low ? 'low' : 'detail') + (report.disk_total_mb > 0 ? '' : '_plain');
      rows.push(row('disk', t('precheck.disk.title'), low ? 'warn' : 'ok',
        t(diskKey, { free: free, total: window.Format.gigabytes(report.disk_total_mb, lang) })));

      rows.push(row('internet', t('precheck.internet.title'), report.internet ? 'ok' : 'warn',
        t(report.internet ? 'precheck.internet.ok' : 'precheck.internet.missing')));

      rows.push(row('sudo', t('precheck.sudo.title'), report.sudo_nopasswd ? 'ok' : 'warn',
        t(report.sudo_nopasswd ? 'precheck.sudo.ok' : 'precheck.sudo.missing')));

      rows.push(row('installed', t('precheck.installed.title'), 'ok', report.installed
        ? t('precheck.installed.found', { version: window.Format.plainVersion(report.installed_bundle_version) })
        : t('precheck.installed.none')));

      if (report.timezone) {
        var utc = includes(warnings, 'TIMEZONE_UTC');
        rows.push(row('timezone', t('precheck.timezone.title'), utc ? 'warn' : 'ok',
          t(utc ? 'precheck.timezone.utc' : 'precheck.timezone.ok', { timezone: report.timezone })));
      }

      warnings.forEach(function (code) {
        if (!includes(ROW_WARNINGS, code)) {
          rows.push(row('warning-' + code, t('warning.' + code), 'warn'));
        }
      });
      blocking.forEach(function (code) {
        if (!includes(ROW_BLOCKING, code)) {
          rows.push(row('blocking-' + code, t('fault.' + code + '.message'), 'bad', '', '', t('fault.' + code + '.remediation')));
        }
      });
      return rows;
    },

    transfer: function (manifest, shell) {
      // Die MQTT-Bruecke laeuft im Dashboard-Binary aus Schritt 60 und hat
      // keinen eigenen Dienst-Schritt - sie zaehlt trotzdem als Dienst.
      var services = (manifest.steps || []).filter(function (step) { return step.service_id; }).length + 1;
      var components = manifest.components || {};
      var coreItems = CORE_COMPONENTS.filter(function (name) { return components[name]; })
        .map(function (name) { return componentItem(name, components[name], shell); });
      var serviceItems = Object.keys(components).filter(function (name) { return !includes(CORE_COMPONENTS, name); })
        .map(function (name) { return componentItem(name, components[name], shell); });
      return [
        { key: 'dashboard', label: shell.t('precheck.transfer.dashboard'), value: shell.tn('precheck.transfer.binaries', 1), items: coreItems },
        { key: 'services', label: shell.t('precheck.transfer.services'), value: String(services), items: serviceItems },
        { key: 'wheels', label: shell.t('precheck.transfer.wheels'), value: String(manifest.wheel_count || 0), items: [] },
        { key: 'units', label: shell.t('precheck.transfer.units'), value: String(manifest.unit_count || 0), items: [] },
        { key: 'templates', label: shell.t('precheck.transfer.templates'), value: String(manifest.template_count || 0), items: [] },
      ];
    },
  };

  window.PrecheckModel = PrecheckModel;
  window.Screens = window.Screens || {};
  window.Screens.screenPrecheck = function screenPrecheck() {
    return {
      report: null,
      manifest: null,
      busy: false,
      // expanded haelt, welche "was uebertragen wird"-Zeilen aufgeklappt sind
      // (Schluessel = line.key). Nur Zeilen mit items koennen aufklappen.
      expanded: {},

      toggleTransfer(key) {
        this.expanded[key] = !this.expanded[key];
      },

      get shell() {
        return window.Installer.shell;
      },

      init() {
        return this.load();
      },

      async load() {
        this.busy = true;
        this.shell.error = null;
        this.shell.progress = 'precheck.progress.loading';
        try {
          var shared = this.shell.shared;
          var results = await Promise.all([
            window.Api.get('/api/precheck'),
            shared.manifest ? Promise.resolve(shared.manifest) : window.Api.get('/api/manifest'),
          ]);
          this.report = results[0];
          this.manifest = results[1];
          shared.manifest = results[1];
        } catch (err) {
          this.shell.fail(err);
        } finally {
          this.busy = false;
          this.shell.progress = null;
        }
      },

      get blocking() {
        return (this.report && this.report.blocking) || [];
      },

      get rows() {
        return this.report && this.manifest ? PrecheckModel.rows(this.report, this.manifest, this.shell) : [];
      },

      get summary() {
        return this.shell.t('precheck.card.text', { count: this.shell.number(this.rows.length) });
      },

      get hint() {
        var rows = this.rows;
        var bad = rows.filter(function (r) { return r.state === 'bad'; }).length;
        var warn = rows.filter(function (r) { return r.state === 'warn'; }).length;
        if (bad) {
          return this.shell.tn('precheck.hint.block', bad);
        }
        return warn ? this.shell.tn('precheck.hint.warn', warn) : this.shell.t('precheck.hint.clear');
      },

      get canProceed() {
        return !!this.report && !this.busy && this.rows.every(function (r) { return r.state !== 'bad'; });
      },

      get sumNumber() {
        return this.manifest ? window.Format.plainVersion(this.manifest.bundle_version) : '';
      },

      get sumUnit() {
        if (!this.manifest) {
          return '';
        }
        if (!this.manifest.bundle_bytes) {
          return this.manifest.arch;
        }
        return this.shell.t('precheck.transfer.unit', {
          arch: this.manifest.arch,
          size: window.Format.megabytes(this.manifest.bundle_bytes, this.shell.lang),
        });
      },

      get transfer() {
        return this.manifest ? PrecheckModel.transfer(this.manifest, this.shell) : [];
      },

      proceed() {
        if (this.canProceed) {
          this.shell.go('configure');
        }
      },

      back() {
        this.shell.back('connect');
      },
    };
  };
})();
