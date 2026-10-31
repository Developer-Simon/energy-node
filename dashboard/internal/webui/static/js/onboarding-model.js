// Entscheidungskern der Ersteinrichtung, Neu erkunden und Begruessung. Reine
// Funktionen ohne DOM. Die Leiste (onboarding.js) holt Fortschritt, Rechte und
// Dienste und reicht sie herein. ctx = {role, can: Set, device, installed,
// t?}: can ist die Menge der Rechte der Sitzung, device 'phone' oder
// 'desktop', installed die Dienste, t der Uebersetzer (key, params). Alle
// Eyebrow-Texte kommen aus t, die Datei selbst enthaelt keine Texte.
(() => {
  const PHONE_MAX = 759;
  const ORDER = ['setup', 'explore', 'welcome'];

  // Ab wie vielen Pixeln Breite das Handy-Layout gilt.
  function deviceOf(width) {
    return width <= PHONE_MAX ? 'phone' : 'desktop';
  }

  // Gilt der Schritt fuer das Geraet? Ohne device-Feld oder mit 'any' immer.
  function deviceFits(step, device) {
    return !step.device || step.device === 'any' || step.device === device;
  }

  // Rechte und Dienste, ohne Geraet.
  function rightsFit(step, ctx) {
    const need = !step.need || ctx.can.has(step.need);
    const service = !step.needs || ctx.installed.includes(step.needs);
    return need && service;
  }

  function stepAllowed(step, ctx) {
    return deviceFits(step, ctx.device) && rightsFit(step, ctx);
  }

  // Eyebrow-Texte nur aus dem Katalog ueber ctx.t.
  function text(ctx, key, params) {
    return ctx.t(key, params);
  }

  // Schritte eines Highlights, die fuer die Sitzung gelten. tour: false oder
  // fehlende Schritte heissen: nur Text, keine Tour.
  function tourSteps(item, ctx) {
    if (item.tour === false || !item.steps) return [];
    return item.steps.filter((s) => stepAllowed(s, ctx));
  }

  // Id der Vorstellung vor der Einrichtung. Sie steht in seen, wenn sie
  // verlassen wurde, und hat kein Ziel.
  const INTRO_ID = 'setup-intro';
  function introStep(ctx) {
    return {
      id: INTRO_ID, intro: true, panel: null, target: null,
      title: 'onboarding.setup.intro.title',
      text: 'onboarding.setup.intro.text',
      eyebrow: text(ctx, 'onboarding.eyebrow.setup_intro', {}),
      dot: 0, dots: 0,
    };
  }

  // Baut die Schrittliste eines Laufs. from = Id, ab der es losgeht.
  function buildSteps({ mode, from = null, setup = [], explore = [], welcome = [], areas = [], seen = {}, status = {}, ctx }) {
    if (mode === 'setup') {
      if (ctx.role !== 'admin') return [];
      const stateOf = (id) => status[id] || 'open';
      // Schritte ohne bekannten Bereich entfallen, sonst fehlt das Eyebrow.
      const eligible = setup.filter((s) => stepAllowed(s, ctx) && areas.some((a) => a.id === s.area));
      const open = eligible.filter((s) => !['done', 'installer'].includes(stateOf(s.id)) || s.id === from);
      const start = from ? Math.max(0, open.findIndex((s) => s.id === from)) : 0;
      const steps = open.slice(start).map((s) => {
        const inArea = open.filter((o) => o.area === s.area);
        const areaIndex = areas.findIndex((a) => a.id === s.area);
        const label = areas[areaIndex].label;
        const eyebrow = text(ctx, 'onboarding.eyebrow.setup',
          { n: areaIndex + 1, total: areas.length, area: label });
        return { ...s, eyebrow, dot: inArea.indexOf(s), dots: inArea.length };
      });
      // Vorstellung vor dem ersten Schritt, nur beim ganzen Lauf und solange sie
      // nicht gesehen ist. Ohne Ziel: die Leiste zeigt nur den Text.
      if (!from && !seen[INTRO_ID] && steps.length) {
        return [introStep(ctx), ...steps];
      }
      return steps;
    }
    if (mode === 'change') {
      const step = setup.find((s) => s.id === from && stepAllowed(s, ctx));
      if (!step) return [];
      const eyebrow = text(ctx, 'onboarding.eyebrow.change', {});
      return [{ ...step, eyebrow, dot: 0, dots: 1 }];
    }
    if (mode === 'explore') {
      // Ohne from: die ungesehenen der aktuellen Version. Mit from (aus der
      // Checkliste): genau dieses Highlight, auch aus aelteren Versionen.
      const version = from
        ? explore.find((v) => v.items.some((item) => item.id === from))
        : explore.find((v) => v.current);
      if (!version) return [];
      const items = version.items.filter((item) => (from ? item.id === from : !seen[item.id]) && tourSteps(item, ctx).length);
      const short = version.version.replace(/\.0$/, '');
      const flat = items.flatMap((item) => tourSteps(item, ctx)
        .map((s) => ({ ...s, id: item.id, title: item.title, text: item.text, check: 'leave' })));
      return flat.map((s, i) => ({
        ...s,
        eyebrow: text(ctx, 'onboarding.eyebrow.explore', { version: short, i: i + 1, total: flat.length }),
        dot: i,
        dots: flat.length,
      }));
    }
    if (mode === 'welcome') {
      const steps = welcome.filter((s) => stepAllowed(s, ctx));
      return steps.map((s, i) => ({
        ...s,
        check: 'leave',
        eyebrow: text(ctx, 'onboarding.eyebrow.welcome', { i: i + 1, total: steps.length }),
        dot: i,
        dots: steps.length,
      }));
    }
    return [];
  }

  // Wie viele aktuelle Highlights noch ungesehen sind und eine Tour fuer das Geraet haben.
  function unseenCount(explore, seen, ctx) {
    return explore.filter((v) => v.current)
      .flatMap((v) => v.items)
      .filter((item) => !seen[item.id] && tourSteps(item, ctx).length).length;
  }

  // Zustand eines Highlights fuer die Liste "Neu erkunden".
  function exploreState(item, seen, ctx) {
    const steps = item.tour === false ? [] : (item.steps || []);
    if (!steps.length) return 'text';
    const allowedByRights = steps.filter((s) => rightsFit(s, ctx));
    if (!allowedByRights.length) return 'text';
    if (!allowedByRights.some((s) => deviceFits(s, ctx.device))) return 'other-device';
    return seen[item.id] ? 'seen' : 'new';
  }

  // Nach einem Groessenwechsel: erster Schritt ab index, der fuer device gilt, sonst -1.
  function nextAfterResize(steps, index, device) {
    for (let i = Math.max(0, index); i < steps.length; i += 1) {
      if (deviceFits(steps[i], device)) return i;
    }
    return -1;
  }

  // Warteschlange nach einem beendeten Lauf. welcome verdraengt explore.
  function queueAfter(queue, finishedMode) {
    const left = new Set(queue.filter((m) => m !== finishedMode && ORDER.includes(m)));
    if (left.has('welcome')) left.delete('explore');
    return ORDER.filter((m) => left.has(m));
  }

  window.obModel = {
    deviceOf,
    stepAllowed,
    buildSteps,
    unseenCount,
    exploreState,
    nextAfterResize,
    queueAfter,
  };
})();
