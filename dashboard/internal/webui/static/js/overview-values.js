// Zieht die nicht-energetischen Uebersichtskarten nach, statt sie neu zu
// bauen.
//
// Bis 2026-08 wurde #overview-live bei jedem SSE-Ereignis komplett per
// outerHTML ersetzt. Gemessen kostete allein der Tausch 62 % der CPU-Zeit
// eines im Leerlauf offenen Tabs (TaskDuration 4371 ms je 30 s, davon 2701 ms
// der Tausch) und hielt die Knotenzahl beim Vierfachen der eigentlichen
// Seite. Stufe 2 hat die sieben Energiegrafiken vom Tausch geloest; hier
// folgen entity_value, entity_group und diagnostics.
//
// Warum nachziehen und nicht rendern: die *Struktur* dieser Karten haengt am
// Layout (ein Ref, eine Ref-Liste, gar nichts), nicht an der Registry - und
// Layoutwechsel behalten den Voll-Tausch. Zwischen zwei Layoutwechseln
// aendern sich nur Zahl, Punkt und ein paar Klassen. Ein zweiter Renderer in
// JavaScript wuerde das Markup verdoppeln, genau wie es
// classifyEntityCategory heute schon in zwei Sprachen gibt.
//
// Preis dieser Entscheidung: eine Entitaet, die neu in der Registry
// auftaucht, bekommt ihren Chip erst beim naechsten Voll-Aufbau. Das ist ein
// Discovery-Ereignis, kein Messwert.
//
// Die gemeinsamen Bausteine (fehlende Werte, Statustexte, Zeitstempel)
// stehen in entity-values.js - base.html laedt sie davor.
(() => {
  const {merge, statusMessage, setDotClass, availabilityState, timestampNode} = window.entityValues;

  function valueMarkup(document, value, deviceClass) {
    const strong = document.createElement('strong');
    if (!value.has_value) {
      strong.textContent = '-';
    } else if (deviceClass === 'timestamp') {
      strong.appendChild(timestampNode(document, value.value));
    } else {
      strong.textContent = value.value;
    }
    return [strong];
  }

  function applyValueCard(card, value) {
    const document = card.ownerDocument;
    const numeric = card.querySelector('.entity-value-num');
    if (numeric) {
      numeric.replaceChildren(...valueMarkup(document, value, card.dataset.deviceClass || ''));
      const unit = card.dataset.unit || '';
      if (value.has_value && unit) {
        const span = document.createElement('span');
        span.className = 'entity-value-unit';
        span.textContent = unit;
        numeric.appendChild(span);
      }
    }
    setDotClass(card.querySelector('.entity-value-dot'), 'entity-value-dot', availabilityState(value));
  }

  function applyGroupChip(chip, value) {
    const document = chip.ownerDocument;
    const numeric = chip.querySelector('.entity-group-chip-val');
    if (numeric) {
      if (!value.has_value) {
        numeric.textContent = '-';
      } else if ((chip.dataset.deviceClass || '') === 'timestamp') {
        numeric.replaceChildren(timestampNode(document, value.value));
      } else {
        numeric.textContent = value.value;
      }
      const unit = chip.dataset.unit || '';
      if (value.has_value && unit) {
        const span = document.createElement('span');
        span.textContent = unit;
        numeric.appendChild(span);
      }
    }
    setDotClass(chip.querySelector('.entity-group-chip-dot'), 'entity-group-chip-dot', availabilityState(value));
  }

  // Gibt die Statustexte zurueck, statt sie selbst zu setzen: sie gehoeren
  // in commandStates der Alpine-Komponente (deviceTileMixin), und diese
  // Datei kennt Alpine nicht. dashboard.js uebernimmt sie von hier.
  function applyEntityValues(root, entities, delta = false) {
    if (!root || !entities) return {};
    const messages = {};
    const applyOne = element => {
      const id = element.dataset.entityId;
      if (!id) return;
      const value = merge(entities[id]);

      if (element.classList.contains('entity-value-card')) applyValueCard(element, value);
      else applyGroupChip(element, value);

      element.classList.toggle('stale', Boolean(value.stale));
      if (element.tagName === 'BUTTON') {
        element.disabled = Boolean(value.pending);
        if (value.pending) element.setAttribute('aria-busy', 'true');
        else element.removeAttribute('aria-busy');
      }
      if (element.querySelector('.entity-command-status')) messages[id] = statusMessage(value);
    };

    if (delta) {
      // Nur die im Push genannten Karten - der Rest bleibt stehen. Das ist
      // der ganze Sinn des Deltas: nicht mehr O(alle Karten) je Ereignis.
      const escapeId = (id) => {
        if (typeof CSS !== 'undefined' && CSS.escape) return CSS.escape(id);
        return id.replace(/[\\"]/g, '\\$&');
      };
      for (const id of Object.keys(entities)) {
        const element = root.querySelector(
          `.entity-value-card[data-entity-id="${escapeId(id)}"], .entity-group-chip[data-entity-id="${escapeId(id)}"]`,
        );
        if (element) applyOne(element);
      }
    } else {
      for (const element of root.querySelectorAll('.entity-value-card[data-entity-id], .entity-group-chip[data-entity-id]')) {
        applyOne(element);
      }
    }
    return messages;
  }

  // Die Kachel hat zwei Gestalten (siehe "diagnostics-summary-card" in
  // overview.html): bei status_class "ok" einen Satz, sonst drei Zaehler und
  // optional die schlechteste Geraetezeile. Beim Nachziehen muss zwischen
  // beiden umgeschaltet werden - eine Warnung, die auftaucht, ist genau der
  // Fall, den Pruefpunkt 2 der Spec verlangt.
  function countBlock(document, summary) {
    const wrap = document.createElement('div');
    wrap.className = 'diagnostics-summary-counts';
    const columns = [
      ['bad', summary.critical, 'Kritisch'],
      ['warn', summary.warning, 'Warnung'],
      ['info', summary.info, 'Hinweis'],
    ];
    for (const [kind, count, label] of columns) {
      const column = document.createElement('div');
      column.className = `diagnostics-summary-count diagnostics-summary-count-${kind}`;
      const strong = document.createElement('strong');
      strong.textContent = String(count ?? 0);
      const span = document.createElement('span');
      span.textContent = label;
      column.append(strong, span);
      wrap.appendChild(column);
    }
    return wrap;
  }

  function worstBlock(document, worst) {
    const wrap = document.createElement('div');
    wrap.className = `diagnostics-summary-worst diagnostics-summary-worst-${worst.status_class || 'unknown'}`;
    const name = document.createElement('span');
    name.className = 'diagnostics-summary-worst-name';
    name.textContent = worst.name || '';
    const score = document.createElement('span');
    score.className = 'diagnostics-summary-worst-score';
    score.textContent = String(worst.score ?? '');
    wrap.append(name, score);
    return wrap;
  }

  function applyDiagnostics(root, summary) {
    if (!root || !summary) return;
    const card = root.querySelector('.diagnostics-summary-card');
    if (!card) return;
    const document = card.ownerDocument;

    setDotClass(card.querySelector('.diagnostics-summary-dot'), 'diagnostics-summary-dot', summary.status_class || 'ok');
    // Der Kopf bleibt stehen; ersetzt wird nur alles darunter. So bleibt der
    // x-on:click-Handler am Knopf unangetastet.
    const header = card.querySelector('.diagnostics-summary-top');
    const body = [];
    if ((summary.status_class || 'ok') === 'ok') {
      const paragraph = document.createElement('p');
      paragraph.className = 'diagnostics-summary-ok';
      paragraph.textContent = 'Alles in Ordnung';
      body.push(paragraph);
    } else {
      body.push(countBlock(document, summary));
      if (summary.worst_device) body.push(worstBlock(document, summary.worst_device));
    }
    card.replaceChildren(...(header ? [header] : []), ...body);
  }

  window.overviewValues = {applyEntityValues, applyDiagnostics};
})();
