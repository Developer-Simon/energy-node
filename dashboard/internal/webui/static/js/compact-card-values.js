// Zieht die Kompakt-Karten des Geraete-Tabs nach, statt sie neu zu bauen.
//
// Schwestermodul zu device-tile-values.js: dort die Steuerungskacheln, hier
// die read-only Kompakt-Karten (compact-card in devices.html). Beide teilen
// sich die Primitive aus entity-values.js.
//
// Was die Karte *strukturell* zeigt - welche bis zu drei Zeilen
// (priorityEntities) und welche Ampel am Rand (deviceAvailability) - haengt
// an der Discovery und am ersten Messwert je Entitaet. Aendert sich daran
// etwas, wechselt webui.CompactStructureFingerprint und dashboard.js tauscht
// #devices-live ganz. Diese Datei ist nur fuer die Zeit dazwischen
// zustaendig, und in der aendert sich genau eines: der Wert einer schon
// sichtbaren Zeile (Text, Einheit, stale, Zeitstempel).
(() => {
  const {merge, timestampNode} = window.entityValues;

  // compact-card.html setzt bei fehlendem Wert einen Halbgeviertstrich
  // (U+2013), nicht den Bindestrich, den device-tile.html nimmt. Tausch und
  // Nachziehen muessen dieselbe DOM ergeben, sonst springt die Darstellung
  // beim naechsten Strukturwechsel sichtbar um.
  const NO_VALUE = '–';

  // Wert und Einheit stehen im selben <span>, mit einem Leerzeichen
  // dazwischen - genau so, wie compact-card sie rendert.
  function applyRow(row, value) {
    const span = row.querySelector('.compact-card-row-value');
    if (!span) return;
    const unit = row.dataset.unit || '';
    if (!value.has_value) {
      span.textContent = NO_VALUE;
    } else if ((row.dataset.deviceClass || '') === 'timestamp') {
      span.replaceChildren(timestampNode(row.ownerDocument, value.value));
      if (unit) span.append(` ${unit}`);
    } else {
      span.textContent = unit ? `${value.value} ${unit}` : value.value;
    }
    span.classList.toggle('stale', Boolean(value.stale));
  }

  function applyCompactCards(root, entities, delta = false) {
    if (!root || !entities) return;
    const applyOne = row => {
      const id = row.dataset.entityId;
      if (id) applyRow(row, merge(entities[id]));
    };

    if (delta) {
      const escapeId = id => (typeof CSS !== 'undefined' && CSS.escape)
        ? CSS.escape(id)
        : id.replace(/[\\"]/g, '\\$&');
      for (const id of Object.keys(entities)) {
        const row = root.querySelector(`.compact-card-row[data-entity-id="${escapeId(id)}"]`);
        if (row) applyOne(row);
      }
    } else {
      for (const row of root.querySelectorAll('.compact-card-row[data-entity-id]')) applyOne(row);
    }
  }

  window.compactCardValues = {applyCompactCards};
})();
