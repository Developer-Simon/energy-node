(() => {
  const t = (key, params) => (window.I18n ? window.I18n.t(key, params) : key);
  const apiError = (body, fallbackKey) => (window.I18n ? window.I18n.error(body, fallbackKey) : (body && body.message) || fallbackKey || 'common.request_failed');

  const requestJSON = async (url, options) => {
    // Every URL of this file goes through here: behind a reverse proxy the
    // base path is in __DASHBOARD_BASE_PATH__, on direct access it is empty.
    const response = await fetch(`${window.__DASHBOARD_BASE_PATH__ || ''}${url}`, options);
    const body = await response.json().catch(() => ({}));
    if (!response.ok) {
      throw new Error(apiError(body));
    }
    return body;
  };

  // Literal keys only, so the catalog scanner sees every text.
  const ERROR_KEYS = {
    auth: 'network.error.auth',
    not_found: 'network.error.not_found',
    no_address: 'network.error.no_address',
    no_gateway: 'network.error.no_gateway',
    failed: 'network.error.failed',
  };
  const LEVEL_KEYS = ['', 'network.level.1', 'network.level.2', 'network.level.3', 'network.level.4'];
  const STATUS_POLL_MS = 10000;
  const TICK_MS = 1000;
  const SCAN_SETTLE_MS = 1800;
  const EMPTY_FLOW = {ssid: null, steps: [], result: null, message: ''};

  const errorText = (code) => t(ERROR_KEYS[code] || ERROR_KEYS.failed);
  const levelOf = (dbm) => (dbm >= -55 ? 4 : dbm >= -65 ? 3 : dbm >= -75 ? 2 : 1);
  // Number and time formatting only through I18n, see format-guard.test.mjs.
  const bandLabel = (band) => {
    if (!band) return '';
    const value = window.I18n ? window.I18n.formatNumber(Number(band), 1) : String(band);
    return `${value} GHz`;
  };
  const clock = (stamp) => (window.I18n ? window.I18n.formatTime(stamp, {hour: '2-digit', minute: '2-digit'}) : '') || String(stamp || '');

  window.networkPage = () => ({
    status: null,
    statusError: '',
    actionError: '',
    scanning: false,
    scannedAt: null,
    openSsid: null,
    password: '',
    showPw: false,
    hotspotPwShown: false,
    // Kept in memory only, filled on "Zeigen" and dropped again on "Verbergen",
    // when the hotspot goes off and when the page is left.
    hotspotSecret: {ssid: '', password: ''},
    hidden: {ssid: '', password: ''},
    csrfToken: '',
    loadedAt: 0,
    dismissedSwitch: null,
    timer: null,

    async init() {
      const session = await requestJSON('/api/v1/auth/session').catch(() => null);
      this.csrfToken = (session && session.csrf_token) || '';
      await this.load();
      if (!this.timer) this.timer = window.setInterval(() => this.tick(), TICK_MS);
    },

    // One clock for both rhythms: every second while a switch runs and has no
    // result, every 10 s otherwise. Nothing is polled while the page is hidden.
    tick() {
      if (!this.$el || this.$el.offsetParent === null) return;
      const sw = this.status && this.status.switch;
      const running = Boolean(sw) && !sw.result;
      if (running || Date.now() - this.loadedAt >= STATUS_POLL_MS) this.load();
    },

    async load() {
      try {
        this.status = await requestJSON('/api/v1/network/status', {cache: 'no-store'});
        this.statusError = '';
        this.loadedAt = Date.now();
        if (!this.scannedAt) this.scannedAt = Date.now();
      } catch (error) {
        this.statusError = error.message;
      }
    },

    async post(url, body) {
      return requestJSON(url, {
        method: 'POST',
        headers: {'Content-Type': 'application/json', 'X-CSRF-Token': this.csrfToken},
        body: JSON.stringify(body),
      });
    },

    // Stops the poll and drops the hotspot password when the page is left.
    destroy() {
      if (this.timer) window.clearInterval(this.timer);
      this.timer = null;
      this.dropHotspotSecret();
    },

    // Saves the fallback mode and the hotspot switch at once. The shown value
    // changes first and goes back to the previous one with an error toast when
    // the request fails.
    async saveSettings(next) {
      if (this.locked() || !this.canChange()) return;
      const previous = {fallback: this.fallbackMode(), hotspot: this.hotspotOn()};
      if (previous.fallback === next.fallback && previous.hotspot === next.hotspot) return;
      this.status.settings = {...next};
      try {
        await requestJSON('/api/v1/network/settings', {
          method: 'PUT',
          headers: {'Content-Type': 'application/json', 'X-CSRF-Token': this.csrfToken},
          body: JSON.stringify(next),
        });
      } catch (error) {
        this.status.settings = previous;
        this.$store.toasts.push(error.message, 'critical');
      }
    },

    setFallback(mode) {
      this.saveSettings({fallback: mode, hotspot: this.hotspotOn()});
    },

    setHotspot(on) {
      if (!on) this.dropHotspotSecret();
      this.saveSettings({fallback: this.fallbackMode(), hotspot: Boolean(on)});
    },

    // The password is only fetched on "Zeigen". A failed fetch leaves the
    // password hidden and shows the error.
    async toggleHotspotPw() {
      if (this.hotspotPwShown) {
        this.dropHotspotSecret();
        return;
      }
      try {
        const secret = await requestJSON('/api/v1/network/hotspot-secret', {cache: 'no-store'});
        this.hotspotSecret = {ssid: secret.ssid || '', password: secret.password || ''};
        this.hotspotPwShown = true;
      } catch (error) {
        this.dropHotspotSecret();
        this.$store.toasts.push(error.message, 'critical');
      }
    },

    dropHotspotSecret() {
      this.hotspotPwShown = false;
      this.hotspotSecret = {ssid: '', password: ''};
    },

    async scan() {
      if (this.scanning || this.locked()) return;
      this.scanning = true;
      this.actionError = '';
      try {
        await this.post('/api/v1/network/wifi/scan', {});
        await new Promise((resolve) => window.setTimeout(resolve, SCAN_SETTLE_MS));
        await this.load();
        this.scannedAt = Date.now();
      } catch (error) {
        this.actionError = error.message;
      } finally {
        this.scanning = false;
      }
    },

    async connect(net) {
      if (this.locked()) return;
      const body = {ssid: net.ssid, password: this.password, hidden: false, profile_uuid: net.profile_uuid || ''};
      await this.startConnect(body);
    },

    async connectHidden() {
      if (this.locked() || !this.hidden.ssid) return;
      const body = {ssid: this.hidden.ssid, password: this.hidden.password, hidden: true, profile_uuid: ''};
      this.hidden = {ssid: '', password: ''};
      await this.startConnect(body);
    },

    // The password lives in the request body only: it is taken out of the
    // component before the call and never kept after it.
    async startConnect(body) {
      this.actionError = '';
      this.dismissedSwitch = null;
      this.showPw = false;
      this.password = '';
      try {
        await this.post('/api/v1/network/wifi/connect', body);
      } catch (error) {
        this.actionError = error.message;
      } finally {
        body.password = '';
        await this.load();
      }
    },

    async forget(net) {
      if (this.locked()) return;
      this.actionError = '';
      try {
        await this.post('/api/v1/network/wifi/forget', {profile_uuid: net.profile_uuid || ''});
        this.openSsid = null;
      } catch (error) {
        this.actionError = error.message;
      }
      await this.load();
    },

    toggle(net) {
      const flow = this.flow();
      if (flow.ssid && flow.ssid !== net.ssid && !flow.result) return;
      this.openSsid = this.openSsid === net.ssid ? null : net.ssid;
      this.password = '';
      this.showPw = false;
    },

    finish() {
      const sw = this.status && this.status.switch;
      this.dismissedSwitch = sw ? sw.started_at : null;
      this.openSsid = null;
      this.password = '';
    },

    locked() { return this.unmanaged(); },
    // Settings can change once the status is loaded and no switch is running.
    canChange() {
      if (!this.status) return false;
      const sw = this.status.switch;
      return !(sw && !sw.result);
    },
    unmanaged() { return Boolean(this.status) && !this.status.managed; },
    hasWifi() { return this.status ? Boolean(this.status.has_wifi) : true; },
    countryMissing() { return Boolean(this.status) && Boolean(this.status.has_wifi) && !this.status.country; },
    links() { return (this.status && this.status.links) || []; },
    savedCount() { return ((this.status && this.status.networks) || []).filter((n) => n.saved).length; },
    fallbackMode() { return (this.status && this.status.settings && this.status.settings.fallback) || 'parallel'; },
    hotspotOn() { return Boolean(this.status && this.status.settings && this.status.settings.hotspot); },
    hotspotActive() { return Boolean(this.status && this.status.hotspot && this.status.hotspot.active); },
    // The address is fixed. Name and password come from the secret endpoint and are only set while shown.
    hotspotInfo() {
      return {
        ssid: this.hotspotPwShown ? this.hotspotSecret.ssid : '',
        password: this.hotspotPwShown ? this.hotspotSecret.password : '',
        address: '10.42.0.1',
      };
    },

    wifiLink() { return this.links().find((l) => l.id === 'wifi') || null; },
    viaWifi() { const w = this.wifiLink(); return Boolean(w && w.route); },
    online() { return this.links().some((l) => l.route && l.state === 'up'); },

    sortedNets() {
      const nets = (this.status && this.status.networks) || [];
      return [...nets].sort((a, b) =>
        (Number(b.current) - Number(a.current)) ||
        (Number(b.saved) - Number(a.saved)) ||
        (b.signal_dbm - a.signal_dbm));
    },

    scanNote() {
      if (this.scanning) return t('network.scan.running');
      const seconds = Math.max(1, Math.round((Date.now() - (this.scannedAt || Date.now())) / 1000));
      return t('network.scan.note', {count: this.sortedNets().length, seconds});
    },

    levelWord(level) { return LEVEL_KEYS[level] ? t(LEVEL_KEYS[level]) : ''; },
    levelLabel(level) { return this.levelWord(level); },
    levelNote(net) { return t('network.net.connected_note', {level: this.levelWord(net.level)}); },
    netState(net) {
      if (net.current) return `${t('network.net.connected')} · `;
      if (net.saved) return `${t('network.net.saved')} · `;
      return '';
    },
    bandLabel(band) { return bandLabel(band); },

    linkIcon(link) {
      if (link.id === 'lan') return '#ico-ethernet';
      return link.state === 'none' ? '#ico-wifi-off' : '#ico-wifi';
    },

    linkTone(link) {
      if (link.state === 'hotspot') return 'warn';
      if (link.state === 'error') return 'bad';
      if (link.state === 'up') return link.route || link.id === 'lan' ? 'ok' : 'info';
      return 'muted';
    },

    linkStateLabel(link) {
      switch (link.state) {
        case 'up': return link.id === 'wifi' && !link.route ? t('network.state.standby') : t('network.state.connected');
        case 'down': return link.id === 'lan' ? t('network.state.no_cable') : t('network.state.down');
        case 'off': return t('network.state.not_set_up');
        case 'error': return t('network.state.error');
        case 'none': return t('network.state.no_module');
        case 'blocked': return t('network.state.blocked');
        case 'hotspot': return t('network.state.hotspot');
        default: return link.state || '';
      }
    },

    linkError(link) {
      if (!link.last_error) return '';
      if (!link.last_error_ssid) return errorText(link.last_error);
      return t('network.error.last', {
        ssid: link.last_error_ssid,
        time: clock(link.last_error_at),
        error: errorText(link.last_error),
      });
    },

    linkFacts(link) {
      const facts = [];
      const add = (key, value) => { if (value) facts.push([t(key), value]); };
      if (link.id === 'lan') {
        add('network.facts.ip', link.address);
        add('network.facts.gateway', link.gateway);
        add('network.facts.dns', (link.dns || []).join(', '));
        if (link.speed_mbps) add('network.facts.speed', t('network.facts.speed_value', {speed: link.speed_mbps}));
        return link.state === 'up' ? facts : [];
      }
      if (link.state === 'hotspot') {
        add('network.facts.network', link.ssid);
        add('network.facts.address', link.address);
        if (this.status && this.status.hotspot) add('network.facts.devices', t('network.hotspot.clients', {count: this.status.hotspot.clients}));
        return facts;
      }
      if (link.state !== 'up' && link.state !== 'error') return facts;
      add('network.facts.network', link.ssid);
      if (link.signal_dbm !== null && link.signal_dbm !== undefined) {
        add('network.facts.signal', t('network.facts.signal_value', {quality: this.levelWord(levelOf(link.signal_dbm)), dbm: link.signal_dbm}));
      }
      add('network.facts.ip', link.address);
      add('network.facts.gateway', link.gateway);
      if (link.band) {
        const band = link.channel ? t('network.facts.band_channel', {band: bandLabel(link.band), channel: link.channel}) : bandLabel(link.band);
        add('network.facts.band', band);
      }
      return facts;
    },

    // The progress rows come from status.switch, so they also show after a reload.
    flow() {
      const sw = this.status && this.status.switch;
      if (!sw || (this.dismissedSwitch && this.dismissedSwitch === sw.started_at)) return EMPTY_FLOW;
      const steps = (sw.steps || []).map((step) => ({
        key: step.key,
        state: step.state,
        note: step.state === 'fail' && sw.error ? errorText(sw.error) : (step.detail || ''),
      }));
      let message = '';
      if (sw.result === 'ok') {
        message = sw.address ? t('network.flow.ok_address', {address: sw.address}) : t('network.flow.ok_saved');
      } else if (sw.result) {
        message = sw.rolled_back ? t('network.flow.fail_rolled_back') : t('network.flow.fail_kept');
      }
      return {ssid: sw.ssid || null, steps, result: sw.result || null, message};
    },
  });
})();
