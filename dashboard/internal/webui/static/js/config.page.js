(() => {
  const requestJSON = async (url, options) => {
    // The single chokepoint for every URL literal in this file: behind a
    // reverse-proxy subpath base.html puts the prefix into
    // __DASHBOARD_BASE_PATH__; on direct access it is empty.
    const response = await fetch(`${window.__DASHBOARD_BASE_PATH__ || ''}${url}`, options);
    const body = await response.json();
    if (!response.ok) throw new Error(body.message || "Anfrage fehlgeschlagen");
    return body;
  };

  // Motion nur, wenn das System sie zulaesst - und nie in Testumgebungen ohne
  // matchMedia. Deckt die Array-Eintrags-Animationen ab; die reinen CSS-
  // Uebergaenge tragen ihre eigene prefers-reduced-motion-Regel in manager.css.
  const motionOK = () =>
    typeof window !== 'undefined' &&
    typeof window.matchMedia === 'function' &&
    !window.matchMedia('(prefers-reduced-motion: reduce)').matches;

  const configPanel = () => ({
    configs: [],
    selectedName: new URLSearchParams(window.location.search).get('config') || '',
    schema: null,
    value: null,
    topics: [],
    topicSamples: new Map(),
    datalistSeq: 0,
    editorText: '',
    loading: false,
    saving: false,
    reloading: false,
    shellyPresets: [],
    presetsError: '',
    reloadFailed: false,
    reloadError: '',
    // Beruehrt-Marke statt Wertevergleich: readNode() laeuft ueber den
    // ganzen Formularbaum und darf nicht an jedem Tastendruck haengen.
    // Falsch-positiv ("getippt und wieder geloescht") ist die harmlose
    // Richtung - die Leiste behauptet nie faelschlich "gespeichert".
    formDirty: false,
    actionsCompact: false,
    actionsFloating: false,

    get configCount() {
      return `${this.configs.length} verwaltete Konfiguration(en)`;
    },

    // Der Rohtext-Editor traegt seinen eigenen Speichern-Button, deshalb
    // reicht hier der ehrliche Textvergleich gegen den geladenen Stand.
    get editorDirty() {
      if (this.value === null || this.value === undefined) return false;
      return this.editorText !== JSON.stringify(this.value, null, 2);
    },

    get selectedLabel() {
      const selected = this.configs.find(config => config.name === this.selectedName);
      return (selected && selected.label) || this.selectedName;
    },

    // Speichern und Dienst-Neuladen sind serverseitig ein Vorgang: Save()
    // schreibt die Datei und ruft direkt danach reload() (internal/config/
    // config.go). Die Frage steht deshalb VOR dem PUT - danach waere sie eine
    // Frage nach etwas, das schon geschehen ist.
    async confirmSave() {
      return this.$store.modal.confirm({
        title: 'Speichern und Dienst neu laden?',
        body: `„${this.selectedLabel}“ wird überschrieben. Der zugehörige Dienst übernimmt die neue Konfiguration sofort und baut seine Verbindungen neu auf.`,
        confirmLabel: 'Speichern',
        cancelLabel: 'Abbrechen',
      });
    },

    get actionStatusText() {
      if (this.loading) return 'Wird geladen ...';
      if (this.saving) return 'Speichert ...';
      if (this.formDirty) return 'Ungespeicherte Änderungen';
      if (this.editorDirty) return 'JSON-Text geändert';
      return 'Alles gespeichert';
    },

    // Die Leiste klebt unten und schrumpft beim Scrollen auf Punkt plus
    // Speichern-Button zusammen; ein Klick auf die Leiste (oder ein Fokus
    // per Tastatur) klappt sie wieder auf. Angedockt am Seitenende gilt
    // .is-compact nicht mehr - siehe manager.css.
    initActionBar() {
      this.$nextTick(() => {
        let pending = false;
        window.addEventListener('scroll', () => {
          if (pending) return;
          pending = true;
          window.requestAnimationFrame(() => {
            pending = false;
            if (this.actionsFloating) this.actionsCompact = true;
          });
        }, {passive: true});
        const sentinel = this.$refs.actionSentinel;
        if (!sentinel || typeof window.IntersectionObserver !== 'function') {
          this.actionsFloating = true;
          return;
        }
        new window.IntersectionObserver(entries => {
          this.actionsFloating = !entries[entries.length - 1].isIntersecting;
        }).observe(sentinel);
      });
    },

    expandActions() {
      this.actionsCompact = false;
    },

    async load() {
      this.loading = true;
      try {
        // Payload samples are a convenience only - an older server without the
        // endpoint (or a transient failure) must not block the whole form.
        const [topics, samples, configs] = await Promise.all([
          requestJSON('/api/v1/topics'),
          requestJSON('/api/v1/topics/samples').catch(() => []),
          requestJSON('/api/v1/configurations'),
        ]);
        this.topics = topics;
        this.topicSamples = new Map((samples || []).map(sample => [sample.topic, sample]));
        this.configs = configs;
        if (!this.configs.some(config => config.name === this.selectedName)) {
          this.selectedName = this.configs[0]?.name || '';
        }
        // Der Fehlerzustand kommt jetzt aus der Liste mit und ueberlebt damit
        // ein Neuladen der Seite - nicht nur den unmittelbaren Speichervorgang.
        const selected = this.configs.find(config => config.name === this.selectedName);
        this.reloadFailed = Boolean(selected && selected.reload_failed);
        this.reloadError = (selected && selected.reload_error) || '';
        await this.loadConfig();
      } catch (error) {
        this.$store.toasts.push(error.message, 'critical');
      } finally {
        this.loading = false;
      }
    },

    async loadConfig() {
      if (!this.selectedName) return;
      this.loading = true;
      try {
        const name = encodeURIComponent(this.selectedName);
        [this.schema, this.value] = await Promise.all([
          requestJSON(`/api/v1/configurations/${name}/schema`),
          requestJSON(`/api/v1/configurations/${name}`),
        ]);
        this.shellyPresets = [];
        this.presetsError = '';
        if (this.selectedName === 'shelly_devices') {
          try {
            this.shellyPresets = await requestJSON('/api/v1/shelly/presets');
          } catch (error) {
            this.presetsError = error.message;
          }
        }
        this.editorText = JSON.stringify(this.value, null, 2);
        this.renderForm();
      } catch (error) {
        this.$store.toasts.push(error.message, 'critical');
      } finally {
        this.loading = false;
      }
    },

    shellyIdentityKeys() {
      return ['id', 'name', 'host', 'auth_user', 'auth_password'];
    },

    shellyTechnicalKeys(itemSchema) {
      const identityKeys = new Set(this.shellyIdentityKeys());
      return Object.keys(itemSchema.properties || {}).filter(key => !identityKeys.has(key));
    },

    shellyFieldValue(source, itemSchema, key) {
      if (source && source[key] !== undefined) return source[key];
      const propertySchema = itemSchema.properties && itemSchema.properties[key];
      return propertySchema ? propertySchema.default : undefined;
    },

    findMatchingShellyPreset(itemValue, itemSchema) {
      const technicalKeys = this.shellyTechnicalKeys(itemSchema);
      return (this.shellyPresets || []).find(preset => technicalKeys.every(key =>
        this.shellyFieldValue(itemValue, itemSchema, key) === this.shellyFieldValue(preset.properties, itemSchema, key)
      )) || null;
    },

    applyShellyPreset(itemElement, itemSchema, preset) {
      const schemaNode = itemElement.querySelector('.schema-node');
      const currentValue = this.readNode(schemaNode) || {};
      const merged = {};
      this.shellyIdentityKeys().forEach(identityKey => {
        if (currentValue[identityKey] !== undefined) merged[identityKey] = currentValue[identityKey];
      });
      Object.assign(merged, preset.properties || {});
      schemaNode.replaceWith(this.renderNode(itemSchema, merged, 'Eintrag'));
      this.formDirty = true;
      // warning statt info: das ist eine Handlungsanweisung, sie darf nicht
      // nach acht Sekunden verschwinden.
      this.$store.toasts.push(`Preset "${preset.name || preset.id}" übernommen. Zum Sichern „Speichern“ in der Leiste unten klicken.`, 'warning');
    },

    async reloadService() {
      if (!this.selectedName) return;
      this.reloading = true;
      try {
        const name = encodeURIComponent(this.selectedName);
        const response = await requestJSON(`/api/v1/configurations/${name}/reload`, {method: 'POST'});
        this.reloadFailed = Boolean(response.reload_failed);
        this.reloadError = response.reload_error || '';
        this.$store.toasts.push('Dienst-Konfiguration neu geladen.');
      } catch (error) {
        this.$store.toasts.push(error.message, 'critical');
      } finally {
        this.reloading = false;
      }
    },

    titleFor(schema, fallback) {
      return schema.title || schema.description || fallback;
    },

    isTopicField(schema, key) {
      return schema.type === 'string' && (schema.format === 'mqtt-topic' || key.endsWith('_topic'));
    },

    numericStep(schema) {
      return window.SchemaForm.numericStep(schema);
    },

    isJsonKeyField(schema, key) {
      return schema.type === 'string' && (schema.format === 'mqtt-json-key' || key.endsWith('_json_key'));
    },

    // A *_json_key field reads its value out of the payload of the sibling
    // *_topic field, so the suggestions have to follow that topic.
    topicFieldFor(key) {
      return key.endsWith('_json_key') ? `${key.slice(0, -'_json_key'.length)}_topic` : '';
    },

    hasTopicSample(topic) {
      const sample = this.topicSamples.get(topic);
      return Boolean(sample && sample.payload);
    },

    // Topics nach Geraet gruppieren: die flache Liste wird mit jedem weiteren
    // Geraet unbrauchbarer. Der Geraetename kommt aus /api/v1/topics/samples;
    // faellt der Aufruf aus (er ist bewusst fehlertolerant), bleibt genau eine
    // Gruppe uebrig und die Auswahl sieht aus wie vorher.
    groupedTopics() {
      const groups = new Map();
      this.topics.forEach(topic => {
        const sample = this.topicSamples.get(topic);
        const device = (sample && sample.device) || '';
        if (!groups.has(device)) groups.set(device, []);
        groups.get(device).push(topic);
      });
      const named = [...groups.entries()]
        .filter(([device]) => device)
        .sort((a, b) => a[0].localeCompare(b[0], 'de'));
      const unnamed = groups.get('') || [];
      if (unnamed.length) named.push(['Sonstige', unnamed]);
      return named;
    },

    appendGroupedTopicOptions(control, displayedValue) {
      const groups = this.groupedTopics();
      // Eine einzige Gruppe waere nur ein leeres Rahmenlabel - dann bleibt es
      // die flache Liste.
      const flat = groups.length <= 1;
      groups.forEach(([device, topics]) => {
        let target = control;
        if (!flat) {
          target = document.createElement('optgroup');
          target.label = device;
          control.append(target);
        }
        topics.forEach(topic => target.append(new Option(
          this.hasTopicSample(topic) ? topic : `${topic} (noch keine Daten)`,
          topic, false, topic === displayedValue)));
      });
    },

    shortPayload(text) {
      const trimmed = String(text).trim();
      return trimmed.length > 120 ? `${trimmed.slice(0, 120)}…` : trimmed;
    },

    // extract_value() in battery_soc_mqtt.py applies float() unconditionally,
    // and Shelly reports some numbers as strings - both count as numeric here.
    isNumericValue(value) {
      if (typeof value === 'number') return Number.isFinite(value);
      return typeof value === 'string' && value.trim() !== '' && Number.isFinite(Number(value));
    },

    // The generic render/read engine lives in schema-form.js; everything
    // config-specific (Topic pickers, *_json_key linking, Shelly presets,
    // battery voltage helpers) is fed in here as hooks.
    get schemaContext() {
      return {
        motionOK,
        hooks: {
          control: (args) => this.configControl(args),
          afterObject: (objectNode) => {
            this.linkJsonKeyFields(objectNode);
            this.linkBatteryVoltageMeasurement(objectNode);
          },
          arrayItemHeader: (header, args) => this.shellyItemHeader(header, args),
          onDirty: () => { this.formDirty = true; },
        },
      };
    },

    renderNode(schema, value, label, required = false, key = '') {
      return window.SchemaForm.renderNode(this.schemaContext, schema, value, label, required, key);
    },

    primitiveControl(schema, value, label, key, required) {
      return window.SchemaForm.primitiveControl(this.schemaContext, schema, value, label, key, required);
    },

    // Replaces the plain text/number/enum control for the two config-only
    // field kinds. Returns null for everything else so schema-form.js falls
    // back to its generic control.
    configControl({ schema, key, required, displayedValue, node }) {
      if (this.isTopicField(schema, key)) {
        const control = document.createElement('select');
        if (!required) control.add(new Option('', ''));
        this.appendGroupedTopicOptions(control, displayedValue);
        if (displayedValue && !this.topics.includes(displayedValue)) control.add(new Option(`${displayedValue} (aktuell)`, displayedValue, true, true));
        return control;
      }
      if (this.isJsonKeyField(schema, key)) {
        // A <datalist> suggests without restricting: the key may be absent
        // from the last payload and still be correct, so a <select> would
        // throw away a perfectly valid value.
        const control = document.createElement('input');
        control.type = 'text';
        const datalist = document.createElement('datalist');
        datalist.id = `schema-json-keys-${++this.datalistSeq}`;
        control.setAttribute('list', datalist.id);
        node.dataset.schemaJsonKeyFor = this.topicFieldFor(key);
        return { control, datalist };
      }
      return null;
    },

    // Adds the Shelly preset picker to an array-item header, but only for the
    // top-level shelly_devices array.
    shellyItemHeader(header, { itemSchema, itemValue, itemEl, key }) {
      if (!(key === '' && this.selectedName === 'shelly_devices')) return;
      const presetSelect = document.createElement('select');
      presetSelect.className = 'shelly-preset-select';
      presetSelect.setAttribute('aria-label', 'Shelly-Preset auswählen');
      presetSelect.disabled = this.shellyPresets.length === 0;
      presetSelect.add(new Option('Preset wählen…', ''));
      this.shellyPresets.forEach(preset => presetSelect.add(new Option(preset.name || preset.id, preset.id)));
      const matchingPreset = this.findMatchingShellyPreset(itemValue, itemSchema);
      if (matchingPreset) presetSelect.value = matchingPreset.id;
      const applyPreset = document.createElement('button');
      applyPreset.type = 'button';
      applyPreset.className = 'shelly-preset-apply';
      applyPreset.textContent = 'Preset übernehmen';
      applyPreset.setAttribute('aria-label', 'Ausgewähltes Shelly-Preset auf diesen Eintrag anwenden');
      applyPreset.addEventListener('click', () => {
        const preset = this.shellyPresets.find(candidate => candidate.id === presetSelect.value);
        if (preset) this.applyShellyPreset(itemEl, itemSchema, preset);
      });
      header.append(presetSelect, applyPreset);
    },

    // Runs after all children of an object exist, so the wiring does not
    // depend on whether the schema lists *_topic before or after *_json_key.
    linkJsonKeyFields(objectNode) {
      objectNode.querySelectorAll('[data-schema-json-key-for]').forEach(keyNode => {
        const topicNode = objectNode.querySelector(`[data-schema-key="${keyNode.dataset.schemaJsonKeyFor}"]`);
        const topicControl = topicNode?.querySelector('.schema-control');
        if (!topicControl) return;
        const apply = () => this.applyJsonKeySuggestions(keyNode, topicControl.value);
        topicControl.addEventListener('change', apply);
        keyNode.querySelector('.schema-control').addEventListener('input', apply);
        apply();
      });
    },

    // Der Messknopf gehoert nur an die Batterie-Konfiguration: nur dort gibt es
    // eine Zellzahl, mit der sich aus der Bankspannung eine Zellspannung
    // zurueckrechnen laesst. Sonderfall an einer Konfiguration bei generischem
    // Formular sonst - dasselbe Muster wie bei den Shelly-Presets.
    isBatteryConfig() {
      return this.selectedName === 'battery_soc_devices';
    },

    batteryLiveState(objectNode) {
      const idControl = objectNode.querySelector('[data-schema-key="id"] .schema-control');
      const id = idControl && idControl.value;
      if (!id) return null;
      const sample = this.topicSamples.get(`outstation/${id}/state`);
      if (!sample || !sample.payload) return null;
      try {
        const payload = JSON.parse(sample.payload);
        if (!payload || typeof payload !== 'object' || Array.isArray(payload)) return null;
        return {payload, at: sample.at};
      } catch (error) {
        return null;
      }
    },

    // Die Namen im State-Payload haengen von der Topologie ab (siehe
    // battery_soc_mqtt.py: BankState "pack" bei parallel, "bank_a"/"bank_b"
    // bei series) - dieselbe Unterscheidung wie der Python-Runtime muss hier
    // nachvollzogen werden, sonst passen die Payload-Keys nicht.
    batteryUnits(objectNode) {
      const topologyControl = objectNode.querySelector('[data-schema-key="topology"] .schema-control');
      const topology = (topologyControl && topologyControl.value) || 'parallel';
      if (topology === 'series') {
        return [
          {name: 'bank_a', label: 'Bank A', cellKey: 'bank_a_cell_count'},
          {name: 'bank_b', label: 'Bank B', cellKey: 'bank_b_cell_count'},
        ];
      }
      return [{name: 'pack', label: 'Bus', cellKey: 'bank_a_cell_count'}];
    },

    batteryCellVoltages(objectNode, unit) {
      const live = this.batteryLiveState(objectNode);
      if (!live) return null;
      // Die Zellzahl kommt aus dem FORMULAR, nicht aus der gespeicherten
      // Konfiguration - sonst rechnet der Knopf nach einer noch nicht
      // gespeicherten Aenderung mit der alten Zahl.
      const cellControl = objectNode.querySelector(`[data-schema-key="${unit.cellKey}"] .schema-control`);
      const cellCount = Number(cellControl && cellControl.value);
      const packVoltage = Number(live.payload[`${unit.name}_voltage_v`]);
      const corrected = Number(live.payload[`${unit.name}_corrected_v_per_cell`]);
      const rawUsable = Number.isFinite(packVoltage) && Number.isFinite(cellCount) && cellCount > 0;
      return {
        raw: rawUsable ? packVoltage / cellCount : null,
        corrected: Number.isFinite(corrected) ? corrected : null,
      };
    },

    measuredAgo(at) {
      if (!at) return '';
      const seconds = Math.max(0, Math.round((Date.now() - new Date(at).getTime()) / 1000));
      return seconds < 90 ? ` (vor ${seconds} s)` : ` (vor ${Math.round(seconds / 60)} min)`;
    },

    async refreshTopicSamples() {
      const samples = await requestJSON('/api/v1/topics/samples');
      this.topicSamples = new Map((samples || []).map(sample => [sample.topic, sample]));
    },

    linkBatteryVoltageMeasurement(objectNode) {
      if (!this.isBatteryConfig()) return;
      ['empty_v_per_cell', 'full_v_per_cell'].forEach(key => {
        const targetNode = objectNode.querySelector(`[data-schema-key="${key}"]`);
        if (!targetNode) return;
        const block = document.createElement('div');
        block.className = 'battery-measure';
        targetNode.append(block);
        this.renderBatteryMeasureBlock(block, objectNode, targetNode);
      });
    },

    renderBatteryMeasureBlock(block, objectNode, targetNode) {
      block.replaceChildren();
      const control = targetNode.querySelector('.schema-control');
      const live = this.batteryLiveState(objectNode);
      const head = document.createElement('div');
      head.className = 'battery-measure-head';
      const caption = document.createElement('span');
      caption.textContent = live
        ? `Aktuell gemessen${this.measuredAgo(live.at)}:`
        : 'Keine Live-Daten für diese Anlage.';
      const refresh = document.createElement('button');
      refresh.type = 'button';
      refresh.className = 'battery-measure-refresh';
      refresh.textContent = 'Aktualisieren';
      refresh.addEventListener('click', () => {
        // Erst mit dem vorhandenen Stand neu zeichnen: dann folgt der Block
        // sofort einer geaenderten Zellzahl, auch wenn der Abruf scheitert.
        this.renderBatteryMeasureBlock(block, objectNode, targetNode);
        this.refreshTopicSamples()
          .then(() => this.renderBatteryMeasureBlock(block, objectNode, targetNode))
          .catch(error => this.$store.toasts.push(error.message, 'critical'));
      });
      head.append(caption, refresh);
      block.append(head);
      if (!live) return;
      this.batteryUnits(objectNode).forEach(unit => {
        const values = this.batteryCellVoltages(objectNode, unit);
        if (!values || (values.raw === null && values.corrected === null)) return;
        const row = document.createElement('div');
        row.className = 'battery-measure-row';
        const name = document.createElement('span');
        name.textContent = unit.label;
        row.append(name);
        [['roh', values.raw], ['lastkorrigiert', values.corrected]].forEach(([kind, value]) => {
          if (value === null) return;
          const button = document.createElement('button');
          button.type = 'button';
          button.className = 'battery-measure-apply';
          button.textContent = `${kind} ${value.toFixed(3)} V/Zelle übernehmen`;
          button.addEventListener('click', () => {
            control.value = value.toFixed(3);
            // Ohne diese Events bliebe das Feld als "Default" markiert und
            // readNode() wuerde den uebernommenen Wert wieder verwerfen.
            control.dispatchEvent(new Event('input', {bubbles: true}));
            control.dispatchEvent(new Event('change', {bubbles: true}));
          });
          row.append(button);
        });
        block.append(row);
      });
    },

    applyJsonKeySuggestions(keyNode, topic) {
      const datalist = keyNode.querySelector('datalist');
      const control = keyNode.querySelector('.schema-control');
      const setHint = (text, warn = false) => {
        let hint = keyNode.querySelector('.schema-hint');
        if (!text) {
          hint?.remove();
          return;
        }
        if (!hint) {
          hint = document.createElement('p');
          keyNode.append(hint);
        }
        hint.className = warn ? 'schema-hint warn' : 'schema-hint';
        hint.textContent = text;
      };
      datalist.replaceChildren();
      control.placeholder = '';
      if (!topic) {
        control.placeholder = 'Erst Topic auswählen';
        setHint('');
        return;
      }
      const sample = this.topicSamples.get(topic);
      if (!sample || !sample.payload) {
        setHint('Auf diesem Topic lag noch keine Nachricht — Key kann nicht vorgeschlagen werden.');
        return;
      }
      let payload;
      try {
        payload = JSON.parse(sample.payload);
      } catch (error) {
        payload = undefined;
      }
      if (payload === null || typeof payload !== 'object' || Array.isArray(payload)) {
        // json.loads() succeeds on a bare number too, and data[key] then
        // raises - the service reads nothing at all and never says so.
        setHint(`Payload ist kein JSON-Objekt (${this.shortPayload(sample.payload)}) — Feld leer lassen, dann wird die erste Zahl aus dem Payload gelesen.`, true);
        return;
      }
      const keys = Object.keys(payload);
      const numericKeys = keys.filter(candidate => this.isNumericValue(payload[candidate]));
      const otherKeys = keys.filter(candidate => !numericKeys.includes(candidate));
      [...numericKeys, ...otherKeys].forEach(candidate => datalist.append(new Option(candidate, candidate)));
      control.placeholder = numericKeys[0] || '';
      const available = numericKeys.length ? numericKeys.join(', ') : '(keine)';
      if (control.value) {
        setHint(`Payload: ${this.shortPayload(sample.payload)} — verfügbare Zahlenfelder: ${available}`);
      } else {
        // Without a key extract_value() regexes the first number out of the
        // raw text - on {"id":0,"apower":12.5} that is the 0.
        setHint(`Payload ist JSON — ohne Key wird die erste Zahl im Rohtext gelesen, das ist meist der falsche Wert. Verfügbare Zahlenfelder: ${available}`, true);
      }
    },

    readNode(node) {
      return window.SchemaForm.readNode(node);
    },

    renderForm() {
      this.$refs.schemaForm.replaceChildren(this.renderNode(this.schema, this.value, this.titleFor(this.schema, this.selectedName)));
      this.formDirty = false;
    },

    async saveForm() {
      // The form carries `novalidate`, so an invalid control no longer aborts
      // the submit silently. Optional properties live inside a collapsed
      // <details>, which the browser cannot focus - open it first, otherwise
      // the message points at a field nobody can see.
      const invalid = this.$refs.schemaForm.querySelector('.schema-control:invalid');
      if (invalid) {
        invalid.closest('details')?.setAttribute('open', 'open');
        this.$store.toasts.push(`Eingabe prüfen (${invalid.previousElementSibling?.textContent?.trim() || ''}): ${invalid.validationMessage}`, 'critical');
        invalid.focus();
        // focus() scrollt nur so weit, dass das Feld gerade am Rand steht -
        // unter der schwebenden Aktionsleiste waere es damit halb verdeckt.
        invalid.scrollIntoView?.({block: 'center'});
        invalid.reportValidity();
        return;
      }
      if (!await this.confirmSave()) return;
      this.saving = true;
      try {
        const value = this.readNode(this.$refs.schemaForm.querySelector('.schema-node'));
        const response = await requestJSON(`/api/v1/configurations/${encodeURIComponent(this.selectedName)}`, {
          method: 'PUT',
          headers: {'Content-Type': 'application/json'},
          body: JSON.stringify(value),
        });
        this.reloadFailed = Boolean(response.reload_failed);
        this.reloadError = response.reload_error || '';
        // Vor dem Ueberschreiben pruefen: der Rohtext-Editor haelt einen
        // eigenen, hier verworfenen Stand - das darf nicht stumm passieren.
        const discardedEditorText = this.editorDirty;
        this.value = value;
        this.editorText = JSON.stringify(value, null, 2);
        this.formDirty = false;
        this.$store.toasts.push(this.reloadFailed ? 'Konfiguration gespeichert.' : 'Konfiguration gespeichert, Dienst neu geladen.');
        if (discardedEditorText) this.$store.toasts.push('Der JSON-Text wurde dabei durch den Formularstand ersetzt.', 'warning');
      } catch (error) {
        this.$store.toasts.push(error.message, 'critical');
      } finally {
        this.saving = false;
      }
    },

    resetForm() {
      this.renderForm();
      this.$store.toasts.push('Formular zurückgesetzt.');
    },

    resetEditor() {
      this.editorText = JSON.stringify(this.value, null, 2);
      this.$store.toasts.push('JSON-Text zurückgesetzt.');
    },

    async saveEditor() {
      // Erst pruefen, dann fragen: nach einer Bestaetigung an einem kaputten
      // JSON abzubrechen waere eine Frage umsonst.
      try {
        JSON.parse(this.editorText);
      } catch (error) {
        this.$store.toasts.push(error.message, 'critical');
        return;
      }
      if (!await this.confirmSave()) return;
      this.saving = true;
      try {
        const response = await requestJSON(`/api/v1/configurations/${encodeURIComponent(this.selectedName)}`, {
          method: 'PUT',
          headers: {'Content-Type': 'application/json'},
          body: this.editorText,
        });
        this.reloadFailed = Boolean(response.reload_failed);
        this.reloadError = response.reload_error || '';
        this.$store.toasts.push(this.reloadFailed ? 'Konfiguration gespeichert.' : 'Konfiguration gespeichert, Dienst neu geladen.');
        await this.loadConfig();
      } catch (error) {
        this.$store.toasts.push(error.message, 'critical');
      } finally {
        this.saving = false;
      }
    },

    revisionConfig() {
      return {
        basePath: () => `/api/v1/configurations/${encodeURIComponent(this.selectedName)}`,
        current: () => this.value,
        reload: () => this.loadConfig(),
        label: 'Revisionen dieser Konfiguration',
      };
    },
  });


  const register = () => {
    if (window.Alpine) window.Alpine.data("configPanel", configPanel);
  };
  if (window.Alpine) register(); else document.addEventListener("alpine:init", register, {once: true});
})();
