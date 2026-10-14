// Spielt das SSE-Thema devices in die Geraetekacheln ein, statt das ganze
// Raster zu tauschen. Der Server rechnet alles (DeviceLive und CompactRows in
// webui.go), hier wird nur gepatcht: Ampel der Kompaktkarte, Statuspunkt und
// Rail der Detailkachel, Zeilen der automatischen Kompaktkarte. Das Markup
// bleibt in den Go-Templates: neue Zeilen entstehen aus <template
// data-compact-row>, und bei einem Strukturwechsel holt der Aufrufer genau
// diese Kachel neu (replace).
(() => {
  const t = (key, params) => (window.I18n ? window.I18n.t(key, params) : key);
  const tn = (key, n, params) => (window.I18n ? window.I18n.tn(key, n, params) : key);
  const AVAILABILITY = ['online', 'offline', 'unknown'];
  const RAILS = ['is-offline', 'is-degraded'];
  const DOT_CLASSES = ['ok', 'warn', 'bad', 'unknown'];

  // Die Kacheln in root. Im Geraete-Tab ist die Karte selbst der Traeger, in
  // der Uebersicht das Rasterfeld um sie herum: es traegt Item-ID und
  // Struktur und wird beim Strukturwechsel als Ganzes ersetzt.
  function hostsIn(root) {
    const live = root.querySelector('#devices-live');
    if (live) {
      return [...live.querySelectorAll('.device-tile[data-device-id], .compact-card[data-device-id]')]
        .map(el => ({el, id: el.dataset.deviceId, card: el}));
    }
    return [...root.querySelectorAll('.layout-grid-item[data-layout-item-kind="device"]')]
      .map(el => ({el, id: el.dataset.layoutItemRef || '', card: el.querySelector('.device-tile, .compact-card')}));
  }

  function statusLabel(status) {
    if (status.class === 'bad') return tn('tile.status.offline', status.offline || 0);
    if (status.class === 'warn') return t('tile.status.stale');
    if (status.class === 'ok') return t('tile.status.online');
    return '';
  }

  function applyAvailability(card, availability) {
    if (!card || !card.classList.contains('compact-card')) return;
    const value = AVAILABILITY.includes(availability) ? availability : 'unknown';
    for (const name of AVAILABILITY) card.classList.toggle(`is-${name}`, name === value);
    const status = card.querySelector('.compact-card-status');
    if (!status) return;
    for (const name of AVAILABILITY) status.classList.toggle(`compact-card-status-${name}`, name === value);
    // i18n-keys: status.online, status.offline, status.unknown
    const text = t(`status.${value}`);
    const last = status.lastChild;
    if (last && last.nodeType === 3) last.nodeValue = text; else status.append(text);
  }

  function applyStatus(card, status) {
    if (!card || !status || !card.classList.contains('device-tile')) return;
    for (const rail of RAILS) card.classList.toggle(rail, rail === status.rail);
    const dot = card.querySelector('.device-tile-dot');
    if (!dot) return;
    for (const name of DOT_CLASSES) dot.classList.toggle(`device-tile-dot-${name}`, name === status.class);
    const label = statusLabel(status);
    if (label) dot.setAttribute('title', label); else dot.removeAttribute('title');
  }

  // Baut die Zeilen einer automatischen Kompaktkarte neu. false heisst: das
  // geht nur per Fragment (Wechsel zwischen Zeilen und Leertext, Vorlage
  // fehlt).
  function applyRows(card, rows, root, values) {
    if (!card || card.dataset.compactAuto !== 'true' || !Array.isArray(rows)) return true;
    const container = card.querySelector('.compact-card-rows');
    const ids = rows.map(row => row.id).join(',');
    if (((container && container.dataset.rows) || '') === ids) return true;
    const template = root.querySelector('template[data-compact-row]');
    if (!container || !rows.length || !template) return false;
    const built = rows.map(row => {
      const node = template.content.firstElementChild.cloneNode(true);
      node.dataset.entityId = row.id;
      node.dataset.deviceClass = row.device_class || '';
      node.dataset.unit = row.unit || '';
      node.querySelector('.compact-card-row-label').textContent = row.label;
      return node;
    });
    container.replaceChildren(...built);
    container.dataset.rows = ids;
    // Der Wert einer neu gewaehlten Zeile kam meist in einem frueheren
    // Delta. Der Aufrufer reicht seinen Wert-Cache herein.
    const subset = {};
    for (const row of rows) if (values && values[row.id]) subset[row.id] = values[row.id];
    window.compactCardValues?.applyCompactCards(container, subset, true);
    return true;
  }

  function apply(root, detail, {values, replace} = {}) {
    if (!root) return true;
    const hosts = hostsIn(root);
    if (!hosts.length) return true;
    const devices = detail && detail.devices;
    if (!devices || typeof devices !== 'object') return false;
    const delta = Boolean(detail.devices_delta);
    // Geraete-Tab: ein neues oder ignoriertes Geraet aendert die Liste. Das
    // ist selten und geht ueber den Tausch des ganzen Tabs.
    if (root.querySelector('#devices-live') && Array.isArray(detail.devices_list)
        && hosts.map(host => host.id).join(',') !== detail.devices_list.join(',')) return false;
    for (const host of hosts) {
      const has = Object.prototype.hasOwnProperty.call(devices, host.id);
      if (delta && !has) continue;
      const entry = has ? devices[host.id] : null;
      if ((entry ? entry.structure : '') !== (host.el.dataset.structure || '')) {
        if (replace) replace(host.el);
        continue;
      }
      if (!entry) continue;
      applyAvailability(host.card, entry.availability);
      applyStatus(host.card, entry.status);
      if (!applyRows(host.card, entry.rows, root, values) && replace) replace(host.el);
    }
    return true;
  }

  window.deviceStructure = {apply};
})();
