// Einzelne Einstellungen, die auf Unterseiten (Speicher, MQTT) statt unter
// "Allgemein" gespeichert werden. PUT /api/v1/settings ersetzt das ganze
// Dokument, deshalb liest saveSettingsFields zuerst das Dokument, setzt nur
// die Felder aus patch und schreibt es zurueck. Alle anderen Felder bleiben
// dabei unveraendert.
//
// Die Funktionen sind bewusst globale Deklarationen, damit Tests sie direkt
// aufrufen koennen. fetchJSON(url, options) liefert den JSON-Koerper und wirft
// bei einer Antwort ohne Erfolg einen Error mit .status (HTTP-Code).

// Gleiche Anfrage wie in settings.page.js: Basispfad vor die URL, JSON zurueck.
async function settingsRequest(url, options) {
  const response = await fetch(`${window.__DASHBOARD_BASE_PATH__ || ''}${url}`, options);
  const body = await response.json().catch(() => ({}));
  if (!response.ok) {
    const error = new Error((window.I18n ? window.I18n.error(body) : (body && body.message)) || 'common.request_failed');
    error.status = response.status;
    throw error;
  }
  return body;
}

async function saveSettingsFields(patch, fetchJSON) {
  const current = await fetchJSON('/api/v1/settings');
  const merged = {...current, ...patch};
  const saved = await fetchJSON('/api/v1/settings', {
    method: 'PUT',
    headers: {'Content-Type': 'application/json'},
    body: JSON.stringify(merged),
  });
  return saved && typeof saved === 'object' && Object.keys(saved).length ? saved : merged;
}

// Sekunden zwischen zwei Schreibvorgaengen wie viele pro Tag.
function writesPerDay(seconds) {
  const value = Number(seconds);
  if (!Number.isFinite(value) || value <= 0) return 0;
  return Math.round(86400 / value);
}

function settingsFieldText(key, params) {
  return window.I18n ? window.I18n.t(key, params) : key;
}

// name ist der camelCase-Name wie in settings.page.js (sweepIntervalSeconds),
// der Schluessel im Dokument ist die snake_case-Form.
function settingsField(name, min, max, stepSize) {
  const key = name.replace(/[A-Z]/g, (letter) => `_${letter.toLowerCase()}`);
  return {
    key,
    min,
    max,
    stepSize,
    value: min,
    saved: min,
    csrfToken: '',
    timer: null,

    init() {
      this.$watch('value', () => this.scheduleSave());
      this.load();
    },

    async load() {
      try {
        const [current, session] = await Promise.all([
          settingsRequest('/api/v1/settings'),
          settingsRequest('/api/v1/auth/session').catch(() => null),
        ]);
        const stored = Number(current[this.key]);
        this.saved = stored;
        this.value = stored;
        this.csrfToken = (session && session.csrf_token) || '';
      } catch (error) {
        this.$store.toasts.push(error.message, 'critical');
      }
    },

    get valid() {
      const value = Number(this.value);
      return Number.isInteger(value) && value >= this.min && value <= this.max;
    },

    step(dir) {
      const current = Number.isFinite(Number(this.value)) ? Number(this.value) : this.min;
      this.value = Math.min(this.max, Math.max(this.min, current + dir * this.stepSize));
    },

    scheduleSave() {
      clearTimeout(this.timer);
      this.timer = setTimeout(() => this.save(), 600);
    },

    async save() {
      clearTimeout(this.timer);
      if (!this.valid) {
        this.$store.toasts.push(settingsFieldText('settings.validation_error'), 'critical');
        return;
      }
      const value = Number(this.value);
      if (value === this.saved) return;
      const fetchWithCsrf = (url, options) => settingsRequest(url, {
        ...options,
        headers: {...((options && options.headers) || {}), 'X-CSRF-Token': this.csrfToken},
      });
      try {
        await saveSettingsFields({[this.key]: value}, fetchWithCsrf);
        this.saved = value;
        this.$store.toasts.push(settingsFieldText('settings.saved'));
      } catch (error) {
        // Der Wert auf der Seite zeigt wieder, was gespeichert ist.
        this.value = this.saved;
        this.$store.toasts.push(error.message, 'critical');
      }
    },

    writesPerDay() {
      return writesPerDay(this.value);
    },

    writesText() {
      const count = writesPerDay(this.value);
      const formatted = window.I18n ? window.I18n.formatNumber(count, 0) : String(count);
      return settingsFieldText('settings.sweep.writes', {n: formatted});
    },
  };
}

(() => {
  const register = () => {
    if (!window.Alpine) return;
    Alpine.data('settingsField', settingsField);
  };
  if (window.Alpine) register(); else document.addEventListener('alpine:init', register, {once: true});
})();
