(() => {
  const requestJSON = async (url, options) => {
    // Einziger Ort mit einem URL-Literal in dieser Datei: hinter einem
    // Reverse-Proxy-Unterpfad legt base.html das Praefix in
    // __DASHBOARD_BASE_PATH__, bei Direktzugriff ist es leer.
    const response = await fetch(`${window.__DASHBOARD_BASE_PATH__ || ''}${url}`, options);
    const body = await response.json();
    if (!response.ok) throw new Error(body.message || "Anfrage fehlgeschlagen");
    return body;
  };

  // Wiederverwendbare Revisionsverwaltung.
  //
  // config.basePath  API-Basis ohne abschliessenden Schraegstrich,
  //                  z. B. '/api/v1/layout', oder eine Funktion, die diese
  //                  live liefert, wenn die besitzende Seite zwischen
  //                  mehreren Konfigurationen umschalten kann. Die
  //                  Komponente haengt '/revisions', '/revisions/{name}'
  //                  und '/restore' an.
  // config.current   Funktion, die den aktuell im Editor stehenden Wert
  //                  liefert. Wird nur fuer den Diff gebraucht.
  // config.reload    Funktion, die die besitzende Seite nach einem Restore
  //                  neu laedt.
  // config.label     Optionale Beschriftung im Ausklapper.
  const revisionPanel = (config = {}) => ({
    // config.basePath kann ein fester String oder - wenn die besitzende
    // Seite zwischen mehreren Konfigurationen umschalten kann (z. B. die
    // Datei-Auswahl auf der Konfigurationsseite) - eine Funktion sein, die
    // jedes Mal live ausgewertet wird statt beim Erzeugen der Komponente
    // einmalig eingefroren zu werden.
    getBasePath: typeof config.basePath === 'function' ? config.basePath : () => config.basePath || '',
    currentValue: typeof config.current === 'function' ? config.current : () => null,
    reloadOwner: typeof config.reload === 'function' ? config.reload : async () => {},
    label: config.label || 'Revisionen',

    revisions: [],
    selectedRevision: '',
    revisionText: '',
    revisionView: 'diff',
    cropDiff: true,
    open: false,
    loading: false,
    restoring: false,
    loaded: false,

    get basePath() {
      return this.getBasePath();
    },

    // Wechselt die besitzende Seite die zugrunde liegende Konfiguration
    // (basePath aendert sich), ist die geladene Revisionsliste fuer die neu
    // ausgewaehlte Datei nicht mehr gueltig - Panel schliessen und Zustand
    // verwerfen, damit ein erneutes Aufklappen frisch laedt.
    init() {
      this.$watch('basePath', () => this.reset());
    },

    reset() {
      this.open = false;
      this.loaded = false;
      this.revisions = [];
      this.selectedRevision = '';
      this.revisionText = '';
      this.clearDiff();
    },

    // Erst beim Aufklappen laden: die Liste liest jede Revisionsdatei, um
    // ihre Pruefsumme zu bilden. Auf dem Pi ist das nichts, was bei jedem
    // Seitenaufbau nebenbei passieren soll.
    async toggle() {
      this.open = !this.open;
      if (this.open && !this.loaded) await this.load();
    },

    async load() {
      if (!this.basePath) return;
      this.loading = true;
      try {
        this.revisions = await requestJSON(`${this.basePath}/revisions`);
        this.loaded = true;
        this.selectedRevision = '';
        this.revisionText = '';
        this.clearDiff();
      } catch (error) {
        this.$store.toasts.push(error.message, 'critical');
      } finally {
        this.loading = false;
      }
    },

    // Von der besitzenden Seite nach jedem Speichern aufzurufen, damit die
    // Liste nicht veraltet - aber nur, wenn sie ueberhaupt schon geladen ist.
    async refresh() {
      if (this.loaded) await this.load();
    },

    clearDiff() {
      if (this.$refs.revisionDiff) this.$refs.revisionDiff.replaceChildren();
    },

    async previewRevision() {
      if (!this.selectedRevision) {
        this.revisionText = '';
        this.clearDiff();
        return;
      }
      try {
        const value = await requestJSON(`${this.basePath}/revisions/${encodeURIComponent(this.selectedRevision)}`);
        this.revisionText = JSON.stringify(value, null, 2);
        this.renderRevisionDiff(value, this.currentValue());
      } catch (error) {
        this.$store.toasts.push(error.message, 'critical');
      }
    },

    setRevisionView(view) {
      this.revisionView = view;
      if (this.selectedRevision) this.previewRevision();
    },

    renderRevisionDiff(revisionValue, currentValue) {
      const revisionLines = JSON.stringify(revisionValue, null, 2).split('\n');
      const currentLines = JSON.stringify(currentValue, null, 2).split('\n');
      const table = Array.from({length: revisionLines.length + 1}, () => Array(currentLines.length + 1).fill(0));
      for (let revisionIndex = revisionLines.length - 1; revisionIndex >= 0; revisionIndex--) for (let currentIndex = currentLines.length - 1; currentIndex >= 0; currentIndex--) table[revisionIndex][currentIndex] = revisionLines[revisionIndex] === currentLines[currentIndex] ? table[revisionIndex + 1][currentIndex + 1] + 1 : Math.max(table[revisionIndex + 1][currentIndex], table[revisionIndex][currentIndex + 1]);
      const result = [{text: '--- Revision', className: 'diff-removed', reference: true}, {text: '+++ Aktueller Stand', className: 'diff-added', reference: true}];
      let revisionIndex = 0; let currentIndex = 0; let revisionLine = 1; let currentLine = 1;
      while (revisionIndex < revisionLines.length || currentIndex < currentLines.length) {
        if (revisionIndex < revisionLines.length && currentIndex < currentLines.length && revisionLines[revisionIndex] === currentLines[currentIndex]) { result.push({text: `  ${revisionLines[revisionIndex]}`, className: '', oldLine: revisionLine++, newLine: currentLine++}); revisionIndex++; currentIndex++; }
        else if (currentIndex < currentLines.length && (revisionIndex === revisionLines.length || table[revisionIndex][currentIndex + 1] >= table[revisionIndex + 1][currentIndex])) { result.push({text: `+ ${currentLines[currentIndex]}`, className: 'diff-added', oldLine: null, newLine: currentLine++}); currentIndex++; }
        else { result.push({text: `- ${revisionLines[revisionIndex]}`, className: 'diff-removed', oldLine: revisionLine++, newLine: null}); revisionIndex++; }
      }
      const lines = this.cropDiff ? this.cropDiffLines(result) : result;
      this.$refs.revisionDiff.replaceChildren(...lines.map(line => {
        const node = document.createElement('span');
        node.className = `diff-line ${line.className}`;
        if (line.reference) node.append(document.createElement('span'), document.createElement('span'));
        else {
          const oldReference = document.createElement('span'); oldReference.className = 'diff-reference'; oldReference.textContent = line.oldLine ?? '';
          const newReference = document.createElement('span'); newReference.className = 'diff-reference'; newReference.textContent = line.newLine ?? '';
          node.append(oldReference, newReference);
        }
        const text = document.createElement('span'); text.textContent = `${line.text}\n`; node.append(text);
        return node;
      }));
    },

    cropDiffLines(result) {
      const changed = result.map(line => line.className === 'diff-added' || line.className === 'diff-removed');
      const content = text => text.replace(/^\s*[+\- ]?\s?/, '');
      const indent = text => content(text).search(/\S|$/);
      const isID = text => /^"id"\s*:/.test(content(text));
      const isObjectEnd = text => /^[}]\s*,?$/.test(content(text));
      const changedHasID = result.some((line, index) => changed[index] && isID(line.text));
      const idNearChange = index => result.some((line, changeIndex) => {
        if (!changed[changeIndex] || changeIndex === index) return false;
        const first = Math.min(index, changeIndex); const last = Math.max(index, changeIndex); const idIndent = indent(result[index].text);
        return !result.slice(first + 1, last).some(candidate => isObjectEnd(candidate.text) && indent(candidate.text) <= idIndent);
      });
      const keep = result.map((line, index) => line.reference || changed[index] || (isID(line.text) && !changedHasID && idNearChange(index)) || changed[index - 1] || changed[index + 1]);
      const marked = [];
      result.forEach((line, index) => { if (keep[index]) marked.push(line); else if (marked.length && marked[marked.length - 1].className !== 'diff-cropped') marked.push({text: '...', className: 'diff-cropped', reference: true}); });
      return marked;
    },

    async restoreRevision() {
      if (!this.selectedRevision) return;
      const confirmed = await this.$store.modal.confirm({
        title: 'Revision wiederherstellen?',
        body: 'Der aktuelle Stand wird überschrieben. Er bleibt als neue Revision erhalten.',
        confirmLabel: 'Wiederherstellen',
        danger: true,
      });
      if (!confirmed) return;
      this.restoring = true;
      try {
        await requestJSON(`${this.basePath}/restore`, {
          method: 'POST',
          headers: {'Content-Type': 'application/json'},
          body: JSON.stringify({revision: this.selectedRevision}),
        });
        this.$store.toasts.push('Revision wiederhergestellt.');
        await this.reloadOwner();
        await this.load();
      } catch (error) {
        this.$store.toasts.push(error.message, 'critical');
      } finally {
        this.restoring = false;
      }
    },
  });

  const register = () => {
    if (window.Alpine) window.Alpine.data("revisionPanel", revisionPanel);
  };
  if (window.Alpine) register(); else document.addEventListener("alpine:init", register, {once: true});
})();
