// Aktionen der Tourschritte. Ein Schritt kann Eintraege haben, die vorher etwas
// aufklappen oder einen Reiter waehlen, damit das Ziel sichtbar wird. Diese
// Datei fuehrt sie aus und nimmt sie wieder zurueck, sobald der Schritt endet,
// aber nur, wenn der Zustand noch so ist, wie die Tour ihn hinterlassen hat.
//
// Ein Eintrag ist entweder eine data-tour-ID (Klick auf das Element) oder ein
// benannter Eintrag '@name', der ueber action() registriert ist. Ein benannter
// Eintrag setzt den Bereich (scope), in dem die folgenden IDs gesucht werden.
//
// Ein Lauf (Run) haelt die Eintraege, was schon ausgefuehrt wurde (records),
// den Bereich, ob der Nutzer selbst eingegriffen hat (takenOver) und die
// gerade laufende Ausfuehrung (pending).
//   Run = { entries, records: [{ entry, el, scope, revert }], scope, takenOver, pending }
(() => {
  const named = {};

  // Registriert eine benannte Aktion. run({ waitFor }) liefert { el, scope, revert }.
  const action = (name, def) => { named[name] = def; };

  // 'true' heisst offen, 'false' zu, alles andere zeigt keinen Zustand.
  const flag = (el, name) => {
    const v = el.getAttribute(name);
    if (v === 'true') return 'open';
    if (v === 'false') return 'closed';
    return null;
  };

  // Zustand eines Bedienelements. Ein Reiter zeigt seine Auswahl ueber
  // aria-selected, sonst gilt aria-expanded, aria-pressed oder das open
  // eines details-Elements. Ohne eines davon ist der Zustand null.
  const stateOf = (el) => {
    if (el.getAttribute('role') === 'tab') {
      const selected = flag(el, 'aria-selected');
      if (selected) return selected;
    }
    const expanded = flag(el, 'aria-expanded') || flag(el, 'aria-pressed');
    if (expanded) return expanded;
    if (el.tagName === 'DETAILS') return el.open ? 'open' : 'closed';
    return null;
  };

  // Erstes sichtbares Element mit der ID, zuerst im Bereich, dann im Dokument.
  const firstVisible = (root, id) => [...root.querySelectorAll(`[data-tour="${id}"]`)]
    .find((el) => window.obSpotlight.visibleRect(el)) || null;
  const find = (id, scope) => (scope && firstVisible(scope, id)) || firstVisible(document, id);

  // Anzahl gleicher Eintraege am Anfang beider Listen.
  const sharedPrefix = (a, b) => {
    let n = 0;
    while (n < a.length && n < b.length && a[n] === b[n]) n += 1;
    return n;
  };

  // Ein Klickeintrag. Ist das Element schon offen, wird nicht geklickt und
  // nichts zurueckgenommen. Bei einem Reiter merkt sich der Lauf den bisher
  // gewaehlten Reiter derselben Reiterleiste, der beim Zuruecknehmen wieder
  // gewaehlt wird. Die Zurueckname klickt nur, wenn der Zustand noch dem
  // entspricht, den der Lauf hinterlassen hat.
  const clickEntry = async (run, entry, { alive, show, waitFor }) => {
    const el = await waitFor(() => find(entry, run.scope), 1500);
    if (!el) return false;
    if (stateOf(el) === 'open') {
      run.records.push({ entry, el, scope: run.scope, revert: null });
      return true;
    }
    const tab = el.getAttribute('role') === 'tab';
    const list = tab ? el.closest('[role="tablist"]') : null;
    const before = list?.querySelector('[role="tab"][aria-selected="true"]') || null;
    await show(el);
    if (!alive()) return false;
    el.click();
    // Alpine setzt aria-* asynchron, deshalb erst nach einem Tick den Zustand lesen.
    await new Promise((resolve) => setTimeout(resolve, 0));
    const after = stateOf(el);
    const revert = () => {
      if (!el.isConnected || stateOf(el) !== after) return;
      if (!tab) el.click();
      else if (before?.isConnected) before.click();
    };
    run.records.push({ entry, el, scope: run.scope, revert });
    return true;
  };

  // Ein benannter Eintrag. Er setzt den Bereich fuer die folgenden Eintraege.
  const namedEntry = async (run, entry, { show, waitFor }) => {
    const name = entry.slice(1);
    const def = Object.hasOwn(named, name) ? named[name] : null;
    if (!def) return false;
    const rec = await def.run({ waitFor, show });
    if (!rec) return false;
    run.records.push({ entry, el: rec.el, scope: rec.scope, revert: rec.revert });
    await show(rec.el);
    run.scope = rec.scope;
    return true;
  };

  // Fuehrt die Eintraege ab records.length aus. Der Lauf stoppt, wenn alive()
  // false wird oder ein Element fehlt.
  const runEntries = async (run, opts) => {
    for (let i = run.records.length; i < run.entries.length; i += 1) {
      if (!opts.alive()) return;
      const entry = run.entries[i];
      const ok = entry.startsWith('@')
        ? await namedEntry(run, entry, opts)
        : await clickEntry(run, entry, opts);
      if (!ok) return;
    }
  };

  // Setzt run.pending, solange die Eintraege laufen. Danach ist es wieder null.
  const apply = (run, opts) => {
    const work = runEntries(run, opts);
    run.pending = work;
    return work.finally(() => { if (run.pending === work) run.pending = null; });
  };

  // Merkt sich, ob der Nutzer selbst in der Tour klickt, tippt oder etwas
  // aendert. Klicks im Dock und nicht vertrauenswuerdige Ereignisse zaehlen
  // standardmaessig nicht. Gibt die Funktion zum Abmelden zurueck.
  const watchTakeover = (run, ignore = (event) => !event.isTrusted || event.target.closest?.('.ob-dock')) => {
    const onEvent = (event) => { if (!ignore(event)) run.takenOver = true; };
    const types = ['click', 'input', 'change'];
    for (const type of types) document.addEventListener(type, onEvent, true);
    return () => { for (const type of types) document.removeEventListener(type, onEvent, true); };
  };

  // Nimmt prev zurueck, soweit die neuen Eintraege ihn nicht mehr tragen. Der
  // gemeinsame Anfang bleibt offen. Vorher wird ein laufender apply abgewartet.
  // Hat der Nutzer eingegriffen, wird nichts zurueckgenommen und nichts behalten.
  const next = async (prev, entries) => {
    if (prev?.pending) await prev.pending;
    if (!prev || prev.takenOver) return { entries, records: [], scope: null, takenOver: false, pending: null };
    const keep = Math.min(sharedPrefix(prev.entries, entries), prev.records.length);
    for (let i = prev.records.length - 1; i >= keep; i -= 1) prev.records[i].revert?.();
    const records = prev.records.slice(0, keep);
    const scope = keep > 0 ? records[keep - 1].scope : null;
    return { entries, records, scope, takenOver: false, pending: null };
  };

  window.obActions = { action, named, stateOf, find, sharedPrefix, apply, next, watchTakeover };

  // Oeffnet die erste Regel mit einer Bedingung des Typs im Bearbeiten-Modus.
  // Gibt es keine, legt der Lauf eine deaktivierte Beispielregel mit genau
  // dieser Bedingung an. Zuruecknehmen entfernt sie wieder, aber nur, solange
  // sie unveraendert ist.
  const rulesPage = () => window.Alpine.$data(document.getElementById('automations-panel'));
  const itemOf = (id) => [...document.querySelectorAll('li[data-rule-id]')]
    .find((li) => li.getAttribute('data-rule-id') === id) || null;
  const ruleAction = (type, makeCondition) => {
    const hasType = (rule) => (rule.conditions || []).some((c) => c.type === type);
    return {
      run: async ({ waitFor }) => {
        const page = rulesPage();
        if (!page) return null;
        const existing = page.document.rules.find(hasType);
        const created = !existing;
        const id = existing ? existing.id : `regel_${Date.now()}`;
        const before = { expanded: Boolean(page.expanded[id]), editing: Boolean(page.editing[id]) };
        if (existing) {
          page.startEditing(existing);
        } else {
          page.insertEntry('rule', {
            id,
            name: window.I18n.t('onboarding.example_rule'),
            enabled: false,
            history_enabled: true,
            cooldown_seconds: 60,
            conditions: [makeCondition()],
            actions: [],
          });
          page.undo = null;
        }
        const snapshot = JSON.stringify(page.document.rules.find((r) => r.id === id));

        const revert = () => {
          const rules = page.document.rules;
          const rule = rules.find((r) => r.id === id);
          if (!rule || JSON.stringify(rule) !== snapshot) return;
          if (created) {
            rules.splice(rules.findIndex((r) => r.id === id), 1);
            delete page.expanded[id];
            delete page.editing[id];
            delete page.newIds[id];
            return;
          }
          if (!before.editing) page.discardEditing(rule);
          if (!before.expanded) page.expanded[id] = false;
        };

        const li = await waitFor(() => itemOf(id), 1500);
        if (!li) {
          revert();
          return null;
        }
        return { el: li.querySelector('.automation-rule-head') || li, scope: li, revert };
      },
    };
  };

  action('time-rule', ruleAction('time_window', () => ({ type: 'time_window', start: '08:00', end: '18:00', weekdays: [] })));
  action('sun-rule', ruleAction('sun_window', () => ({ type: 'sun_window', from: 'sunrise', from_offset_min: 0, to: 'sunset', to_offset_min: 0, weekdays: [] })));
  action('balance-rule', ruleAction('balance_threshold', () => ({ type: 'balance_threshold', field: 'grid_export', comparison: 'above', threshold: 800, hysteresis: 0, hold_seconds: 300 })));

  // Der Konfigurationsbereich mit dem Dienst Batteriestand. Der Lauf setzt den
  // Dienst in den Daten der Seite (selectedName) und laedt das Formular neu.
  // Ein DOM-Wechsel am Auswahlfeld reicht nicht, Alpine uebernimmt ihn nicht.
  // Zuruecknehmen setzt den alten Dienst wieder, aber nur, solange die Seite
  // noch beim Batteriestand steht.
  const CONFIG_SOC = 'battery_soc_devices';
  const configPage = () => {
    const panel = document.getElementById('config-panel');
    return panel && window.Alpine ? window.Alpine.$data(panel) : null;
  };
  action('battery-soc', {
    run: async ({ waitFor }) => {
      const select = document.getElementById('config-name');
      if (!select) return null;
      const page = await waitFor(() => {
        const data = configPage();
        return data && (data.configs || []).some((c) => c.name === CONFIG_SOC) ? data : null;
      }, 3000);
      if (!page) return null;
      const old = page.selectedName;
      const revert = () => {
        if (!select.isConnected || page.selectedName !== CONFIG_SOC) return;
        page.selectedName = old;
        page.loadConfig();
      };
      page.selectedName = CONFIG_SOC;
      await page.loadConfig();
      const form = await waitFor(() => {
        const el = document.getElementById('schema-form');
        return el && el.children.length > 0 ? el : null;
      }, 1500);
      if (!form) {
        revert();
        return null;
      }
      return { el: select, scope: null, revert };
    },
  });

  // Oeffnet das erste sichtbare Geraet im Detaildialog. Der Dialog kommt aus
  // einem nachgeladenen Skript, deshalb wartet der Lauf laenger. Zuruecknehmen
  // schliesst den Dialog, wenn er noch offen ist.
  action('device-modal', {
    run: async ({ waitFor, show }) => {
      const trigger = [...document.querySelectorAll('.device-detail-trigger')]
        .find((el) => window.obSpotlight.visibleRect(el)) || null;
      if (!trigger) return null;
      const close = () => {
        if (!document.querySelector('#device-modal[open]')) return;
        window.Alpine.$data(document.getElementById('devices-panel')).closeDeviceDetail();
      };
      await show(trigger, { slow: true });
      trigger.click();
      const dialog = await waitFor(() => document.querySelector('#device-modal[open]'), 3000);
      if (!dialog) {
        close();
        return null;
      }
      return { el: dialog, scope: dialog, revert: close };
    },
  });

  // Zeigt einen Demo-Toast, solange der Schritt laeuft. Zuruecknehmen blendet
  // ihn wieder aus. Erscheint das Element nicht, wird der Toast ebenfalls ausgeblendet.
  action('demo-toast', {
    run: async ({ waitFor }) => {
      const store = window.Alpine.store('toasts');
      const id = store.push(window.I18n.t('onboarding.demo_toast'), 'info');
      if (id === null || id === undefined) return null;
      store.hold(id, 'tour');
      const revert = () => store.dismiss(id);
      const el = await waitFor(() => {
        const all = document.querySelectorAll('.notification-toast');
        return all.length > 0 ? all[all.length - 1] : null;
      }, 1500);
      if (!el) {
        revert();
        return null;
      }
      return { el, scope: null, revert };
    },
  });
})();
