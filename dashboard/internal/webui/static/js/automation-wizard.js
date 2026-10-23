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

  window.__automationWizard = {
    buildContext,
    devicesFor,
    gridNet,
  };
})();
