// Verhalten der Toasts am Zeiger: Wegwischen nach rechts mit 1:1-Verfolgung,
// Gummiband nach links, Ablauf anhalten bei Hover, Fokus und Wischen.
// Registriert die Alpine-Direktive x-toast-gesture="toast". Die Zustaende
// selbst (offen, geht, Restzeit) liegen im Store in notify.js; hier wird nur
// gemessen und weitergereicht. Die reinen Rechenfunktionen stehen unter
// window.__toastGesture, damit die node --test-Suite sie ohne DOM pruefen kann.
(() => {
  // Wie beim Scrollen: wohin liefe der Toast aus, wenn man ihn jetzt losliesse.
  const DECELERATION = 0.998;
  // Ab dieser Strecke gilt eine Bewegung als Wischen und nicht als Klick.
  const SLOP_PX = 6;
  const HISTORY = 5;
  const FLY_MIN_MS = 120;
  const FLY_MAX_MS = 260;

  function project(velocity, rate = DECELERATION) {
    return (velocity / 1000) * rate / (1 - rate);
  }

  // Je weiter ueber die Grenze, desto weniger folgt der Toast dem Zeiger.
  function rubberband(offset, dimension, constant = 0.55) {
    return (offset * dimension * constant) / (dimension + constant * Math.abs(offset));
  }

  // Entscheidung nach dem Endpunkt, auf den der Schwung hinauslaeuft, nicht
  // nach dem Loslasspunkt: ein kurzer schneller Wisch zaehlt, ein langsames
  // Schieben bis zur Haelfte und Zurueckziehen nicht.
  function shouldDismiss(offset, velocity, width) {
    return offset + project(velocity) > width * 0.5;
  }

  function releaseVelocity(history) {
    if (history.length < 2) return 0;
    const first = history[0];
    const last = history[history.length - 1];
    return last.t > first.t ? ((last.x - first.x) / (last.t - first.t)) * 1000 : 0;
  }

  window.__toastGesture = { project, rubberband, shouldDismiss, releaseVelocity };

  document.addEventListener('alpine:init', () => {
    window.Alpine.directive('toast-gesture', (el, { expression }, { evaluate, effect, cleanup }) => {
      const store = () => window.Alpine.store('toasts');
      const id = () => evaluate(expression).id;

      let pointerId = null;
      let startX = 0;
      let offset = 0;
      let dragging = false;
      let history = [];

      // Eine wiederholte Meldung pulsiert kurz, damit man sieht, dass sie
      // erneut kam. Bei reduzierter Bewegung bleibt es beim Zaehler.
      let lastRun = evaluate(expression).run;
      effect(() => {
        const { run } = evaluate(expression);
        if (run === lastRun) return;
        lastRun = run;
        if (!el.animate || window.matchMedia('(prefers-reduced-motion: reduce)').matches) return;
        el.animate(
          [{ scale: 1 }, { scale: 1.03 }, { scale: 1 }],
          { duration: 260, easing: 'cubic-bezier(0.23, 1, 0.32, 1)' },
        );
      });

      const hold = (reason) => store().hold(id(), reason);
      const release = (reason) => store().release(id(), reason);

      const onEnter = (event) => { if (event.pointerType !== 'touch') hold('hover'); };
      const onLeave = (event) => { if (event.pointerType !== 'touch') release('hover'); };
      const onFocusIn = () => hold('focus');
      const onFocusOut = () => release('focus');

      const onDown = (event) => {
        if (event.button !== 0 || event.target.closest('button')) return;
        pointerId = event.pointerId;
        startX = event.clientX;
        history = [{ x: event.clientX, t: event.timeStamp }];
        dragging = false;
      };

      const onMove = (event) => {
        if (event.pointerId !== pointerId) return;
        const dx = event.clientX - startX;
        if (!dragging) {
          if (Math.abs(dx) < SLOP_PX) return;
          dragging = true;
          el.setPointerCapture(pointerId);
          el.classList.add('is-dragging');
          hold('drag');
        }
        // Der Toast bleibt dort unter dem Finger, wo man ihn gegriffen hat.
        offset = dx > 0 ? dx : rubberband(dx, el.offsetWidth);
        el.style.transform = `translate3d(${offset}px, 0, 0)`;
        history.push({ x: event.clientX, t: event.timeStamp });
        if (history.length > HISTORY) history.shift();
      };

      const onEnd = (event) => {
        if (event.pointerId !== pointerId) return;
        pointerId = null;
        if (!dragging) return;
        dragging = false;
        el.classList.remove('is-dragging');
        const velocity = releaseVelocity(history);
        const width = el.offsetWidth;
        const current = offset;
        offset = 0;
        if (event.type !== 'pointercancel' && shouldDismiss(current, velocity, width)) {
          // Der Rest des Wegs laeuft mit der Geschwindigkeit des Fingers weiter.
          const rest = width + 24 - current;
          const ms = velocity > 0
            ? Math.min(FLY_MAX_MS, Math.max(FLY_MIN_MS, (rest / velocity) * 1000 * 1.6))
            : FLY_MAX_MS;
          el.style.transition = `transform ${ms}ms cubic-bezier(.2, .7, .4, 1), opacity ${ms}ms linear`;
          el.style.transform = 'translate3d(calc(100% + 24px), 0, 0)';
          el.style.opacity = '0';
          store().dismiss(id(), 'swipe');
        } else {
          el.style.transition = 'transform .42s cubic-bezier(.32, .72, 0, 1)';
          el.style.transform = '';
          setTimeout(() => { el.style.transition = ''; }, 440);
        }
        release('drag');
      };

      el.addEventListener('pointerenter', onEnter);
      el.addEventListener('pointerleave', onLeave);
      el.addEventListener('focusin', onFocusIn);
      el.addEventListener('focusout', onFocusOut);
      el.addEventListener('pointerdown', onDown);
      el.addEventListener('pointermove', onMove);
      el.addEventListener('pointerup', onEnd);
      el.addEventListener('pointercancel', onEnd);

      cleanup(() => {
        el.removeEventListener('pointerenter', onEnter);
        el.removeEventListener('pointerleave', onLeave);
        el.removeEventListener('focusin', onFocusIn);
        el.removeEventListener('focusout', onFocusOut);
        el.removeEventListener('pointerdown', onDown);
        el.removeEventListener('pointermove', onMove);
        el.removeEventListener('pointerup', onEnd);
        el.removeEventListener('pointercancel', onEnd);
      });
    });
  });
})();
