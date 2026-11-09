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
  const MIN_PASSWORD = 8;

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
    drag: null,
    fitTimer: 0,

    // Import steps follow in the next part of this page (10c2). The fields
    // stay so the markup renders.
    dropOver: false,
    checking: false,
    importError: null,
    source: null,
    sourceKind: 'ok',
    unlockPw: '',
    unlockFailed: false,
    previewAreas: [],
    envRows: [],

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
    downloadArchive(item) {
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
      // 10c2: the import steps (pick, unlock, preview, env) come with openImport.
      if (this.flow === 'import') return ['pick'];
      return [];
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
    // 10c2: the import flow (file, unlock, preview, environment, apply).
    openImport() {},
    readFile() {},

    primaryLabel() {
      if (this.step === 'export') return t('backup.hero.create');
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
      }
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
        return; // the next tick asks again
      }
      if (job.id !== this.jobId) return;
      const phases = job.phases || [];
      const done = phases.filter((phase) => phase.done).length;
      this.taskIndex = Math.min(job.current || 0, Math.max(this.tasks.length - 1, 0));
      this.progress = phases.length ? done / phases.length : 0;
      if (job.running) return;
      this.stopPolling();
      if (job.error) {
        this.flowError = t('error.with_detail', { message: t('backup.run.failed'), detail: job.error });
        this.go('export', -1);
        return;
      }
      this.taskIndex = this.tasks.length;
      this.progress = 1;
      await this.finish(job);
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
      return this.doneKind === 'export' ? t('backup.run.note.export') : '';
    },
    doneTitle() {
      return this.doneKind === 'export' ? t('backup.done.export.title') : '';
    },
    doneNote() {
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
    // Inhalt im selben Schritt hat sich geaendert (Fehlermeldung, Pruefen).
    refit() {},

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
