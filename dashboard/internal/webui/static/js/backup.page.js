(() => {
  const t = (key, params) => (window.I18n ? window.I18n.t(key, params) : key);
  const apiError = (body, fallbackKey) => (window.I18n ? window.I18n.error(body, fallbackKey) : (body && body.message) || fallbackKey || 'common.request_failed');

  // The single place where a backup URL gets the reverse-proxy prefix that
  // base.html puts into __DASHBOARD_BASE_PATH__; on direct access it is empty.
  const apiURL = (path) => `${window.__DASHBOARD_BASE_PATH__ || ''}${path}`;

  const requestJSON = async (path, options) => {
    const response = await fetch(apiURL(path), options);
    const body = await response.json().catch(() => ({}));
    if (!response.ok) {
      const error = new Error(apiError(body));
      error.code = body.code || '';
      throw error;
    }
    return body;
  };

  // Areas as the API names them. The data areas can be switched, the config
  // areas are always part of an archive.
  const AREA_LABELS = {
    settings: 'backup.area.settings',
    devices: 'backup.area.devices',
    users: 'backup.area.users',
    secrets: 'backup.area.secrets',
    stats: 'backup.area.stats',
    history: 'backup.area.history',
    battery: 'backup.area.battery',
  };
  const DATA_AREAS = ['stats', 'history', 'battery'];
  const CONFIG_AREAS = ['settings', 'devices', 'users'];
  const TRIGGER_LABELS = {
    manual: 'backup.trigger.manual',
    auto: 'backup.trigger.auto',
    update: 'backup.trigger.update',
    pre_import: 'backup.trigger.pre_import',
  };
  const PLACE_LABELS = { system: 'backup.place.system' };
  const JOB_POLL_MS = 1000;
  // Polls without the job in a row before the run step gives up (about a minute).
  const JOB_MISS_MAX = 60;
  const RESTART_MAX_MS = 120 * 1000;
  const MIN_PASSWORD = 8;

  // Import: the job phases map to the four tasks of the run screen. The
  // health wait after the restart is the last task.
  const IMPORT_TASKS = ['backup.run.task.safety', 'backup.run.task.replace', 'backup.run.task.restart', 'backup.run.task.check'];
  const IMPORT_PHASE_TASK = { safety: 0, stage: 1, stop: 1, restart: 2 };
  // Areas that stay off by default: an old history or battery state is a choice.
  const DEFAULT_OFF_AREAS = ['history', 'battery'];
  const IMPORT_ERRORS = {
    backup_corrupt: ['backup.import.corrupt.title', 'backup.import.corrupt.text'],
    backup_incompatible: ['backup.import.incompatible.title', 'backup.import.incompatible.text'],
    backup_too_new: ['backup.import.too_new.title', 'backup.import.too_new.text'],
    backup_no_space: ['backup.import.no_space.title', 'backup.import.no_space.text'],
  };
  const CHANGE_KINDS = { new: 'backup.change.new', mod: 'backup.change.mod', del: 'backup.change.del' };
  // The backend names list entries in German, these keys translate them.
  const ENTRY_LABELS = { Seite: 'backup.entry.page', Regel: 'backup.entry.rule', Szenario: 'backup.entry.scenario', 'Gerät': 'backup.entry.device' }; // i18n-ignore
  const AREA_NOTES = { stats: 'backup.preview.note.stats', battery: 'backup.preview.note.battery' };
  const ENV_LABELS = { broker: 'backup.env.row.broker', port: 'backup.env.row.port', bridge: 'backup.env.row.bridge', tailscale: 'backup.env.row.tailscale' };
  const ENV_OFF = { bridge: 'backup.env.off.bridge', tailscale: 'backup.env.off.tailscale' };
  const IMPORT_CODES = { interrupted: 'backup.import.code.interrupted', apply_failed: 'backup.import.code.apply_failed' };

  // Bytes as KB, MB or GB with the language's decimal separator.
  const formatSize = (bytes) => {
    const value = Number(bytes) || 0;
    const n = window.I18n.formatNumber;
    if (value >= 1024 ** 3) return `${n(value / 1024 ** 3, 1)} GB`;
    if (value >= 1024 ** 2) return `${n(value / 1024 ** 2, 1)} MB`;
    if (value >= 1024) return `${n(value / 1024, 0)} KB`;
    return `${n(value, 0)} B`;
  };

  const formatWhen = (value) => window.I18n.formatDateTime(value, { dateStyle: 'long', timeStyle: 'short' });

  const ease = 'cubic-bezier(.32, .72, 0, 1)';
  const reduced = () => Boolean(window.matchMedia?.('(prefers-reduced-motion: reduce)').matches);

  const backupPage = () => ({
    // Stored settings as the API answered them. PUT sends them back with the
    // edited fields changed, so fields the page does not show keep their value.
    settings: null,
    place: '',
    lastSkipped: { at: null, reason: '' },
    archives: [],
    dataAreas: [],
    auto: { beforeUpdate: true, schedule: true, every: 'week', keep: 5, secrets: false, content: { stats: true, history: true, battery: false } },
    autoSaved: '',
    canSecrets: false,
    // Note under the Zugangsdaten switches when the node holds no credentials.
    secretsNote: '',
    csrfToken: '',

    flow: null,
    step: '',
    dir: 1,
    exp: { content: {}, secrets: false, protect: false, pw: '', pw2: '', target: 'download' },
    flowError: '',
    jobId: '',
    resultId: '',
    doneKind: '',
    tasks: [],
    taskIndex: 0,
    progress: 0,
    timer: 0,
    missed: 0,
    drag: null,
    fitTimer: 0,

    // Import flow: the archive being checked, its password step and preview.
    dropOver: false,
    checking: false,
    importError: null,
    source: null,
    locked: false,
    unlockPw: '',
    unlockFailed: false,
    previewAreas: [],
    envRows: [],
    importFailed: false,
    importCode: '',
    disabledUsers: [],
    rollbackId: '',
    restarting: false,
    awaiting: false,

    async init() {
      const session = await requestJSON('/api/v1/auth/session').catch(() => null);
      this.csrfToken = (session && session.csrf_token) || '';
      this.canSecrets = Boolean(session && Array.isArray(session.roles) && session.roles.includes('backup_secrets'));
      await this.load();
    },

    async load() {
      try {
        this.apply(await requestJSON('/api/v1/backup/status'));
      } catch (error) {
        this.$store.toasts.push(error.message, 'critical');
      }
    },

    apply(status) {
      const settings = status.settings || {};
      const content = settings.content || {};
      this.settings = settings;
      this.place = status.place || '';
      this.lastSkipped = status.last_skipped || { at: null, reason: '' };
      const secretsArea = (status.areas || []).find((area) => area.key === 'secrets');
      this.secretsNote = secretsArea && secretsArea.size === 0 ? t('backup.secrets.none') : '';
      this.archives = (status.archives || []).map((item) => this.archiveView(item));
      this.dataAreas = (status.areas || [])
        .filter((area) => DATA_AREAS.includes(area.key))
        .map((area) => ({ key: area.key, label: t(AREA_LABELS[area.key]), size: formatSize(area.size) }));
      this.auto = {
        beforeUpdate: Boolean(settings.before_update),
        schedule: Boolean(settings.schedule),
        every: settings.every === 'day' ? 'day' : 'week',
        keep: settings.keep || 5,
        secrets: Boolean(settings.secrets),
        content: Object.fromEntries(DATA_AREAS.map((key) => [key, Boolean(content[key])])),
      };
      this.autoSaved = JSON.stringify(this.auto);
    },

    archiveView(item) {
      return {
        id: item.id,
        when: formatWhen(item.created_at),
        trigger: TRIGGER_LABELS[item.trigger] ? t(TRIGGER_LABELS[item.trigger]) : item.trigger,
        size: formatSize(item.size),
        version: item.version,
        place: PLACE_LABELS[item.place] ? t(PLACE_LABELS[item.place]) : item.place,
        tags: (item.areas || [])
          .filter((key) => AREA_LABELS[key] && !CONFIG_AREAS.includes(key) && key !== 'secrets')
          .map((key) => t(AREA_LABELS[key])),
        secrets: Boolean(item.secrets),
        locked: Boolean(item.locked),
        fresh: false,
      };
    },

    // ---- Start und Automatik --------------------------------------------

    heroTone() {
      return this.archives.length && !this.lastSkipped.at ? 'ok' : 'warn';
    },
    heroTitle() {
      if (!this.archives.length) return t('backup.hero.none.title');
      return t('backup.hero.last.title', { when: this.archives[0].when });
    },
    heroNote() {
      if (!this.archives.length) return t('backup.hero.none.note');
      if (this.lastSkipped.at) {
        return t(this.lastSkipped.reason === 'no_location' ? 'backup.hero.skipped.no_location' : 'backup.hero.skipped.error');
      }
      const last = this.archives[0];
      return t('backup.hero.last.note', { trigger: last.trigger, size: last.size, next: this.nextRun() });
    },
    // The next run of the saved schedule, as text.
    nextRun() {
      const saved = JSON.parse(this.autoSaved || '{}');
      if (!saved.schedule) return t('backup.hero.no_schedule');
      return saved.every === 'day' ? t('backup.auto.next_day') : t('backup.auto.next_week');
    },
    archiveSummary() {
      const place = PLACE_LABELS[this.place] ? t(PLACE_LABELS[this.place]) : this.place;
      return window.I18n.tn('backup.summary.archives', this.archives.length, { place });
    },
    autoDirty() {
      return JSON.stringify(this.auto) !== this.autoSaved;
    },
    async saveAuto() {
      if (!this.settings) return;
      const body = {
        ...this.settings,
        before_update: this.auto.beforeUpdate,
        schedule: this.auto.schedule,
        every: this.auto.every,
        keep: this.auto.keep,
        secrets: this.auto.secrets,
        // A content object replaces the stored one as a whole, so the stored
        // config areas go along. The secrets area follows the automatic switch.
        content: { ...this.settings.content, ...this.auto.content, secrets: this.auto.secrets },
      };
      try {
        await this.send('PUT', '/api/v1/backup/settings', body);
        await this.load();
        this.$store.toasts.push(t('common.saved'), 'info');
      } catch (error) {
        this.$store.toasts.push(error.message, 'critical');
      }
    },
    async removeArchive(item) {
      const confirmed = await this.$store.modal.confirm({
        title: t('backup.archives.delete_title', { when: item.when }),
        body: t('backup.archives.delete_body'),
        confirmLabel: t('common.delete'),
        danger: true,
      });
      if (!confirmed) return false;
      try {
        await this.send('DELETE', `/api/v1/backup/archives/${encodeURIComponent(item.id)}`);
        await this.load();
        return true;
      } catch (error) {
        this.$store.toasts.push(error.message, 'critical');
        return false;
      }
    },
    // An open archive with secrets downloads only with backup_secrets, the server checks it too.
    canDownload(item) { return this.canSecrets || !item.secrets || Boolean(item.locked); },
    downloadTitle(item) { return this.canDownload(item) ? t('backup.archives.download') : t('backup.preview.forbidden'); },
    downloadArchive(item) {
      if (!this.canDownload(item)) return;
      this.download(item.id);
    },
    // An anchor with the download attribute: the browser saves the file the
    // route sends as an attachment.
    download(id) {
      const link = document.createElement('a');
      link.href = apiURL(`/api/v1/backup/archives/${encodeURIComponent(id)}`);
      link.download = '';
      document.body.appendChild(link);
      link.click();
      link.remove();
    },

    async send(method, path, body) {
      const options = { method, headers: { 'X-CSRF-Token': this.csrfToken } }; // i18n-ignore
      if (body !== undefined) {
        options.headers['Content-Type'] = 'application/json'; // i18n-ignore
        options.body = JSON.stringify(body);
      }
      return requestJSON(path, options);
    },

    // ---- Ablauf -------------------------------------------------------
    flowTitle() {
      if (this.step === 'done') return '';
      return { export: t('backup.flow.title.export'), import: t('backup.flow.title.import') }[this.flow] || '';
    },
    stepList() {
      if (this.flow !== 'import') return [];
      const list = ['pick'];
      if (this.locked) list.push('unlock');
      list.push('preview');
      if (this.envRows.length) list.push('env');
      return list;
    },
    stepCount() { return this.stepList().length; },
    stepIndex() { return this.stepList().indexOf(this.step); },
    canBack() { return this.stepIndex() > 0; },

    open(flow, step) {
      this.flow = flow;
      this.step = step;
      document.documentElement.classList.add('bs-flow-active');
      this.$nextTick(() => {
        const stage = document.querySelector('.bs-flow-stage');
        stage.style.height = '';
        stage.scrollTop = 0;
        document.querySelector('.bs-flow').focus({ preventScroll: true });
      });
    },
    closeFlow() {
      if (this.step === 'run') return;
      this.stopPolling();
      this.flow = null;
      document.documentElement.classList.remove('bs-flow-active');
      const sheet = document.querySelector('.bs-flow');
      sheet.style.transition = '';
      sheet.style.transform = '';
    },
    openExport() {
      this.flowError = '';
      this.exp = { content: { ...this.auto.content }, secrets: false, protect: false, pw: '', pw2: '', target: 'download' };
      this.open('export', 'export');
    },
    // Opens the import flow. Without an item the file step comes first, with a
    // stored archive the archive is checked right away.
    async openImport(item) {
      this.resetImport();
      this.open('import', 'pick');
      if (!item) return;
      await this.sendArchive({ 'Content-Type': 'application/json' }, JSON.stringify({ archive: item.id })); // i18n-ignore
    },
    resetImport() {
      this.importError = null;
      this.checking = false;
      this.locked = false;
      this.unlockPw = '';
      this.unlockFailed = false;
      this.flowError = '';
      this.previewAreas = [];
      this.envRows = [];
      this.source = null;
      this.importFailed = false;
      this.importCode = '';
      this.disabledUsers = [];
      this.rollbackId = '';
      this.restarting = false;
      this.awaiting = false;
      this.dropOver = false;
    },
    async readFile(file) {
      if (!file) return;
      this.importError = null;
      await this.sendArchive({ 'Content-Type': 'application/octet-stream' }, file); // i18n-ignore
    },
    // Sends the archive, as an upload or as a stored id. A password answer
    // opens the unlock step, errors stay on the pick step.
    async sendArchive(headers, body) {
      this.checking = true;
      let answer;
      try {
        answer = await requestJSON('/api/v1/backup/import', {
          method: 'POST',
          headers: { 'X-CSRF-Token': this.csrfToken, ...headers }, // i18n-ignore
          body,
        });
      } catch (error) {
        this.checking = false;
        if (error.code === 'password_required') {
          this.locked = true;
          this.go('unlock', 1);
        } else {
          this.importError = this.importErrorFor(error);
        }
        return;
      }
      this.checking = false;
      this.showPreview(answer);
      this.go('preview', 1);
    },
    importErrorFor(error) {
      const keys = IMPORT_ERRORS[error.code];
      if (!keys) return { title: t('backup.import.generic.title'), text: error.message };
      return { title: t(keys[0]), text: t(keys[1]) };
    },
    // Maps the preview answer to the preview areas and the environment rows.
    // The environment step only exists when the archive comes from another device.
    showPreview(answer) {
      const manifest = answer.manifest || {};
      this.source = {
        host: manifest.hostname || '',
        when: formatWhen(manifest.created_at),
        version: manifest.node_version || '',
      };
      const secretsInArchive = Boolean(manifest.secrets);
      this.previewAreas = (answer.areas || []).map((item) => this.previewArea(item, secretsInArchive));
      this.envRows = answer.different_device ? (answer.environment || []).map((row) => this.envRow(row)) : [];
    },
    previewArea(item, secretsInArchive) {
      const key = item.area;
      // Secrets, and users with password hashes, need the backup_secrets role.
      const forbidden = !this.canSecrets && (key === 'secrets' || (key === 'users' && secretsInArchive));
      const available = Boolean(item.available) && !forbidden;
      return {
        key,
        label: t(AREA_LABELS[key]),
        available,
        on: available && !DEFAULT_OFF_AREAS.includes(key),
        open: false,
        summary: this.areaSummary(item, forbidden, secretsInArchive),
        changes: (item.changes || []).map((change) => ({
          kind: change.kind,
          kindLabel: t(CHANGE_KINDS[change.kind] || CHANGE_KINDS.mod),
          text: this.changeText(change),
        })),
        note: this.areaNote(item),
      };
    },
    // The users note says that the existing passwords stay when the archive has none.
    areaNote(item) {
      if (item.passwords_kept) return t('backup.preview.passwords_kept');
      return AREA_NOTES[item.area] ? t(AREA_NOTES[item.area]) : '';
    },
    areaSummary(item, forbidden, secretsInArchive) {
      if (forbidden) return t('backup.preview.forbidden');
      if (!item.available && item.area === 'secrets' && secretsInArchive) return t('backup.preview.secrets_users_only');
      if (!item.available) return t('backup.preview.absent');
      if (DATA_AREAS.includes(item.area)) return formatSize(item.size);
      const changes = (item.changes || []).length;
      return changes ? window.I18n.tn('backup.preview.count', changes) : t('backup.preview.same');
    },
    changeText(change) {
      const label = ENTRY_LABELS[change.label] ? t(ENTRY_LABELS[change.label]) : change.label;
      return change.name ? `${label}: ${change.name}` : label;
    },
    envRow(row) {
      return {
        key: row.key,
        label: ENV_LABELS[row.key] ? t(ENV_LABELS[row.key]) : row.key,
        here: row.here || '',
        archive: row.archive || '',
        use: 'here',
        off: row.off ? (ENV_OFF[row.off] ? t(ENV_OFF[row.off]) : row.off) : '',
      };
    },
    async unlock() {
      this.unlockFailed = false;
      try {
        const answer = await this.send('POST', '/api/v1/backup/import/unlock', { password: this.unlockPw });
        this.unlockPw = '';
        this.showPreview(answer);
        this.go('preview', 1);
      } catch (error) {
        if (error.code === 'wrong_password') {
          this.unlockFailed = true;
          return;
        }
        this.backToPick(this.importErrorFor(error));
      }
    },
    // Leaves the import with an error on the pick step. The server has dropped
    // the session, so no preview or password state is kept.
    backToPick(importError) {
      this.checking = false;
      this.locked = false;
      this.unlockPw = '';
      this.unlockFailed = false;
      this.flowError = '';
      this.previewAreas = [];
      this.envRows = [];
      this.source = null;
      this.importError = importError;
      this.go('pick', -1);
    },

    primaryLabel() {
      if (this.step === 'export') return t('backup.hero.create');
      if (this.step === 'unlock') return t('backup.button.unlock');
      if (this.step === 'preview') return this.envRows.length ? t('backup.button.next') : t('backup.button.apply');
      if (this.step === 'env') return t('backup.button.apply');
      if (this.step === 'done') {
        if (this.flow === 'export' && this.exp.target === 'download') return t('backup.archives.download');
        return t('backup.button.done');
      }
      return '';
    },
    primaryEnabled() {
      if (this.step === 'export') {
        return !this.exp.protect || ([...this.exp.pw].length >= MIN_PASSWORD && this.exp.pw === this.exp.pw2);
      }
      if (this.step === 'unlock') return this.unlockPw.length > 0;
      if (this.step === 'preview') return this.previewAreas.some((area) => area.on);
      return true;
    },
    flowBack() {
      const list = this.stepList();
      const at = list.indexOf(this.step);
      if (at > 0) this.go(list[at - 1], -1);
    },
    flowPrimary() {
      if (!this.primaryEnabled()) return;
      if (this.step === 'done') {
        if (this.flow === 'export' && this.exp.target === 'download' && this.resultId) this.download(this.resultId);
        this.closeFlow();
      } else if (this.step === 'export') {
        this.startExport();
      } else if (this.step === 'unlock') {
        this.unlock();
      } else if (this.step === 'preview' && this.envRows.length) {
        this.go('env', 1);
      } else if (this.step === 'preview' || this.step === 'env') {
        this.applyImport();
      }
    },
    // Rolls back to the safety archive of the last import, from the done step.
    rollbackImport() {
      if (!this.rollbackId) return Promise.resolve();
      return this.openImport({ id: this.rollbackId, locked: false });
    },

    // Sends the choices. The server answers with a job, the run screen follows it.
    async applyImport() {
      this.flowError = '';
      const areas = Object.fromEntries(this.previewAreas.map((area) => [area.key, Boolean(area.on)]));
      const useArchive = Object.fromEntries(this.envRows.map((row) => [row.key, row.use === 'archive']));
      let started;
      try {
        started = await this.send('POST', '/api/v1/backup/import/apply', { areas, use_archive: useArchive });
      } catch (error) {
        if (error.code === 'backup_no_import') {
          this.backToPick(this.importErrorFor(error));
          return;
        }
        this.flowError = error.message;
        return;
      }
      this.jobId = started.job;
      this.run(IMPORT_TASKS.map((key) => t(key)), 'import');
    },

    // Sends the export. The server answers with a job, whose phases the
    // progress shows. Errors stay in the step.
    async startExport() {
      this.flowError = '';
      const protect = this.exp.protect;
      const body = {
        areas: this.exportAreas(),
        secrets: this.exp.secrets,
        password: protect ? this.exp.pw : '',
        target: this.exp.target === 'node' ? 'store' : 'download',
      };
      let started;
      try {
        started = await this.send('POST', '/api/v1/backup/archives', body);
      } catch (error) {
        this.flowError = error.message;
        return;
      }
      this.jobId = started.job;
      this.run([t(protect ? 'backup.run.task.encrypt' : 'backup.run.task.write'), t('backup.run.task.finish')], 'export');
    },
    exportAreas() {
      const areas = { settings: true, devices: true, users: true, secrets: this.exp.secrets };
      for (const key of DATA_AREAS) areas[key] = Boolean(this.exp.content[key]);
      return areas;
    },

    run(tasks, kind) {
      this.stopPolling();
      this.tasks = tasks;
      this.taskIndex = 0;
      this.progress = 0;
      this.doneKind = kind;
      this.restarting = false;
      this.awaiting = false;
      this.missed = 0;
      this.go('run', 1);
      this.timer = setInterval(() => this.pollJob(), JOB_POLL_MS);
    },
    stopPolling() {
      clearInterval(this.timer);
      this.timer = 0;
    },
    async pollJob() {
      let job;
      try {
        job = await requestJSON('/api/v1/backup/job');
      } catch (error) {
        await this.missedJob();
        return; // the next tick asks again
      }
      if (job.id !== this.jobId) {
        // An export job does not come back once it is gone, the dashboard has restarted.
        if (this.doneKind === 'export') {
          this.giveUpExport();
          return;
        }
        await this.missedJob();
        return;
      }
      this.missed = 0;
      const phases = job.phases || [];
      const done = phases.filter((phase) => phase.done).length;
      const phaseKey = (phases[job.current] || {}).key;
      if (this.doneKind === 'import') {
        this.restarting = phaseKey === 'restart';
        this.taskIndex = IMPORT_PHASE_TASK[phaseKey] || 0;
      } else {
        this.taskIndex = Math.min(job.current || 0, Math.max(this.tasks.length - 1, 0));
      }
      this.progress = phases.length ? done / phases.length : 0;
      if (job.running) return;
      this.stopPolling();
      if (job.error) {
        // The server has consumed the import session, so the file has to be picked again.
        if (this.doneKind === 'import') {
          this.backToPick({ title: t('backup.import.failed'), text: job.error });
          return;
        }
        this.flowError = t('error.with_detail', { message: t('backup.run.failed'), detail: job.error });
        this.go('export', -1);
        return;
      }
      // An import ends with the restart, so its result is read after the restart.
      if (this.doneKind === 'import') {
        await this.awaitRestart();
        return;
      }
      this.taskIndex = this.tasks.length;
      this.progress = 1;
      await this.finish(job);
    },
    // Counts a poll without the job. An import waits for the restart, an export
    // gives up after JOB_MISS_MAX polls in a row, so the run step can always close.
    async missedJob() {
      this.missed += 1;
      if (this.doneKind === 'import') {
        if (this.restarting || this.missed >= JOB_MISS_MAX) await this.awaitRestart();
        return;
      }
      if (this.missed >= JOB_MISS_MAX) this.giveUpExport();
    },
    giveUpExport() {
      this.stopPolling();
      this.flowError = t('backup.export.interrupted');
      this.go('export', -1);
    },
    // The dashboard is gone while it restarts. Polls the health endpoint until it
    // answers again, then reads the result the restarted process wrote. Gives up
    // after RESTART_MAX_MS.
    async awaitRestart() {
      if (this.awaiting) return;
      this.awaiting = true;
      this.stopPolling();
      this.taskIndex = this.tasks.length - 1;
      const deadline = Date.now() + RESTART_MAX_MS;
      let down = false;
      while (Date.now() < deadline) {
        const up = await requestJSON('/api/v1/health').then(() => true, () => false); // i18n-ignore
        if (!up) {
          down = true;
        } else if (down) {
          const result = await requestJSON('/api/v1/backup/import/result').catch(() => null); // i18n-ignore
          // An empty answer is {}, a written result always carries finished_at.
          if (result && result.finished_at) {
            await this.finishImport(result);
            return;
          }
        }
        await new Promise((resolve) => setTimeout(resolve, JOB_POLL_MS));
      }
      await this.finishImport({ ok: false, code: 'interrupted' });
    },
    // Shows the result of an import on the done step. A failure offers the
    // rollback to the newest safety archive, which the restarted process no
    // longer knows by id.
    async finishImport(result) {
      this.stopPolling();
      this.importFailed = !result.ok;
      this.importCode = result.code || '';
      this.disabledUsers = result.disabled_users || [];
      this.rollbackId = '';
      this.taskIndex = this.tasks.length;
      this.progress = 1;
      try {
        const status = await requestJSON('/api/v1/backup/status');
        this.apply(status);
        const safety = (status.archives || []).find((item) => item.trigger === 'pre_import');
        if (!result.ok && safety) this.rollbackId = safety.id;
      } catch (error) {
        // The list keeps what it had, the done step still shows the result.
      }
      this.go('done', 1);
    },
    async finish(job) {
      if (this.doneKind === 'export' && this.exp.target === 'download') {
        this.resultId = job.result;
        this.go('done', 1);
        return;
      }
      await this.load();
      this.archives = this.archives.map((item) => ({ ...item, fresh: item.id === job.result }));
      this.go('done', 1);
    },
    runNote() {
      if (this.doneKind === 'import') return t('backup.run.note.import');
      return this.doneKind === 'export' ? t('backup.run.note.export') : '';
    },
    doneTitle() {
      if (this.doneKind === 'import') return t(this.importFailed ? 'backup.done.import.failed' : 'backup.done.import.title');
      return this.doneKind === 'export' ? t('backup.done.export.title') : '';
    },
    doneNote() {
      if (this.doneKind === 'import') {
        if (this.importFailed) return t(IMPORT_CODES[this.importCode] || IMPORT_CODES.apply_failed);
        if (this.disabledUsers.length) return t('backup.done.import.users');
        return t('backup.done.import.note');
      }
      if (this.doneKind !== 'export') return '';
      return this.exp.target === 'download' ? t('backup.done.export.download') : t('backup.done.export.node');
    },

    // ---- Bewegung -----------------------------------------------------
    // Schrittwechsel: Hoehe gleitet zum neuen Inhalt, der Inhalt kommt aus
    // der Richtung des Fortschritts. Beides laesst sich unterbrechen.
    go(next, dir) {
      const stage = document.querySelector('.bs-flow-stage');
      const from = stage.getBoundingClientRect().height;
      this.step = next;
      this.$nextTick(() => {
        stage.scrollTop = 0;
        const pane = stage.querySelector(`[data-step="${next}"]`);
        if (reduced()) { stage.style.height = ''; return; }
        stage.style.height = 'auto';
        const to = stage.getBoundingClientRect().height;
        stage.style.height = `${from}px`;
        void stage.offsetHeight;
        stage.style.height = `${to}px`;
        pane.animate(
          [{ opacity: 0, transform: `translateX(${dir * 18}px)` }, { opacity: 1, transform: 'none' }],
          { duration: 240, easing: ease },
        );
        clearTimeout(this.fitTimer);
        this.fitTimer = setTimeout(() => { stage.style.height = ''; }, 320);
      });
    },
    // Blatt am Griff ziehen. Folgt dem Finger 1:1, oben Gummiband, beim
    // Loslassen entscheidet die Projektion des Schwungs, nicht die Position.
    dragStart(event) {
      if (this.step === 'run') return;
      event.currentTarget.setPointerCapture(event.pointerId);
      const sheet = document.querySelector('.bs-flow');
      sheet.style.transition = 'none';
      this.drag = { y: event.clientY, h: sheet.offsetHeight, history: [] };
    },
    dragMove(event) {
      if (!this.drag) return;
      const motion = window.__obMotion;
      const delta = event.clientY - this.drag.y;
      const shown = delta < 0 ? motion.rubberband(delta, this.drag.h) : delta;
      document.querySelector('.bs-flow').style.transform = `translateY(${shown}px)`;
      this.drag.shown = shown;
      this.drag.history.push({ v: event.clientY, t: event.timeStamp });
      if (this.drag.history.length > 5) this.drag.history.shift();
    },
    dragEnd() {
      if (!this.drag) return;
      const motion = window.__obMotion;
      const sheet = document.querySelector('.bs-flow');
      const velocity = motion.releaseVelocity(this.drag.history);
      const projected = (this.drag.shown || 0) + motion.project(velocity, 0.99);
      const dismiss = projected > this.drag.h * 0.45;
      this.drag = null;
      sheet.style.transition = '';
      if (dismiss) this.closeFlow();
      else sheet.style.transform = '';
    },
  });

  const register = () => {
    if (!window.Alpine) return;
    Alpine.data('backupPage', backupPage);
  };
  if (window.Alpine) register(); else document.addEventListener('alpine:init', register, {once: true});
})();
