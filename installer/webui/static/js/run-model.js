// Das Laufmodell: rein, ohne DOM und ohne Uhr. Es nimmt Ereignisse des Stroms
// und beantwortet, was der Ausfuehrungsbildschirm zeigt. Jede Zeit stammt aus
// dem Feld at des Ereignisses, nie aus der Ankunft im Browser.
(function () {
  'use strict';

  var LOGIN_URL = /https:\/\/login\.tailscale\.com\/\S+/;
  var MAX_LOG = 2000;
  var MASK = '***';
  // Die Skip-Gruende der Bootstrap-Skripte (scripts/bootstrap/lib) sind feste
  // Woerter; uebersetzt wird ueber run.skip.<slug>.
  var SKIP_REASONS = {
    'nicht ausgewaehlt': 'deselected',
    'bereits erledigt': 'done',
    'login ausstehend': 'login_pending',
  };

  function create(runId) {
    return {
      runId: runId, started: false, finished: false, ok: null, code: '', detail: '', failedStep: '',
      mode: '', only: '', startedAt: 0, finishedAt: 0, lastAt: 0,
      steps: {}, log: [], logCount: 0, loginUrl: '', loginStep: '', loginPending: false,
    };
  }

  function stepOf(model, id) {
    if (!model.steps[id]) {
      model.steps[id] = { state: 'wait', detail: '', beganAt: 0, endedAt: 0, lastLine: '' };
    }
    return model.steps[id];
  }

  function push(model, entry) {
    model.logCount += 1;
    entry.key = model.logCount;
    model.log.push(entry);
    if (model.log.length > MAX_LOG) {
      model.log.splice(0, model.log.length - MAX_LOG);
    }
  }

  function apply(model, type, data) {
    var at = data.at || 0;
    if (type === 'run-started') {
      if (data.run_id !== model.runId || model.started) {
        return false;
      }
      model.started = true;
      model.mode = data.mode || '';
      model.only = data.only || '';
      model.startedAt = at;
      model.lastAt = at;
      return true;
    }
    if (!model.started || model.finished) {
      return false;
    }
    if (at > model.lastAt) {
      model.lastAt = at;
    }
    if (type === 'step') {
      var step = stepOf(model, data.id);
      if (data.state === 'begin') {
        step.state = 'run';
        step.beganAt = at;
        step.detail = '';
      } else {
        step.state = data.state;
        step.endedAt = at;
        step.detail = data.detail || '';
        if (!step.beganAt) {
          step.beganAt = at;
        }
      }
      if (data.state === 'skip' && data.detail === 'login ausstehend') {
        model.loginPending = true;
      }
      push(model, { at: at, stepId: data.id, marker: true, text: '##STEP ' + data.id + ' ' + data.state + (data.detail ? ' ' + data.detail : '') });
      return true;
    }
    if (type === 'log') {
      var line = data.line || '';
      stepOf(model, data.step_id).lastLine = line;
      var match = LOGIN_URL.exec(line);
      if (match) {
        model.loginUrl = match[0];
        model.loginStep = data.step_id;
      }
      push(model, { at: at, stepId: data.step_id, marker: false, text: line });
      return true;
    }
    if (type === 'run-finished') {
      if (data.run_id !== model.runId) {
        return false;
      }
      model.finished = true;
      model.ok = !!data.ok;
      model.code = data.code || '';
      model.detail = data.detail || '';
      model.failedStep = data.step_id || '';
      model.finishedAt = at;
      return true;
    }
    return false;
  }

  function stateOf(model, id) {
    return model.steps[id] ? model.steps[id].state : 'wait';
  }

  function groupState(model, group) {
    var states = group.ids.map(function (id) { return stateOf(model, id); });
    if (states.indexOf('fail') >= 0) {
      return 'fail';
    }
    if (states.indexOf('run') >= 0) {
      return 'run';
    }
    var ended = states.filter(function (s) { return s === 'ok' || s === 'skip'; });
    if (ended.length === states.length) {
      return ended.every(function (s) { return s === 'skip'; }) ? 'skip' : 'ok';
    }
    return 'wait';
  }

  function groupDuration(model, group, now) {
    return group.ids.reduce(function (sum, id) {
      var step = model.steps[id];
      if (!step || !step.beganAt) {
        return sum;
      }
      return sum + Math.max(0, (step.endedAt || now) - step.beganAt);
    }, 0);
  }

  function current(model, groups) {
    var index = -1;
    var i;
    for (i = 0; i < groups.length && index < 0; i++) {
      if (groupState(model, groups[i]) === 'run') {
        index = i;
      }
    }
    for (i = 0; i < groups.length && index < 0; i++) {
      if (groupState(model, groups[i]) === 'wait') {
        index = i;
      }
    }
    if (index < 0) {
      index = groups.length - 1;
    }
    return { number: index + 1, total: groups.length, label: groups[index] ? groups[index].label : '' };
  }

  function progress(model, groups) {
    if (!groups.length) {
      return 0;
    }
    var done = groups.reduce(function (sum, group) {
      var state = groupState(model, group);
      return sum + (state === 'run' ? 0.5 : state === 'wait' ? 0 : 1);
    }, 0);
    return Math.round((done / groups.length) * 100);
  }

  function elapsed(model, now) {
    if (!model.started) {
      return 0;
    }
    return Math.max(0, (model.finished ? model.finishedAt : now) - model.startedAt);
  }

  function skipText(detail, t) {
    var slug = SKIP_REASONS[detail];
    return slug ? t('run.skip.' + slug) : detail || '';
  }

  // faultText: ein Fehlercode aus internal/faults, sonst ein Code der
  // Schicht 2, sonst ausdruecklich unbekannt.
  function faultText(code, t) {
    var fault = 'fault.' + code + '.message';
    var text = t(fault);
    if (text !== fault) {
      return text;
    }
    var error = 'error.' + code;
    text = t(error);
    return text !== error ? text : t('fault.unknown.message', { code: code });
  }

  function segments(text) {
    var pieces = String(text).split(MASK);
    var out = [];
    pieces.forEach(function (piece, index) {
      if (piece) {
        out.push({ secret: false, text: piece });
      }
      if (index < pieces.length - 1) {
        out.push({ secret: true, text: '' });
      }
    });
    return out;
  }

  function lastLines(model, stepId, n) {
    return model.log
      .filter(function (entry) { return !entry.marker && entry.stepId === stepId; })
      .slice(-n)
      .map(function (entry) { return entry.text; });
  }

  function logText(model) {
    return model.log.map(function (entry) {
      return window.Format.clock(entry.at) + '  ' + entry.text;
    }).join('\n') + '\n';
  }

  function outcome(model, groups) {
    return {
      ok: model.ok, code: model.code, detail: model.detail, stepId: model.failedStep, mode: model.mode, only: model.only,
      startedAt: model.startedAt, finishedAt: model.finishedAt,
      loginUrl: model.loginUrl, loginPending: model.loginPending,
      steps: JSON.parse(JSON.stringify(model.steps)), groups: groups,
      lastLines: model.failedStep ? lastLines(model, model.failedStep, 8) : [],
      logText: logText(model),
    };
  }

  window.RunModel = {
    SKIP_REASONS: SKIP_REASONS,
    create: create, apply: apply, groupState: groupState, groupDuration: groupDuration,
    current: current, progress: progress, elapsed: elapsed, skipText: skipText,
    faultText: faultText, segments: segments, lastLines: lastLines, logText: logText, outcome: outcome,
  };
})();
