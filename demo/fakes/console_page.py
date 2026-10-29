"""Seite der Fake-Konsole: Hülle mit Seitenleiste, Reiterfläche und Protokollleiste.

Die Reiter liefern ihre Fragmente per /api/tabs. Die Bausteine (Gruppen,
Zeilen, Schalter, Segmente) und die Helfer unter window.konsole sind der
Vertrag für alle Fragmente.
"""

PAGE_HTML = """<!doctype html>
<html lang="de">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Fake-Konsole</title>
<style>
:root {
  --bg: #f2f2f7;
  --side: #e8e8ed;
  --card: #ffffff;
  --fg: #1c1c1e;
  --muted: #6e6e73;
  --sep: #e5e5ea;
  --accent: #0a66d8;
  --accent-bg: #e8f0fe;
  --red: #d70015;
  --red-bg: #ffe9e7;
  --green: #34c759;
  --orange: #ff9f0a;
  --dot-off: #aeaeb2;
  --switch-off: #d1d1d6;
  --logbg: #1c1c1e;
  --logfg: #e5e5ea;
  --logmuted: #8e8e93;
  --logsrc: #64d2ff;
  --logwarn: #ffd60a;
  --logerr: #ff6961;
}
@media (prefers-color-scheme: dark) {
  :root {
    --bg: #000000;
    --side: #1c1c1e;
    --card: #1c1c1e;
    --fg: #f2f2f7;
    --muted: #98989f;
    --sep: #38383a;
    --accent: #4ea1ff;
    --accent-bg: #0a2a4d;
    --red: #ff6961;
    --red-bg: #3b1412;
    --dot-off: #48484a;
    --switch-off: #48484a;
    --logbg: #111111;
  }
}
[hidden] { display: none !important; }
body {
  margin: 0;
  background: var(--bg);
  color: var(--fg);
  font: 14px/1.45 system-ui, -apple-system, "Segoe UI", sans-serif;
  height: 100vh;
  display: grid;
  grid-template-columns: 240px minmax(0, 1fr);
  grid-template-rows: minmax(0, 1fr) auto;
}
aside#sidebar {
  grid-column: 1;
  grid-row: 1 / 3;
  background: var(--side);
  padding: 16px 10px;
  display: flex;
  flex-direction: column;
  gap: 2px;
  border-right: 1px solid var(--sep);
  min-height: 0;
  overflow-y: auto;
}
aside h1 {
  font-size: 13px;
  font-weight: 600;
  color: var(--muted);
  margin: 0 10px 10px;
  letter-spacing: 0.01em;
}
aside .foot {
  margin-top: auto;
  padding: 10px;
}
.nav {
  all: unset;
  display: grid;
  grid-template-columns: 10px minmax(0, 1fr);
  column-gap: 10px;
  padding: 8px 10px;
  border-radius: 8px;
  cursor: pointer;
}
.nav:hover { background: rgba(127, 127, 127, 0.12); }
.nav:focus-visible { outline: 2px solid var(--accent); }
.nav[aria-selected="true"] {
  background: var(--card);
  box-shadow: 0 1px 2px rgba(0, 0, 0, 0.08);
}
.nav b { font-weight: 500; }
.nav small { color: var(--muted); font-size: 12px; }
.nav .dot { grid-row: 1 / 3; align-self: center; }
main#panels {
  grid-column: 2;
  grid-row: 1;
  min-width: 0;
  min-height: 0;
  overflow: auto;
  padding: 24px 32px 32px;
}
main#panels > div { max-width: 720px; }
.panel-head h2 {
  font-size: 24px;
  line-height: 1.1;
  letter-spacing: -0.02em;
  margin: 0 0 4px;
  font-weight: 700;
}
.panel-meta {
  color: var(--muted);
  margin: 0 0 22px;
  display: flex;
  gap: 6px;
  align-items: center;
  flex-wrap: wrap;
}
.grp { margin: 0 0 22px; }
.grp > h3 {
  font-size: 12px;
  font-weight: 600;
  color: var(--muted);
  text-transform: uppercase;
  letter-spacing: 0.03em;
  margin: 0 0 6px 14px;
}
.list {
  background: var(--card);
  border-radius: 11px;
  overflow: hidden;
}
.row {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 10px;
  padding: 10px 14px;
  min-height: 44px;
  border-bottom: 1px solid var(--sep);
}
.row:last-child { border-bottom: 0; }
.row .v {
  margin-left: auto;
  color: var(--muted);
  text-align: right;
  font-variant-numeric: tabular-nums;
}
.row .end {
  margin-left: auto;
  display: flex;
  gap: 6px;
  align-items: center;
  flex-wrap: wrap;
}
.note {
  font-size: 12px;
  color: var(--muted);
  margin: 6px 14px 0;
  line-height: 1.4;
}
.grp-err {
  font-size: 12px;
  color: var(--red);
  margin: 6px 14px 0;
}
.grp-err:empty { display: none; }
.btn {
  all: unset;
  display: inline-block;
  box-sizing: border-box;
  padding: 5px 12px;
  border-radius: 8px;
  background: var(--accent-bg);
  color: var(--accent);
  font-weight: 500;
  font-size: 13px;
  white-space: nowrap;
  cursor: pointer;
}
.btn:active { transform: scale(0.97); }
.btn:focus-visible { outline: 2px solid var(--accent); outline-offset: 2px; }
.btn:disabled { opacity: 0.5; cursor: default; }
.btn-primary {
  background: var(--accent);
  color: #ffffff;
}
.btn-danger {
  background: var(--red-bg);
  color: var(--red);
}
.switch {
  position: relative;
  display: inline-block;
  width: 42px;
  height: 25px;
  margin-left: auto;
  flex: none;
}
.switch input {
  position: absolute;
  inset: 0;
  width: 100%;
  height: 100%;
  margin: 0;
  opacity: 0;
  cursor: pointer;
  z-index: 1;
}
.switch span {
  position: absolute;
  inset: 0;
  border-radius: 13px;
  background: var(--switch-off);
  transition: background 0.25s;
}
.switch span:after {
  content: "";
  position: absolute;
  left: 2px;
  top: 2px;
  width: 21px;
  height: 21px;
  border-radius: 50%;
  background: #ffffff;
  box-shadow: 0 1px 2px rgba(0, 0, 0, 0.2);
  transition: transform 0.25s;
}
.switch input:checked + span { background: var(--green); }
.switch input:checked + span:after { transform: translateX(17px); }
.switch input:focus-visible + span { outline: 2px solid var(--accent); outline-offset: 2px; }
.seg {
  display: inline-flex;
  flex-wrap: wrap;
  gap: 2px;
  padding: 2px;
  margin-left: auto;
  max-width: 100%;
  background: var(--bg);
  border-radius: 8px;
}
.seg button {
  all: unset;
  padding: 3px 10px;
  border-radius: 6px;
  font-size: 12px;
  font-weight: 500;
  color: var(--fg);
  cursor: pointer;
}
.seg button:focus-visible { outline: 2px solid var(--accent); }
.seg button[aria-pressed="true"] {
  background: var(--card);
  box-shadow: 0 1px 2px rgba(0, 0, 0, 0.12);
}
.pill {
  font-size: 12px;
  padding: 1px 8px;
  border-radius: 10px;
  background: var(--bg);
  color: var(--muted);
}
.pill.pill-warn {
  background: var(--red-bg);
  color: var(--red);
}
.dot {
  width: 9px;
  height: 9px;
  border-radius: 50%;
  display: inline-block;
  flex: none;
  background: var(--dot-off);
}
.dot.lvl-ok { background: var(--green); }
.dot.lvl-changed { background: var(--orange); }
.dot.lvl-error { background: var(--red); }
.dot.lvl-off { background: var(--dot-off); }
.mono { font-family: ui-monospace, SFMono-Regular, Menlo, monospace; }
input, select {
  font: inherit;
  color: inherit;
  background: var(--bg);
  border: 1px solid var(--sep);
  border-radius: 7px;
  padding: 4px 8px;
  max-width: 100%;
}
footer#log {
  grid-column: 2;
  grid-row: 2;
  min-width: 0;
  background: var(--logbg);
  color: var(--logfg);
  font: 12px/1.5 ui-monospace, SFMono-Regular, Menlo, monospace;
  border-top: 1px solid #2c2c2e;
}
.log-head {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  align-items: center;
  padding: 8px 16px;
  font-family: system-ui, -apple-system, "Segoe UI", sans-serif;
}
.log-toggle {
  all: unset;
  margin-right: 8px;
  font-weight: 600;
  font-size: 13px;
  color: var(--logfg);
  cursor: pointer;
}
.log-toggle:focus-visible { outline: 2px solid #0a84ff; }
footer .seg { margin-left: 0; background: #2c2c2e; }
footer .seg button { color: var(--logfg); }
footer .seg button[aria-pressed="true"] {
  background: #0a84ff;
  color: #ffffff;
  box-shadow: none;
}
.log-pause {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  margin-left: auto;
  font-size: 12px;
  color: var(--logmuted);
}
footer .switch { width: 36px; height: 21px; margin-left: 0; }
footer .switch span:after { width: 17px; height: 17px; }
footer .switch input:checked + span:after { transform: translateX(15px); }
.log-list {
  list-style: none;
  margin: 0;
  padding: 0 16px 10px;
  max-height: 150px;
  overflow: auto;
}
.log-list li {
  display: flex;
  gap: 10px;
  white-space: pre-wrap;
  overflow-wrap: anywhere;
}
.log-list .lt { color: var(--logmuted); flex: none; }
.log-list .lsrc { color: var(--logsrc); min-width: 70px; flex: none; }
.log-list .ltxt { min-width: 0; }
.log-list li[data-level="warn"] { color: var(--logwarn); }
.log-list li[data-level="error"] { color: var(--logerr); }
@media (max-width: 699px) {
  body {
    height: auto;
    min-height: 100vh;
    grid-template-columns: minmax(0, 1fr);
    grid-template-rows: auto auto auto;
  }
  aside#sidebar {
    grid-column: 1;
    grid-row: 1;
    flex-direction: row;
    align-items: center;
    flex-wrap: nowrap;
    overflow-x: auto;
    overflow-y: hidden;
    border-right: 0;
    border-bottom: 1px solid var(--sep);
    padding: 12px 16px;
    gap: 6px;
  }
  aside h1 { margin: 0 10px 0 0; white-space: nowrap; }
  aside .foot { margin: 0 0 0 auto; padding: 0; flex: none; }
  .nav { flex: none; white-space: nowrap; }
  main#panels {
    grid-column: 1;
    grid-row: 2;
    overflow: visible;
    padding: 16px;
  }
  footer#log { grid-column: 1; grid-row: 3; }
}
@media (prefers-reduced-motion: reduce) {
  *, *:before, *:after {
    transition: none !important;
    animation: none !important;
  }
}
@media (prefers-contrast: more) {
  .list { border: 1px solid var(--fg); }
  .btn { box-shadow: inset 0 0 0 1px currentColor; }
  .nav[aria-selected="true"] { outline: 1px solid var(--fg); }
  .seg button[aria-pressed="true"] { outline: 1px solid var(--fg); }
}
</style>
</head>
<body>
<aside id="sidebar" role="tablist" aria-label="Reiter">
  <h1>Fake-Konsole</h1>
  <div class="foot">
    <button type="button" class="btn" id="reset-all">Alles auf Start zurücksetzen</button>
  </div>
</aside>
<main id="panels">
  <p class="note" id="empty" hidden>Keine Reiter geladen.</p>
</main>
<footer id="log">
  <div class="log-head">
    <button type="button" class="log-toggle" data-log-toggle aria-expanded="true" aria-controls="log-list">Protokoll</button>
    <div class="seg" role="group" aria-label="Protokollfilter" data-log-filter>
      <button type="button" data-value="tab" aria-pressed="true">Dieser Bereich</button>
      <button type="button" data-value="all" aria-pressed="false">Alle</button>
      <button type="button" data-value="errors" aria-pressed="false">Nur Fehler</button>
    </div>
    <span class="log-pause">
      <span>Pause</span>
      <label class="switch"><input type="checkbox" data-log-pause aria-label="Pause"><span></span></label>
    </span>
  </div>
  <ul class="log-list" id="log-list"></ul>
</footer>
<script>
window.tabs = window.tabs || {};
(function () {
  "use strict";

  var KEYS = { tab: "konsole.tab", open: "konsole.log.open", filter: "konsole.log.filter" };
  var LEVELS = ["ok", "changed", "error", "off"];
  var LOG_MAX = 300;

  function load(key, fallback) {
    try {
      var v = window.localStorage.getItem(key);
      return v === null ? fallback : v;
    } catch (err) {
      return fallback;
    }
  }

  function save(key, value) {
    try {
      window.localStorage.setItem(key, value);
    } catch (err) {
      /* ohne Speicher laeuft die Seite weiter */
    }
  }

  function esc(v) {
    return String(v === null || v === undefined ? "" : v)
      .replace(/&/g, "&amp;")
      .replace(/</g, "&lt;")
      .replace(/>/g, "&gt;")
      .replace(/"/g, "&quot;")
      .replace(/'/g, "&#39;");
  }

  function setUnlessFocused(el, value) {
    if (!el || document.activeElement === el) return;
    if (el.type === "checkbox") el.checked = !!value;
    else el.value = value;
  }

  function setSeg(segEl, value) {
    if (!segEl) return;
    var buttons = segEl.querySelectorAll("button");
    for (var i = 0; i < buttons.length; i++) {
      buttons[i].setAttribute("aria-pressed", String(buttons[i].dataset.value === String(value)));
    }
  }

  var sidebar = document.getElementById("sidebar");
  var panels = document.getElementById("panels");
  var logLines = document.getElementById("log-list");
  var logToggle = document.querySelector("[data-log-toggle]");
  var filterSeg = document.querySelector("[data-log-filter]");
  var pauseBox = document.querySelector("[data-log-pause]");
  var navs = {};
  var tabs = [];
  var active = "";
  var logLast = 0;
  var paused = false;
  var logOpen = load(KEYS.open, "1") !== "0";
  var logFilter = load(KEYS.filter, "tab");

  function activeSources() {
    for (var i = 0; i < tabs.length; i++) {
      if (tabs[i].name === active) return tabs[i].sources || [];
    }
    return [];
  }

  function visible(li) {
    if (logFilter === "errors") return li.dataset.level === "error";
    if (logFilter === "all") return true;
    return li.dataset.source === "konsole" || activeSources().indexOf(li.dataset.source) >= 0;
  }

  function applyFilter() {
    for (var i = 0; i < logLines.children.length; i++) {
      var li = logLines.children[i];
      li.hidden = !visible(li);
    }
  }

  function applyLogState() {
    logLines.hidden = !logOpen;
    logToggle.setAttribute("aria-expanded", String(logOpen));
    setSeg(filterSeg, logFilter);
    applyFilter();
  }

  function refreshStatus() {
    return fetch("/api/status")
      .then(function (resp) { return resp.ok ? resp.json() : null; })
      .then(function (list) {
        if (!list) return;
        for (var i = 0; i < list.length; i++) {
          var item = list[i];
          var nav = navs[item.name];
          if (!nav) continue;
          var level = LEVELS.indexOf(item.level) >= 0 ? item.level : "off";
          nav.querySelector(".dot").className = "dot lvl-" + level;
          nav.querySelector("small").textContent = item.text || "";
        }
      })
      .catch(function () { /* naechster Durchlauf versucht es erneut */ });
  }

  function refreshState() {
    var name = active;
    if (!name) return;
    fetch("/api/" + name + "/state")
      .then(function (resp) { return resp.ok ? resp.json() : null; })
      .then(function (state) {
        var tab = window.tabs[name];
        if (state && name === active && tab && typeof tab.render === "function") tab.render(state);
      })
      .catch(function () { /* naechster Durchlauf versucht es erneut */ });
  }

  function show(name) {
    if (!navs[name]) name = tabs.length ? tabs[0].name : "";
    active = name;
    save(KEYS.tab, name);
    for (var i = 0; i < tabs.length; i++) {
      var on = tabs[i].name === name;
      navs[tabs[i].name].setAttribute("aria-selected", String(on));
      var root = document.getElementById("tab-" + tabs[i].name);
      if (root) root.hidden = !on;
    }
    applyFilter();
    refreshState();
  }

  function buildTabs(list) {
    tabs = list;
    for (var i = 0; i < list.length; i++) {
      var t = list[i];
      var nav = document.createElement("button");
      nav.type = "button";
      nav.className = "nav";
      nav.setAttribute("role", "tab");
      nav.setAttribute("aria-selected", "false");
      nav.dataset.tab = t.name;
      nav.innerHTML = '<span class="dot lvl-off"></span><b>' + esc(t.title) + '</b><small></small>';
      nav.addEventListener("click", (function (name) {
        return function () { show(name); };
      })(t.name));
      sidebar.insertBefore(nav, sidebar.querySelector(".foot"));
      navs[t.name] = nav;

      // innerHTML startet keine Skripte: die Skripte des Fragments neu anlegen
      var holder = document.createElement("div");
      holder.innerHTML = t.html;
      var scripts = holder.querySelectorAll("script");
      for (var j = 0; j < scripts.length; j++) {
        var fresh = document.createElement("script");
        fresh.textContent = scripts[j].textContent;
        scripts[j].replaceWith(fresh);
      }
      while (holder.firstChild) panels.appendChild(holder.firstChild);
    }
    document.getElementById("empty").hidden = list.length > 0;
  }

  function addLine(e) {
    var li = document.createElement("li");
    li.dataset.source = e.source;
    li.dataset.level = e.level;
    var time = document.createElement("span");
    time.className = "lt";
    time.textContent = e.t;
    var src = document.createElement("span");
    src.className = "lsrc";
    src.textContent = e.source;
    var txt = document.createElement("span");
    txt.className = "ltxt";
    txt.textContent = e.text;
    li.append(time, src, txt);
    li.hidden = !visible(li);
    logLines.appendChild(li);
  }

  function pollLog() {
    fetch("/api/log?since=" + logLast)
      .then(function (resp) { return resp.ok ? resp.json() : null; })
      .then(function (data) {
        if (!data) return;
        logLast = data.last;
        for (var i = 0; i < data.entries.length; i++) addLine(data.entries[i]);
        while (logLines.children.length > LOG_MAX) logLines.removeChild(logLines.firstChild);
        if (!paused) logLines.scrollTop = logLines.scrollHeight;
      })
      .catch(function () { /* naechster Durchlauf versucht es erneut */ });
  }

  function post(tab, action, body, fromEl) {
    var grp = fromEl && fromEl.closest ? fromEl.closest(".grp") : null;
    var err = grp ? grp.querySelector(".grp-err") : null;
    if (err) err.textContent = "";
    function fail(text) {
      if (err) err.textContent = text;
      return null;
    }
    return fetch("/api/" + tab + "/" + action, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body || {})
    }).then(function (resp) {
      return resp.json().catch(function () { return {}; }).then(function (data) {
        if (!resp.ok) return fail(data.error || "Fehler " + resp.status + ".");
        var t = window.tabs[tab];
        if (t && typeof t.render === "function") t.render(data);
        refreshStatus();
        return data;
      });
    }, function () {
      return fail("Keine Verbindung zur Konsole.");
    });
  }

  window.konsole = {
    esc: esc,
    setUnlessFocused: setUnlessFocused,
    setSeg: setSeg,
    post: post
  };

  logToggle.addEventListener("click", function () {
    logOpen = !logOpen;
    save(KEYS.open, logOpen ? "1" : "0");
    applyLogState();
  });

  filterSeg.addEventListener("click", function (ev) {
    var b = ev.target.closest ? ev.target.closest("button") : null;
    if (!b || !filterSeg.contains(b)) return;
    logFilter = b.dataset.value;
    save(KEYS.filter, logFilter);
    applyLogState();
  });

  pauseBox.addEventListener("change", function () {
    paused = pauseBox.checked;
    if (!paused) logLines.scrollTop = logLines.scrollHeight;
  });

  document.getElementById("reset-all").addEventListener("click", function () {
    fetch("/api/reset", { method: "POST" })
      .then(function () {
        refreshStatus();
        refreshState();
        pollLog();
      })
      .catch(function () { /* die Abfragen melden die fehlende Verbindung */ });
  });

  applyLogState();

  fetch("/api/tabs")
    .then(function (resp) { return resp.json(); })
    .then(function (list) {
      buildTabs(list);
      show(load(KEYS.tab, ""));
      refreshStatus();
    })
    .catch(function () {
      document.getElementById("empty").hidden = false;
    });

  setInterval(refreshStatus, 2000);
  setInterval(refreshState, 2000);
  pollLog();
  setInterval(pollLog, 1000);
})();
</script>
</body>
</html>
"""
