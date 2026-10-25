// Blatt fuer den Assistenten unter 900 px: Ziehen am Griff folgt dem Finger,
// nach oben wirkt ein Widerstand, beim Loslassen schliesst es oder federt zurueck.
// Gleicher Weg wie .settings-sheet: der Griff faengt den Zeiger, nie die Liste.
(() => {
  const SPRING = 'transform 420ms cubic-bezier(.32, .72, 0, 1)';
  const DECEL = 0.998;
  const MOVES = 5;

  // Apples Widerstand: nach oben wird die Verschiebung gedaempft, je weiter desto weniger.
  function rubber(over, dim, c = 0.55) {
    return (over * dim * c) / (dim + c * Math.abs(over));
  }

  // Strecke in px, die ein Schwung mit v px/s nachlaeuft.
  function project(velocityPxPerS, decel = DECEL) {
    return (velocityPxPerS / 1000) * decel / (1 - decel);
  }

  // Schliessen, wenn der Weg mit dem Nachlauf ueber die halbe Hoehe reicht.
  function shouldClose(dy, velocityPxPerS, height) {
    return dy + project(velocityPxPerS) > height * 0.5;
  }

  // Haengt die Geste an den Griff. Gibt detach() zurueck, das alles wieder loest.
  function attach(grabEl, sheetEl, { onClose, now = () => Date.now() } = {}) {
    let startY = null;
    let moves = [];
    let springTimer = null;

    const reduced = () => Boolean(window.matchMedia && window.matchMedia('(prefers-reduced-motion: reduce)').matches);
    const height = () => sheetEl.offsetHeight || 600;

    // Geschwindigkeit in px/s aus den letzten fuenf Bewegungen.
    const velocity = () => {
      if (moves.length < 2) return 0;
      const first = moves[0];
      const last = moves[moves.length - 1];
      const elapsed = last.t - first.t;
      return elapsed > 0 ? ((last.y - first.y) / elapsed) * 1000 : 0;
    };

    // Ohne Schliessen: zurueck an den Platz, mit der Federkurve, dann Stil zuruecksetzen.
    const spring = () => {
      clearTimeout(springTimer);
      sheetEl.style.transition = reduced() ? '' : SPRING;
      sheetEl.style.transform = '';
      springTimer = setTimeout(() => {
        sheetEl.style.transition = '';
        springTimer = null;
      }, reduced() ? 0 : 420);
    };

    const onDown = (event) => {
      startY = event.clientY;
      moves = [];
      grabEl.setPointerCapture?.(event.pointerId);
    };

    const onMove = (event) => {
      if (startY === null) return;
      const dy = event.clientY - startY;
      moves.push({ t: now(), y: event.clientY });
      if (moves.length > MOVES) moves.shift();
      const offset = dy < 0 ? rubber(dy, height()) : dy;
      sheetEl.style.transition = 'none';
      sheetEl.style.transform = `translateY(${offset}px)`;
    };

    const onUp = (event) => {
      if (startY === null) return;
      const dy = event.clientY - startY;
      const speed = velocity();
      startY = null;
      if (shouldClose(dy, speed, height())) {
        sheetEl.style.transition = '';
        onClose?.();
        return;
      }
      spring();
    };

    const onCancel = () => {
      if (startY === null) return;
      startY = null;
      spring();
    };

    grabEl.addEventListener('pointerdown', onDown);
    grabEl.addEventListener('pointermove', onMove);
    grabEl.addEventListener('pointerup', onUp);
    grabEl.addEventListener('pointercancel', onCancel);

    return () => {
      grabEl.removeEventListener('pointerdown', onDown);
      grabEl.removeEventListener('pointermove', onMove);
      grabEl.removeEventListener('pointerup', onUp);
      grabEl.removeEventListener('pointercancel', onCancel);
      clearTimeout(springTimer);
    };
  }

  window.__automationSheet = { rubber, project, shouldClose, attach };
})();
