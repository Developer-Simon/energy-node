// Bildschirm Ausfuehrung (Vorlage Ausfuehrung). Liest den Strom immer ab
// seq 0; das Laufmodell nimmt nur, was zur eigenen run_id gehoert. So sind
// Start, Neuladen und Wiederaufnahme derselbe Weg.
(function () {
  'use strict';

  window.Screens = window.Screens || {};
  window.Screens.screenRun = function screenRun() {
    return {
      model: null,
      groups: [],
      manifest: null,
      selection: null,
      // offset: Uhr des Wirts minus Uhr des Browsers, aus hello.at.
      offset: 0,
      tick: 0,
      follow: true,
      cancelling: false,
      stream: null,
      timer: null,

      get shell() {
        return window.Installer.shell;
      },

      async init() {
        var run = this.shell.shared.run;
        if (!run) {
          return;
        }
        this.model = window.RunModel.create(run.runId);
        var shared = this.shell.shared;
        try {
          var results = await Promise.all([
            shared.manifest ? Promise.resolve(shared.manifest) : window.Api.get('/api/manifest'),
            shared.selection ? Promise.resolve(shared.selection) : window.Api.get('/api/selection'),
          ]);
          this.manifest = shared.manifest = results[0];
          this.selection = shared.selection = results[1];
          this.shell.bar.sub = this.shell.t('run.bar', {
            version: window.Format.plainVersion(this.manifest.bundle_version),
            arch: this.manifest.arch,
          });
        } catch (err) {
          this.shell.fail(err);
        }
        var self = this;
        this.stream = window.Events.open({
          since: 0,
          onEvent: function (type, data) { self.onEvent(type, data); },
          onRestart: function (hello) { self.onRestart(hello); },
        });
        this.timer = window.setInterval(function () { self.tick += 1; }, 1000);
      },

      onEvent(type, data) {
        if (type === 'hello') {
          if (data.at) {
            this.offset = data.at - Date.now();
          }
          return;
        }
        if (!window.RunModel.apply(this.model, type, data)) {
          return;
        }
        if (type === 'run-started') {
          this.onStarted();
        } else if (type === 'log' || type === 'step') {
          this.scrollLog();
        }
        if (this.model.finished) {
          this.stop();
          this.shell.finishRun(window.RunModel.outcome(this.model, this.groups));
        }
      },

      // onRestart: der Wirt ist neu gestartet und hat den Lauf wieder
      // aufgenommen. Sein Bus spielt ihn ab seq 1 neu ab, mit run-started
      // unter der ID, die hello nennt - das Modell faengt unter ihr von vorn an.
      onRestart(hello) {
        var run = this.shell.shared.run;
        if (hello.run_id) {
          run.runId = hello.run_id;
        }
        this.model = window.RunModel.create(run.runId);
      },

      onStarted() {
        var run = this.shell.shared.run;
        run.mode = this.model.mode;
        run.only = this.model.only;
        if (run.resumed) {
          this.shell.entry = this.model.mode === 'repair' ? 'diagnose' : this.model.mode;
        }
        if (!this.manifest) {
          this.groups = [];
        } else if (this.model.only) {
          this.groups = [{
            key: 'only', ids: [this.model.only], subs: null,
            label: window.Services.stepLabel(this.manifest, this.model.only, this.shell),
          }];
        } else {
          this.groups = window.Services.runGroups(this.manifest, this.selection, this.shell);
        }
      },

      stop() {
        if (this.stream) {
          this.stream.close();
          this.stream = null;
        }
        if (this.timer) {
          window.clearInterval(this.timer);
          this.timer = null;
        }
      },

      destroy() {
        this.stop();
      },

      async cancel() {
        if (this.cancelling) {
          return;
        }
        this.cancelling = true;
        try {
          await window.Api.post('/api/cancel', {});
        } catch (err) {
          this.cancelling = false;
          this.shell.fail(err);
        }
      },

      save() {
        window.Download.text('energy-node-protokoll-' + window.Download.stamp(new Date()) + '.txt', window.RunModel.logText(this.model));
      },

      openLogin() {
        window.open(this.model.loginUrl, '_blank', 'noopener');
      },

      get now() {
        void this.tick;
        return Date.now() + this.offset;
      },

      get progressText() {
        if (!this.groups.length) {
          return '';
        }
        var at = window.RunModel.current(this.model, this.groups);
        return this.shell.t('run.progress', { current: at.number, total: at.total, name: at.label });
      },

      get elapsedText() {
        if (!this.model || !this.model.started) {
          return '';
        }
        return this.shell.t('run.elapsed', { time: window.Format.elapsed(window.RunModel.elapsed(this.model, this.now)) });
      },

      get progress() {
        return this.model ? window.RunModel.progress(this.model, this.groups) : 0;
      },

      detailFor(group, state) {
        var model = this.model;
        var t = this.shell.t.bind(this.shell);
        var pick = function (wanted) {
          return group.ids.filter(function (id) { return model.steps[id] && model.steps[id].state === wanted; })[0];
        };
        if (state === 'run') {
          return model.steps[pick('run')].lastLine;
        }
        if (state === 'fail') {
          return window.RunModel.faultText(model.steps[pick('fail')].detail, t);
        }
        if (state === 'skip') {
          return window.RunModel.skipText(model.steps[group.ids[0]].detail, t);
        }
        if (group.subs) {
          var on = group.subs.filter(function (sub) { return sub.on; }).length;
          return t('run.services.selection', { selected: on, total: group.subs.length });
        }
        return '';
      },

      get parts() {
        if (!this.model) {
          return [];
        }
        var self = this;
        var now = this.now;
        var parts = [];
        this.groups.forEach(function (group) {
          var state = window.RunModel.groupState(self.model, group);
          var ms = window.RunModel.groupDuration(self.model, group, now);
          parts.push({
            key: 'stp-' + group.key, type: 'stp', state: state,
            cls: 'stp' + (state === 'run' ? ' now' : state === 'wait' ? ' wait' : ''),
            title: group.label, detail: self.detailFor(group, state),
            time: ms > 0 ? window.Format.duration(ms) : '', subs: null,
          });
          if (self.model.loginUrl && group.ids.indexOf(self.model.loginStep) >= 0) {
            parts.push({ key: 'call', type: 'call', url: self.model.loginUrl, subs: null });
          }
          if (group.subs) {
            parts.push({ key: 'subs-' + group.key, type: 'subs', subs: group.subs });
          }
        });
        return parts;
      },

      get logEntries() {
        if (!this.model) {
          return [];
        }
        return this.model.log.map(function (entry) {
          return {
            key: entry.key, clock: window.Format.clock(entry.at), marker: entry.marker, text: entry.text,
            segments: entry.marker ? [] : window.RunModel.segments(entry.text),
          };
        });
      },

      // Das Protokoll folgt dem Ende, solange der Betreiber nicht hochscrollt (A4).
      scrollLog() {
        if (!this.follow || !this.$refs || !this.$refs.log || !this.$nextTick) {
          return;
        }
        var log = this.$refs.log;
        this.$nextTick(function () { log.scrollTop = log.scrollHeight; });
      },

      onLogScroll(event) {
        var el = event.target;
        this.follow = el.scrollHeight - el.scrollTop - el.clientHeight < 24;
      },
    };
  };
})();
