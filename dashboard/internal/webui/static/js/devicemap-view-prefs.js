// Browser-side view preferences of the device map (layers, wiring type
// filter, grid). Pure: no DOM, no Cytoscape. A view is a full object, a
// sparse view holds only the keys that differ from the defaults, so that a
// change of the defaults on the server still reaches a browser that has not
// set that key itself.
(() => {
  const STORAGE_KEY = 'devicemap-view';
  const NESTED = Object.freeze(['layers', 'wiring_types']);
  const isObject = value => value !== null && typeof value === 'object' && !Array.isArray(value);

  const merge = (base, ...overrides) => {
    const view = {...base};
    NESTED.forEach(key => { if (isObject(base[key])) view[key] = {...base[key]}; });
    overrides.forEach(override => {
      Object.keys(override || {}).forEach(key => {
        if (NESTED.includes(key) && isObject(override[key])) view[key] = {...(view[key] || {}), ...override[key]};
        else view[key] = override[key];
      });
    });
    return view;
  };

  const diff = (base, view) => {
    const sparse = {};
    Object.keys(view || {}).forEach(key => {
      if (NESTED.includes(key) && isObject(view[key])) {
        const inner = {};
        Object.keys(view[key]).forEach(sub => {
          if (!isObject(base[key]) || base[key][sub] !== view[key][sub]) inner[sub] = view[key][sub];
        });
        if (Object.keys(inner).length) sparse[key] = inner;
      } else if (base[key] !== view[key]) {
        sparse[key] = view[key];
      }
    });
    return sparse;
  };

  const isEmpty = sparse => Object.keys(sparse || {}).length === 0;

  // Keeps only keys known to the full view, with the same type. A nested
  // object keeps only its known keys, an empty result falls away.
  const load = (storage, known) => {
    try {
      const parsed = JSON.parse(storage.getItem(STORAGE_KEY));
      if (!isObject(parsed)) return {};
      const sparse = {};
      Object.keys(known).forEach(key => {
        if (!(key in parsed)) return;
        const value = parsed[key];
        if (NESTED.includes(key)) {
          if (!isObject(value) || !isObject(known[key])) return;
          const inner = {};
          Object.keys(known[key]).forEach(sub => {
            if (sub in value && typeof value[sub] === typeof known[key][sub]) inner[sub] = value[sub];
          });
          if (Object.keys(inner).length) sparse[key] = inner;
        } else if (typeof value === typeof known[key]) {
          sparse[key] = value;
        }
      });
      return sparse;
    } catch (error) {
      return {};
    }
  };

  const store = (storage, sparse) => {
    try {
      if (isEmpty(sparse)) storage.removeItem(STORAGE_KEY);
      else storage.setItem(STORAGE_KEY, JSON.stringify(sparse));
    } catch (error) {
      // Storage may be full or blocked, the view then simply is not kept.
    }
  };

  window.DeviceMapViewPrefs = {STORAGE_KEY, NESTED, merge, diff, isEmpty, load, store};
})();
