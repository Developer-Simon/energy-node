// Seite "Lernfunktionen" in den Einstellungen. Reine Hilfen fuer die Zeilen
// (Speicherverbrauch, "Lernt seit", neue Funktionen) und die Alpine-Daten
// learningPage() fuer die Karten. Die Hilfen laufen ohne Alpine, damit Tests
// sie direkt aufrufen koennen.
(() => {
  const DAY_SECONDS = 86400;
  const KB = 1000;
  const MB = 1000 * KB;

  const defaultT = (key, params) => (window.I18n ? window.I18n.t(key, params) : key);

  // Zahlen folgen der globalen Zahlenformatierung (I18n.formatNumber).
  const numberText = (value, decimals) => (window.I18n ? window.I18n.formatNumber(value, decimals) : value.toFixed(decimals));

  // Ab 100000 Bytes in MB mit einer Nachkommastelle, darunter ganze kB.
  const MB_FROM = 100000;
  const sizeText = (bytes, t) => {
    if (bytes >= MB_FROM) return t('settings.learning.size.mb', {value: numberText(bytes / MB, 1)});
    return t('settings.learning.size.kb', {value: numberText(Math.round(bytes / KB), 0)});
  };

  // usage ist autolearn.Usage aus GET /api/v1/learning/status: bytes,
  // written_per_day (Bytes je Tag). Ohne Schreibrate entfaellt der Teil danach.
  function usageText(usage, t = defaultT) {
    const bytes = Math.max(0, Number(usage && usage.bytes) || 0);
    const perDay = Math.max(0, Number(usage && usage.written_per_day) || 0);
    const size = sizeText(bytes, t);
    if (!perDay) return t('settings.learning.usage.stored', {size});
    const rate = t('settings.learning.usage.rate', {size: sizeText(perDay, t)});
    return t('settings.learning.usage.written', {size, rate});
  }

  // feature ist ein Eintrag aus status.features ({on, since, devices}); since
  // ist Unix-Zeit in Sekunden. Ohne since steht da "noch nichts gelernt".
  function sinceText(feature, t = defaultT, now = Date.now()) {
    const since = Number(feature && feature.since) || 0;
    const devices = Math.max(0, Number(feature && feature.devices) || 0);
    if (since <= 0) return t('settings.learning.since_none');
    const days = Math.max(0, Math.floor((now / 1000 - since) / DAY_SECONDS));
    // Plural wie I18n.tn: .one bei genau einem Tag, sonst .other.
    const key = days === 1 ? 'settings.learning.since.one' : 'settings.learning.since.other';
    return t(key, {days: numberText(days, 0), devices: numberText(devices, 0)});
  }

  // Neu aktive Funktionen: status.new, aber nur die, die noch eingeschaltet sind.
  function newFeatures(status) {
    const features = (status && status.features) || {};
    const fresh = (status && Array.isArray(status.new)) ? status.new : [];
    return fresh.filter(key => Boolean(features[key] && features[key].on));
  }

  window.LearningSettings = {usageText, sinceText, newFeatures};

  // Gleiche Anfrage wie in settings.page.js: Basispfad vor die URL, JSON zurueck.
  const requestJSON = async (url, options) => {
    const response = await fetch(`${window.__DASHBOARD_BASE_PATH__ || ''}${url}`, options);
    const body = await response.json().catch(() => ({}));
    if (!response.ok) {
      const message = window.I18n ? window.I18n.error(body) : (body && body.message) || 'common.request_failed';
      throw new Error(message);
    }
    return body;
  };

  const t = defaultT;
  const picker = () => (window.DevicePicker ? window.DevicePicker.mixin() : {});

  // Einzelwahl der Aussentemperatur: nur Entitaeten mit device_class
  // temperature. Der Abschnitt ist das Geraet, damit gleich benannte
  // Sensoren zu unterscheiden sind.
  const temperatureItems = devices => devices.flatMap(device => (device.entities || [])
    .filter(entity => entity.device_class === 'temperature' && entity.unique_id)
    .map(entity => ({
      id: entity.unique_id,
      name: entity.name || entity.unique_id,
      kind: 'device',
      section: device.name || '',
      valueText: '',
      iconMarkup: '',
      memberOf: '',
    })));

  // Geraet und Entitaet zu einer unique_id, null wenn die Entitaet fehlt.
  const findEntity = (devices, uniqueId) => {
    for (const device of devices || []) {
      const entity = (device.entities || []).find(item => item.unique_id === uniqueId);
      if (entity) return {device, entity};
    }
    return null;
  };

  const learningPage = () => ({
    ...picker(),
    status: null,
    loading: false,
    error: '',
    busy: false,
    csrfToken: '',
    // edit_setup aus der Sitzung. Nur mit diesem Recht gibt es den Hinweis
    // "neu aktiv" und die Quittung (der Server prueft es ohnehin).
    learnCanEdit: true,
    devices: null,
    // Ausgenommene Geraete (IDs) je settings.learning_excluded_devices.
    learning_excluded_devices: [],
    // Aussentemperatur-Entitaet (unique_id) je settings.automation_outdoor_entity, leer wenn keine gewaehlt ist.
    automation_outdoor_entity: '',
    // Standort ist in den Automationen eingetragen (status.location_set). Solange
    // der Stand nicht geladen ist, bleibt der Hinweis verborgen.
    location_set: true,

    // Die Auswahl gehoert der Seite (settings.page.js, speichert sie). Diese
    // Seite bekommt den Stand beim Laden und meldet jede Aenderung zurueck.
    init() {
      window.addEventListener('settings-learning-loaded', (event) => {
        this.learning_excluded_devices = Array.isArray(event.detail && event.detail.ids) ? [...event.detail.ids] : [];
      });
    },

    // Hinweis "neu aktiv" oben auf der Seite: es gibt neue Funktionen, die
    // Einrichtung ist fertig und der Nutzer darf die Einrichtung aendern.
    get newHintVisible() {
      return this.learnCanEdit && Boolean(this.status && this.status.setup_done) && this.newKeys.length > 0;
    },

    get newHintTitle() {
      const n = this.newKeys.length;
      const key = this.status && this.status.fresh ? 'settings.learning.new.fresh_title' : 'settings.learning.new.title';
      return window.I18n ? window.I18n.tn(key, n, {n: numberText(n, 0)}) : key;
    },

    // Frisches System und Update haben je einen Einleitungssatz, dahinter steht
    // derselbe Satz zu Speicher und Abschalten.
    get newHintBody() {
      const lead = this.status && this.status.fresh ? t('settings.learning.new.body_fresh') : t('settings.learning.new.body_update');
      return `${lead} ${t('settings.learning.new.tail')}`;
    },

    isNew(key) {
      return this.newKeys.includes(key);
    },

    // Schaltet eine Zeile ab, gilt sie damit als gesehen. Einschalten quittiert nichts.
    switchedOff(key, on) {
      if (!on) this.acknowledge([key]);
    },

    // Quittiert die Schluessel: sie bleiben in learning_acknowledged stehen und
    // gelten danach nicht mehr als neu. Die Liste der Quittungen kommt aus dem
    // Status, damit keine frueher quittierte Funktion verloren geht.
    // Anfrage mit dem CSRF-Token der Sitzung, so schreiben alle Aktionen der Seite.
    csrfRequest(url, options) {
      return requestJSON(url, {
        ...options,
        headers: {...((options && options.headers) || {}), 'X-CSRF-Token': this.csrfToken},
      });
    },

    async acknowledge(keys) {
      const acknowledged = [...new Set([...((this.status && this.status.acknowledged) || []), ...keys])];
      try {
        await saveSettingsFields({learning_acknowledged: acknowledged}, (url, options) => this.csrfRequest(url, options));
        window.dispatchEvent(new CustomEvent('learning-acknowledged', {detail: {ids: acknowledged}}));
        await this.load();
      } catch (error) {
        this.$store.toasts.push(error.message, 'critical');
      }
    },

    setExcluded(ids) {
      this.learning_excluded_devices = [...ids];
      window.dispatchEvent(new CustomEvent('learning-excluded-changed', {detail: {ids: [...ids]}}));
    },

    async load() {
      this.loading = true;
      this.error = '';
      try {
        const [status, session, settings, devices] = await Promise.all([
          requestJSON('/api/v1/learning/status'),
          requestJSON('/api/v1/auth/session').catch(() => null),
          requestJSON('/api/v1/settings').catch(() => null),
          requestJSON('/api/v1/devices').catch(() => null),
        ]);
        this.status = status;
        this.csrfToken = (session && session.csrf_token) || '';
        this.learnCanEdit = session ? session.edit_setup === true : true;
        this.automation_outdoor_entity = settings && typeof settings.automation_outdoor_entity === 'string' ? settings.automation_outdoor_entity : '';
        // Fehlt das Feld (aelterer Server), bleibt der Hinweis verborgen.
        this.location_set = !(status && status.location_set === false);
        if (Array.isArray(devices)) this.devices = devices;
        // Der Punkt am Reiter Einstellungen in der Seitenleiste folgt dem Hinweis.
        window.dispatchEvent(new CustomEvent('learning-new-changed', {detail: {open: this.newHintVisible}}));
      } catch (error) {
        this.error = error.message;
      } finally {
        this.loading = false;
      }
    },

    // Hinweis "Standort fehlt" im Bereich der Muster: nur ohne Standort. Ob die
    // Muster an sind, prueft die Karte selbst (automationPatternsDisabled).
    get locationMissing() {
      return !this.location_set;
    },

    // Oeffnet die Automationen. Dort sitzt das Feld Standort im aufklappbaren Bereich.
    openLocationSettings() {
      window.dispatchEvent(new CustomEvent('dashboard-open-panel', {detail: {panel: 'automations-panel'}}));
    },

    get usage() {
      return usageText(this.status && this.status.usage, t);
    },

    get newKeys() {
      return newFeatures(this.status);
    },

    sinceLabel(key) {
      return sinceText(this.status && this.status.features && this.status.features[key], t);
    },

    excludedLabel() {
      const n = this.learning_excluded_devices.length;
      if (!n) return t('settings.learning.exclude.none');
      return window.I18n ? window.I18n.tn('settings.learning.exclude.count', n, {n: numberText(n, 0)}) : String(n);
    },

    async loadDevices() {
      if (this.devices) return this.devices;
      try {
        const list = await requestJSON('/api/v1/devices');
        this.devices = Array.isArray(list) ? list : [];
      } catch (error) {
        this.$store.toasts.push(error.message, 'critical');
        return [];
      }
      return this.devices;
    },

    // Mehrfachwahl. Ein leeres Ergebnis heisst bei der Picker-Schnittstelle
    // "abgebrochen", deshalb laesst sich hier nicht alles auf einmal abwaehlen.
    async chooseExcluded() {
      const devices = await this.loadDevices();
      if (!devices.length) return;
      const ids = await this.openPicker({
        title: t('settings.learning.exclude.title'),
        items: devices.map(device => ({
          id: device.id,
          kind: 'device',
          name: device.name || device.id,
          section: device.manufacturer || '',
          valueText: '',
          iconMarkup: '',
          memberOf: '',
        })),
        mode: 'multi',
        selected: [...this.learning_excluded_devices],
        confirmLabel: t('settings.learning.exclude.confirm'),
      });
      if (ids) this.setExcluded(ids);
    },

    // Hebt die Ausnahmen auf. Der Picker selbst kann nicht auf leer bestaetigen.
    clearExcluded() {
      this.setExcluded([]);
    },

    // Zeile "Aussentemperatur": Name des Geraets und der Entitaet. Fehlt die
    // gespeicherte Entitaet in der Registry, steht dort "nicht mehr vorhanden".
    outdoorLabel() {
      const uniqueId = this.automation_outdoor_entity;
      if (!uniqueId) return t('settings.learning.outdoor.none');
      if (!this.devices) return '';
      const found = findEntity(this.devices, uniqueId);
      return found ? `${found.device.name}, ${found.entity.name}` : t('settings.learning.outdoor.missing');
    },

    // Einzelwahl unter den Temperatur-Entitaeten, eine Auswahl wird sofort gespeichert.
    async chooseOutdoor() {
      const devices = await this.loadDevices();
      if (!devices.length) return;
      const uniqueId = await this.openPicker({
        title: t('settings.learning.outdoor.pick_title'),
        items: temperatureItems(devices),
        mode: 'single',
        selected: this.automation_outdoor_entity ? [this.automation_outdoor_entity] : [],
        confirmLabel: t('settings.learning.outdoor.confirm'),
      });
      if (uniqueId) await this.setOutdoor(uniqueId);
    },

    clearOutdoor() {
      return this.setOutdoor('');
    },

    // Speichert die Entitaet ueber saveSettingsFields, bei Fehler kommt der alte Wert zurueck.
    async setOutdoor(uniqueId) {
      const previous = this.automation_outdoor_entity;
      this.automation_outdoor_entity = uniqueId;
      try {
        await saveSettingsFields({automation_outdoor_entity: uniqueId}, (url, options) => this.csrfRequest(url, options));
      } catch (error) {
        this.automation_outdoor_entity = previous;
        this.$store.toasts.push(error.message, 'critical');
      }
    },

    async reset() {
      if (this.busy) return false;
      const confirmed = await this.$store.modal.confirm({
        title: t('settings.learning.reset.confirm_title'),
        body: t('settings.learning.reset.confirm_body'),
        confirmLabel: t('settings.learning.reset.confirm'),
        danger: true,
      });
      if (!confirmed) return false;
      this.busy = true;
      try {
        await requestJSON('/api/v1/learning/reset', {
          method: 'POST',
          headers: {'X-CSRF-Token': this.csrfToken},
        });
        this.$store.toasts.push(t('settings.learning.reset.done'));
        await this.load();
        return true;
      } catch (error) {
        this.$store.toasts.push(error.message, 'critical');
        return false;
      } finally {
        this.busy = false;
      }
    },
  });

  const register = () => {
    if (!window.Alpine) return;
    Alpine.data('learningPage', learningPage);
  };
  if (window.Alpine) register(); else document.addEventListener('alpine:init', register, {once: true});
})();
