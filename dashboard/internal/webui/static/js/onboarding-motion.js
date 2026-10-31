// Bewegung fuer Leiste und Spotlight: eine Feder nach Apples Modell
// (Daempfung + Antwortzeit statt Masse/Steifigkeit). Sie startet immer vom
// Wert, der gerade auf dem Schirm steht, und behaelt beim Umzielen ihre
// Geschwindigkeit. Deshalb kann man jede Bewegung mitten im Lauf greifen oder
// umlenken, ohne Sprung. Keine Bibliothek: ein rAF-Takt fuer alle Werte einer
// Feder. Die reinen Rechenfunktionen stehen unter window.__obMotion fuer die
// node --test-Suite.
(() => {
  // Wie beim Scrollen: wohin liefe eine Bewegung aus, wenn man jetzt losliesse.
  const DECELERATION = 0.998;

  function project(velocity, rate = DECELERATION) {
    return (velocity / 1000) * rate / (1 - rate);
  }

  // Je weiter ueber die Grenze, desto weniger folgt die Flaeche dem Finger.
  function rubberband(offset, dimension, constant = 0.55) {
    return (offset * dimension * constant) / (dimension + constant * Math.abs(offset));
  }

  // Geschwindigkeit in px/s aus den letzten Zeigerpunkten {v, t}.
  function releaseVelocity(history) {
    if (history.length < 2) return 0;
    const first = history[0];
    const last = history[history.length - 1];
    return last.t > first.t ? ((last.v - first.v) / (last.t - first.t)) * 1000 : 0;
  }

  // Ein Schritt der Federgleichung, in Teilschritten fuer Stabilitaet bei
  // ruckelnden Frames. damping 1 = kritisch gedaempft, kein Ueberschwingen.
  function integrate(value, velocity, target, dt, { response, damping }) {
    const stiffness = (2 * Math.PI / response) ** 2;
    const friction = (4 * Math.PI * damping) / response;
    const steps = 4;
    const h = dt / steps;
    for (let i = 0; i < steps; i++) {
      const accel = -stiffness * (value - target) - friction * velocity;
      velocity += accel * h;
      value += velocity * h;
    }
    return [value, velocity];
  }

  const reducedMotion = () => Boolean(window.matchMedia?.('(prefers-reduced-motion: reduce)').matches);

  // Eine Feder ueber mehrere benannte Werte ({x, y, w, h}). Jeder Wert laeuft
  // unabhaengig (2D nie als eine Distanz), aber in einem gemeinsamen Takt.
  // onUpdate(values) bekommt jeden Frame, onRest() einmal beim Stillstand.
  function createSpring({ response = 0.4, damping = 1, onUpdate, onRest } = {}) {
    const config = { response, damping };
    let values = null;
    let velocity = {};
    let target = {};
    let frame = 0;
    let last = 0;

    const emit = () => onUpdate?.({ ...values });

    const tick = (now) => {
      const dt = Math.min(0.032, Math.max(0.001, (now - last) / 1000));
      last = now;
      let moving = false;
      for (const key of Object.keys(target)) {
        const [v, vel] = integrate(values[key], velocity[key] || 0, target[key], dt, config);
        values[key] = v;
        velocity[key] = vel;
        if (Math.abs(v - target[key]) > 0.1 || Math.abs(vel) > 0.5) moving = true;
      }
      if (!moving) {
        values = { ...target };
        velocity = {};
        frame = 0;
        emit();
        onRest?.();
        return;
      }
      emit();
      frame = requestAnimationFrame(tick);
    };

    const api = {
      // Sofort auf den Wert, ohne Bewegung (erster Auftritt, reduzierte Bewegung).
      set(next) {
        cancelAnimationFrame(frame);
        frame = 0;
        values = { ...next };
        target = { ...next };
        velocity = {};
        emit();
      },
      // Zum Ziel federn. Laeuft schon eine Bewegung, wird nur das Ziel
      // getauscht: Wert und Geschwindigkeit bleiben, es gibt keinen Knick.
      // velocity (px/s je Wert) uebergibt den Schwung einer Geste.
      to(next, { velocity: handoff } = {}) {
        if (!values || reducedMotion()) {
          api.set(next);
          onRest?.();
          return;
        }
        target = { ...target, ...next };
        if (handoff) velocity = { ...velocity, ...handoff };
        if (!frame) {
          last = performance.now();
          frame = requestAnimationFrame(tick);
        }
      },
      // Wert und Ziel gemeinsam verschieben (Scrollen): folgt 1:1, die
      // laufende Bewegung zum Ziel bleibt erhalten.
      shift(delta) {
        if (!values) return;
        for (const key of Object.keys(delta)) {
          values[key] += delta[key];
          target[key] += delta[key];
        }
        if (!frame) emit();
      },
      // Anhalten, wo die Feder gerade steht (Finger greift zu).
      stop() {
        cancelAnimationFrame(frame);
        frame = 0;
        if (values) target = { ...values };
        velocity = {};
      },
      get values() { return values ? { ...values } : null; },
      get moving() { return frame !== 0; },
    };
    return api;
  }

  window.__obMotion = { project, rubberband, releaseVelocity, integrate };
  window.obMotion = { createSpring, project, rubberband, releaseVelocity, reducedMotion };
})();
