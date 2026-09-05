// Zieht die Geraetekacheln nach, statt sie neu zu bauen.
//
// Bis 2026-08 tauschte der Geraete-Tab bei jedem SSE-Ereignis sein ganzes
// Fragment #devices-live per outerHTML. Fuer dasselbe Muster in der
// Uebersicht waren 62 % der CPU-Zeit eines im Leerlauf offenen Tabs
// gemessen, und die Knotenzahl lag beim Dreieinhalbfachen der eigentlichen
// Seite (34 572 -> 9 952). Dieselbe Kachel steht auch als "device"-Karte im
// Raster; beide bedient diese Datei.
//
// Warum nachziehen und nicht rendern: das Markup soll an genau einem Ort
// bleiben (device-tile.html). Was die Kachel *strukturell* zeigt - welche
// Entitaeten, wie sie heissen, welches Icon, welcher Regler - haengt an der
// Discovery. Aendert sich daran etwas, wechselt der Fingerabdruck aus
// registry.StructureFingerprint und dashboard.js tauscht wie frueher. Diese
// Datei ist nur fuer die Zeit dazwischen zustaendig, und in der aendern sich
// vier Dinge: Wert, Zeilenbreite, Regler und Schaltknopf.
(() => {
  const {merge, statusMessage, timestampNode} = window.entityValues;

  function setBusy(control, pending) {
    control.disabled = Boolean(pending);
    if (pending) control.setAttribute('aria-busy', 'true');
    else control.removeAttribute('aria-busy');
  }

  // Wert und Einheit stehen im selben <span>, mit einem Leerzeichen
  // dazwischen - genau so, wie device-tile.html sie rendert. Ein Tausch und
  // ein Nachziehen muessen dieselbe DOM ergeben, sonst springt die
  // Darstellung beim naechsten Strukturwechsel sichtbar um.
  function applyValue(cell, value) {
    const span = cell.querySelector('.device-tile-entity-value');
    if (!span) return;
    const unit = cell.dataset.unit || '';
    if (!value.has_value) {
      span.textContent = '-';
    } else if ((cell.dataset.deviceClass || '') === 'timestamp') {
      span.replaceChildren(timestampNode(cell.ownerDocument, value.value));
      if (unit) span.append(` ${unit}`);
    } else {
      span.textContent = unit ? `${value.value} ${unit}` : value.value;
    }
    span.classList.toggle('stale', Boolean(value.stale));
    applyTextWidth(cell, value, unit);
  }

  // device-tile.html verbreitert die Zeile, sobald ein einheitenloser Sensor
  // laenger als 20 Zeichen wird ($isLongTextSensor): ein freier Text
  // braucht das titelschuetzende Raster, ein kurzes "ON" nicht. Das ist die
  // einzige Klasse, die vom Wert abhaengt - ohne sie hier bliebe die Zeile
  // bis zum naechsten Tausch zu schmal.
  //
  // Die Zahlen-Variante und echte text-Entitaeten haben ihre Klasse dagegen
  // fest am Component haengen und werden nicht angefasst.
  function applyTextWidth(cell, value, unit) {
    if (cell.classList.contains('device-tile-entity-number')) return;
    if ((cell.dataset.component || '') === 'text') return;
    cell.classList.toggle('device-tile-entity-text', !unit && value.has_value && value.value.length > 20);
  }

  // Ein Regler, an dem gerade jemand zieht, darf nicht unter der Hand
  // umspringen: der Push kommt bis zu einmal je Sekunde, das Ziehen dauert
  // laenger. Beim Tausch stellte sich die Frage nicht - htmx ersetzte das
  // Element ohnehin. Der Fokus ist hier die verlaessliche Auskunft.
  function applySlider(cell, value) {
    const slider = cell.querySelector('input.entity-number-slider');
    if (!slider || slider === cell.ownerDocument.activeElement) return;
    if (value.has_value) slider.value = value.value;
    const output = slider.nextElementSibling;
    if (output && output.tagName === 'OUTPUT') output.textContent = value.has_value ? value.value : '-';
    setBusy(slider, value.pending);
  }

  // data-command-value ist kein Anzeigewert, sondern die Grundlage der
  // Toggle-Nutzlast in device-tile.js (payload_on/payload_off gegen den
  // aktuellen Wert). Bleibt es beim Nachziehen stehen, schaltet der naechste
  // Klick in die falsche Richtung.
  function applyButton(cell, value) {
    const button = cell.querySelector('button.entity-command-button');
    if (!button) return;
    button.dataset.commandValue = value.has_value ? value.value : '';
    setBusy(button, value.pending);
  }

  // Gibt die Statustexte zurueck, statt sie selbst zu setzen: sie gehoeren
  // in commandStates der Alpine-Komponente (deviceTileMixin), und diese
  // Datei kennt Alpine nicht. dashboard.js uebernimmt sie von hier -
  // dieselbe Arbeitsteilung wie in overview-values.js.
  function applyDeviceTiles(root, entities, delta = false) {
    if (!root || !entities) return {};
    const messages = {};
    const applyOne = cell => {
      const id = cell.dataset.entityId;
      if (!id) return;
      const value = merge(entities[id]);
      applyValue(cell, value);
      applySlider(cell, value);
      applyButton(cell, value);
      if (cell.querySelector('.entity-command-status')) messages[id] = statusMessage(value);
    };

    if (delta) {
      const escapeId = (id) => {
        if (typeof CSS !== 'undefined' && CSS.escape) return CSS.escape(id);
        return id.replace(/[\\"]/g, '\\$&');
      };
      for (const id of Object.keys(entities)) {
        const cell = root.querySelector(`.device-tile-entity[data-entity-id="${escapeId(id)}"]`);
        if (cell) applyOne(cell);
      }
    } else {
      for (const cell of root.querySelectorAll('.device-tile-entity[data-entity-id]')) applyOne(cell);
    }
    return messages;
  }

  window.deviceTileValues = {applyDeviceTiles};
})();
