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
    const rec = await def.run({ waitFor });
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

  // Platzhalter bis Task 4: der Go-Test verlangt @time-rule im Katalog.
  action('time-rule', { run: async () => { throw new Error('Task 4'); } });
})();
