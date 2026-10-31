// Spotlight fuer Tourschritte: dunkelt die Seite ab und laesst um das
// Zielelement ein Fenster frei, dazu ein Ring in der Akzentfarbe. Das Fenster
// ist ein clip-path auf der Abdunkelung, kein Rahmen am Element selbst, und
// die ganze Ebene laesst Zeiger durch: das Ziel bleibt bedienbar, der Rest
// der Seite auch. Zwischen zwei Schritten wandert das Fenster mit einer
// kritisch gedaempften Feder vom aktuellen Wert aus. Beim Scrollen folgt es
// dem Ziel 1:1.
(() => {
  const PAD = 8;
  const MAX_RADIUS = 18;

  // Rechteck mit runden Ecken als Loch in einer bildschirmgrossen Flaeche.
  function holePath(width, height, { x, y, w, h }, radius) {
    const r = Math.max(0, Math.min(radius, w / 2, h / 2));
    return `path(evenodd, "M0 0H${width}V${height}H0Z`
      + `M${x + r} ${y}H${x + w - r}A${r} ${r} 0 0 1 ${x + w} ${y + r}`
      + `V${y + h - r}A${r} ${r} 0 0 1 ${x + w - r} ${y + h}`
      + `H${x + r}A${r} ${r} 0 0 1 ${x} ${y + h - r}`
      + `V${y + r}A${r} ${r} 0 0 1 ${x + r} ${y}Z")`;
  }

  // Sichtbar heisst: im Layout und nicht weggeklappt. Ein Ziel ohne Flaeche
  // (x-show, eingeklapptes Menue) zaehlt als fehlend.
  function visibleRect(el) {
    if (!el || !el.isConnected) return null;
    const rect = el.getBoundingClientRect();
    if (rect.width < 2 || rect.height < 2) return null;
    return rect;
  }

  function create(root) {
    const dim = root.querySelector('.ob-spot-dim');
    const ring = root.querySelector('.ob-spot-ring');
    let target = null;
    let radius = 12;
    let lastRect = null;
    let observer = null;
    let pending = 0;
    let lostTimer = 0;
    let wasLost = false;
    let poll = 0;
    let onLost = null;
    let onFound = null;
    let resolve = null;

    const paint = (box) => {
      dim.style.clipPath = holePath(window.innerWidth, window.innerHeight, box, radius);
      ring.style.transform = `translate(${box.x}px, ${box.y}px)`;
      // Breite und Hoehe auf einem freistehenden, fest positionierten Element
      // ohne Kinder (contain: strict): kostet keinen Seiten-Reflow.
      ring.style.width = `${box.w}px`;
      ring.style.height = `${box.h}px`;
      ring.style.borderRadius = `${radius}px`;
    };

    const spring = window.obMotion.createSpring({ response: 0.42, damping: 1, onUpdate: paint });

    const boxOf = (rect) => ({ x: rect.left - PAD, y: rect.top - PAD, w: rect.width + PAD * 2, h: rect.height + PAD * 2 });

    // Scrollen und Groessenaenderung: einmal pro Frame nachmessen. Reine
    // Verschiebung wird 1:1 mitgenommen, eine neue Groesse federt.
    const follow = () => {
      if (pending) return;
      pending = requestAnimationFrame(() => {
        pending = 0;
        if (!target) return;
        // Live-Tausch (Uebersicht, Fragmente): das Element ist ersetzt worden.
        // Ueber den Resolver das neue holen und weiter beobachten.
        if (!target.isConnected && resolve) {
          const fresh = resolve();
          if (fresh) {
            target = fresh;
            observer?.disconnect();
            observer?.observe(target);
          }
        }
        const rect = visibleRect(target);
        if (!rect) {
          // Kurz weg (Tab-Wechsel, Neuaufbau) zaehlt nicht: erst nach 400 ms
          // ausblenden und melden.
          if (!lostTimer) {
            lostTimer = setTimeout(() => {
              root.classList.remove('is-on');
              wasLost = true;
              onLost?.();
            }, 400);
          }
          return;
        }
        clearTimeout(lostTimer);
        lostTimer = 0;
        if (!root.classList.contains('is-on')) {
          spring.set(boxOf(rect));
          root.classList.add('is-on');
        }
        if (wasLost) {
          wasLost = false;
          onFound?.();
        }
        const next = boxOf(rect);
        if (lastRect) spring.shift({ x: rect.left - lastRect.left, y: rect.top - lastRect.top });
        spring.to({ w: next.w, h: next.h });
        lastRect = rect;
      });
    };

    const listen = (on) => {
      const method = on ? 'addEventListener' : 'removeEventListener';
      window[method]('scroll', follow, { capture: true, passive: true });
      window[method]('resize', follow, { passive: true });
      // Taucht ein verlorenes Ziel ohne Scrollen wieder auf (Reiter zurueck),
      // merkt das nur ein langsamer Takt.
      clearInterval(poll);
      if (on) poll = setInterval(() => { if (!root.classList.contains('is-on')) follow(); }, 500);
      observer?.disconnect();
      observer = null;
      if (on && target && 'ResizeObserver' in window) {
        observer = new ResizeObserver(follow);
        observer.observe(target);
      }
    };

    return {
      // Ein neues Ziel: liegt schon ein Fenster, wandert es von dort aus.
      // resolve() findet das Ziel wieder, falls es ersetzt wird.
      show(el, { onLost: lost, onFound: found, resolve: again } = {}) {
        const rect = visibleRect(el);
        if (!rect) return false;
        target = el;
        resolve = again || null;
        onLost = lost;
        onFound = found;
        clearTimeout(lostTimer);
        lostTimer = 0;
        wasLost = false;
        lastRect = rect;
        const style = getComputedStyle(el);
        radius = Math.min(MAX_RADIUS, (parseFloat(style.borderTopLeftRadius) || 6) + PAD);
        root.classList.remove('is-done');
        const box = boxOf(rect);
        const wasOn = root.classList.contains('is-on');
        if (!wasOn || !spring.values) {
          spring.set(box);
          ring.classList.remove('is-arriving');
          void ring.offsetWidth;
          ring.classList.add('is-arriving');
        } else {
          spring.to(box);
        }
        root.classList.add('is-on');
        listen(true);
        return true;
      },
      // Zwischen zwei Schritten: Fenster bleibt sichtbar stehen (die Feder
      // wandert spaeter von hier aus), aber das alte Ziel wird nicht mehr
      // beobachtet und kann nichts mehr als verloren melden.
      hold() {
        clearTimeout(lostTimer);
        lostTimer = 0;
        wasLost = false;
        target = null;
        resolve = null;
        lastRect = null;
        listen(false);
      },
      hide() {
        clearTimeout(lostTimer);
        lostTimer = 0;
        wasLost = false;
        target = null;
        resolve = null;
        lastRect = null;
        listen(false);
        root.classList.remove('is-on', 'is-done');
      },
      // Bestaetigung beim Abhaken: Ring wechselt auf die Erledigt-Farbe und
      // pulsiert einmal nach aussen.
      done() {
        root.classList.remove('is-done');
        void ring.offsetWidth;
        root.classList.add('is-done');
      },
      // Leiste und Spotlight teilen sich den Bildschirm: liefert das
      // aktuelle Zielrechteck, damit die Leiste ausweichen kann.
      rect: () => (target ? visibleRect(target) : null),
      element: () => target,
      refresh: follow,
    };
  }

  window.__obSpotlight = { holePath };
  window.obSpotlight = { create, visibleRect };
})();
