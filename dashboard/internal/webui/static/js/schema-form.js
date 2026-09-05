(() => {
  'use strict';

  // Generic JSON-Schema form engine. Extracted from config.page.js so the
  // Systemkonfiguration on the Einstellungsseite renders the same widgets.
  // Everything config-specific (MQTT-Topic pickers, Shelly presets, battery
  // voltage helpers, *_json_key linking) is injected through ctx.hooks; a
  // plain caller passes { hooks: {} } and gets object/array/primitive fields.
  //
  //   ctx = {
  //     motionOK: () => boolean,                       // optional
  //     hooks: {                                       // every entry optional
  //       control:         ({schema,key,required,displayedValue,node}) => Element | {control,datalist} | null,
  //       afterObject:     (objectNode, schema, value) => void,
  //       arrayItemHeader: (headerEl, {itemSchema,itemValue,itemEl,key,label}) => void,
  //       onDirty:         () => void,
  //     },
  //   }

  const defaultMotionOK = () =>
    typeof window !== 'undefined' &&
    typeof window.matchMedia === 'function' &&
    !window.matchMedia('(prefers-reduced-motion: reduce)').matches;

  const titleFor = (schema, fallback) => schema.title || schema.description || fallback;

  const numericStep = (schema) => {
    const multiple = Number(schema.multipleOf);
    if (Number.isFinite(multiple) && multiple > 0) {
      // HTML anchors `step` at `min`, JSON Schema anchors `multipleOf` at
      // zero - only pass it through when both agree.
      const bound = schema.minimum ?? schema.exclusiveMinimum;
      if (bound === undefined || Number.isInteger(bound / multiple)) return String(multiple);
      return 'any';
    }
    // Without step="any" the browser default step=1 rejects every decimal,
    // which silently blocked saving numbers such as 0.98.
    return schema.type === 'integer' ? '1' : 'any';
  };

  const exclusiveBound = (bound, type, direction) => {
    // HTML has no exclusive bounds. For integers the exclusive bound maps
    // exactly onto the neighbouring value; for decimals the bound itself
    // stays as a coarse hint and the server validator rejects the boundary.
    if (type !== 'integer') return bound;
    return direction > 0 ? Math.floor(bound) + 1 : Math.ceil(bound) - 1;
  };

  // A field the schema pins to exactly one value - const, a one-entry enum,
  // or a numeric range with minimum === maximum (e.g. schema_version). It is
  // rendered but locked: an editable box that can only hold one value is a
  // trap. Returns the pinned value, or undefined when the field is free.
  const singleValueOf = (schema) => {
    if (schema.const !== undefined) return schema.const;
    if (Array.isArray(schema.enum) && schema.enum.length === 1) return schema.enum[0];
    if ((schema.type === 'integer' || schema.type === 'number') &&
        schema.minimum !== undefined && schema.minimum === schema.maximum) {
      return schema.minimum;
    }
    return undefined;
  };

  function primitiveControl(ctx, schema, value, label, key, required) {
    const node = document.createElement('div');
    node.className = 'schema-node';
    node.dataset.schemaType = schema.type || 'string';
    if (key) node.dataset.schemaKey = key;
    node.dataset.schemaRequired = required ? 'true' : 'false';
    const defaulted = (value === undefined || value === null) && schema.default !== undefined;
    const locked = singleValueOf(schema);
    let displayedValue = defaulted ? schema.default : value;
    if (locked !== undefined && (displayedValue === undefined || displayedValue === null)) {
      displayedValue = locked;
    }
    const caption = document.createElement('label');
    caption.textContent = label;
    if (required) {
      const marker = document.createElement('span');
      marker.className = 'schema-required';
      marker.textContent = ' *';
      caption.append(marker);
    }
    node.append(caption);
    let control;
    let datalist = null;
    const hooked = ctx.hooks && ctx.hooks.control &&
      ctx.hooks.control({ schema, key, required, displayedValue, node });
    if (hooked) {
      control = hooked.control || hooked;
      datalist = hooked.datalist || null;
    } else if (Array.isArray(schema.enum)) {
      control = document.createElement('select');
      if (!required) control.add(new Option('', ''));
      schema.enum.forEach(item => control.add(new Option(String(item), String(item), false, item === displayedValue)));
    } else if (schema.type === 'boolean') {
      control = document.createElement('input');
      control.type = 'checkbox';
    } else {
      control = document.createElement('input');
      const numeric = schema.type === 'integer' || schema.type === 'number';
      control.type = numeric ? 'number' : 'text';
      if (numeric) control.step = numericStep(schema);
    }
    control.className = 'schema-control';
    if (locked !== undefined && schema.type !== 'boolean') {
      // readOnly keeps an <input> in the submitted form and readable by
      // readNode; <select> ignores readOnly, so lean on its single option.
      if (control.tagName === 'SELECT') control.setAttribute('aria-readonly', 'true');
      else control.readOnly = true;
      control.classList.add('schema-readonly');
      node.dataset.schemaLocked = 'true';
    }
    if (defaulted) {
      control.classList.add('schema-default');
      node.dataset.schemaDefault = 'true';
    }
    const clearDefault = () => {
      if (node.dataset.schemaDefault !== 'true') return;
      node.dataset.schemaDefault = 'false';
      control.classList.remove('schema-default');
    };
    control.addEventListener('input', clearDefault);
    control.addEventListener('change', clearDefault);
    if (required && schema.type !== 'boolean') control.required = true;
    if (schema.minLength !== undefined) control.minLength = schema.minLength;
    if (schema.minimum !== undefined) control.min = schema.minimum;
    else if (schema.exclusiveMinimum !== undefined) control.min = exclusiveBound(schema.exclusiveMinimum, schema.type, 1);
    if (schema.maximum !== undefined) control.max = schema.maximum;
    else if (schema.exclusiveMaximum !== undefined) control.max = exclusiveBound(schema.exclusiveMaximum, schema.type, -1);
    if (schema.type === 'boolean') {
      node.dataset.booleanUnset = displayedValue === undefined || displayedValue === null ? 'true' : 'false';
      control.checked = Boolean(displayedValue);
      const wrapper = document.createElement('div');
      wrapper.className = 'boolean-control';
      // Native checkbox stays the value carrier (readNode reads control.checked);
      // the look is the switch from base.css - it needs the wrapping <label>,
      // otherwise it renders but does not toggle.
      const toggle = document.createElement('label');
      toggle.className = 'settings-toggle';
      const track = document.createElement('span');
      track.className = 'settings-toggle-track';
      const thumb = document.createElement('span');
      thumb.className = 'settings-toggle-thumb';
      track.append(thumb);
      toggle.append(control, track);
      wrapper.append(toggle);
      if (!required) {
        const reset = document.createElement('button');
        reset.type = 'button';
        reset.className = 'boolean-reset';
        reset.textContent = 'Zurücksetzen';
        reset.hidden = node.dataset.booleanUnset === 'true';
        reset.addEventListener('click', () => {
          control.checked = false;
          node.dataset.booleanUnset = 'true';
          reset.hidden = true;
        });
        control.addEventListener('change', () => {
          node.dataset.booleanUnset = 'false';
          reset.hidden = false;
        });
        wrapper.append(reset);
      }
      node.append(wrapper);
    } else {
      if (displayedValue !== undefined && displayedValue !== null) control.value = displayedValue;
      node.append(control);
      if (datalist) node.append(datalist);
    }
    if (schema.description) {
      const help = document.createElement('p');
      help.className = 'schema-help';
      help.textContent = schema.description;
      node.append(help);
    }
    return node;
  }

  function renderNode(ctx, schema, value, label, required = false, key = '') {
    schema = schema || { type: 'string' };
    const motionOK = ctx.motionOK || defaultMotionOK;
    if (schema.type === 'object') {
      const node = document.createElement('fieldset');
      node.className = 'schema-node schema-object';
      node.dataset.schemaType = 'object';
      if (key) node.dataset.schemaKey = key;
      const legend = document.createElement('legend');
      legend.textContent = label;
      node.append(legend);
      const requiredFields = new Set(schema.required || []);
      const optionalFields = document.createElement('div');
      optionalFields.className = 'schema-optional-fields';
      Object.entries(schema.properties || {}).forEach(([propertyKey, childSchema]) => {
        const child = renderNode(ctx, childSchema, value && value[propertyKey], titleFor(childSchema, propertyKey), requiredFields.has(propertyKey), propertyKey);
        if (requiredFields.has(propertyKey)) node.append(child); else optionalFields.append(child);
      });
      if (optionalFields.children.length) {
        const optionalGroup = document.createElement('details');
        optionalGroup.className = 'schema-optional-group';
        const summary = document.createElement('summary');
        summary.textContent = 'Optionale Eigenschaften';
        optionalGroup.append(summary, optionalFields);
        node.append(optionalGroup);
      }
      if (ctx.hooks && ctx.hooks.afterObject) ctx.hooks.afterObject(node, schema, value);
      return node;
    }
    if (schema.type === 'array') {
      const node = document.createElement('fieldset');
      node.className = 'schema-node schema-array';
      node.dataset.schemaType = 'array';
      if (key) node.dataset.schemaKey = key;
      const legend = document.createElement('legend');
      legend.textContent = label;
      node.append(legend);
      const items = document.createElement('div');
      items.className = 'array-items';
      node.append(items);
      const itemSchema = schema.items || { type: 'string' };
      const markDirty = () => { if (ctx.hooks && ctx.hooks.onDirty) ctx.hooks.onDirty(); };
      const appendItem = (itemValue, animateIn = false) => {
        const item = document.createElement('div');
        item.className = 'array-item';
        const header = document.createElement('div');
        header.className = 'array-item-header';
        const itemLabel = document.createElement('strong');
        itemLabel.textContent = `${label} ${items.children.length + 1}`;
        header.append(itemLabel);
        if (ctx.hooks && ctx.hooks.arrayItemHeader) {
          ctx.hooks.arrayItemHeader(header, { itemSchema, itemValue, itemEl: item, key, label });
        }
        const remove = document.createElement('button');
        remove.type = 'button';
        remove.textContent = 'Entfernen';
        remove.addEventListener('click', () => {
          const finalize = () => {
            item.remove();
            [...items.children].forEach((entry, index) => entry.querySelector('strong').textContent = `${label} ${index + 1}`);
            markDirty();
          };
          if (!motionOK()) { finalize(); return; }
          let done = false;
          const finalizeOnce = () => { if (done) return; done = true; finalize(); };
          item.classList.add('is-leaving');
          item.addEventListener('animationend', finalizeOnce, { once: true });
          setTimeout(finalizeOnce, 400);
        });
        header.append(remove);
        item.append(header, renderNode(ctx, itemSchema, itemValue, 'Eintrag'));
        items.append(item);
        if (animateIn && motionOK()) {
          item.classList.add('is-entering');
          item.addEventListener('animationend', () => item.classList.remove('is-entering'), { once: true });
        }
      };
      (Array.isArray(value) ? value : []).forEach(entry => appendItem(entry));
      const addButton = document.createElement('button');
      addButton.type = 'button';
      addButton.textContent = 'Eintrag hinzufügen';
      addButton.addEventListener('click', () => {
        appendItem(undefined, true);
        markDirty();
      });
      node.append(addButton);
      return node;
    }
    return primitiveControl(ctx, schema, value, label, key, required);
  }

  function readNode(node) {
    const type = node.dataset.schemaType;
    if (type === 'object') {
      const result = {};
      const fields = [];
      [...node.children].forEach(child => {
        if (child.classList.contains('schema-node')) {
          fields.push(child);
        } else if (child.classList.contains('schema-optional-group')) {
          // Optional properties render inside a collapsible <details> wrapper;
          // their .schema-node elements are not direct children of `node` but
          // must still be read and saved.
          fields.push(...[...child.querySelector('.schema-optional-fields').children].filter(entry => entry.classList.contains('schema-node')));
        }
      });
      fields.forEach(child => {
        const value = readNode(child);
        if (value !== undefined) result[child.dataset.schemaKey] = value;
      });
      return result;
    }
    if (type === 'array') return [...node.querySelector('.array-items').children].map(item => readNode(item.querySelector('.schema-node')));
    const control = node.querySelector('.schema-control');
    if (node.dataset.schemaRequired !== 'true' && node.dataset.schemaDefault === 'true') return undefined;
    if (type === 'boolean') {
      if (node.dataset.schemaRequired !== 'true' && node.dataset.booleanUnset === 'true') return undefined;
      return control.checked;
    }
    if (control.value === '' && node.dataset.schemaRequired !== 'true') return undefined;
    if (type === 'integer') return Number.parseInt(control.value, 10);
    if (type === 'number') return Number(control.value);
    return control.value;
  }

  // Lists the dotted paths of keys present in `value` that the schema does
  // not declare at an object with additionalProperties:false. The form would
  // silently drop those on save, so the caller can fall back to a raw editor
  // instead of quietly rewriting the file.
  function findUnknownKeys(schema, value, path = '') {
    if (!schema || value === null || typeof value !== 'object') return [];
    const found = [];
    if (schema.type === 'object' && !Array.isArray(value)) {
      const known = new Set(Object.keys(schema.properties || {}));
      if (schema.additionalProperties === false) {
        Object.keys(value).forEach(childKey => {
          if (!known.has(childKey)) found.push(path ? `${path}.${childKey}` : childKey);
        });
      }
      Object.entries(schema.properties || {}).forEach(([childKey, childSchema]) => {
        if (value[childKey] !== undefined) {
          found.push(...findUnknownKeys(childSchema, value[childKey], path ? `${path}.${childKey}` : childKey));
        }
      });
    } else if (schema.type === 'array' && Array.isArray(value)) {
      value.forEach((entry, index) => {
        found.push(...findUnknownKeys(schema.items || {}, entry, `${path}[${index}]`));
      });
    }
    return found;
  }

  window.SchemaForm = {
    renderNode,
    readNode,
    primitiveControl,
    numericStep,
    exclusiveBound,
    titleFor,
    singleValueOf,
    findUnknownKeys,
  };
})();
