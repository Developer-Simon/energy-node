"""Oberflaeche des Reiters System der Fake-Konsole (HTML-Fragment).

Gleiche Bausteine wie die anderen Reiter. system_html() setzt die Dienste und
die Ergebnisbezeichnungen ein. Das Skript setzt window.tabs.system. Aktionen
laufen ueber window.konsole.post.
"""

import json

from fake_journal_helper import KEYS

JOURNAL_BUTTONS = (("normal", "Normal"), ("errors", "Viele Fehler"), ("empty", "Leer"))

SYSTEM_HTML = """<div id="tab-system">
<style>
#tab-system [data-inject-message] { flex: 1; min-width: 10em; }
</style>

<div class="panel-head">
  <h2>System</h2>
  <p class="panel-meta">Journal-Helper und Systemaktionen</p>
</div>

<div class="grp" data-group="journal">
  <h3>Service-Logs</h3>
  <div class="list">
__JOURNAL_ROWS__
    <div class="row">
      <select data-inject-key aria-label="Dienst">__KEY_OPTIONS__</select>
      <input type="text" data-inject-message placeholder="Meldung" autocomplete="off" spellcheck="false" aria-label="Meldung">
      <select data-inject-priority aria-label="Priorität"><option value="error">Fehler</option><option value="info">Info</option></select>
      <span class="end"><button type="button" class="btn btn-primary" data-act="inject">Zeile einfügen</button></span>
    </div>
  </div>
  <p class="note">Eine eingefügte Zeile erscheint beim nächsten Abruf in den Service-Logs.</p>
  <p class="grp-err"></p>
</div>

<div class="grp" data-group="helper">
  <h3>Helper</h3>
  <div class="list">
    <div class="row">Antwort
      <span class="end"><span class="seg" data-helper>
        <button type="button" data-act="helper" data-value="normal" aria-pressed="false">Normal</button>
        <button type="button" data-act="helper" data-value="denied" aria-pressed="false">Verweigert</button>
        <button type="button" data-act="helper" data-value="hang" aria-pressed="false">Hängt</button>
        <button type="button" data-act="helper" data-value="fail" aria-pressed="false">Fehler</button>
      </span></span>
    </div>
  </div>
  <p class="note">Verweigert antwortet wie sudo ohne Rechte. Hängt wartet 15 s und überschreitet so das Zeitlimit des Dashboards von 10 s.</p>
  <p class="grp-err"></p>
</div>

<div class="grp" data-group="actions">
  <h3>Systemaktionen</h3>
  <div class="list">
    <div class="row">Antwort auf Aktionen
      <span class="end"><span class="seg" data-actions>
        <button type="button" data-act="actions" data-value="ok" aria-pressed="false">Erfolg</button>
        <button type="button" data-act="actions" data-value="fail" aria-pressed="false">Fehler</button>
      </span></span>
    </div>
    <div data-recent></div>
  </div>
  <p class="note">Es wird nichts wirklich neu gestartet oder ausgeschaltet.</p>
  <p class="grp-err"></p>
</div>
<script>
(function () {
  const root = document.getElementById("tab-system");
  if (!root) return;

  const RESULT_LABELS = __RESULT_LABELS__;
  const $ = (sel) => root.querySelector(sel);

  function setPressed(container, value) {
    if (!container) return;
    container.querySelectorAll("button").forEach((btn) => {
      btn.setAttribute("aria-pressed", btn.dataset.value === value ? "true" : "false");
    });
  }

  function renderRecent(list) {
    const box = $("[data-recent]");
    box.replaceChildren();
    if (!list.length) {
      const row = document.createElement("div");
      row.className = "row";
      row.textContent = "Noch keine Aktion angefordert.";
      box.appendChild(row);
      return;
    }
    list.forEach((item) => {
      const row = document.createElement("div");
      row.className = "row";
      row.textContent = item.label;
      const end = document.createElement("span");
      end.className = "end";
      const pill = document.createElement("span");
      pill.className = item.result === "ok" ? "pill" : "pill pill-warn";
      pill.textContent = RESULT_LABELS[item.result] || item.result;
      const time = document.createElement("span");
      time.className = "v";
      time.textContent = item.t ? new Date(item.t * 1000).toLocaleTimeString("de-DE") : "";
      end.append(pill, " ", time);
      row.appendChild(end);
      box.appendChild(row);
    });
  }

  function render(state) {
    Object.keys(state.journal).forEach((key) => {
      setPressed(root.querySelector('[data-journal="' + key + '"]'), state.journal[key]);
    });
    setPressed($("[data-helper]"), state.helper);
    setPressed($("[data-actions]"), state.actions);
    renderRecent(state.recent);
  }

  root.addEventListener("click", (event) => {
    const btn = event.target.closest("[data-act]");
    if (!btn || !root.contains(btn)) return;
    switch (btn.dataset.act) {
      case "journal":
        window.konsole.post("system", "journal",
          { key: btn.parentElement.dataset.journal, mode: btn.dataset.value }, btn);
        break;
      case "helper":
        window.konsole.post("system", "helper", { mode: btn.dataset.value }, btn);
        break;
      case "actions":
        window.konsole.post("system", "actions", { result: btn.dataset.value }, btn);
        break;
      case "inject":
        window.konsole.post("system", "inject", {
          key: $("[data-inject-key]").value,
          message: $("[data-inject-message]").value,
          priority: $("[data-inject-priority]").value,
        }, btn);
        break;
      default:
        break;
    }
  });

  window.tabs = window.tabs || {};
  window.tabs.system = { render: render };
})();
</script>
</div>
"""


def _journal_row(key: str) -> str:
    buttons = "".join(
        f'<button type="button" data-act="journal" data-value="{value}" aria-pressed="false">{label}</button>'
        for value, label in JOURNAL_BUTTONS
    )
    return (f'    <div class="row">{key}<span class="end"><span class="seg" data-journal="{key}">'
            f"{buttons}</span></span></div>")


def system_html(result_labels: dict) -> str:
    """Fragment fuer den Reiter: je Dienst eine Zeile, Auswahl fuer Inject, Ergebnisbezeichnungen."""
    services = sorted(KEYS)
    rows = "\n".join(_journal_row(key) for key in services)
    options = "".join(f'<option value="{key}">{key}</option>' for key in services)
    html = SYSTEM_HTML.replace("__JOURNAL_ROWS__", rows).replace("__KEY_OPTIONS__", options)
    return html.replace("__RESULT_LABELS__", json.dumps(result_labels, ensure_ascii=False))
