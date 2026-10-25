(() => {
  const t = (key, params) => (window.I18n ? window.I18n.t(key, params) : key);
  const apiError = (body, fallbackKey) => (window.I18n ? window.I18n.error(body, fallbackKey) : (body && body.message) || fallbackKey || 'common.request_failed');

  const requestJSON = async (url, options) => {
    // The single chokepoint for every URL literal in this file: behind a
    // reverse-proxy subpath base.html puts the prefix into
    // __DASHBOARD_BASE_PATH__; on direct access it is empty.
    const response = await fetch(`${window.__DASHBOARD_BASE_PATH__ || ''}${url}`, options);
    const body = await response.json().catch(() => ({}));
    if (!response.ok) {
      throw new Error(apiError(body));
    }
    return body;
  };

  // Card ids. Local accounts use their name as id. The guest preset and the
  // card for a new account are fixed ids. Usernames cannot contain a colon, so
  // these never collide with an account name ('__new__' would, underscores are allowed).
  const GUEST_PRESET_ID = 'preset:guest';
  const NEW_ID = 'draft:new';
  // Until GET /api/v1/users answers, the policy is the server default.
  const DEFAULT_POLICY = Object.freeze({min_length: 10, min: 8, max: 64});
  // The sessions tab reloads its list this often while it is open.
  const SESSIONS_MS = 30000;
  const USERNAME_PATTERN = /^[a-z0-9._-]{3,32}$/;
  // How long the "saved" note shows in a card head.
  const SAVED_MS = 1800;
  // Collapse time of a removed row (spec section 8: 240 to 260 ms). With
  // prefers-reduced-motion the row only fades, so the wait is shorter.
  const LEAVE_MS = 260;
  const LEAVE_MS_REDUCED = 160;

  // Code points, like Go runes: an umlaut or emoji counts once.
  const codePoints = (value) => [...(value || '')].length;
  const wait = (ms) => new Promise((resolve) => window.setTimeout(resolve, ms));
  const leaveDelay = () => (window.matchMedia && window.matchMedia('(prefers-reduced-motion: reduce)').matches
    ? LEAVE_MS_REDUCED
    : LEAVE_MS);

  const sortedRoles = (roles) => [...(roles || [])].sort();
  const sameRoles = (a, b) => JSON.stringify(a) === JSON.stringify(b);
  const enc = (name) => encodeURIComponent(name);

  // Permission rules (which entries are locked, what a category switch does,
  // normalisation, the HTTP warning) live in window.UsersModel. This file only
  // holds drafts and calls the API.
  const usersPage = () => ({
    loading: false,
    error: '',
    csrfToken: '',
    self: '',
    catalog: [],
    // Raw accounts from GET /api/v1/users, without hashes.
    accounts: [],
    // Password policy from GET /api/v1/users: {min_length, min, max}.
    policy: {...DEFAULT_POLICY},
    // The minimum length as typed in the policy card, a number.
    policyDraft: DEFAULT_POLICY.min_length,
    // Open tab of the users page: 'accounts' or 'sessions'.
    tab: 'accounts',
    // Logged-in sessions from GET /api/v1/user-sessions.
    sessions: [],
    sessionsError: '',
    // Id of the 30 s reload timer while the sessions tab is open, else null.
    sessionTimer: null,
    // The guest preset card. Its roles are guest_roles.
    presets: [],
    migrationNotice: false,
    // Unsaved state per card id: {roles, disabled}. Survives switching cards.
    drafts: {},
    // The one open card id, or null.
    openId: null,
    // 'cardId|category' -> true when the permission list of a category is unfolded.
    openGroups: {},
    // Draft of a new account, null when no card is being created:
    // {username, password}. Its roles live in drafts.new.
    creating: null,
    // Card id whose request is running, '' when none.
    saving: '',
    // Card id -> true for a short time after a successful save.
    savedFlags: {},
    // Row id (account or guest session) -> true while its collapse runs
    // before it leaves the list.
    leaving: {},

    async init() {
      const session = await requestJSON('/api/v1/auth/session').catch(() => null);
      this.csrfToken = (session && session.csrf_token) || '';
      await this.load();
    },

    async load() {
      this.loading = true;
      this.error = '';
      try {
        const body = await requestJSON('/api/v1/users');
        this.accounts = body.users || [];
        const keepPolicyDraft = this.policyDirty();
        this.policy = {...DEFAULT_POLICY, ...(body.password_policy || {})};
        if (!keepPolicyDraft) this.policyDraft = this.policy.min_length;
        this.presets = [{id: GUEST_PRESET_ID, roles: sortedRoles(body.guest_roles)}];
        this.catalog = body.catalog || [];
        this.self = body.self || '';
        this.migrationNotice = Boolean(body.migration_notice);
      } catch (error) {
        this.error = error.message;
      } finally {
        this.loading = false;
      }
    },

    // Stops the sessions timer when the page goes away.
    destroy() {
      this.stopSessionTimer();
    },

    // --- identity helpers -------------------------------------------------

    // 'preset' for the guest card, 'new' for the creation card, else 'account'.
    cardKind(id) {
      if (id === GUEST_PRESET_ID) return 'preset';
      if (id === NEW_ID) return 'new';
      return 'account';
    },

    // The UsersModel account kind: 'guest' for the preset, 'local' otherwise.
    kindOf(id) {
      return this.cardKind(id) === 'preset' ? 'guest' : 'local';
    },

    accountFor(id) {
      if (this.cardKind(id) === 'preset') return this.presets[0] || null;
      if (this.cardKind(id) === 'new') return null;
      return this.accounts.find((account) => account.username === id) || null;
    },

    isSelf(id) {
      return this.cardKind(id) === 'account' && id === this.self;
    },

    // Roles and disabled state as stored on the server. A new card starts empty.
    savedSnapshot(id) {
      const account = this.accountFor(id);
      if (!account) return {roles: [], disabled: false};
      return {roles: sortedRoles(account.roles), disabled: Boolean(account.disabled)};
    },

    draftOf(id) {
      if (!this.drafts[id]) this.drafts[id] = this.savedSnapshot(id);
      return this.drafts[id];
    },

    // --- drafts and dirty state -------------------------------------------

    isDirty(id) {
      if (this.cardKind(id) === 'new') return this.creating !== null;
      if (!this.drafts[id]) return false;
      const draft = this.drafts[id];
      const saved = this.savedSnapshot(id);
      return !sameRoles(sortedRoles(draft.roles), saved.roles) || Boolean(draft.disabled) !== saved.disabled;
    },

    // Clicking the open card closes it, clicking another one switches. Drafts stay.
    toggleCard(id) {
      this.openId = this.openId === id ? null : id;
    },

    isGroupOpen(id, category) {
      return Boolean(this.openGroups[`${id}|${category}`]);
    },

    toggleGroupOpen(id, category) {
      const key = `${id}|${category}`;
      this.openGroups[key] = !this.openGroups[key];
    },

    // --- permissions ------------------------------------------------------

    get categories() {
      const seen = [];
      this.catalog.forEach((entry) => {
        if (!seen.includes(entry.category)) seen.push(entry.category);
      });
      return seen;
    },

    // Entries of a category the card may hold, in catalog order.
    entriesFor(id, category) {
      return window.UsersModel.available(this.catalog, this.kindOf(id))
        .filter((entry) => entry.category === category);
    },

    lockContext(id) {
      const otherActiveAdmins = this.accounts.filter((account) => account.username !== id
        && !account.disabled
        && (account.roles || []).includes('user_management')).length;
      return {self: this.isSelf(id), otherActiveAdmins};
    },

    // {key: reason} for the draft. Reasons: 'requires', 'last_admin', 'self'.
    // The key '__active' carries the reason for the active switch.
    locksFor(id) {
      return window.UsersModel.locks(this.catalog, {kind: this.kindOf(id)}, this.draftOf(id).roles, this.lockContext(id));
    },

    lockFor(id, key) {
      return this.locksFor(id)[key] || '';
    },

    // The permission a 'requires' lock needs, for the "Braucht Ansehen" text.
    lockRequires(key) {
      const entry = this.catalog.find((item) => item.key === key);
      return (entry && entry.requires) || '';
    },

    toggleRole(id, key) {
      const entry = this.catalog.find((item) => item.key === key);
      if (!entry || !window.UsersModel.available(this.catalog, this.kindOf(id)).includes(entry)) return;
      const draft = this.draftOf(id);
      const held = draft.roles.includes(key);
      const lock = this.lockFor(id, key);
      if (held && lock === 'last_admin') return;
      if (!held && lock) return;
      const roles = held ? draft.roles.filter((role) => role !== key) : [...draft.roles, key];
      draft.roles = window.UsersModel.normalize(this.catalog, roles);
    },

    toggleGroup(id, category) {
      const draft = this.draftOf(id);
      draft.roles = window.UsersModel.toggleGroup(
        this.catalog,
        {kind: this.kindOf(id)},
        draft.roles,
        category,
        this.lockContext(id),
      );
    },

    groupStateFor(id, category) {
      return window.UsersModel.groupState(this.catalog, this.kindOf(id), category, this.draftOf(id).roles);
    },

    // Saved state per category, for the card head. Rows: {category, label, n, m}.
    summaryFor(id) {
      return window.UsersModel.summary(this.catalog, this.kindOf(id), this.savedSnapshot(id).roles);
    },

    // The HTTP warning box: null, or {withheld: [keys], safe: boolean}.
    warningFor(id) {
      if (this.kindOf(id) !== 'local') return null;
      const account = this.accountFor(id) || {};
      return window.UsersModel.httpWarning(this.catalog, this.draftOf(id).roles, Boolean(account.has_http_password));
    },

    setActive(id, active) {
      if (this.lockFor(id, '__active')) return;
      this.draftOf(id).disabled = !active;
    },

    // Head tags as i18n keys (users.tag.*), in the order of the prototype.
    tagsFor(id) {
      const tags = [];
      const kind = this.cardKind(id);
      if (this.isSelf(id)) tags.push('users.tag.you');
      if (kind === 'new') tags.push('users.tag.new');
      if (kind === 'account' && this.draftOf(id).disabled) tags.push('users.tag.disabled');
      const warning = this.warningFor(id);
      if (warning) tags.push(warning.safe ? 'users.tag.own_http_password' : 'users.tag.restricted');
      if (this.isDirty(id) && kind !== 'new' && this.openId !== id) tags.push('users.tag.unsaved');
      return tags;
    },

    justSaved(id) {
      return Boolean(this.savedFlags[id]);
    },

    markSaved(id) {
      this.savedFlags[id] = true;
      window.setTimeout(() => {
        delete this.savedFlags[id];
      }, SAVED_MS);
    },

    // --- save, discard ----------------------------------------------------

    async send(method, url, body) {
      const options = {method, headers: {'X-CSRF-Token': this.csrfToken}}; // i18n-ignore
      if (body !== undefined) {
        options.headers['Content-Type'] = 'application/json'; // i18n-ignore
        options.body = JSON.stringify(body);
      }
      return requestJSON(url, options);
    },

    // Asks first when the save adds elevated permissions. Resolves to false
    // when the user cancels, then nothing is sent.
    async confirmElevated(id, roles) {
      const added = window.UsersModel.newlyElevated(this.catalog, this.savedSnapshot(id).roles, roles);
      if (added.length === 0) return true;
      const list = added.map((key) => t(`users.role.${key}.label`)).join(', ');
      const confirmed = await this.$store.modal.confirm({
        title: t('users.elevated.confirm_title'),
        body: t('users.elevated.confirm_body', {list}),
        confirmLabel: t('users.elevated.confirm'),
        danger: true,
      });
      return Boolean(confirmed);
    },

    // Elevated permissions the draft holds, catalog order. Empty for none.
    elevatedFor(id) {
      return window.UsersModel.elevatedKeys(this.catalog, this.draftOf(id).roles);
    },

    async save(id) {
      if (this.saving) return false;
      if (this.cardKind(id) === 'new') return this.createAccount();
      if (!this.isDirty(id)) return false;
      const draft = this.draftOf(id);
      const roles = window.UsersModel.normalize(this.catalog, draft.roles);
      if (!await this.confirmElevated(id, roles)) return false;
      this.saving = id;
      try {
        if (this.cardKind(id) === 'preset') {
          await this.send('PUT', '/api/v1/users/guest-roles', {roles});
        } else {
          await this.send('PUT', `/api/v1/users/${enc(id)}`, {roles, disabled: Boolean(draft.disabled)});
        }
        // Reload before the draft goes: a render in between would rebuild the
        // draft from the old account and show it as unsaved.
        await this.load();
        delete this.drafts[id];
        this.markSaved(id);
        return true;
      } catch (error) {
        this.$store.toasts.push(error.message, 'critical');
        return false;
      } finally {
        this.saving = '';
      }
    },

    async createAccount() {
      if (!this.createValid()) return false;
      const username = this.creating.username;
      const roles = window.UsersModel.normalize(this.catalog, this.draftOf(NEW_ID).roles);
      if (!await this.confirmElevated(NEW_ID, roles)) return false;
      this.saving = NEW_ID;
      try {
        await this.send('POST', '/api/v1/users', {username, password: this.creating.password, roles});
        // Reload before the draft goes: a render in between would rebuild the
        // username's draft from the list, which does not have it yet.
        await this.load();
        this.creating = null;
        delete this.drafts[NEW_ID];
        this.openId = username;
        this.markSaved(username);
        return true;
      } catch (error) {
        this.$store.toasts.push(error.message, 'critical');
        return false;
      } finally {
        this.saving = '';
      }
    },

    discard(id) {
      if (this.cardKind(id) === 'new') {
        this.cancelCreate();
        return;
      }
      delete this.drafts[id];
    },

    // --- creating ---------------------------------------------------------

    startCreate() {
      if (!this.creating) {
        this.creating = {username: '', password: ''};
        this.drafts[NEW_ID] = {roles: [], disabled: false};
      }
      this.openId = NEW_ID;
    },

    cancelCreate() {
      this.creating = null;
      delete this.drafts[NEW_ID];
      if (this.openId === NEW_ID) this.openId = null;
    },

    createNameValid() {
      const name = (this.creating && this.creating.username) || '';
      return USERNAME_PATTERN.test(name)
        && !name.startsWith('guest-')
        && !this.accounts.some((account) => account.username === name);
    },

    createPasswordValid() {
      return codePoints(this.creating && this.creating.password) >= this.passwordMin;
    },

    createValid() {
      return this.creating !== null && this.createNameValid() && this.createPasswordValid();
    },

    // --- passwords and policy ---------------------------------------------

    // The minimum password length the server enforces, 10 until loaded.
    get passwordMin() {
      return Number(this.policy.min_length) || DEFAULT_POLICY.min_length;
    },

    policyDirty() {
      return Number(this.policyDraft) !== this.policy.min_length;
    },

    // '' when the draft is a whole number within the bounds, else the message.
    get policyRangeError() {
      const value = Number(this.policyDraft);
      if (Number.isInteger(value) && value >= this.policy.min && value <= this.policy.max) return '';
      return t('users.policy.range_error', {min: this.policy.min, max: this.policy.max});
    },

    cancelPolicy() {
      this.policyDraft = this.policy.min_length;
    },

    async savePolicy() {
      if (!this.policyDirty() || this.policyRangeError) return false;
      const done = await this.action(
        () => this.send('PUT', '/api/v1/users/password-policy', {min_length: Number(this.policyDraft)}),
        t('users.policy.saved'),
      );
      if (done) this.markSaved('policy');
      return done;
    },

    // --- sessions tab -----------------------------------------------------

    // Opens or leaves a tab. Leaving the sessions tab stops its timer, opening
    // it again starts exactly one.
    async setTab(name) {
      this.tab = name === 'sessions' ? 'sessions' : 'accounts';
      this.stopSessionTimer();
      if (this.tab !== 'sessions') return;
      this.sessionTimer = window.setInterval(() => this.loadSessions(), SESSIONS_MS);
      await this.loadSessions();
    },

    stopSessionTimer() {
      if (this.sessionTimer === null) return;
      window.clearInterval(this.sessionTimer);
      this.sessionTimer = null;
    },

    async loadSessions() {
      try {
        const body = await requestJSON('/api/v1/user-sessions');
        this.sessions = body.sessions || [];
        this.sessionsError = '';
      } catch (error) {
        this.sessionsError = error.message;
      }
    },

    // {username: [session, ...]} for the account cards.
    get sessionsByUser() {
      return window.UsersModel.groupSessions(this.sessions);
    },

    sessionCountFor(username) {
      return (this.sessionsByUser[username] || []).length;
    },

    // The session this page runs in cannot be ended from here.
    canEndSession(session) {
      return !session.current;
    },

    // --- passwords and sessions -------------------------------------------

    // Runs one request, shows the message on success and reloads the data.
    async action(request, message) {
      try {
        await request();
        if (message) this.$store.toasts.push(message);
        await this.load();
        return true;
      } catch (error) {
        this.$store.toasts.push(error.message, 'critical');
        return false;
      }
    },

    // Like action(), but the row of id collapses first and only then leaves
    // the list. Used for deleting an account and ending a guest session.
    async leave(id, request, message) {
      try {
        await request();
      } catch (error) {
        this.$store.toasts.push(error.message, 'critical');
        return false;
      }
      if (message) this.$store.toasts.push(message);
      this.leaving[id] = true;
      await wait(leaveDelay());
      try {
        await this.load();
      } finally {
        delete this.leaving[id];
      }
      return true;
    },

    async resetPassword(id, password) {
      if (codePoints(password) < this.passwordMin) return false;
      return this.action(
        () => this.send('PUT', `/api/v1/users/${enc(id)}/password`, {password}),
        t('users.password.toast', {name: id}),
      );
    },

    async setHTTPPassword(id, password) {
      if (codePoints(password) < this.passwordMin) return false;
      return this.action(
        () => this.send('PUT', `/api/v1/users/${enc(id)}/http-password`, {password}),
        t('users.http_warning.set_toast', {name: id}),
      );
    },

    // An empty value removes the HTTP password (the server reads it that way).
    async removeHTTPPassword(id) {
      return this.action(
        () => this.send('PUT', `/api/v1/users/${enc(id)}/http-password`, {password: ''}),
        t('users.http_warning.removed_toast'),
      );
    },

    async remove(id) {
      const confirmed = await this.$store.modal.confirm({
        title: t('users.delete_confirm.title', {name: id}),
        body: t('users.delete_confirm.body'),
        confirmLabel: t('users.delete_confirm.confirm'),
        danger: true,
      });
      if (!confirmed) return false;
      const done = await this.leave(
        id,
        () => this.send('DELETE', `/api/v1/users/${enc(id)}`),
        t('users.delete_confirm.toast', {name: id}),
      );
      if (done) {
        delete this.drafts[id];
        if (this.openId === id) this.openId = null;
      }
      return done;
    },

    async endSession(id) {
      const session = this.sessions.find((item) => item.id === id);
      const name = session ? session.username : '';
      const confirmed = await this.$store.modal.confirm({
        title: t('users.sessions.end_confirm.title', {name}),
        body: t('users.sessions.end_confirm.body'),
        confirmLabel: t('users.sessions.end'),
        danger: true,
      });
      if (!confirmed) return false;
      try {
        await this.send('DELETE', `/api/v1/user-sessions/${enc(id)}`);
      } catch (error) {
        this.$store.toasts.push(error.message, 'critical');
        return false;
      }
      this.$store.toasts.push(t('users.sessions.ended_toast', {name}));
      await this.loadSessions();
      return true;
    },
  });

  const register = () => {
    if (!window.Alpine) return;
    Alpine.data('usersPage', usersPage);
  };
  if (window.Alpine) register(); else document.addEventListener('alpine:init', register, {once: true});
})();
