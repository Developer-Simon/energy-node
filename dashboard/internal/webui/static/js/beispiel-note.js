// Hinweis zum Beispiel oben auf der Seite (base.html, data-beispiel-note).
// "Simulation aus" und "Simulation aus und Beispiel entfernen" rufen
// POST /api/v1/beispiel/stop auf. Ohne behaltene Eintraege wird die Seite neu
// geladen. Behaltene Eintraege (vom Nutzer veraendert) stehen dagegen im
// Hinweis, damit der Nutzer sie sieht.
(() => {
  const basePath = () => window.__DASHBOARD_BASE_PATH__ || '';

  // Eintraege kommen als "kind:name", angezeigt wird nur der Name.
  const stripKind = (entry) => {
    const i = entry.indexOf(':');
    return i >= 0 ? entry.slice(i + 1) : entry;
  };

  const defaultDeps = {
    fetch: (url, options) => window.fetch(url, options),
    reload: () => window.location.reload(),
  };

  const requestJSON = async (deps, url, options) => {
    const response = await deps.fetch(url, options);
    const body = await response.json().catch(() => ({}));
    if (!response.ok) throw new Error(`HTTP ${response.status}`);
    return body;
  };

  // Der CSRF-Token kommt wie auf den anderen Seiten aus GET /api/v1/auth/session.
  const stopExample = async (deps, remove) => {
    const session = await requestJSON(deps, `${basePath()}/api/v1/auth/session`);
    return requestJSON(deps, `${basePath()}/api/v1/beispiel/stop`, {
      method: 'POST',
      headers: {
        'X-CSRF-Token': (session && session.csrf_token) || '',
        'Content-Type': 'application/json',
      },
      body: JSON.stringify({ remove }),
    });
  };

  const bind = (root, deps = defaultDeps) => {
    const keptTemplate = root.dataset.beispielKept || '';
    const failedText = root.dataset.beispielFailed || '';
    const msg = root.querySelector('[data-beispiel-msg]');
    const buttons = Array.from(root.querySelectorAll('[data-beispiel-off], [data-beispiel-remove]'));
    let busy = false;

    const show = (text) => {
      if (!msg) return;
      msg.textContent = text;
      msg.hidden = false;
    };
    const hide = () => {
      if (msg) msg.hidden = true;
    };

    const run = async (remove) => {
      if (busy) return;
      busy = true;
      buttons.forEach((b) => { b.disabled = true; });
      hide();
      try {
        const body = await stopExample(deps, remove);
        const kept = Array.isArray(body && body.kept) ? body.kept.filter(Boolean) : [];
        if (kept.length > 0) {
          const names = kept.map(stripKind).join(', ');
          show(keptTemplate.replace('{names}', () => names));
        } else {
          deps.reload();
        }
      } catch (error) {
        show(failedText);
      } finally {
        busy = false;
        buttons.forEach((b) => { b.disabled = false; });
      }
    };

    buttons.forEach((b) => {
      if (b.hasAttribute('data-beispiel-off')) b.addEventListener('click', () => run(false));
      if (b.hasAttribute('data-beispiel-remove')) b.addEventListener('click', () => run(true));
    });
  };

  window.beispielNote = { bind, stripKind };

  const root = document.querySelector('[data-beispiel-note]');
  if (root) bind(root);
})();
