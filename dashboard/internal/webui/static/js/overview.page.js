(() => {
  // Bewusst winzig: diese Datei laedt mit JEDEM Aufruf der Uebersicht. Alles,
  // was mehr kann als den Modus umlegen, gehoert in layout-editor.js, das erst
  // beim ersten Editieren nachkommt.
  // Der zuletzt erzeugte overviewShell(). layout-editor.js' "Speichern &
  // schliessen" feuert layout-editor:request-leave an document, und die Huelle
  // ruft daraufhin leaveEdit() - save() hat unsaved schon geleert, der
  // Waechter fragt also nicht erneut.
  let shell;

  // Spaltenzahl aus der Breite - dieselbe auto-fill-Rechnung wie
  // repeat(auto-fill, minmax(18rem, 1fr)) mit gap .8rem in base.css.
  const columnsFor = width => {
    const rem = parseFloat(getComputedStyle(document.documentElement).fontSize) || 16;
    return Math.max(1, Math.floor((width + .8 * rem) / (18 * rem + .8 * rem)));
  };

  // Groessenklasse -> belegte Spuren. Bildet die Container-Queries nach, die
  // jede Klasse oberhalb der Spaltenzahl auf 1 / -1 zurueckfallen lassen.
  const tracksFor = (span, columns) => (span === 'full' ? columns : Math.min(Number(span) || 1, columns));

  // Bricht die Kacheln in Zeilen und dehnt in jeder unvollstaendigen Zeile die
  // groesste Kachel in den Rest - bei Gleichstand die letzte. Nur die Ansicht
  // tut das: im Editor soll man sehen, was eingestellt ist, Luecken inklusive.
  const fillRows = (spans, columns) => {
    const tracks = spans.map(span => tracksFor(span, columns));
    let start = 0, used = 0;
    const closeRow = end => {
      const rest = columns - used;
      if (rest <= 0 || end <= start) return;
      let pick = start;
      for (let i = start; i < end; i++) if (tracks[i] >= tracks[pick]) pick = i;
      tracks[pick] += rest;
    };
    for (let i = 0; i < tracks.length; i++) {
      if (used + tracks[i] > columns) { closeRow(i); start = i; used = 0; }
      used += tracks[i];
    }
    closeRow(tracks.length);
    return tracks;
  };

  const register = () => {
    document.addEventListener('layout-editor:request-leave', () => shell?.leaveEdit());
    const overviewShell = () => (shell = {
      mode: 'view',
      busy: false,
      error: '',
      // Deklariert, damit Alpines mergeProxies() die Zuweisung nicht in den
      // aeusseren Bereich (dashboardShell) durchreicht - dort ueberlebte sie
      // jeden Fragment-Tausch.
      _fillObserver: null,
      _onSwap: null,

      // Der Aufruf geht an die Huelle (dashboardShell); im Test wird er ersetzt.
      editorAssetsReady() { return window.__dashboardShell__.editorAssetsReady(); },

      // Zeilenfuellung auf die aktuellen Kacheln anwenden. Im Bearbeitungsmodus
      // wird nur zurueckgesetzt - dort gilt die Einstellung, nicht die Optik.
      applyRowFill(columns) {
        const panel = document.getElementById('overview-panel');
        const grid = panel?.querySelector('.layout-grid');
        if (!grid) return;
        const cards = [...grid.querySelectorAll('.layout-grid-item')];
        if (panel.dataset.mode === 'edit') {
          for (const card of cards) card.style.removeProperty('grid-column');
          return;
        }
        const cols = columns || columnsFor(grid.clientWidth);
        const spans = cards.map(card => (card.className.match(/layout-grid-item-(\d+|full)/) || [, '1'])[1]);
        const tracks = fillRows(spans, cols);
        cards.forEach((card, i) => {
          if (tracks[i] >= cols) card.style.gridColumn = '1 / -1';
          else card.style.gridColumn = `span ${tracks[i]}`;
        });
      },

      // Neu rechnen, wenn sich die Spaltenzahl aendern kann oder die Karten
      // ausgetauscht wurden (htmx-Swap auf #overview-live).
      watchRowFill() {
        // Diese Huelle steht IN #overview-live: jeder Fragment-Tausch
        // (Seitenwechsel, Live-Auffrischung) erzeugt sie neu, waehrend
        // #overview-panel und #layout-editor-root stehen bleiben. Der Modus
        // haengt darum am Panel, nicht an dieser Huelle - von dort kommt er
        // zurueck, und der Editor haengt sich auf das frische Raster.
        const panel = document.getElementById('overview-panel');
        if (panel?.dataset.mode === 'edit') {
          this.mode = 'edit';
          document.dispatchEvent(new CustomEvent('layout-editor:unmount'));
          document.dispatchEvent(new CustomEvent('layout-editor:mount', {detail: {root: panel}}));
        }
        const run = () => this.applyRowFill();
        run();
        if (window.ResizeObserver) {
          const grid = document.querySelector('.layout-grid');
          if (grid) { this._fillObserver = new ResizeObserver(run); this._fillObserver.observe(grid); }
        }
        this._onSwap = run;
        document.body?.addEventListener('htmx:afterSwap', this._onSwap);
      },

      // Diese Huelle steht in #overview-live und stirbt bei JEDEM Fragment-
      // Tausch (Seitenwechsel, Live-Auffrischung); Alpine ruft destroy() dabei
      // auf. Ohne das Aufraeumen sammelten sich mit jedem Wechsel ein
      // ResizeObserver und ein htmx:afterSwap-Zuhoerer mehr an, die alle
      // weiter auf das jeweils aktuelle Raster losgingen - nach ein paar
      // Seitenwechseln rechnete die Zeilenfuellung ein Dutzend Mal je Anstrich.
      destroy() {
        this._fillObserver?.disconnect?.();
        this._fillObserver = null;
        if (this._onSwap) document.body?.removeEventListener('htmx:afterSwap', this._onSwap);
        this._onSwap = null;
      },

      async enterEdit() {
        if (this.mode === 'edit' || this.busy) return;
        this.busy = true;
        this.error = '';
        try {
          await this.editorAssetsReady();
        } catch (cause) {
          // Stehenbleiben statt halb umschalten - ohne Gridstack gibt es
          // keinen Editor, nur ein kaputtes Raster.
          this.error = 'Der Editor konnte nicht geladen werden. Verbindung pruefen und erneut versuchen.';
          return;
        } finally {
          this.busy = false;
        }
        // Das Editor-Fragment (Toolbox, Options-Modal, Waechter-Modal,
        // edit-only-Werkzeugleiste, Revisionen) steht nicht in der Seite -
        // es kommt hier per GET und wird als letztes Kind von #overview-panel
        // eingesetzt. Der Wurzelknoten traegt x-data="layoutEditor()"; erst
        // Alpine.initTree() laesst dessen init() laufen (bindet die
        // mount/unmount-Ereignisse, setzt window.__layoutEditor__) und x-init
        // ruft load(). Danach - nicht vorher - feuert layout-editor:mount.
        const panel = document.getElementById('overview-panel');
        if (!panel) return;
        if (!panel.querySelector('#layout-editor-root')) {
          let markup;
          try {
            const response = await fetch(panel.dataset.editorFragment, {headers: {'X-Requested-With': 'XMLHttpRequest'}});
            if (!response.ok) throw new Error(String(response.status));
            markup = await response.text();
          } catch (cause) {
            this.error = 'Der Editor konnte nicht geladen werden. Verbindung pruefen und erneut versuchen.';
            return;
          }
          const root = document.createElement('div');
          root.id = 'layout-editor-root';
          root.setAttribute('x-data', 'layoutEditor()');
          root.setAttribute('x-init', 'load()');
          root.innerHTML = markup;
          panel.appendChild(root);
          window.Alpine?.initTree(root);
        }
        this.mode = 'edit';
        panel.dataset.mode = 'edit';
        this.applyRowFill();
        document.dispatchEvent(new CustomEvent('layout-editor:mount', {detail: {root: panel}}));
      },

      async leaveEdit() {
        if (this.mode !== 'edit') return;
        const editor = window.__layoutEditor__;
        if (editor && editor.unsaved && !(await editor.confirmLeave('mode'))) return;
        this.mode = 'view';
        const panel = document.getElementById('overview-panel');
        if (!panel) return;
        panel.dataset.mode = 'view';
        document.dispatchEvent(new CustomEvent('layout-editor:unmount'));
        this.applyRowFill();
        panel.querySelector('#layout-editor-root')?.remove();
      },
    });
    overviewShell.fillRows = fillRows;
    overviewShell.columnsFor = columnsFor;
    window.Alpine.data('overviewShell', overviewShell);
  };
  if (window.Alpine) register();
  else document.addEventListener('alpine:init', register);
})();
