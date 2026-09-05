// Darstellungszustand der Energiekacheln, gemeinsam fuer alle sieben.
//
// #overview-live wird bei jedem SSE-Registry-Ereignis komplett per outerHTML
// ersetzt (dashboard.js refreshLiveFragment()), jede Kachel darin also
// mehrmals pro Minute abgerissen und neu gebaut. Alles, was am Alpine-Objekt
// haengt, faengt damit bei jedem Update wieder bei null an: jede Zahl springt
// auf ihren neuen Wert, und jede laufende CSS-Animation schnappt zurueck auf
// Phase 0.
//
// Apples Regel dafuer lautet, immer vom *Darstellungswert* zu animieren - dem,
// was in diesem Moment auf dem Schirm steht - nie vom Zielwert. Deshalb liegen
// die Darstellungswerte hier draussen im Modul, je Layout-Kachel abgelegt, und
// die frisch montierte Komponente macht da weiter, wo ihre Vorgaengerin
// aufgehoert hat.
//
// Bis 2026-08 stand diese Mechanik privat in energy-flow.js und galt darum nur
// fuer eine von sieben Karten.
(() => {
  // Kritisch gedaempfte Feder (Daempfungsverhaeltnis 1.0) in Apples
  // Parametrisierung: `response` ist, wie schnell der Wert das Ziel erreicht,
  // keine Dauer - eine Feder hat keine, ihre Ruhezeit faellt aus der Physik.
  // 0.4s ist der Wert, den Apple fuers Umpositionieren ausliefert; kein
  // Ueberschwingen, weil dieser Bewegung keine Geste mit Schwung vorausging.
  //
  // Geschlossen geloest statt schrittweise integriert: die Schnappschuesse
  // kommen ueber einen Ereignisstrom, dt reicht also von einem Einzelbild bis
  // zu mehreren Sekunden (verborgener Tab, haengender Broker). Ein naiver
  // Euler-Schritt wird bei grossem dt instabil, die geschlossene Form nie.
  const SPRING_RESPONSE = 0.4;
  const SETTLE_VALUE = 0.5;    // unterhalb der Rundung von formatPower()
  const SETTLE_VELOCITY = 2;

  // Autarkie und Eigenverbrauch sind Anteile zwischen 0 und 1. Fuer sie hiesse
  // eine Ruheschwelle von 0.5, im ersten Schritt fertig zu sein - die Zahl
  // spraenge wieder. Ihre Schwelle liegt darum unter der Rundung von
  // formatPercent() (0,1 Prozentpunkt) statt unter der von formatPower().
  const FRACTION_KEYS = new Set(['balance.autarkie', 'balance.eigenverbrauch']);
  const FRACTION_SETTLE_VALUE = 0.0005;
  const FRACTION_SETTLE_VELOCITY = 0.002;

  function settleFor(name) {
    return FRACTION_KEYS.has(name)
      ? [FRACTION_SETTLE_VALUE, FRACTION_SETTLE_VELOCITY]
      : [SETTLE_VALUE, SETTLE_VELOCITY];
  }

  function advanceSpring(spring, target, dt, settleValue = SETTLE_VALUE, settleVelocity = SETTLE_VELOCITY) {
    const omega = (2 * Math.PI) / SPRING_RESPONSE;
    const offset = spring.value - target;
    const slope = spring.velocity + omega * offset;
    const decay = Math.exp(-omega * dt);
    const projected = offset + slope * dt;
    spring.value = target + projected * decay;
    spring.velocity = (slope - omega * projected) * decay;
    if (Math.abs(spring.value - target) < settleValue && Math.abs(spring.velocity) < settleVelocity) {
      spring.value = target;
      spring.velocity = 0;
      return false;
    }
    return true;
  }

  // Wie weit eine CSS-Laufschrift schon gelaufen war. Die Animation eines neu
  // erzeugten Elements beginnt zwangslaeufig bei Offset 0; eine negative
  // animation-delay spult sie auf `phase` vor, sodass die Striche nach dem
  // Neuaufbau mitten im Schritt weiterlaufen statt zurueckzuspringen.
  function advancePhase(previous, now) {
    if (!previous) return 0;
    if (!previous.running || !(previous.duration > 0)) return previous.phase;
    return (previous.phase + Math.max(0, now - previous.at) / 1000 / previous.duration) % 1;
  }

  // Die Groessen der Bilanz, die eine Grafik als Menge zeichnet - jede bekommt
  // eine eigene Feder. Bewusst eine Liste statt "alles, was eine Zahl ist":
  // gap_tolerance_w ist eine Schwelle, battery_capacity_kwh ein Typenschild,
  // load_source eine Auskunft - gefedert wuerde davon nichts besser, nur
  // unscharf. battery_soc und battery_energy_kwh sind Durchreichungen aus
  // values.* und dort bereits gefedert.
  //
  // Bis 2026-08 standen hier nur load_total und load_measured. Weil balanceOf()
  // aber den fertigen snapshot.balance des Servers bevorzugt und die Karten
  // ihre Segmente daraus bauen (composeBalance liest n.pv, n.base, n.wallbox
  // ...), sprang faktisch jedes Ringsegment und jeder KPI auf seinen neuen
  // Wert. Gefedert war nur der Zweig, der die Bilanz aus values.* nachrechnet -
  // und den nimmt kein Live-Schnappschuss.
  const BALANCE_KEYS = [
    'pv', 'grid_import', 'grid_export', 'battery_charge', 'battery_discharge',
    'wallbox', 'heat_pump', 'load_total', 'load_measured', 'base',
    'gap_raw', 'gap_applied', 'gap_absorbed', 'netz', 'total',
    'autarkie', 'eigenverbrauch',
  ];

  // Jeder Skalar, den eine Grafik liest, flach in einem Schluesselraum. Dass
  // die Schluesselmenge deckungsgleich mit der des Schnappschusses bleibt, ist
  // der Punkt: hasValue()'s hasOwnProperty-Pruefungen - und damit die
  // Unterscheidung "nicht zugeordnet" gegen "genau 0 W", auf der die Karten
  // stehen - ueberstehen die Ersetzung unveraendert.
  function animatable(snapshot) {
    const targets = new Map();
    const values = (snapshot && snapshot.values) || {};
    for (const role of Object.keys(values)) {
      if (typeof values[role] === 'number') targets.set(`values.${role}`, values[role]);
    }
    const balance = snapshot && snapshot.balance;
    if (balance) {
      for (const field of BALANCE_KEYS) {
        if (typeof balance[field] === 'number') targets.set(`balance.${field}`, balance[field]);
      }
    }
    return targets;
  }

  // Ein flacher Stellvertreter fuer `snapshot`, dessen Zahlen die gerade
  // sichtbaren sind. Alles andere - die Frische-Liste der Rollen,
  // balance.load_source, die Schluesselmenge selbst - geht unveraendert durch,
  // damit balanceOf() und die Praedikate der Karten darauf genauso laufen wie
  // auf dem echten Schnappschuss und die einzige Wahrheit darueber bleiben,
  // was gezeichnet wird.
  function withPresentedValues(snapshot, springs) {
    const values = {};
    for (const role of Object.keys((snapshot && snapshot.values) || {})) {
      const spring = springs.get(`values.${role}`);
      values[role] = spring ? spring.value : snapshot.values[role];
    }
    const presented = {...snapshot, values};
    if (snapshot && snapshot.balance) {
      presented.balance = {...snapshot.balance};
      for (const field of BALANCE_KEYS) {
        const spring = springs.get(`balance.${field}`);
        if (spring) presented.balance[field] = spring.value;
      }
    }
    return presented;
  }

  const clock = () => (window.performance && window.performance.now ? window.performance.now() : Date.now());

  // Je Layout-Kachel: die Federn ihrer Zahlen und die Phasen ihrer
  // Laufschriften. `seeded` unterscheidet den allerersten Aufbau (alles muss
  // einfach dastehen, sonst waere der Seitenaufbau selbst eine Animation) von
  // jedem spaeteren.
  const cards = new Map();

  function stateFor(key) {
    let state = cards.get(key);
    if (!state) {
      state = { springs: new Map(), phases: new Map(), seeded: false };
      cards.set(key, state);
    }
    return state;
  }

  // Eine Bildschleife fuer alle montierten Energiekacheln. Sie laeuft nur,
  // solange irgendeine Feder noch unterwegs ist, und haelt an, sobald alles
  // eingeschwungen ist - eine Kachel, die still steht, kostet nichts.
  const instances = new Set();
  let frameHandle = null;
  let frameAt = 0;

  // Alle lebenden Karten-Handles, adressiert ueber ihren Layout-Schluessel.
  // publish() ist der Weg, auf dem ein einzelner SSE-Schnappschuss alle
  // sieben Energiegrafiken erreicht, ohne dass eine Kartendatei davon weiss.
  const subscribers = new Map();

  function publish(raw) {
    for (const [key, handle] of [...subscribers]) {
      // Nach einem outerHTML-Tausch haengt die alte Karte noch am Register,
      // steht aber nicht mehr im Dokument. Gleiche Pruefung wie im Frame-Tick.
      if (!handle.alive()) { subscribers.delete(key); continue; }
      handle.update(raw);
    }
  }

  function scheduleFrame() {
    if (frameHandle !== null || typeof window.requestAnimationFrame !== 'function') return;
    frameAt = clock();
    frameHandle = window.requestAnimationFrame(runFrame);
  }

  function runFrame() {
    frameHandle = null;
    const now = clock();
    // Gedeckelt: ein Tab, der eine Minute im Hintergrund lag, bekommt sonst
    // ein einzelnes Bild mit einer Minute dt - jede Feder spraenge in diesem
    // Schritt auf ihr Ziel, genau der Sprung, gegen den das hier gebaut ist.
    const dt = Math.min(Math.max((now - frameAt) / 1000, 0), .25);
    frameAt = now;
    let moving = false;
    for (const instance of instances) {
      // Nach dem outerHTML-Tausch haengt die alte Kachel noch hier, ist aber
      // nicht mehr im Dokument. Sie muss *vor* dem Tick raus, sonst schiebt
      // sie dieselben Federn im selben Bild ein zweites Mal weiter.
      if (!instance.alive()) { instances.delete(instance); continue; }
      if (instance.step(dt)) moving = true;
    }
    if (moving) scheduleFrame();
  }

  function present({ key, raw, reducedMotion, alive, apply }) {
    const state = stateFor(key);
    // raw und targets sind veraenderlich, weil update() neue Zielwerte
    // einspielt, ohne dass die Karte neu montiert wird. draw() und step()
    // lesen beide aus diesen Bindungen.
    let current = raw;
    let targets = animatable(current);

    function retarget(next) {
      current = next;
      targets = animatable(current);
      for (const [name, target] of targets) {
        if (state.springs.has(name)) continue;
        // Eine Rolle, die eben noch nicht auf dem Schirm war. Beim allerersten
        // Aufbau muss alles einfach dastehen. Taucht eine Rolle spaeter zum
        // ersten Mal auf, steigt sie aus der Null hoch, was sich wie ein
        // Ankommen liest statt wie ein Aufblitzen.
        state.springs.set(name, { value: state.seeded && !reducedMotion ? 0 : target, velocity: 0 });
      }
      for (const name of [...state.springs.keys()]) {
        if (!targets.has(name)) state.springs.delete(name);
      }
      // Reduzierte Bewegung heisst hier: der Messwert steht sofort richtig da.
      // Fuer eine Zahlenanzeige ist das die passende, nicht-vestibulaere
      // Entsprechung - eine Ersatzbewegung waere hier keine Hilfe.
      if (reducedMotion) {
        for (const [name, target] of targets) Object.assign(state.springs.get(name), { value: target, velocity: 0 });
      }
      state.seeded = true;
    }
    retarget(raw);

    // Zweites Argument: der rohe, ungefederte Schnappschuss. Die sechs
    // Kartendateien brauchen nur das erste (den bereits interpolierten
    // Stellvertreter) - energy-flow.js dagegen federt seine Werte selbst
    // (eigene Opacity-Federn, eigene Phasen-Verankerung) und braucht darum
    // das rohe Ziel, nicht ein zweites Mal ein bereits gefedertes.
    const draw = () => apply(withPresentedValues(current, state.springs), current);

    const step = dt => {
      let moving = false;
      for (const [name, target] of targets) {
        const spring = state.springs.get(name);
        if (!spring) continue;
        const [settleValue, settleVelocity] = settleFor(name);
        advanceSpring(spring, target, dt, settleValue, settleVelocity);
        if (spring.value !== target) moving = true;
      }
      draw();
      return moving;
    };

    // Rufe apply sofort mit den aktuellen Federwerten auf - entweder sind sie
    // schon beim Ziel (erster Aufbau) oder zeigen den sichtbaren Wert aus einer
    // vorherigen Montage (spaetere Aufbauten). Die Komponente kann den
    // Schnappschuss also sofort lesen.
    draw();

    let released = false;
    const handle = { alive: () => !released && alive(), step };
    const startIfMoving = () => {
      if (reducedMotion) return;
      if (![...targets].some(([name, target]) => state.springs.get(name).value !== target)) return;
      instances.add(handle);
      scheduleFrame();
    };
    startIfMoving();
    const api = {
      release() {
        released = true;
        instances.delete(handle);
        if (subscribers.get(key) === api) subscribers.delete(key);
      },
      // Neue Zielwerte fuer eine Karte, die im Dokument stehen bleibt. Die
      // Federn behalten ihren Darstellungswert und laufen von dort zum neuen
      // Ziel - genau das, was der Neuaufbau frueher nur ueber stateFor(key)
      // hinbekommen hat.
      update(next) {
        if (released) return;
        retarget(next);
        draw();
        startIfMoving();
      },
      alive: () => !released && alive(),
    };
    // Eine zweite Montage unter demselben Schluessel loest die erste ab -
    // beim Fragment-Tausch entsteht die neue Karte, bevor die alte ihr
    // release() bekommt.
    subscribers.set(key, api);
    return api;
  }

  // Rueckfallwert der CSS-Laufschriften (.energy-flow-line.is-active,
  // .energy-ring-flow-dash, .energy-band-flow-dash). Haelt den Wert von
  // --energy-dash-duration in base.css; werden beide nicht zusammen
  // geaendert, rechnet die Phasenfortschreibung falsch.
  const DEFAULT_DASH_SECONDS = 1.4;

  // Der Wert fuer animation-delay einer Laufschrift, die den Neuaufbau
  // ueberleben soll. Die Karten bauen ihr SVG als Markup-Zeichenkette (in
  // <svg> klont <template x-for> nicht), deshalb gibt das hier eine
  // Zeichenkette zum Einsetzen zurueck statt an einem Element zu schreiben.
  function dashDelay(key, id, durationSeconds = DEFAULT_DASH_SECONDS) {
    const state = stateFor(key);
    const now = clock();
    const duration = durationSeconds > 0 ? durationSeconds : DEFAULT_DASH_SECONDS;
    const phase = advancePhase(state.phases.get(id), now);
    state.phases.set(id, { phase, duration, at: now, running: true });
    const value = -phase * duration;
    // Preserve negative zero sign in the formatted output
    const sign = (value === 0 && 1/value === -Infinity) ? '-' : '';
    return sign + value.toFixed(3) + 's';
  }

  function pauseDash(key, id) {
    const state = stateFor(key);
    const previous = state.phases.get(id);
    if (!previous || !previous.running) return;
    const now = clock();
    state.phases.set(id, { phase: advancePhase(previous, now), duration: previous.duration, at: now, running: false });
  }

  window.EnergyPresentation = {
    SPRING_RESPONSE, SETTLE_VALUE, SETTLE_VELOCITY, DEFAULT_DASH_SECONDS,
    BALANCE_KEYS, FRACTION_SETTLE_VALUE, FRACTION_SETTLE_VELOCITY, settleFor,
    advanceSpring, advancePhase, animatable, withPresentedValues,
    present, publish, dashDelay, pauseDash,
  };
})();
