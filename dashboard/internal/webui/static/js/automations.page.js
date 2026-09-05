(() => {
  const requestJSON = async (url, options) => {
    const response = await fetch(`${window.__DASHBOARD_BASE_PATH__ || ''}${url}`, options);
    const body = await response.json();
    if (!response.ok) {
      const error = new Error(body.message || 'Anfrage fehlgeschlagen');
      // Der Status wandert mit, damit Aufrufer ein 404 vom Netzwerkfehler
      // unterscheiden koennen: fehlt das Geraet "automation", ist das ein
      // Dauerzustand (kein Automations-Dienst), kein voruebergehender Fehler.
      error.status = response.status;
      throw error;
    }
    return body;
  };

  // Eine Tabelle je Bilanzfeld: Klartext, Icon-Symbol im Sprite, Einheit.
  const BALANCE_FIELD_INFO = {
    grid_export:       { label: 'Netzeinspeisung',     icon: 'ico-grid',     unit: 'W' },
    grid_import:       { label: 'Netzbezug',           icon: 'ico-grid',     unit: 'W' },
    pv:                { label: 'PV-Leistung',         icon: 'ico-sun',      unit: 'W' },
    load_total:        { label: 'Hausverbrauch',       icon: 'ico-load',     unit: 'W' },
    base:              { label: 'Übriger Verbrauch',   icon: 'ico-load',     unit: 'W' },
    wallbox:           { label: 'Wallbox',             icon: 'ico-wallbox',  unit: 'W' },
    heat_pump:         { label: 'Wärmepumpe',          icon: 'ico-heatpump', unit: 'W' },
    battery_charge:    { label: 'Batterie laden',      icon: 'ico-battery',  unit: 'W' },
    battery_discharge: { label: 'Batterie entladen',   icon: 'ico-battery',  unit: 'W' },
    gap_applied:       { label: 'Bilanzlücke',         icon: 'ico-warning',  unit: 'W' },
    autarkie:          { label: 'Autarkiegrad',        icon: 'ico-flash',    unit: '%' },
    eigenverbrauch:    { label: 'Eigenverbrauchsquote', icon: 'ico-flash',   unit: '%' },
    battery_soc:          { label: 'Batterie-Füllstand',    icon: 'ico-battery', unit: '%' },
    battery_capacity_kwh: { label: 'Speicher-Kapazität',    icon: 'ico-battery', unit: 'kWh' },
    battery_energy_kwh:   { label: 'Gespeicherte Energie',  icon: 'ico-battery', unit: 'kWh' },
  };

  const COMPARISON_WORDS = { above: 'über', below: 'unter', equals: 'gleich', not_equals: 'ungleich' };

  const GATE_VERDICTS = {
    fired:              { tone: 'ok',   label: 'Ausgelöst' },
    hold_pending:       { tone: 'wait', label: 'Wartet auf Haltedauer' },
    cooldown:           { tone: 'info', label: 'Sperrzeit läuft' },
    conditions_not_met: { tone: 'off',  label: 'Bedingungen nicht erfüllt' },
    settling:           { tone: 'off',  label: 'Beruhigungsphase nach Dienststart' },
    balance_stale:      { tone: 'bad',  label: 'Energie-Bilanz veraltet' },
    blocked:            { tone: 'bad',  label: 'Aktion blockiert' },
    error:              { tone: 'bad',  label: 'Fehler' },
    disabled:           { tone: 'off',  label: 'Regel ist aus' },
  };

  const HISTORY_RESULT_INFO = {
    fired:   { tone: 'ok',  label: 'Ausgelöst' },
    blocked: { tone: 'bad', label: 'Blockiert' },
    error:   { tone: 'bad', label: 'Fehler' },
  };

  function historyResultInfo(result) {
    return HISTORY_RESULT_INFO[result] || { tone: 'off', label: result || 'Unbekannt' };
  }

  function historyActionText(action) {
    if (!action) return '';
    if (action.severity !== undefined) return `„${action.title || ''}" — ${action.message || ''}`;
    if (action.blocked) return action.reason || 'blockiert';
    return `${action.topic} → ${action.payload}`;
  }

  // Was der Bearbeiten-Modus als Bedingung anbieten darf. "balance" heisst
  // bewusst Energiewert und nicht PV-Ueberschuss: der Typ balance_threshold
  // deckt jedes Bilanzfeld ab - Netzbezug, Autarkiegrad, Batterie-Fuellstand -,
  // die PV-Einspeisung ist nur die haeufigste Vorbelegung.
  const CONDITION_TYPES = [
    { key: 'balance', icon: 'ico-sun', label: 'Energiewert',
      hint: 'Ein Wert aus der Energiebilanz, zum Beispiel Netzeinspeisung, Hausverbrauch oder Batterie-Füllstand.' },
    { key: 'entity', icon: 'ico-thermo', label: 'Gerätewert',
      hint: 'Ein Messwert eines Geräts, zum Beispiel eine Temperatur oder ein Schaltzustand.' },
    { key: 'time', icon: 'ico-clock', label: 'Zeitfenster',
      hint: 'Gilt nur zwischen zwei Uhrzeiten.' },
  ];

  // "entity" legt keine Aktion an, sondern oeffnet den Geraete-Assistenten -
  // dort entscheidet erst die Wahl zwischen Ein/Aus/Umschalten/Sollwert,
  // welche publish-Aktion daraus wird.
  const ACTION_TYPES = [
    { key: 'entity', icon: 'ico-switch', label: 'Gerät schalten', advanced: false,
      hint: 'Ein bekanntes Gerät ein- oder ausschalten oder einen Sollwert setzen.' },
    { key: 'notify', icon: 'ico-bell', label: 'Benachrichtigung', advanced: false,
      hint: 'Eine Meldung im Dashboard anzeigen, ohne etwas zu schalten.' },
    { key: 'mqtt', icon: 'ico-topic', label: 'MQTT-Befehl', advanced: true,
      hint: 'Topic und Payload von Hand eintragen — nur nötig für Geräte, die das Dashboard nicht kennt.' },
  ];

  // Die Erklaerungen zu den Fachbegriffen. Die Begriffe selbst bleiben in der
  // Oberflaeche stehen, damit sie in der Doku und in der rules.json
  // wiederzufinden sind - erklaert wird daneben.
  const FIELD_HELP = {
    hysteresis: 'Rückschaltabstand: Erst wenn der Wert um diesen Betrag wieder unter die Schwelle fällt, gilt die Bedingung als nicht mehr erfüllt. Verhindert dauerndes Ein- und Ausschalten dicht an der Schwelle.',
    hold_seconds: 'Haltedauer: So lange muss die Bedingung ununterbrochen erfüllt sein, bevor die Regel auslöst. Bei Regeln mit Schaltbefehl mindestens 30 Sekunden.',
    cooldown_seconds: 'Sperrzeit: Nach dem Auslösen pausiert die Regel so lange, bevor sie erneut auslösen darf. Bei Regeln mit Schaltbefehl mindestens 30 Sekunden.',
    tick_interval_s: 'Tick-Intervall: So oft prüft der Dienst alle Regeln.',
    settling_seconds: 'Beruhigungsfenster: Nach einem Neustart des Dienstes wartet er so lange, bevor die erste Regel auslösen darf — die Messwerte sind direkt nach dem Start noch unvollständig.',
    balance_max_age_s: 'Maximales Bilanzalter: Ist die letzte Energiebilanz älter als das, gilt sie als veraltet und Regeln mit Energiewert-Bedingung lösen nicht aus.',
    publish_allowed_prefixes: 'Leer gelassen erlaubt das Dashboard beim Speichern automatisch genau die command_topics aller aktuell bekannten Geräte-Entitäten — neu hinzukommende Geräte oder Entitäten brauchen dafür ein erneutes Speichern. Sobald hier etwas eingetragen wird, gilt nur noch diese Liste.',
    history_limit: 'Wie viele der letzten Auslösungen je Regel aufbewahrt werden — ältere fallen raus. Gilt nur für Regeln, bei denen unten im Regel-Editor „Verlauf speichern" aktiviert ist.',
    history_enabled: 'Verlauf speichern: Zeichnet die Auslösungen dieser Regel auf, damit sie hier unter „Verlauf anzeigen" erscheinen. Ist der Schalter aus, prüft die Regel weiter, führt aber keine Liste.',
    history_persist: 'Bleibt der Haken gesetzt, übersteht der Verlauf einen Neustart des Dienstes (Datei im Geräteverzeichnis). Ausgeschaltet zeichnet der Dienst weiterhin auf — die Oberfläche zeigt den Verlauf weiter an —, schreibt ihn aber nie auf die Platte.',
    retain: 'Retained: Der Broker merkt sich die Nachricht und liefert sie an jeden neuen Abonnenten aus. Für Schaltbefehle meist unerwünscht.',
    scale: 'Skalierung: Der Bilanzwert wird mit diesem Faktor multipliziert, bevor er gesendet wird.',
    offset: 'Offset: Dieser Betrag wird nach der Skalierung addiert — mit einem negativen Wert hält man zum Beispiel eine Reserve zurück.',
    json_key: 'JSON-Key: Ist der Payload ein JSON-Dokument, wird der Wert unter diesem Schlüssel gelesen.',
    topic: 'Topic: Die MQTT-Adresse, unter der die Nachricht veröffentlicht wird.',
    payload: 'Payload: Der Inhalt, der unter dem Topic gesendet wird — bei einem Schalter typischerweise ON oder OFF.',
  };

  const SERVICE_TEST_TIMEOUT_MS = 10000;
  const SERVICE_TEST_POLL_MS = 1000;

  const isNumber = (candidate) => typeof candidate === 'number' && Number.isFinite(candidate);

  function formatSeconds(seconds) {
    const value = isNumber(seconds) ? Math.max(0, seconds) : 0;
    if (value < 60) return `${Math.round(value)} s`;
    if (value < 3600) return `${Math.round(value / 60)} min`;
    if (value < 86400) return `${Math.round(value / 3600)} h`;
    return `${Math.round(value / 86400)} d`;
  }

  function conditionState(report) {
    if (!report || report.value === null || report.value === undefined) return 'novalue';
    if (!report.raw_met) return 'unmet';
    if (!report.met) return 'pending';
    return 'met';
  }

  function isPercentCondition(condition, entity) {
    if (condition.type === 'entity_value') {
      return Boolean(entity) && entity.unit_of_measurement === '%';
    }
    if (condition.type !== 'balance_threshold') return false;
    return (BALANCE_FIELD_INFO[condition.field] || {}).unit === '%';
  }

  function meterScale(condition, report, entity) {
    if (condition.type === 'time_window') return { min: 0, max: 1440, kind: 'time' };
    if (isPercentCondition(condition, entity)) return { min: 0, max: 100, kind: 'percent' };
    const value = isNumber(report && report.value) ? report.value : null;
    const target = isNumber(report && report.target) ? report.target : null;
    if ((value !== null && value < 0) || (target !== null && target < 0)) {
      const bound = Math.max(Math.abs(value || 0), Math.abs(target || 0)) * 1.1 || 1;
      return { min: -bound, max: bound, kind: 'signed' };
    }
    const max = Math.max((target || 0) * 2, (value || 0) * 1.1, 1);
    return { min: 0, max, kind: 'linear' };
  }

  function meterFraction(value, scale) {
    if (!isNumber(value) || !scale || scale.max === scale.min) return 0;
    return Math.min(1, Math.max(0, (value - scale.min) / (scale.max - scale.min)));
  }

  function gateDetail(ruleState) {
    if (!ruleState) return '';
    if (ruleState.result === 'hold_pending') {
      const remaining = (ruleState.conditions || [])
        .map((entry) => (isNumber(entry.hold_remaining) ? entry.hold_remaining : 0))
        .reduce((highest, current) => Math.max(highest, current), 0);
      return `Noch ${formatSeconds(remaining)} bis zum Auslösen.`;
    }
    if (ruleState.result === 'cooldown') return `Noch ${formatSeconds(ruleState.cooldown_remaining)}.`;
    if (ruleState.reason) return ruleState.reason;
    return '';
  }

  function gateVerdict(ruleState, online) {
    if (!online) return { tone: 'off', label: 'Dienst offline', detail: 'Es liegen keine Live-Werte vor.' };
    const entry = GATE_VERDICTS[ruleState && ruleState.result];
    if (!entry) return { tone: 'off', label: 'Zustand unbekannt', detail: '' };
    return { tone: entry.tone, label: entry.label, detail: gateDetail(ruleState) };
  }

  const DEVICE_CLASS_ICONS = {
    battery: 'ico-battery', power: 'ico-flash', energy: 'ico-flash',
    temperature: 'ico-thermo', current: 'ico-flash', voltage: 'ico-flash',
  };

  function describeCondition(condition, entity) {
    const type = condition && condition.type;
    if (type === 'balance_threshold') {
      const info = BALANCE_FIELD_INFO[condition.field] || { label: condition.field, icon: 'ico-flash', unit: '' };
      const hysteresis = condition.hysteresis ? `, Hysterese ${condition.hysteresis}` : '';
      return { icon: info.icon, title: info.label, unit: info.unit,
               summary: `${COMPARISON_WORDS[condition.comparison] || condition.comparison} ${condition.threshold} ${info.unit}${hysteresis}` };
    }
    if (type === 'entity_value') {
      // Fehlt die Entitaet - noch nicht geladen, oder aus dem Register
      // verschwunden -, tritt das gespeicherte Topic als Titel ein. Die Regel
      // laeuft in dem Fall unveraendert weiter, nur die Karte ist karger.
      const unit = (entity && entity.unit_of_measurement) || '';
      const icon = (entity && DEVICE_CLASS_ICONS[entity.device_class]) || 'ico-topic';
      const expected = condition.text !== undefined && condition.text !== '' ? condition.text : condition.value;
      const suffix = unit ? ` ${unit}` : '';
      return { icon, title: (entity && entity.name) || condition.topic || 'Entitätswert', unit,
               summary: `${COMPARISON_WORDS[condition.comparison] || condition.comparison} ${expected}${suffix}` };
    }
    if (type === 'topic_value') {
      const expected = condition.text !== undefined && condition.text !== '' ? condition.text : condition.value;
      return { icon: 'ico-topic', title: condition.topic || 'Topic-Wert', unit: '',
               summary: `${COMPARISON_WORDS[condition.comparison] || condition.comparison} ${expected}` };
    }
    if (type === 'time_window') {
      const days = (condition.weekdays || []).length ? ', nur an ausgewählten Tagen' : '';
      return { icon: 'ico-clock', title: 'Zeitfenster', unit: '',
               summary: `${condition.start} – ${condition.end}${days}` };
    }
    // Vorwaertskompatibilitaet: Spec A bringt weitere Typen. Bis dahin - und
    // falls je ein unbekannter Typ auftaucht - wird eine neutrale Karte
    // gezeichnet, statt die ganze Ansicht scheitern zu lassen.
    return { icon: 'ico-flash', title: type ? `Bedingung (${type})` : 'Unbekannte Bedingung',
             unit: '', summary: '' };
  }

  function describeAction(action, entity) {
    if (action && action.type === 'notification') {
      return { icon: 'ico-bell', kind: 'notify', title: action.title || 'Benachrichtigung' };
    }
    if (action && action.type === 'publish') {
      // Steht hinter der Aktion eine bekannte Entitaet (entity_id gesetzt und
      // im Register gefunden), zeigt die Karte deren Namen und Geraeteklasse
      // wie eine entity_value-Bedingung - fehlt die Entitaet, bleibt das
      // rohe Topic als Titel stehen, die Regel laeuft unveraendert weiter.
      const icon = (entity && DEVICE_CLASS_ICONS[entity.device_class]) || 'ico-switch';
      const title = (entity && entity.name) || action.topic || 'MQTT-Befehl';
      return { icon, kind: 'command', title };
    }
    return { icon: 'ico-flash', kind: 'command', title: 'Unbekannte Aktion' };
  }

  function canTest(context) {
    if (!context || context.hasRole === false) {
      return { allowed: false, reason: 'Für Automations-Tests fehlt die Berechtigung.' };
    }
    if (!context.online) return { allowed: false, reason: 'Der Automations-Dienst ist offline.' };
    if (context.dirty) return { allowed: false, reason: 'Erst speichern — getestet wird die gespeicherte Regel.' };
    return { allowed: true, reason: '' };
  }

  window.__automationsView = {
    conditionState, meterScale, meterFraction, gateVerdict, isPercentCondition,
    describeCondition, describeAction, formatSeconds, canTest,
    historyResultInfo, historyActionText,
    BALANCE_FIELD_INFO, GATE_VERDICTS, CONDITION_TYPES, ACTION_TYPES, FIELD_HELP,
  };

  const BALANCE_FIELDS = [
    ['grid_export', 'Netzeinspeisung'], ['grid_import', 'Netzbezug'], ['pv', 'PV-Leistung'],
    ['load_total', 'Hausverbrauch'], ['base', 'Übriger Verbrauch'], ['wallbox', 'Wallbox'],
    ['heat_pump', 'Wärmepumpe'], ['battery_charge', 'Batterie laden'], ['battery_discharge', 'Batterie entladen'],
    ['gap_applied', 'Bilanzlücke'], ['autarkie', 'Autarkiegrad'], ['eigenverbrauch', 'Eigenverbrauchsquote'],
    ['battery_soc', 'Batterie-Füllstand'], ['battery_capacity_kwh', 'Speicher-Kapazität'],
    ['battery_energy_kwh', 'Gespeicherte Energie'],
  ];

  const RESULT_LABELS = {
    fired: 'Ausgelöst',
    conditions_not_met: 'Bedingungen nicht erfüllt',
    hold_pending: 'Wartet auf Haltedauer',
    cooldown: 'Sperrzeit',
    settling: 'Beruhigungsphase',
    balance_stale: 'Bilanz veraltet',
    blocked: 'Blockiert',
    disabled: 'Deaktiviert',
    error: 'Fehler',
  };

  const emptyDocument = () => ({
    version: 1,
    settings: { tick_interval_s: 10, settling_seconds: 60, balance_max_age_s: 30,
                history_limit: 10, history_persist: true,
                publish_allowed_prefixes: [], publish_allowed_prefixes_auto: false },
    rules: [],
  });

  const automationsPanel = () => ({
    loading: false,
    saving: false,
    document: emptyDocument(),
    entities: [],
    readableEntities: [],
    devices: [],
    runtimeState: null,
    online: false,
    csrfToken: '',
    balanceFields: BALANCE_FIELDS,
    expanded: {},
    liveHistory: {},
    historyExpanded: {},
    editing: {},
    savedDocument: null,
    lastStateAt: 0,
    hasAutomationsRole: false,
    nowTick: Date.now(),

    // Der Assistent fuehrt nur durch eine frisch angelegte Regel: Schritt 1
    // WENN, Schritt 2 DANN, Schritt 3 Feineinstellungen. Er legt kein eigenes
    // Datenmodell an - er blendet nur, was schon da ist, schrittweise ein.
    wizard: { active: false, step: 1, ruleId: '' },

    get publishAllowedPrefixesText() {
      return (this.document.settings.publish_allowed_prefixes || []).join('\n');
    },
    set publishAllowedPrefixesText(value) {
      this.document.settings.publish_allowed_prefixes = value.split('\n').map((line) => line.trim()).filter(Boolean);
    },

    async load() {
      this.loading = true;
      try {
        const [doc, session] = await Promise.all([
          requestJSON('/api/v1/configurations/automation_rules'),
          requestJSON('/api/v1/auth/session').catch(() => null),
          this.loadDeviceCatalog(),
        ]);
        this.document = doc && doc.rules ? doc : emptyDocument();
        // Ein auf der Platte gespeichertes Dokument kann aelter sein als ein
        // seither neu hinzugekommenes settings-Feld (z.B. history_limit/
        // history_persist) - ohne diesen Merge wuerde ein fehlendes Feld im
        // Formular als "aus"/leer erscheinen, obwohl der Python-Dienst dafuer
        // seinen eigenen, oft anderen Default anwendet (Dataclass-Default ist
        // die echte Semantik von "nicht gesetzt", siehe AGENTS.md).
        this.document.settings = { ...emptyDocument().settings, ...this.document.settings };
        if (this.document.settings.publish_allowed_prefixes_auto) {
          // Der zuletzt gespeicherte Stand enthaelt die vom Dashboard
          // errechneten Topics - angezeigt wird trotzdem ein leeres Feld,
          // sonst wirkt der Default wie eine manuelle Eingabe.
          this.document.settings.publish_allowed_prefixes = [];
        }
        this.savedDocument = JSON.parse(JSON.stringify(this.document));
        this.csrfToken = (session && session.csrf_token) || '';
        // "automations" liefert der Sitzungsendpunkt seit Task 3; ohne die
        // Rolle werden die Test-Knoepfe gar nicht erst angeboten.
        this.hasAutomationsRole = Boolean(session && session.automations);
        this.loadRuntimeState();
        // dashboard.js verteilt jedes Registry-Update (SSE, mit
        // 30-Sekunden-Fallback) als 'registry-updated' auf window.
        if (!this._registryListener) {
          // Dieselbe Bremse, die publishRegistryUpdate und notifications.js
          // schon haben: ein Hintergrund-Tab braucht keinen Tick.
          this._registryListener = () => {
            if (document.visibilityState !== 'visible') return;
            this.refreshFromRegistry();
          };
          window.addEventListener('registry-updated', this._registryListener);
        }
        // "erfüllt seit" wird rein aus dem "since"-Zeitstempel und der
        // aktuellen Zeit berechnet - dafuer braucht es keine neuen Daten vom
        // Dienst, nur einen reaktiven Sekundentakt im Browser.
        if (!this._clockTimer) {
          this._clockTimer = setInterval(() => { this.nowTick = Date.now(); }, 1000);
          // Node hat hier ein Timeout-Objekt mit unref() (haelt den Prozess
          // z.B. in Tests nicht am Leben); im Browser ist es nur eine Zahl.
          if (typeof this._clockTimer.unref === 'function') this._clockTimer.unref();
        }
      } catch (error) {
        this.$store.toasts.push(error.message, 'critical');
      } finally {
        this.loading = false;
      }
    },

    flattenCommandableEntities(devices) {
      const result = [];
      for (const device of devices || []) {
        for (const entity of device.entities || []) {
          if (entity.command_topic) result.push({ ...entity, deviceName: device.name });
        }
      }
      return result;
    },

    commandableEntityGroups() {
      const groups = new Map();
      for (const entity of this.entities || []) {
        const name = entity.deviceName || 'Ohne Gerät';
        if (!groups.has(name)) groups.set(name, []);
        groups.get(name).push(entity);
      }
      return Array.from(groups.entries());
    },

    // Beim Speichern mit leerem Praefix-Feld eingesetzt (siehe save()): die
    // exakten command_topics aller bekannten steuerbaren Entitaeten, nicht
    // ein geratebasierter Wildcard-Praefix - neue Entitaeten an bekannten
    // Geraeten brauchen dafuer ein erneutes Speichern, genau wie neue Geraete.
    computeAutoPublishPrefixes() {
      const topics = new Set((this.entities || []).map((entity) => entity.command_topic).filter(Boolean));
      return Array.from(topics).sort();
    },

    flattenReadableEntities(devices) {
      const result = [];
      for (const device of devices || []) {
        for (const entity of device.entities || []) {
          // template_supported kommt aus Go - ob der Dienst den Wert lesen
          // kann, wird dort und nur dort entschieden.
          if (entity.state_topic && entity.template_supported) {
            result.push({ ...entity, deviceName: device.name });
          }
        }
      }
      return result;
    },

    entityForCondition(condition) {
      if (!condition || !condition.entity_id) return null;
      return (this.readableEntities || []).find((entity) => entity.unique_id === condition.entity_id) || null;
    },

    readableEntityGroups() {
      const groups = new Map();
      for (const entity of this.readableEntities || []) {
        const name = entity.deviceName || 'Ohne Gerät';
        if (!groups.has(name)) groups.set(name, []);
        groups.get(name).push(entity);
      }
      return Array.from(groups.entries());
    },

    computeConditionDrift(condition, entities) {
      if (!condition || !condition.entity_id) return { drifted: false, missing: false, currentTopic: null, currentTemplate: null };
      const entity = (entities || []).find((candidate) => candidate.unique_id === condition.entity_id);
      if (!entity) return { drifted: false, missing: true, currentTopic: null, currentTemplate: null };
      const currentTemplate = entity.value_template || '';
      const drifted = entity.state_topic !== condition.topic || currentTemplate !== (condition.value_template || '');
      return { drifted, missing: false, currentTopic: entity.state_topic, currentTemplate };
    },

    adoptConditionEntity(condition) {
      const entity = this.entityForCondition(condition);
      if (!entity) return;
      condition.topic = entity.state_topic;
      condition.value_template = entity.value_template || '';
    },

    addEntityValueCondition(rule) {
      rule.conditions.push({ type: 'entity_value', entity_id: '', topic: '', value_template: '',
                             comparison: 'below', value: 20, hold_seconds: 60 });
    },

    onConditionEntityChange(condition) {
      this.adoptConditionEntity(condition);
    },

    entityByUniqueId(uniqueId) {
      return (this.entities || []).find((entity) => entity.unique_id === uniqueId) || null;
    },

    automationDevice() {
      return (this.devices || []).find((device) => device.id === 'automation') || null;
    },

    // Der Geraetekatalog speist die Auswahlfelder und die Auto-Praefixe. Er
    // ist rein strukturell - command_topic, state_topic, template_supported,
    // unique_id, name - und aendert sich nur bei Discovery-Aenderungen, nie
    // im Sekundentakt. Deshalb wird er genau zweimal geholt: beim Oeffnen des
    // Tabs und vor dem Speichern. Bewusst getragene Einschraenkung: ein
    // waehrend geoeffnetem Tab neu entdecktes Geraet erscheint erst nach
    // einem Neuladen in den Dropdowns. Das nicht wieder mit einem Vollabruf
    // pro Tick zu "reparieren" ist der ganze Punkt - der saubere Ersatz ist
    // ?view=index aus dem zweiten Serverlast-Spec.
    async loadDeviceCatalog() {
      this.devices = (await requestJSON('/api/v1/devices')) || [];
      this.entities = this.flattenCommandableEntities(this.devices);
      this.readableEntities = this.flattenReadableEntities(this.devices);
    },

    loadRuntimeState() {
      this.applyRuntimeStateFrom(this.automationDevice());
    },

    // Nimmt das Geraet als Argument statt es aus this.devices zu suchen -
    // damit bedient dieselbe Funktion beide Wege: Erstladen aus der Liste,
    // Tick aus der Einzelroute.
    applyRuntimeStateFrom(automationDevice) {
      if (!automationDevice) {
        this.online = false;
        return;
      }
      const stateEntity = (automationDevice.entities || []).find((entity) => entity.object_id === 'state');
      // Die MQTT-Availability des "state"-Sensors ist zugleich das
      // online/offline-Signal des Dienstes - eine eigene "online"-Entity gibt
      // es im Register nicht.
      // entity.value ist der value_template-reduzierte Wert (rules_enabled) -
      // das Go-Registry kennt kein json_attributes_topic, deshalb steckt das
      // volle Zustandsdokument nur im rohen, ungetemplateten Payload des
      // state-Topics (last_message.payload).
      const rawDocument = stateEntity && stateEntity.last_message && stateEntity.last_message.payload;
      this.applyStateDocument(rawDocument, Boolean(stateEntity && stateEntity.available));
      const historyEntity = (automationDevice.entities || []).find((entity) => entity.object_id === 'history');
      const rawHistory = historyEntity && historyEntity.last_message && historyEntity.last_message.payload;
      this.applyHistoryDocument(rawHistory);
    },

    // Pro SSE-Tick wird genau ein Geraet geholt (10,9 KB statt 448 KB): der
    // Tick veraendert ausschliesslich den Laufzeitzustand der Automation.
    // Die Auswahlfelder haengen an loadDeviceCatalog() und bleiben stehen.
    async refreshFromRegistry() {
      try {
        const device = await requestJSON('/api/v1/devices/automation');
        this.applyRuntimeStateFrom(device);
      } catch (error) {
        // 404 = diese Instanz hat keinen Automations-Dienst; das ist derselbe
        // Zustand, den loadRuntimeState() aus einer Liste ohne das Geraet
        // ableitet. Alles andere ist voruebergehend - der naechste Tick
        // versucht es erneut und darf die Ansicht nicht leeren.
        if (error.status === 404) this.applyRuntimeStateFrom(null);
      }
    },

    applyStateDocument(raw, online) {
      this.online = Boolean(online);
      if (!raw) return;
      try {
        this.runtimeState = JSON.parse(raw);
        this.lastStateAt = Date.now();
      } catch (error) {
        // Ein halb geschriebenes State-Dokument darf die Ansicht nicht leeren -
        // der alte Stand bleibt stehen, bis der naechste Tick kommt.
      }
    },

    applyHistoryDocument(raw) {
      if (!raw) return;
      try {
        const parsed = JSON.parse(raw);
        this.liveHistory = parsed.rules || {};
      } catch (error) {
        // ein halb geschriebenes Dokument darf die Ansicht nicht leeren
      }
    },

    ruleState(rule) {
      const rules = this.runtimeState && this.runtimeState.rules;
      return (rules && rules[rule.id]) || null;
    },

    conditionReport(rule, index) {
      const state = this.ruleState(rule);
      return (state && state.conditions && state.conditions[index]) || null;
    },

    conditionState(rule, index) {
      return window.__automationsView.conditionState(this.conditionReport(rule, index));
    },

    describeCondition(condition) {
      return window.__automationsView.describeCondition(condition, this.entityForCondition(condition));
    },

    describeAction(action) {
      return window.__automationsView.describeAction(action, this.entityByUniqueId(action.entity_id));
    },

    gateVerdict(rule) {
      return window.__automationsView.gateVerdict(this.ruleState(rule), this.online);
    },

    formatSeconds(seconds) {
      return window.__automationsView.formatSeconds(seconds);
    },

    conditionValueText(rule, index) {
      const report = this.conditionReport(rule, index);
      if (!report || report.value === null || report.value === undefined) return '—';
      return String(report.value);
    },

    conditionStateText(rule, index) {
      const report = this.conditionReport(rule, index);
      const state = this.conditionState(rule, index);
      if (state === 'novalue') return 'kein Wert — Topic unbekannt oder Bilanz veraltet';
      if (state === 'unmet') return 'Schwelle nicht erreicht';
      const since = report.since ? ` seit ${this.formatSeconds((this.nowTick / 1000) - report.since)}` : '';
      if (state === 'pending') return `erfüllt${since} — noch ${this.formatSeconds(report.hold_remaining)} Haltedauer`;
      return `erfüllt${since}`;
    },

    // Liefert eine Liste von Textbausteinen, keine HTML-Zeichenkette - das
    // Template rendert sie ueber x-for/x-text, damit nichts ungeprueft als
    // HTML in die Seite gelangt.
    gateMeta(rule) {
      const state = this.ruleState(rule);
      if (!state) return [];
      const parts = [`Sperrzeit ${this.formatSeconds(state.cooldown_remaining)} von ${this.formatSeconds(rule.cooldown_seconds)}`];
      if (state.fired_at) parts.push(`zuletzt ausgelöst ${new Date(state.fired_at * 1000).toLocaleTimeString('de-DE')}`);
      parts.push(`bisher ${state.fire_count || 0}×`);
      return parts;
    },

    meterStyle(rule, index) {
      const view = window.__automationsView;
      const condition = rule.conditions[index];
      const report = this.conditionReport(rule, index);
      const scale = view.meterScale(condition, report, this.entityForCondition(condition));
      const value = report && typeof report.value === 'number' ? report.value : null;
      return { width: `${(view.meterFraction(value, scale) * 100).toFixed(1)}%` };
    },

    markStyle(rule, index) {
      const view = window.__automationsView;
      const condition = rule.conditions[index];
      const report = this.conditionReport(rule, index);
      const scale = view.meterScale(condition, report, this.entityForCondition(condition));
      const target = report && typeof report.target === 'number' ? report.target : null;
      if (target === null) return { display: 'none' };
      return { left: `${(view.meterFraction(target, scale) * 100).toFixed(1)}%` };
    },

    actionPreview(rule, index) {
      const state = this.ruleState(rule);
      const preview = state && state.actions && state.actions[index];
      if (!preview) return { blocked: false, text: 'noch keine Vorschau' };
      if (preview.blocked) return { blocked: true, text: preview.reason || 'blockiert' };
      if (rule.actions[index] && rule.actions[index].type === 'notification') {
        return { blocked: false, text: `„${preview.title || ''}" — ${preview.message || ''}` };
      }
      return { blocked: false, text: `${preview.topic} → ${preview.payload}` };
    },

    renderMiniChain(rule) {
      return {
        conditions: rule.conditions.map((condition, index) => ({
          icon: this.describeCondition(condition).icon,
          state: this.conditionState(rule, index),
        })),
        actions: rule.actions.map((action) => ({ icon: this.describeAction(action).icon })),
        gate: this.gateVerdict(rule),
      };
    },

    isDirty(rule) {
      if (!this.savedDocument) return false;
      const saved = (this.savedDocument.rules || []).find((entry) => entry.id === rule.id);
      if (!saved) return true;
      return JSON.stringify(saved) !== JSON.stringify(rule);
    },

    startEditing(rule) { this.editing[rule.id] = true; this.expanded[rule.id] = true; },
    stopEditing(rule) { this.editing[rule.id] = false; },
    toggleExpanded(rule) { this.expanded[rule.id] = !this.expanded[rule.id]; },

    testState(rule, index) {
      const preview = this.actionPreview(rule, index);
      const base = window.__automationsView.canTest({
        online: this.online, dirty: this.isDirty(rule), hasRole: this.hasAutomationsRole,
      });
      if (!base.allowed) return base;
      if (preview.blocked) {
        return { allowed: true, reason: 'Die Aktion ist blockiert — der Test zeigt nur die Begründung.' };
      }
      return base;
    },

    confirmTest(rule, index) {
      const action = rule.actions[index];
      if (!action || action.type !== 'publish') return { required: false, question: '' };
      const preview = this.actionPreview(rule, index);
      return {
        required: true,
        question: `Diese Aktion wird jetzt wirklich ausgeführt:\n\n${preview.text}\n\nFortfahren?`,
      };
    },

    async awaitTestResult(ruleId, actionIndex, sentAt, options = {}) {
      const timeoutMs = options.timeoutMs || SERVICE_TEST_TIMEOUT_MS;
      const pollMs = options.pollMs || SERVICE_TEST_POLL_MS;
      const deadline = Date.now() + timeoutMs;
      // Das Ergebnis-Topic ist retained. Ein Ergebnis zaehlt nur, wenn es
      // juenger ist als die eigene Anfrage - sonst wuerde das letzte Ergebnis
      // vom Vortag als Antwort durchgehen.
      // Wie bei "state" (siehe loadRuntimeState) hat test_result einen
      // value_template ({{ value_json.status }}); das Go-Registry rendert
      // damit .value auf den blossen Status-String (z.B. "published") -
      // kein gueltiges JSON. Das volle Dokument mit rule_id/action_index/at
      // steckt nur im rohen last_message.payload.
      const sentAtSeconds = sentAt / 1000;
      while (Date.now() < deadline) {
        const automationDevice = this.automationDevice();
        const entity = automationDevice
          && (automationDevice.entities || []).find((candidate) => candidate.object_id === 'test_result');
        const rawDocument = entity && entity.last_message && entity.last_message.payload;
        if (rawDocument) {
          try {
            const result = JSON.parse(rawDocument);
            if (result.rule_id === ruleId && result.action_index === actionIndex && result.at >= sentAtSeconds) {
              return result;
            }
          } catch (error) {
            // unvollstaendiges Dokument - beim naechsten Durchlauf erneut versuchen
          }
        }
        await new Promise((resolve) => setTimeout(resolve, pollMs));
        await this.refreshFromRegistry();
      }
      return null;
    },

    async testAction(rule, index, options = {}) {
      const permission = this.testState(rule, index);
      if (!permission.allowed) return;
      if (!options.skipConfirm) {
        const question = this.confirmTest(rule, index);
        if (question.required) {
          const confirmed = await this.$store.modal.confirm({
            title: question.question,
            confirmLabel: 'Test auslösen',
          });
          if (!confirmed) return;
        }
      }
      const sentAt = Date.now();
      try {
        await requestJSON('/api/v1/automations/test', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': this.csrfToken },
          body: JSON.stringify({ rule_id: rule.id, action_index: index }),
        });
      } catch (error) {
        this.notify(`Test fehlgeschlagen: ${error.message}`, 'critical');
        return;
      }
      const result = await this.awaitTestResult(rule.id, index, sentAt, options);
      if (!result) {
        this.notify('Keine Rückmeldung vom Automations-Dienst.', 'warning');
        return;
      }
      if (result.status === 'published') {
        this.notify(`Test ausgeführt: ${result.topic} → ${result.payload}`, 'info');
      } else if (result.status === 'blocked') {
        this.notify(`Test blockiert: ${result.reason}`, 'warning');
      } else {
        this.notify(`Test fehlgeschlagen: ${result.reason}`, 'critical');
      }
    },

    notify(message, severity) {
      this.$store.toasts.push(message, severity);
    },

    addRule() {
      const id = `regel_${Date.now()}`;
      this.document.rules.push({ id, name: 'Neue Regel', enabled: false, cooldown_seconds: 60, conditions: [], actions: [] });
    },

    startRuleWizard() {
      this.addRule();
      const rule = this.document.rules[this.document.rules.length - 1];
      this.wizard = { active: true, step: 1, ruleId: rule.id };
      this.expanded[rule.id] = true;
      this.editing[rule.id] = true;
    },

    wizardRule() {
      return this.document.rules.find((rule) => rule.id === this.wizard.ruleId) || null;
    },

    // Schritt 1 braucht eine Bedingung, Schritt 2 eine Aktion - genau die
    // Bedingung, an der validateRuleBeforeSave() sonst erst beim Speichern
    // scheitert. Schritt 3 hat nichts zu erzwingen.
    wizardCanAdvance() {
      const rule = this.wizardRule();
      if (!rule) return false;
      if (this.wizard.step === 1) return rule.conditions.length > 0;
      if (this.wizard.step === 2) return rule.actions.length > 0;
      return true;
    },

    wizardNext() {
      if (!this.wizardCanAdvance()) return;
      if (this.wizard.step >= 3) return this.wizardFinish();
      this.wizard.step += 1;
    },

    wizardBack() {
      if (this.wizard.step > 1) this.wizard.step -= 1;
    },

    // Beendet nur die Fuehrung - die Regel bleibt im Bearbeiten-Modus offen,
    // damit man ohne Bruch weitermachen kann. Gespeichert wird wie immer
    // ausdruecklich ueber die Werkzeugleiste.
    wizardFinish() {
      const ruleId = this.wizard.ruleId;
      this.wizard = { active: false, step: 1, ruleId: '' };
      if (ruleId) {
        this.editing[ruleId] = true;
        this.expanded[ruleId] = true;
      }
    },

    isWizardRule(rule) {
      return this.wizard.active && this.wizard.ruleId === rule.id;
    },

    // Welche Abschnitte der Regelkarte der Assistent gerade zeigt.
    wizardShows(rule, section) {
      if (!this.isWizardRule(rule)) return true;
      if (section === 'conditions') return this.wizard.step >= 1;
      if (section === 'actions') return this.wizard.step >= 2;
      if (section === 'tuning') return this.wizard.step >= 3;
      return true;
    },

    // Der Loeschknopf traegt seit dem Umbau nur noch ein Icon - ein
    // Fehlgriff ist dadurch leichter, also fragt er nach. Endgueltig weg ist
    // die Regel trotzdem erst mit dem naechsten Speichern.
    async removeRule(ruleId) {
      const rule = this.document.rules.find((entry) => entry.id === ruleId);
      const confirmed = await this.$store.modal.confirm({
        title: `Regel „${(rule && rule.name) || ruleId}" löschen?`,
        body: 'Die Regel verschwindet aus der Liste und ist mit dem nächsten Speichern endgültig weg.',
        confirmLabel: 'Löschen',
        danger: true,
      });
      if (!confirmed) return;
      this.document.rules = this.document.rules.filter((entry) => entry.id !== ruleId);
    },

    toggleEnabled(ruleId) {
      const rule = this.document.rules.find((entry) => entry.id === ruleId);
      if (rule) rule.enabled = !rule.enabled;
    },

    conditionTypes() { return CONDITION_TYPES; },
    actionTypes() { return ACTION_TYPES; },
    fieldHelp(key) { return FIELD_HELP[key] || ''; },

    addBalanceCondition(rule) {
      rule.conditions.push({ type: 'balance_threshold', field: 'grid_export', comparison: 'above', threshold: 800, hysteresis: 100, hold_seconds: 300 });
    },

    addTimeWindowCondition(rule) {
      rule.conditions.push({ type: 'time_window', start: '08:00', end: '18:00', weekdays: [] });
    },

    // Ein Dispatcher statt drei Knoepfen im Template: die Kacheln kommen aus
    // CONDITION_TYPES, also muss auch das Anlegen ueber denselben Schluessel
    // gehen. Unbekannter Schluessel legt bewusst nichts an.
    addCondition(rule, key) {
      if (key === 'balance') return this.addBalanceCondition(rule);
      if (key === 'entity') return this.addEntityValueCondition(rule);
      if (key === 'time') return this.addTimeWindowCondition(rule);
    },

    // scope ist der x-data-Bereich der Regelzeile; "entity" legt keine Aktion
    // an, sondern klappt dort den Geraete-Assistenten auf.
    addAction(rule, key, scope) {
      if (key === 'notify') return void rule.actions.push(this.buildNotificationAction());
      if (key === 'mqtt') return void rule.actions.push(this.buildMqttAction());
      if (key === 'entity' && scope) scope.showEntityWizard = true;
    },

    removeCondition(rule, index) {
      rule.conditions.splice(index, 1);
    },

    buildSwitchAction(entity, targetState) {
      return {
        type: 'publish', topic: entity.command_topic, retain: false, payload_source: 'constant',
        payload: targetState === 'on' ? entity.payload_on : entity.payload_off,
        entity_id: entity.unique_id,
      };
    },

    buildSetpointAction(entity, source) {
      const action = {
        type: 'publish', topic: entity.command_topic, retain: false,
        payload_source: source.mode, entity_id: entity.unique_id,
        min: entity.min_value, max: entity.max_value, step: entity.step,
      };
      if (source.mode === 'balance') {
        action.field = source.field;
        action.scale = source.scale ?? 1;
        action.offset = source.offset ?? 0;
      } else {
        action.payload = String(source.value ?? '');
      }
      return action;
    },

    buildToggleAction(entity) {
      return {
        type: 'publish', topic: entity.command_topic, retain: false, payload_source: 'toggle',
        source_topic: entity.state_topic, value_template: entity.value_template || '',
        payload_on: entity.payload_on, payload_off: entity.payload_off, entity_id: entity.unique_id,
      };
    },

    buildMqttAction() {
      return { type: 'publish', topic: '', retain: false, payload_source: 'constant', payload: '' };
    },

    buildNotificationAction() {
      return { type: 'notification', severity: 'info', title: '', message: '' };
    },

    removeAction(rule, index) {
      rule.actions.splice(index, 1);
    },

    computeDrift(action, entities) {
      if (!action.entity_id) return { drifted: false, currentTopic: null };
      const entity = (entities || []).find((candidate) => candidate.unique_id === action.entity_id);
      if (!entity || entity.command_topic === action.topic) return { drifted: false, currentTopic: entity ? entity.command_topic : null };
      return { drifted: true, currentTopic: entity.command_topic };
    },

    badgeLabel(result) {
      return RESULT_LABELS[result] || result;
    },

    validateRuleBeforeSave(rule) {
      const errors = [];
      if (!/^[a-z0-9][a-z0-9_-]{0,63}$/.test(rule.id || '')) errors.push(`Regel ${rule.name}: ungültige ID`);
      if (!rule.conditions || rule.conditions.length < 1 || rule.conditions.length > 8) errors.push(`Regel ${rule.name}: 1-8 Bedingungen nötig`);
      if (!rule.actions || rule.actions.length < 1 || rule.actions.length > 8) errors.push(`Regel ${rule.name}: 1-8 Aktionen nötig`);
      const hasPublish = (rule.actions || []).some((action) => action.type === 'publish');
      if (hasPublish) {
        if ((rule.cooldown_seconds || 0) < 30) errors.push(`Regel ${rule.name}: cooldown_seconds muss >= 30 sein (Regel mit Publish-Aktion)`);
        for (const condition of rule.conditions || []) {
          if ('hold_seconds' in condition && condition.hold_seconds < 30) {
            errors.push(`Regel ${rule.name}: hold_seconds muss >= 30 sein (Regel mit Publish-Aktion)`);
          }
        }
      }
      return errors;
    },

    validateDocumentBeforeSave() {
      return (this.document.rules || []).flatMap((rule) => this.validateRuleBeforeSave(rule));
    },

    async save() {
      const errors = this.validateDocumentBeforeSave();
      if (errors.length > 0) {
        // Validierungsfehler vor dem Speichern - bleibt als critical stehen,
        // waehrend der Nutzer korrigiert.
        this.$store.toasts.push(errors.join(' / '), 'critical');
        return;
      }
      this.saving = true;
      try {
        // Ein leeres Praefix-Feld bleibt in der Anzeige leer (kein manuell
        // wirkender Auto-Default), aber gespeichert wird die konkrete Liste
        // aller aktuell bekannten command_topics - siehe
        // computeAutoPublishPrefixes(). this.document selbst bleibt dabei
        // unveraendert, sonst wuerde das Feld nach dem Speichern ploetzlich
        // befuellt angezeigt.
        // Die Auto-Praefixe entstehen aus this.entities, und die wird seit A6
        // nicht mehr pro Tick aktualisiert. Ein veralteter Praefix-Satz waere
        // fail-closed und damit ein fachlicher Fehler (der Dienst darf dann
        // nicht mehr publizieren), kein Darstellungsproblem - deshalb hier
        // einmal frisch holen. Ein Abruf pro Speichervorgang ist zu
        // verschmerzen; das Speichern ist selten und interaktiv ausgeloest.
        await this.loadDeviceCatalog();
        const manualPrefixes = this.document.settings.publish_allowed_prefixes || [];
        const settingsToSave = manualPrefixes.length > 0
          ? { ...this.document.settings, publish_allowed_prefixes_auto: false }
          : { ...this.document.settings, publish_allowed_prefixes: this.computeAutoPublishPrefixes(),
              publish_allowed_prefixes_auto: true };
        await requestJSON('/api/v1/configurations/automation_rules', {
          method: 'PUT',
          headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': this.csrfToken },
          body: JSON.stringify({ ...this.document, settings: settingsToSave }),
        });
        this.$store.toasts.push('Gespeichert.');
        this.savedDocument = JSON.parse(JSON.stringify(this.document));
        await this.pollRuntimeStatus();
      } catch (error) {
        this.$store.toasts.push(error.message, 'critical');
      } finally {
        this.saving = false;
      }
    },

    async pollRuntimeStatus() {
      const deadline = Date.now() + 10000;
      while (Date.now() < deadline) {
        try {
          const device = await requestJSON('/api/v1/devices/automation');
          const statusEntity = (device.entities || []).find((entity) => entity.object_id === 'status');
          if (statusEntity && statusEntity.value) {
            const status = JSON.parse(statusEntity.value);
            if (status.runtime_status === 'rejected') {
              this.$store.toasts.push(`abgelehnt: ${status.error}`, 'critical');
              return;
            }
            if (status.runtime_status === 'ok') {
              this.$store.toasts.push('übernommen');
              return;
            }
          }
        } catch (error) {
          // keep polling until the deadline; a transient fetch error here shouldn't abort the feedback loop
        }
        await new Promise((resolve) => setTimeout(resolve, 1000));
      }
    },

    toggleHistory(rule) {
      this.historyExpanded[rule.id] = !this.historyExpanded[rule.id];
    },

    historyEntries(rule) {
      return this.liveHistory[rule.id] || [];
    },

    historyResultInfo(result) {
      return window.__automationsView.historyResultInfo(result);
    },

    historyActionText(action) {
      return window.__automationsView.historyActionText(action);
    },

    historyTimestamp(event) {
      return new Date(event.at * 1000).toLocaleString('de-DE');
    },
  });

  window.Alpine.data('automationsPanel', automationsPanel);
})();
