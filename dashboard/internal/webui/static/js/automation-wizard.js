(() => {
  // Geraetekontext fuer den Automations-Assistenten: welche Geraete Wechselrichter,
  // Wallboxen oder Schalter sind, welches Stellglied und welcher Messwert dazu
  // gehoeren, und ob sie schon von einem Regler-Szenario belegt sind. Keine
  // DOM-Zugriffe. Eingaben sind die Geraete aus /api/v1/devices, der Snapshot aus
  // /api/v1/energy und das aktuelle Regeldokument.
  const t = (key, params) => (window.I18n ? window.I18n.t(key, params) : key);
  const formatNumber = (value) => (window.I18n ? window.I18n.formatNumber(value) : String(value));

  const hasOwn = (object, key) => Object.prototype.hasOwnProperty.call(object || {}, key);
  const isNumber = (value) => typeof value === 'number' && Number.isFinite(value);
  const hasText = (value) => value !== undefined && value !== null && value !== '';
  const hasValue = (entity) => !!entity && entity.has_value !== false && hasText(entity.value);

  // Stellglied: schreibbar, mit Grenzen und ohne Ein-/Aus-Payload.
  const isActuator = (entity) => hasText(entity.command_topic)
    && isNumber(entity.min_value) && isNumber(entity.max_value) && !hasText(entity.payload_on);
  // Schalter: schreibbar mit Ein- und Aus-Payload.
  const isSwitch = (entity) => hasText(entity.command_topic)
    && hasText(entity.payload_on) && hasText(entity.payload_off);
  // Lesbarer Messwert: Zustandsthema mit Template-Unterstuetzung.
  const isReadable = (entity) => hasText(entity.state_topic) && !!entity.template_supported;

  // Rollen aus dem Snapshot: je Entitaet und je Geraet.
  function roleIndex(snapshot) {
    const entityRoles = new Map();
    const deviceRoles = new Map();
    for (const item of (snapshot && snapshot.entities) || []) {
      const role = item.role && item.role.role;
      if (!role) continue;
      entityRoles.set(item.entity_id, role);
      if (!deviceRoles.has(item.device_id)) deviceRoles.set(item.device_id, new Set());
      deviceRoles.get(item.device_id).add(role);
    }
    return { entityRoles, deviceRoles };
  }

  // Ausgaben vorhandener Regler-Szenarien: Topic -> Szenarioname.
  function regulatedTopicsOf(document) {
    const topics = {};
    for (const scenario of (document && document.scenarios) || []) {
      const regulator = scenario.while && scenario.while.regulator;
      const topic = regulator && regulator.output && regulator.output.topic;
      if (hasText(topic) && !hasOwn(topics, topic)) topics[topic] = scenario.name || '';
    }
    return topics;
  }

  function liveText(entity) {
    if (!hasValue(entity)) return '—';
    const number = typeof entity.value === 'number' ? entity.value : Number(entity.value);
    const text = Number.isFinite(number) && String(entity.value).trim() !== ''
      ? formatNumber(number)
      : String(entity.value);
    return entity.unit_of_measurement ? `${text} ${entity.unit_of_measurement}` : text;
  }

  function disabledText(entity, regulatedTopics) {
    if (entity && hasText(entity.command_topic) && hasOwn(regulatedTopics, entity.command_topic)) {
      return t('automations.assistant.device.already_regulated', { name: regulatedTopics[entity.command_topic] });
    }
    return '';
  }

  // Ein Eintrag je Geraet mit Rolle. Ohne Stellglied bleibt entity leer und
  // der Eintrag ist abgeblendet. Die ID faellt dann auf den Messwert oder die
  // erste Entitaet zurueck, damit sie eindeutig bleibt.
  function hardwareEntry(device, role, index, regulatedTopics, extra = {}) {
    const entities = device.entities || [];
    const readable = entities.filter((entity) => isReadable(entity)
      && index.entityRoles.get(entity.unique_id) === role);
    const measured = readable[0];
    const actuator = entities.find((entity) => isActuator(entity));
    const fallback = actuator || measured || entities[0] || {};
    const entry = {
      id: fallback.unique_id || device.id,
      deviceName: device.name,
      entity: actuator || null,
      live: liveText(measured || actuator),
      disabled: disabledText(actuator, regulatedTopics) || (actuator ? '' : t('automations.assistant.device.no_limit')),
      ...extra,
    };
    if (measured) entry.measured = measured;
    return entry;
  }

  function inverterEntries(devices, index, regulatedTopics) {
    return devices
      .filter((device) => (index.deviceRoles.get(device.id) || new Set()).has('pv'))
      .map((device) => hardwareEntry(device, 'pv', index, regulatedTopics));
  }

  function wallboxEntries(devices, index, regulatedTopics) {
    return devices
      .filter((device) => (index.deviceRoles.get(device.id) || new Set()).has('wallbox'))
      .map((device) => {
        const entities = device.entities || [];
        const switchEntity = entities.find((entity) => isSwitch(entity)) || null;
        const plugEntity = entities.find((entity) => isReadable(entity)
          && ['plug', 'connectivity'].includes(entity.device_class)) || null;
        return hardwareEntry(device, 'wallbox', index, regulatedTopics, { switchEntity, plugEntity });
      });
  }

  function switchEntries(devices, regulatedTopics) {
    const result = [];
    for (const device of devices) {
      for (const entity of device.entities || []) {
        if (!isSwitch(entity)) continue;
        result.push({
          id: entity.unique_id,
          deviceName: device.name,
          entity,
          live: liveText(entity),
          disabled: disabledText(entity, regulatedTopics),
        });
      }
    }
    return result;
  }

  function buildContext({ devices = [], snapshot, document } = {}) {
    const index = roleIndex(snapshot);
    const regulatedTopics = regulatedTopicsOf(document);
    const readable = [];
    for (const device of devices) {
      for (const entity of device.entities || []) {
        if (isReadable(entity)) readable.push(entity);
      }
    }
    const settings = (document && document.settings) || {};
    const values = (snapshot && snapshot.values) || {};
    return {
      inverters: inverterEntries(devices, index, regulatedTopics),
      wallboxes: wallboxEntries(devices, index, regulatedTopics),
      switches: switchEntries(devices, regulatedTopics),
      socAvailable: hasOwn(values, 'battery_soc'),
      hasLocation: isNumber(settings.latitude) && isNumber(settings.longitude),
      regulatedTopics,
      usedIds: [
        ...((document && document.rules) || []).map((rule) => rule.id),
        ...((document && document.scenarios) || []).map((scenario) => scenario.id),
      ],
      snapshot,
      readable,
    };
  }

  function devicesFor(filter, ctx) {
    if (filter === 'inverter') return ctx.inverters;
    if (filter === 'wallbox') return ctx.wallboxes;
    if (filter === 'switch') return ctx.switches;
    return [];
  }

  // Spiegel von gridImportPower - gridExportPower in energy-model.js: die
  // getrennten Rollen grid_import/grid_export haben Vorrang, sonst das Vorzeichen
  // der Rolle grid. null, wenn keine Netzrolle im Snapshot steht.
  function gridNet(snapshot) {
    const values = (snapshot && snapshot.values) || {};
    if (!hasOwn(values, 'grid_import') && !hasOwn(values, 'grid_export') && !hasOwn(values, 'grid')) return null;
    const grid = values.grid || 0;
    const importPower = hasOwn(values, 'grid_import')
      ? Math.abs(values.grid_import)
      : (hasOwn(values, 'grid') && grid > 0 ? grid : 0);
    const exportPower = hasOwn(values, 'grid_export')
      ? Math.abs(values.grid_export)
      : (hasOwn(values, 'grid') && grid < 0 ? Math.abs(grid) : 0);
    return importPower - exportPower;
  }

  // Vorlagenkategorien der Suche. Die Vorlagen selbst liegen in Task 4.
  const CATEGORIES = ['feed_in', 'charging', 'switching', 'warning', 'time'];
  const TIME_PATTERN = /^([01]\d|2[0-3]):[0-5]\d$/;
  const WEEKDAYS = [0, 1, 2, 3, 4, 5, 6];

  // Grund, warum eine Option gerade nicht wählbar ist, oder ''.
  function optionUnavailable(option, ctx, answers) {
    if (!option || typeof option.unavailable !== 'function') return '';
    return option.unavailable(ctx, answers) || '';
  }

  // Texte einer Vorlage: fertige Strings am Objekt haben Vorrang, sonst der
  // Katalogschluessel automations.templates.<id>.<feld>.
  function templateText(template, field) {
    if (typeof template[field] === 'string') return template[field];
    return t(`automations.templates.${template.id}.${field}`);
  }

  function templateKeywords(template) {
    if (Array.isArray(template.keywords)) return template.keywords.join(' ');
    return templateText(template, 'keywords');
  }

  // Verfuegbarkeit einer Vorlage aus den Geraetelisten und dem Akkusignal.
  function templateAvailability(template, ctx) {
    const requires = template.requires || {};
    let reason = '';
    let hint = '';
    if (requires.devices) {
      const usable = devicesFor(requires.devices, ctx).filter((item) => !item.disabled);
      if (!usable.length) {
        reason = t(`automations.assistant.unavailable.${requires.devices}`);
      } else {
        hint = usable.length === 1
          ? t('automations.assistant.hint.device_one', { name: usable[0].deviceName })
          : t('automations.assistant.hint.device_many', { n: usable.length });
      }
    }
    if (!reason && requires.soc) {
      if (!ctx.socAvailable) reason = t('automations.assistant.unavailable.soc');
      else if (!hint) hint = t('automations.assistant.hint.soc');
    }
    return { available: !reason, reason, hint: reason ? '' : hint };
  }

  // Suche nach Wortern in Titel, Kurztext und Schluesselwoertern (Kleinschreibung,
  // Umlaute bleiben). Jedes Wort muss treffen. Ohne Suche gelten alle Vorlagen.
  function searchTemplates(templates, { query = '', category = 'all' } = {}, ctx) {
    const words = String(query || '').trim().toLowerCase().split(/\s+/).filter(Boolean);
    const fitting = [];
    const others = [];
    for (const template of templates || []) {
      if (category && category !== 'all' && template.category !== category) continue;
      if (words.length) {
        const haystack = [templateText(template, 'title'), templateText(template, 'blurb'), templateKeywords(template)]
          .join(' ').toLowerCase();
        if (!words.every((word) => haystack.includes(word))) continue;
      }
      (templateAvailability(template, ctx).available ? fitting : others).push(template);
    }
    return { fitting, others };
  }

  // Standardwert je Schritt. Ist der Standard nicht verfügbar, gilt die erste
  // verfügbare Option bzw. das erste verfügbare Gerät.
  function defaultAnswer(step, ctx, answers) {
    const options = step.options || [];
    switch (step.type) {
      case 'device': {
        const kind = typeof step.filter === 'string' ? step.filter : (step.filter && step.filter.role);
        const list = devicesFor(kind, ctx);
        const preferred = list.find((item) => item.id === step.default && !item.disabled);
        const first = list.find((item) => !item.disabled);
        return (preferred || first || {}).id || '';
      }
      case 'choice':
      case 'tiles': {
        const available = (option) => !optionUnavailable(option, ctx, answers);
        const preferred = options.find((option) => option.id === step.default && available(option));
        const first = options.find((option) => available(option));
        if (preferred) return preferred.id;
        if (first) return first.id;
        return step.default === undefined ? '' : step.default;
      }
      case 'presets':
        return step.default !== undefined ? step.default : (options[0] || {}).id;
      case 'number':
        return step.default !== undefined ? step.default : step.min;
      case 'time': {
        const source = step.default || { from: '', to: '', days: WEEKDAYS };
        return { from: source.from, to: source.to, days: [...(source.days || [])] };
      }
      case 'multi': {
        const result = {};
        for (const option of options) {
          result[option.id] = !!option.default && !optionUnavailable(option, ctx, answers);
          if (option.reveal) {
            const value = option.reveal.default;
            result[`${option.id}_value`] = Array.isArray(value) ? [...value] : value;
          }
        }
        return result;
      }
      default:
        return step.default;
    }
  }

  function defaultAnswers(template, ctx) {
    const answers = {};
    for (const step of template.steps || []) {
      answers[step.id] = defaultAnswer(step, ctx, answers);
    }
    return answers;
  }

  // Sichtbare Schritte: ohne when oder mit when(answers) === true, danach die
  // Zusammenfassung. Die Liste ist eine Funktion der Antworten und wird bei jeder
  // Änderung neu berechnet.
  function visibleSteps(template, answers) {
    const steps = (template.steps || []).filter((step) => !step.when || step.when(answers) === true);
    return [...steps, { id: 'summary', type: 'summary' }];
  }

  function stepComplete(step, answer) {
    switch (step.type) {
      case 'device':
        return typeof answer === 'string' && answer !== '';
      case 'number':
        return isNumber(answer)
          && (step.min === undefined || answer >= step.min)
          && (step.max === undefined || answer <= step.max);
      case 'presets':
        return isNumber(answer) && answer > 0;
      case 'time':
        return !!answer && TIME_PATTERN.test(answer.from) && TIME_PATTERN.test(answer.to)
          && answer.from !== answer.to;
      default:
        return true;
    }
  }

  // Kleinste freie ID: base, sonst base_2, base_3 ...
  function uniqueId(base, usedIds) {
    const used = new Set(usedIds || []);
    if (!used.has(base)) return base;
    let counter = 2;
    while (used.has(`${base}_${counter}`)) counter += 1;
    return `${base}_${counter}`;
  }

  // Zeitstempel der frühesten Minute mit mindestens thresholdW Einspeisung.
  // Einspeisung ist negativ in role:grid.
  function firstReached(rows, thresholdW) {
    let found = null;
    for (const row of rows || []) {
      if (!isNumber(row.avg) || row.avg > -thresholdW) continue;
      if (found === null || row.ts < found) found = row.ts;
    }
    return found;
  }

  // Kleinster Minimalwert in den Zeilen, null ohne Zeilen.
  function lowest(rows) {
    const values = (rows || []).map((row) => row.min).filter(isNumber);
    return values.length ? Math.min(...values) : null;
  }

  window.__automationWizard = {
    CATEGORIES,
    buildContext,
    defaultAnswers,
    devicesFor,
    firstReached,
    gridNet,
    lowest,
    optionUnavailable,
    searchTemplates,
    stepComplete,
    templateAvailability,
    uniqueId,
    visibleSteps,
  };
})();
