// Mauerwerk fuer die Karten der Einstellungs-Tabs (.settings-cards). Ein
// normales Raster macht jede Zeile so hoch wie ihre hoechste Karte; steht
// links eine hohe Karte neben einer kurzen, bleibt unter der kurzen ein Loch.
// Hier bekommt das Raster 1px-Zeilen ohne Zeilenabstand, und jede Karte
// spannt so viele Zeilen, wie sie samt Abstand hoch ist. Das vorhandene
// grid-auto-flow: dense legt dann jede Karte in die Spalte, die gerade weiter
// oben endet: rechts wird gestapelt, bis die Spalte die linke ueberholt, dann
// geht es links weiter. Die DOM-Reihenfolge (Tab-Reihenfolge, Screenreader)
// bleibt unangetastet.
//
// Erst attach() setzt .settings-cards--masonry. Vorher, oder ganz ohne
// ResizeObserver, bleibt das normale Raster stehen.
(() => {
  // Rein: gemessene Hoehe samt Aussenabstaenden + Spaltenabstand -> Spannweite
  // in 1px-Zeilen. Der Abstand haengt unten an jeder Karte; den der letzten
  // gleicht der negative margin-bottom in settings-controls.css aus.
  function rowSpan(height, gap) {
    if (!Number.isFinite(height) || height <= 0) return 0;
    return Math.ceil(height + (Number.isFinite(gap) && gap > 0 ? gap : 0));
  }

  function outerHeight(item, view) {
    const style = view.getComputedStyle(item);
    return item.offsetHeight + (parseFloat(style.marginTop) || 0) + (parseFloat(style.marginBottom) || 0);
  }

  // Alle Karten zuerst messen, dann schreiben: eine neue Spannweite aendert
  // die Hoehe einer Karte nicht (align-items: start), nur ihre Lage.
  function apply(grid) {
    const view = grid.ownerDocument.defaultView || window;
    const gap = parseFloat(view.getComputedStyle(grid).columnGap) || 0;
    const items = [...grid.children];
    const spans = items.map(item => rowSpan(outerHeight(item, view), gap));
    // Eine ausgeblendete Karte (x-show) hat Hoehe 0 und wird ohnehin nicht
    // platziert; ihr ResizeObserver meldet sich, sobald sie sichtbar wird.
    items.forEach((item, index) => {
      if (spans[index]) item.style.gridRowEnd = `span ${spans[index]}`;
      else item.style.removeProperty('grid-row-end');
    });
  }

  // Ein Beobachter pro Raster: der ResizeObserver auf jeder Karte deckt
  // Inhalt, Breite und Tabwechsel (display:none -> sichtbar) ab, der
  // MutationObserver Karten, die Alpine per x-for/x-if nachtraegt.
  function attachGrid(grid) {
    if (grid.classList.contains('settings-cards--masonry')) return;
    const view = grid.ownerDocument.defaultView || window;
    if (typeof view.ResizeObserver !== 'function') return;
    grid.classList.add('settings-cards--masonry');
    let frame = 0;
    const schedule = () => {
      if (frame) return;
      frame = view.requestAnimationFrame(() => { frame = 0; apply(grid); });
    };
    const resize = new view.ResizeObserver(schedule);
    const observeChildren = () => {
      resize.disconnect();
      [...grid.children].forEach(item => resize.observe(item));
      schedule();
    };
    if (typeof view.MutationObserver === 'function') {
      new view.MutationObserver(observeChildren).observe(grid, {childList: true});
    }
    observeChildren();
    apply(grid);
  }

  function attach(root) {
    (root || document).querySelectorAll('.settings-cards').forEach(attachGrid);
  }

  window.dashboardSettingsMasonry = {rowSpan, apply, attach};
})();
