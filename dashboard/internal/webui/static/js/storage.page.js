(() => {
  const t = (key, params) => (window.I18n ? window.I18n.t(key, params) : key);
  const apiError = (body, fallbackKey) => (window.I18n ? window.I18n.error(body, fallbackKey) : (body && body.message) || fallbackKey || 'common.request_failed');

  // The single place where a storage URL gets the reverse-proxy prefix that
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

  // The drive list refreshes while the choice step is open, so a drive that
  // is plugged in shows up by itself.
  const DRIVES_POLL_MS = 3000;
  const JOB_POLL_MS = 1000;
  // Polls without an answer in a row before the run step gives up (about two
  // minutes, the dashboard restarts at the end of a job).
  const JOB_MISS_MAX = 120;

  // The run step shows one task per job phase. Format is only in the list
  // when the drive gets formatted.
  const SETUP_TASKS = [
    { phase: 'format', key: 'storage.run.task.format' },
    { phase: 'mount', key: 'storage.run.task.mount' },
    { phase: 'copy', key: 'storage.run.task.copy' },
    { phase: 'verify', key: 'storage.run.task.verify' },
    { phase: 'switch', key: 'storage.run.task.switch' },
  ];
  const REMOVE_TASKS = [
    { phase: 'copy', key: 'storage.run.task.copy_back' },
    { phase: 'verify', key: 'storage.run.task.verify' },
    { phase: 'switch', key: 'storage.run.task.switch' },
  ];

  // Bytes as KB, MB or GB with the language's decimal separator.
  const formatSize = (bytes) => {
    const value = Number(bytes) || 0;
    const n = window.I18n.formatNumber;
    if (value >= 1024 ** 3) return `${n(value / 1024 ** 3, 1)} GB`;
    if (value >= 1024 ** 2) return `${n(value / 1024 ** 2, 1)} MB`;
    if (value >= 1024) return `${n(value / 1024, 0)} KB`;
    return `${n(value, 0)} B`;
  };

  // Exact amounts for the meter tooltips: two decimals, no rounding to a coarse unit.
  const formatExact = (bytes) => {
    const value = Number(bytes) || 0;
    const n = window.I18n.formatNumber;
    if (value >= 1024 ** 3) return `${n(value / 1024 ** 3, 2)} GB`;
    if (value >= 1024 ** 2) return `${n(value / 1024 ** 2, 2)} MB`;
    if (value >= 1024) return `${n(value / 1024, 2)} KB`;
    return `${n(value, 0)} B`;
  };

  const percent = (part, size) => (size > 0 ? Math.min(100, (part / size) * 100) : 0);
  const ease = 'cubic-bezier(.32, .72, 0, 1)';
  const reduced = () => Boolean(window.matchMedia?.('(prefers-reduced-motion: reduce)').matches);

  const storagePage = () => ({
    csrfToken: '',
    // The stored configuration and the watcher state, as GET /api/v1/storage/status answers them.
    config: {},
    mode: 'none',
    since: '',
    system: { size: 0, used: 0 },
    space: { size: 0, used: 0, parts: [] },
    job: {},
    fallback: 'system',
    fallbackPending: false,
    checking: false,

    // Setup and remove flow.
    flow: null,
    step: '',
    drives: [],
    drive: null,
    eraseOk: false,
    use: { data: true, backups: true },
    flowError: '',
    tasks: [],
    taskIndex: 0,
    progress: 0,
    doneKind: '',
    missed: 0,
    restarting: false,
    timer: 0,
    drivesTimer: 0,
    drag: null,
    fitTimer: 0,

    async init() {
      const session = await requestJSON('/api/v1/auth/session').catch(() => null);
      this.csrfToken = (session && session.csrf_token) || '';
      await this.load();
      // The page shows fresh data every time it is opened, not only on the first visit.
      this.$watch('activeSettingsPage', (page) => {
        if (page === 'storage' && !this.flow) this.recheck();
      });
    },

    // Asks the watcher to check the drive right now, then shows the result. Falls
    // back to a plain read when the check cannot run.
    async recheck() {
      try {
        this.apply(await this.send('POST', '/api/v1/storage/recheck'));
      } catch (error) {
        await this.load();
      }
    },

    async load() {
      try {
        this.apply(await requestJSON('/api/v1/storage/status'));
      } catch (error) {
        this.$store.toasts.push(error.message, 'critical');
      }
    },

    apply(body) {
      const config = body.config || {};
      this.config = config;
      this.mode = (body.status && body.status.mode) || 'none';
      this.since = (body.status && body.status.since) || '';
      this.system = body.system || { size: 0, used: 0 };
      this.space = body.drive || { size: 0, used: 0, parts: [] };
      this.job = body.job || {};
      this.fallback = config.fallback === 'ram' ? 'ram' : 'system';
    },

    // ---- Zustand ------------------------------------------------------

    // none: no drive is set up. active: the drive carries the data. missing:
    // the watcher runs on a fallback. pending: the drive is set up, but the
    // watcher has not started yet, which lasts until the next dashboard start.
    scenario() {
      if (!this.config.uuid) return 'none';
      if (this.mode === 'drive') return 'active';
      if (this.mode === 'fallback_system' || this.mode === 'fallback_ram') return 'missing';
      return 'pending';
    },
    statusLabel() {
      const keys = { active: 'storage.status.active', missing: 'storage.status.missing', pending: 'storage.status.pending' };
      return t(keys[this.scenario()] || 'storage.status.pending');
    },
    pendingNote() {
      return t('storage.pending.note');
    },
    sinceText() {
      if (!this.since) return '';
      return window.I18n.formatDateTime(this.since, { hour: '2-digit', minute: '2-digit' });
    },
    // The note names where the running data lives now, from the watcher mode.
    // Without a time the note reads the same, minus the "Seit ..." part.
    missingNote() {
      const ram = this.mode === 'fallback_ram';
      const since = this.sinceText();
      if (!since) return t(ram ? 'storage.missing.note.ram.plain' : 'storage.missing.note.system.plain');
      return t(ram ? 'storage.missing.note.ram' : 'storage.missing.note.system', { since });
    },
    fallbackNote() {
      return t(this.fallback === 'ram' ? 'storage.fallback.note.ram' : 'storage.fallback.note.system');
    },
    driveName() {
      return this.config.name || t('storage.drive.default_name');
    },
    sizeText(bytes) { return formatSize(bytes); },
    systemUsedPercent() { return percent(this.system.used, this.system.size); },
    systemMeterLabel() {
      return t('storage.meter.label', { used: formatSize(this.system.used), size: formatSize(this.system.size) });
    },
    systemFree() { return formatSize(Math.max(0, this.system.size - this.system.used)); },
    drivePart(key) {
      const part = (this.space.parts || []).find((item) => item.key === key);
      return percent(part ? part.size : 0, this.space.size);
    },
    driveMeterLabel() {
      return t('storage.meter.label', { used: formatSize(this.space.used), size: formatSize(this.space.size) });
    },
    driveFree() { return formatSize(Math.max(0, this.space.size - this.space.used)); },
    systemMeterTip() {
      const { size, used } = this.system;
      return t('storage.meter.tip.system', { used: formatExact(used), free: formatExact(Math.max(0, size - used)), size: formatExact(size) });
    },
    driveMeterTip() {
      const { size, used } = this.space;
      const part = (key) => ((this.space.parts || []).find((item) => item.key === key) || { size: 0 }).size;
      return t('storage.meter.tip.drive', {
        data: formatExact(part('runtime')),
        backups: formatExact(part('backups')),
        free: formatExact(Math.max(0, size - used)),
        size: formatExact(size),
      });
    },
    meterStyle(value) { return `flex-basis: ${value}%`; },

    // Laufwerk wechseln (aktiv) oder ersetzen (fehlend). Ohne Laufwerk leer.
    replaceFlow() {
      const scenario = this.scenario();
      if (scenario === 'active') return 'switch';
      if (scenario === 'missing') return 'replace';
      return '';
    },

    // ---- Fallback und Entfernen ---------------------------------------

    async setFallback(value) {
      if (value === this.fallback) return;
      try {
        const body = await this.send('PUT', '/api/v1/storage/fallback', { fallback: value });
        this.fallback = value;
        this.fallbackPending = Boolean(body.applies_after_restart);
      } catch (error) {
        this.$store.toasts.push(error.message, 'critical');
      }
    },
    async retry() {
      if (this.checking) return;
      this.checking = true;
      try {
        await this.recheck();
      } finally {
        this.checking = false;
      }
    },
    async removeDrive() {
      const confirmed = await this.$store.modal.confirm({
        title: t('storage.remove.title'),
        body: t('storage.remove.body'),
        confirmLabel: t('storage.remove.confirm'),
        danger: true,
      });
      if (!confirmed) return;
      try {
        await this.send('POST', '/api/v1/storage/remove');
      } catch (error) {
        this.$store.toasts.push(error.message, 'critical');
        return;
      }
      this.drive = null;
      this.flowError = '';
      this.open('remove', 'run');
      this.run(REMOVE_TASKS, 'remove');
    },

    async send(method, path, body) {
      const options = { method, headers: { 'X-CSRF-Token': this.csrfToken } }; // i18n-ignore
      if (body !== undefined) {
        options.headers['Content-Type'] = 'application/json'; // i18n-ignore
        options.body = JSON.stringify(body);
      }
      return requestJSON(path, options);
    },

    // ---- Ablauf: Laufwerk einrichten ----------------------------------

    flowEl(selector) {
      return document.querySelector(`[data-flow="storage"] ${selector}`);
    },
    // Setup, Wechseln und Ersetzen laufen durch dieselben Schritte.
    isDriveFlow() {
      return this.flow === 'setup' || this.flow === 'switch' || this.flow === 'replace';
    },
    flowTitle() {
      if (this.step === 'done') return '';
      if (this.flow === 'setup') return t('storage.flow.title.setup');
      if (this.flow === 'switch') return t('storage.flow.title.switch');
      if (this.flow === 'replace') return t('storage.flow.title.replace');
      if (this.flow === 'remove') return t('storage.flow.title.remove');
      return '';
    },
    stepList() {
      if (!this.isDriveFlow()) return [];
      return this.drive && this.drive.needsFormat ? ['drives', 'format', 'driveuse'] : ['drives', 'driveuse'];
    },
    stepCount() { return this.stepList().length; },
    stepIndex() { return this.stepList().indexOf(this.step); },
    canBack() { return this.stepIndex() > 0; },
    stepText() {
      return t('backup.flow.step', { current: this.stepIndex() + 1, total: this.stepCount() });
    },

    driveView(item) {
      const detail = [(item.transport || '').toUpperCase(), formatSize(item.size)].filter(Boolean).join(', ');
      return {
        kname: item.kname,
        uuid: item.uuid || '',
        name: item.name || item.kname,
        detail,
        needsFormat: Boolean(item.needs_format),
        contents: item.contents ? t(item.contents) : '',
      };
    },
    async loadDrives() {
      try {
        const list = await requestJSON('/api/v1/storage/drives');
        this.drives = list.map((item) => this.driveView(item));
        // The set up drive is not a choice for switching or replacing it.
        if (this.config.uuid) this.drives = this.drives.filter((item) => item.uuid !== this.config.uuid);
        // A selected drive that was unplugged is no longer a choice.
        if (this.drive) this.drive = this.drives.find((item) => item.kname === this.drive.kname) || null;
      } catch (error) {
        this.flowError = error.message;
      }
    },
    async openSetup() {
      this.flowError = '';
      this.drive = null;
      this.eraseOk = false;
      this.use = { data: true, backups: true };
      this.open('setup', 'drives');
      await this.loadDrives();
    },
    async openReplace() {
      const flow = this.replaceFlow();
      if (!flow) return;
      this.flowError = '';
      this.drive = null;
      this.eraseOk = false;
      this.use = { data: true, backups: true };
      this.open(flow, 'drives');
      await this.loadDrives();
    },

    open(flow, step) {
      this.flow = flow;
      this.step = step;
      document.documentElement.classList.add('bs-flow-active');
      this.syncDrives();
      this.$nextTick(() => {
        const stage = this.flowEl('.bs-flow-stage');
        stage.style.height = '';
        stage.scrollTop = 0;
        this.flowEl('.bs-flow').focus({ preventScroll: true });
      });
    },
    // Closing is not possible while the job runs, unless the caller forces it.
    closeFlow(force = false) {
      if (this.step === 'run' && !force) return;
      this.stopPolling();
      this.syncDrives();
      this.flow = null;
      document.documentElement.classList.remove('bs-flow-active');
      const sheet = this.flowEl('.bs-flow');
      if (sheet) {
        sheet.style.transition = '';
        sheet.style.transform = '';
      }
    },
    // The drive list is only polled while the choice step is on screen.
    syncDrives() {
      if (!this.isDriveFlow() || this.step !== 'drives') {
        clearInterval(this.drivesTimer);
        this.drivesTimer = 0;
        return;
      }
      if (!this.drivesTimer) this.drivesTimer = setInterval(() => this.loadDrives(), DRIVES_POLL_MS);
    },

    primaryLabel() {
      const submit = { switch: 'storage.switch.submit', replace: 'storage.replace.submit' };
      const key = {
        drives: 'storage.flow.next',
        format: 'storage.format.button',
        driveuse: submit[this.flow] || 'storage.use.submit',
        done: 'storage.done.button',
      }[this.step];
      return key ? t(key) : '';
    },
    // The hint under the data and backup choice, only for switching and replacing.
    driveUseNote() {
      if (this.flow === 'switch') return t('storage.switch.note');
      if (this.flow === 'replace') return t('storage.replace.note');
      return '';
    },
    // Only formatting asks for the confirmation. Choosing a drive, and the
    // data and backup choice, need a selection.
    primaryEnabled() {
      if (this.step === 'drives') return Boolean(this.drive);
      if (this.step === 'format') return this.eraseOk;
      if (this.step === 'driveuse') return Boolean(this.drive) && (this.use.data || this.use.backups);
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
        this.closeFlow();
      } else if (this.step === 'drives') {
        this.go(this.drive.needsFormat ? 'format' : 'driveuse', 1);
      } else if (this.step === 'format') {
        this.go('driveuse', 1);
      } else if (this.step === 'driveuse') {
        this.startSetup();
      }
    },
    async startSetup() {
      const drive = this.drive;
      const path = this.flow === 'setup' ? '/api/v1/storage/setup' : '/api/v1/storage/replace';
      this.flowError = '';
      try {
        await this.send('POST', path, {
          kname: drive.kname,
          uuid: drive.uuid,
          format: drive.needsFormat,
          use_data: this.use.data,
          use_backups: this.use.backups,
          fallback: this.fallback,
        });
      } catch (error) {
        this.flowError = error.message;
        return;
      }
      const tasks = SETUP_TASKS.filter((task) => task.phase !== 'format' || drive.needsFormat);
      this.run(tasks, this.flow === 'remove' ? 'remove' : 'setup');
    },

    run(tasks, kind) {
      this.stopPolling();
      this.tasks = tasks.map((task) => ({ phase: task.phase, label: t(task.key) }));
      this.taskIndex = 0;
      this.progress = 0;
      this.doneKind = kind;
      this.missed = 0;
      this.restarting = false;
      this.go('run', 1);
      this.timer = setInterval(() => this.pollJob(), JOB_POLL_MS);
    },
    stopPolling() {
      clearInterval(this.timer);
      this.timer = 0;
    },
    // Reads the job from the status. The job lives in memory, so after the
    // dashboard restarts it is gone, and the stored configuration tells the outcome.
    async pollJob() {
      let body;
      try {
        body = await requestJSON('/api/v1/storage/status');
      } catch (error) {
        this.restarting = true;
        this.missed += 1;
        if (this.missed >= JOB_MISS_MAX) this.failWith(t('storage.run.timeout'));
        return;
      }
      this.missed = 0;
      this.apply(body);
      const job = this.job;
      if (!job.phase && !job.done) {
        this.stopPolling();
        if (this.restartedOk()) {
          await this.finish();
        } else {
          this.fail('storage_failed');
        }
        return;
      }
      const index = this.tasks.findIndex((task) => task.phase === job.phase);
      this.taskIndex = index < 0 ? 0 : index;
      this.progress = this.tasks.length ? this.taskIndex / this.tasks.length : 0;
      if (!job.done) return;
      this.stopPolling();
      if (job.err) {
        this.fail(job.err);
        return;
      }
      this.taskIndex = this.tasks.length;
      this.progress = 1;
      await this.finish();
    },
    restartedOk() {
      if (this.flow === 'remove') return !this.config.uuid;
      return this.mode === 'drive';
    },
    async finish() {
      this.restarting = false;
      await this.load();
      this.go('done', 1);
    },
    fail(code) {
      this.failWith(t('error.with_detail', { message: t('storage.run.failed'), detail: apiError({ code }) }));
    },
    // A failed setup goes back to the choice, a failed removal closes with a message.
    failWith(message) {
      this.stopPolling();
      if (this.isDriveFlow()) {
        this.flowError = message;
        this.go('driveuse', -1);
        return;
      }
      this.closeFlow(true);
      this.$store.toasts.push(message, 'critical');
    },

    runNote() {
      return t('storage.run.note');
    },
    // i18n-keys: error.storage_same_drive, error.storage_no_space, error.storage_backups_lost
    warnText() {
      return this.job && this.job.warn ? t(`error.${this.job.warn}`) : '';
    },
    doneTitle() {
      return t(this.doneKind === 'remove' ? 'storage.done.remove.title' : 'storage.done.setup.title');
    },
    doneNote() {
      return t(this.doneKind === 'remove' ? 'storage.done.remove.note' : 'storage.done.setup.note');
    },

    // ---- Bewegung -----------------------------------------------------
    // Schrittwechsel: Hoehe gleitet zum neuen Inhalt, der Inhalt kommt aus
    // der Richtung des Fortschritts. Beides laesst sich unterbrechen.
    go(next, dir) {
      const stage = this.flowEl('.bs-flow-stage');
      const from = stage.getBoundingClientRect().height;
      this.step = next;
      this.syncDrives();
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
      const sheet = this.flowEl('.bs-flow');
      sheet.style.transition = 'none';
      this.drag = { y: event.clientY, h: sheet.offsetHeight, history: [] };
    },
    dragMove(event) {
      if (!this.drag) return;
      const motion = window.__obMotion;
      const delta = event.clientY - this.drag.y;
      const shown = delta < 0 ? motion.rubberband(delta, this.drag.h) : delta;
      this.flowEl('.bs-flow').style.transform = `translateY(${shown}px)`;
      this.drag.shown = shown;
      this.drag.history.push({ v: event.clientY, t: event.timeStamp });
      if (this.drag.history.length > 5) this.drag.history.shift();
    },
    dragEnd() {
      if (!this.drag) return;
      const motion = window.__obMotion;
      const sheet = this.flowEl('.bs-flow');
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
    Alpine.data('storagePage', storagePage);
  };
  if (window.Alpine) register(); else document.addEventListener('alpine:init', register, {once: true});
})();
