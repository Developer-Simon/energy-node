// Verteilt die am Zeilenende uebrigen Rasterspuren auf die breiteste Kachel
// der jeweiligen Zeile. CSS Grid kann das nicht selbst: `span n` ist eine
// feste Zahl, und repeat(auto-fill, ...) bestimmt die Spurenzahl ohne
// Ruecksicht auf spannende Items - passt die naechste Kachel nicht mehr in
// die Zeile, bleibt der Rest leer. Die Rechnung ist rein arithmetisch, der
// DOM-Teil liest nur die Spurenzahl ab und schreibt das Ergebnis als
// Inline-grid-column zurueck.
//
// Die Container-Queries in base.css (.layout-grid-item-N -> 1 / -1, sobald
// N Spuren nicht mehr passen) bleiben als Grundlage bestehen; fillRows()
// klemmt jede Spannweite auf die Spurenzahl und kommt damit auf dasselbe
// Ergebnis, bevor es den Rest verteilt. Wichtig, weil eine Inline-
// Deklaration die Query ueberschreibt.
(() => {
  const SPAN_CLASS = /^layout-grid-item-(\d+|full)$/;

  // Rein: Wunsch-Spannweiten + Spurenzahl -> tatsaechliche Spannweiten.
  // Packt gierig in Zeilen (dieselbe Reihenfolge, die auto-placement im
  // Raster selbst benutzt) und gibt die uebrigen Spuren am Zeilenende an
  // die breiteste Kachel dieser Zeile; bei Gleichstand an die erste davon.
  // Die letzte, unvollstaendige Zeile wird genauso behandelt - dort gibt es
  // zwar keine naechste Kachel, die den Platz beanspruchen koennte, aber
  // der Rest waere genauso verschenkt.
  function fillRows(spans, columns) {
    if (!Number.isFinite(columns) || columns < 1) return spans.slice();
    const result = spans.map(span => Math.min(Math.max(span, 1), columns));
    let rowStart = 0;
    let used = 0;
    const closeRow = end => {
      const leftover = columns - used;
      if (leftover > 0 && end > rowStart) {
        let widest = rowStart;
        for (let i = rowStart + 1; i < end; i++) {
          if (result[i] > result[widest]) widest = i;
        }
        result[widest] += leftover;
      }
      rowStart = end;
      used = 0;
    };
    for (let i = 0; i < result.length; i++) {
      if (used + result[i] > columns) closeRow(i);
      used += result[i];
    }
    closeRow(result.length);
    return result;
  }

  function declaredSpan(item, columns) {
    for (const name of item.classList) {
      const match = SPAN_CLASS.exec(name);
      if (match) return match[1] === 'full' ? columns : Number(match[1]);
    }
    return 1;
  }

  // getComputedStyle liefert die *benutzten* Spuren ("260px 260px ..."), das
  // ist genau die gesuchte Zahl. Sie wird erst gelesen, nachdem die eigenen
  // Inline-Spannweiten geloescht sind: eine noch stehende Spannweite aus
  // einem breiteren Zustand koennte sonst implizite Spuren erzeugen und die
  // Zaehlung nach oben verfaelschen.
  function columnCount(grid, view) {
    const template = view.getComputedStyle(grid).gridTemplateColumns;
    if (!template || template === 'none') return 0;
    return template.trim().split(/\s+/).filter(Boolean).length;
  }

  function apply(grid) {
    const view = grid.ownerDocument.defaultView || window;
    const items = [...grid.children].filter(item => item.classList.contains('layout-grid-item'));
    if (!items.length) return;
    items.forEach(item => item.style.removeProperty('grid-column'));
    const columns = columnCount(grid, view);
    if (!columns) return;
    const filled = fillRows(items.map(item => declaredSpan(item, columns)), columns);
    items.forEach((item, index) => { item.style.gridColumn = `span ${filled[index]}`; });
  }

  function applyAll() {
    document.querySelectorAll('.layout-grid').forEach(apply);
  }

  // Bewusst getrennt von applyAll(): ein erneutes observe() auf einem bereits
  // beobachteten Element loest den Rueckruf noch einmal aus. Wuerde der
  // Rueckruf selbst neu anhaengen, liefe das im Kreis.
  let observer = null;
  function attach() {
    if (window.ResizeObserver && !observer) observer = new ResizeObserver(() => applyAll());
    if (observer) {
      observer.disconnect();
      document.querySelectorAll('.layout-grid').forEach(grid => observer.observe(grid));
    }
    applyAll();
  }

  window.dashboardLayoutFill = {fillRows, applyAll, attach};

  // htmx tauscht #overview-live per outerHTML aus - das Raster darin ist
  // danach ein anderes Element und braucht einen neuen Beobachter. Der
  // ResizeObserver deckt zusaetzlich den Tabwechsel ab: ein Panel wechselt
  // von display:none auf sichtbar, und genau das meldet er als Groessen-
  // aenderung, ohne dass dashboard.js davon wissen muss.
  document.addEventListener('htmx:afterSwap', () => attach());
  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', () => attach(), {once: true});
  } else {
    attach();
  }
})();
