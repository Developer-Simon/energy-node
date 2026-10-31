// Bildschirm Dashboard (Vorlage Dashboard): was der Installer am fertigen
// Dashboard gleich mit einrichtet. Jede Gruppe ist entweder unveraendert
// (default, null im Zustand), gesetzt (set) oder auf "Später konfigurieren"
// (later). Der Lauf startet hier: Konfiguration reicht nur die Eingaben weiter.
(function () {
  'use strict';

  window.Screens = window.Screens || {};

  var NAME_RE = /^[a-z0-9._-]{3,32}$/;
  var PASSWORD_MIN = 10;

  // Die Gruppen-IDs sind zugleich die IDs der Ersteinrichtung (Task 8).
  // FIELDS nennt je Gruppe die Felder des Zustands; ihr Startwert ist der
  // Zustand "unveraendert".
  var FIELDS = {
    users: { users: [] },
    guest: { guest_access: null },
    theme: { theme: null },
    title: { title: '', subtitle: '' },
    statusbar: { status_bar_items: null, show_runtime_status: null },
    tabwidth: { wide_panels: null },
    updates: { update_check: null },
    metrics: { metrics: null },
  };

  // Chip-Gruppen: Feld, alle IDs in Reihenfolge und die Vorgabe beim ersten Umschalten.
  var CHIPS = {
    statusbar: {
      field: 'status_bar_items',
      all: ['mqtt', 'cache', 'storage', 'uptime', 'version', 'cpu_temp', 'cpu_load', 'ram', 'undervoltage'],
      standard: ['mqtt', 'storage', 'uptime', 'version'],
    },
    tabwidth: {
      field: 'wide_panels',
      all: ['overview', 'devices', 'history', 'diagnostics', 'config', 'energy', 'layout', 'devicemap', 'settings', 'automations'],
      standard: [],
    },
    metrics: {
      field: 'metrics',
      all: ['cpu_temp', 'cpu_load', 'ram', 'disk', 'wifi_signal', 'undervoltage', 'throttled', 'last_boot',
        'ip_address', 'mosquitto', 'tailscale', 'apt_updates'],
      standard: ['cpu_temp', 'cpu_load', 'ram', 'disk'],
    },
  };

  function defaultPrefs() {
    return {
      admin_user: '', users: [], guest_access: null, theme: null, title: '', subtitle: '',
      status_bar_items: null, show_runtime_status: null, wide_panels: null, update_check: null,
      metrics: null, later: [],
    };
  }

  function copy(value) {
    return JSON.parse(JSON.stringify(value));
  }

  function reducedMotion() {
    return typeof window.matchMedia === 'function' && window.matchMedia('(prefers-reduced-motion: reduce)').matches;
  }

  // Farbschema wirkt sofort auf dieses Fenster: ueberblenden statt hart
  // umschalten, damit hell und dunkel nicht springen.
  function applyTheme(value) {
    var apply = function () {
      document.documentElement.dataset.theme = value === 'mint' ? '' : value;
    };
    if (document.startViewTransition && !reducedMotion()) {
      document.startViewTransition(apply);
    } else {
      apply();
    }
  }

  // Ein Startwert [] heisst "gesetzt, sobald Eintraege da sind".
  function differs(value, initial) {
    return Array.isArray(initial) ? value.length > 0 : value !== initial;
  }

  window.Screens.screenDashboard = function screenDashboard() {
    return {
      busy: false,
      // before merkt sich den Zustand vor "Später konfigurieren", damit
      // "Doch jetzt" ihn wiederherstellt.
      before: {},

      get shell() {
        return window.Installer.shell;
      },

      get prefs() {
        return this.shell.shared.dashboardPrefs;
      },

      init() {
        if (!this.shell.shared.dashboardPrefs) {
          this.shell.shared.dashboardPrefs = defaultPrefs();
        }
      },

      isSet(key) {
        var fields = FIELDS[key];
        var prefs = this.prefs;
        return Object.keys(fields).some(function (name) {
          return differs(prefs[name], fields[name]);
        });
      },

      groupState(key) {
        if (this.prefs.later.indexOf(key) >= 0) {
          return 'later';
        }
        return this.isSet(key) ? 'set' : 'default';
      },

      // Später konfigurieren legt die Gruppe still; Doch jetzt holt den
      // vorigen Zustand zurueck.
      toggleLater(key) {
        var prefs = this.prefs;
        var fields = FIELDS[key];
        var index = prefs.later.indexOf(key);
        if (index >= 0) {
          prefs.later.splice(index, 1);
          var saved = this.before[key] || {};
          Object.keys(fields).forEach(function (name) {
            prefs[name] = name in saved ? saved[name] : copy(fields[name]);
          });
          delete this.before[key];
          return;
        }
        var snapshot = {};
        Object.keys(fields).forEach(function (name) {
          snapshot[name] = copy(prefs[name]);
          prefs[name] = copy(fields[name]);
        });
        this.before[key] = snapshot;
        prefs.later.push(key);
      },

      chooseTheme(value) {
        this.prefs.theme = value;
        applyTheme(value);
      },

      chipOn(key, id) {
        var spec = CHIPS[key];
        var list = this.prefs[spec.field];
        return (list === null ? spec.standard : list).indexOf(id) >= 0;
      },

      toggleChip(key, id) {
        var spec = CHIPS[key];
        var on = !this.chipOn(key, id);
        var self = this;
        this.prefs[spec.field] = spec.all.filter(function (item) {
          return item === id ? on : self.chipOn(key, item);
        });
      },

      setAllChips(key, on) {
        this.prefs[CHIPS[key].field] = on ? CHIPS[key].all.slice() : [];
      },

      allChips(key) {
        var self = this;
        return CHIPS[key].all.every(function (id) {
          return self.chipOn(key, id);
        });
      },

      addUser() {
        this.prefs.users.push({ name: '', template: 'operate', password: '' });
      },

      removeUser(index) {
        this.prefs.users.splice(index, 1);
      },

      nameValid(name) {
        return NAME_RE.test(name);
      },

      passwordValid(password) {
        return String(password || '').length >= PASSWORD_MIN;
      },

      userValid(user) {
        return this.nameValid(user.name) && this.passwordValid(user.password);
      },

      get usersValid() {
        var self = this;
        return this.prefs.users.every(function (user) {
          return self.userValid(user);
        });
      },

      get adminValid() {
        var name = this.prefs.admin_user;
        return name === '' || this.nameValid(name);
      },

      get canStart() {
        return !this.busy && !!this.shell.installInput && this.usersValid && this.adminValid;
      },

      async start() {
        if (!this.canStart) {
          return;
        }
        var shell = this.shell;
        var input = shell.installInput;
        this.busy = true;
        shell.error = null;
        try {
          shell.shared.selection = await window.Api.put('/api/selection', { steps: input.steps });
          var response = await window.Api.post('/api/run', {
            mode: 'install',
            mqtt_user: input.mqttUser,
            mqtt_password: input.mqttPassword,
            admin_password: input.adminPassword,
            target_user: input.targetUser,
            target_base: input.targetBase,
          });
          // Die Passwoerter gingen genau einmal hinaus und bleiben nirgends.
          this.prefs.users.forEach(function (user) { user.password = ''; });
          shell.installInput = null;
          shell.startRun(response, { mode: 'install' });
        } catch (err) {
          shell.fail(err);
        } finally {
          this.busy = false;
        }
      },

      back() {
        this.shell.back('configure');
      },
    };
  };
})();
