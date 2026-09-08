(() => {
  const withBase = (url) => `${window.__DASHBOARD_BASE_PATH__ || ''}${url}`;

  const requestJSON = async (url, options) => {
    // The single chokepoint for every URL literal in this file: behind a
    // reverse-proxy subpath base.html puts the prefix into
    // __DASHBOARD_BASE_PATH__; on direct access it is empty.
    const response = await fetch(withBase(url), options);
    const body = await response.json();
    if (!response.ok) {
      throw new Error(body.message || 'Anfrage fehlgeschlagen');
    }
    return body;
  };

  // Bedienoberflaeche fuer /etc/energy-node/config.json. Baut aus dem
  // eingebetteten Schema dasselbe Formular wie der Konfigurationen-Tab
  // (schema-form.js). Nur wenn die Datei Felder enthaelt, die das Schema
  // nicht kennt - oder das Schema gar nicht ladbar ist -, faellt sie auf
  // einen Rohtext-Editor zurueck, damit eine verrutschte Betriebs-
  // konfiguration ueberhaupt noch reparierbar bleibt.
  function systemConfigPanel() {
    return {
      schema: null,
      value: null,
      text: '',
      error: '',
      busy: false,
      loading: false,
      recoverMode: false,
      unknownKeys: [],
      restartRequired: [],
      reloaded: {},
      // Nur-Lese-Liste der Revisions-Dateinamen, wie GET /api/v1/system/config
      // sie inline liefert. Die Systemkonfiguration hat serverseitig keine
      // /revisions-, /revisions/{name}- und /restore-Routen, also kein Diff und
      // kein Wiederherstellen - siehe P1.8 der Dashboard-Ideenliste.
      revisions: [],
      // Formulareingaben aendern das DOM, nicht den Alpine-State; dieser
      // Zaehler wird bei jedem input/change hochgesetzt, damit der dirty-
      // Getter neu ausgewertet wird.
      dirtyTick: 0,

      async load() {
        this.loading = true;
        this.error = '';
        try {
          const [document, schema] = await Promise.all([
            requestJSON('/api/v1/system/config'),
            requestJSON('/api/v1/system/config/schema').catch(() => null),
          ]);
          this.value = document.config;
          this.text = JSON.stringify(this.value, null, 2);
          this.revisions = Array.isArray(document.revisions) ? [...document.revisions].reverse() : [];
          this.schema = schema;
          this.applyLoaded();
        } catch (err) {
          this.error = err.message;
        } finally {
          this.loading = false;
        }
      },

      // Entscheidet nach jedem Laden/Speichern zwischen Formular und
      // Rohtext und baut das Formular neu.
      applyLoaded() {
        this.unknownKeys = this.schema
          ? window.SchemaForm.findUnknownKeys(this.schema, this.value)
          : [];
        this.recoverMode = !this.schema || this.unknownKeys.length > 0;
        if (!this.recoverMode) this.$nextTick(() => this.renderForm());
      },

      renderForm() {
        const root = this.$refs.form;
        if (!root) return;
        root.replaceChildren(window.SchemaForm.renderNode(
          { hooks: {} },
          this.schema,
          this.value,
          window.SchemaForm.titleFor(this.schema, 'Konfiguration'),
        ));
        this.dirtyTick += 1;
      },

      // Liefert den Stand, den ein Speichern schreiben wuerde. Wirft, wenn
      // der Rohtext kaputtes JSON ist.
      currentValue() {
        if (this.recoverMode) return JSON.parse(this.text);
        return window.SchemaForm.readNode(this.$refs.form.querySelector('.schema-node'));
      },

      get dirty() {
        void this.dirtyTick;
        if (this.loading || this.value === null) return false;
        if (!this.recoverMode && !(this.$refs.form && this.$refs.form.querySelector('.schema-node'))) return false;
        try {
          return JSON.stringify(this.currentValue()) !== JSON.stringify(this.value);
        } catch (err) {
          return true; // kaputtes Roh-JSON zaehlt als ungespeicherte Aenderung
        }
      },

      async save() {
        this.busy = true;
        this.error = '';
        let payload;
        try {
          payload = this.currentValue();
        } catch (err) {
          this.error = `Ungueltiges JSON: ${err.message}`;
          this.busy = false;
          return;
        }
        try {
          const session = await requestJSON('/api/v1/auth/session').catch(() => null);
          const csrfToken = (session && session.csrf_token) || '';
          const response = await fetch(withBase('/api/v1/system/config'), {
            method: 'PUT',
            headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken },
            body: JSON.stringify(payload),
          });
          const data = await response.json().catch(() => ({}));
          if (!response.ok) {
            this.error = data.message || `Speichern fehlgeschlagen (HTTP ${response.status})`;
            this.busy = false;
            return;
          }
          this.value = data.config;
          this.text = JSON.stringify(this.value, null, 2);
          this.restartRequired = data.restart_required || [];
          this.reloaded = data.reloaded || {};
          this.applyLoaded();
        } catch (err) {
          this.error = err.message;
        } finally {
          this.busy = false;
        }
      },

      discard() {
        this.text = JSON.stringify(this.value, null, 2);
        if (!this.recoverMode) this.renderForm();
        this.dirtyTick += 1;
      },

      // Der Dateiname ist ein UTC-Zeitstempel plus '.json'
      // (writeSystemConfigRevision, systemconfig.go). Fuer die Anzeige das
      // Suffix abschneiden und 'T' durch ein Leerzeichen ersetzen; unbekannte
      // Formen unveraendert durchreichen.
      revisionLabel(name) {
        return String(name).replace(/\.json$/, '').replace('T', ' ');
      },
    };
  }

  const register = () => {
    if (!window.Alpine) return;
    Alpine.data('systemConfigPanel', systemConfigPanel);
  };
  if (window.Alpine) register(); else document.addEventListener('alpine:init', register, { once: true });
})();
