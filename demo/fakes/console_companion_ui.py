"""Oberflaeche des Reiters Companion (HTML-Fragment fuer die Fake-Konsole).

Das Fragment nutzt die Bausteine der Seite (Gruppen, Zeilen, Schalter,
Fehlerzeilen) und bringt nur Tabellenregeln mit. Die Seite fuegt es per
innerHTML ein und startet das Skript danach neu. Das Skript setzt
window.tabs.companion = {render}. Aktionen laufen ueber
window.konsole.post, render(state) ueberschreibt keine Eingabefelder, die
gerade den Fokus haben.
"""

COMPANION_HTML = """<div id="tab-companion">
<style>
#tab-companion .co-table input[type=text], #tab-companion .co-table input[type=password] { width: 9em; }
#tab-companion input[type=number] { width: 7em; }
#tab-companion input[data-code] { flex: 1; min-width: 10em; max-width: 16em; }
#tab-companion .co-tablewrap { overflow-x: auto; padding: 4px 14px; }
#tab-companion table { border-collapse: collapse; }
#tab-companion th, #tab-companion td { text-align: left; padding: 4px 6px; border-bottom: 1px solid var(--sep); vertical-align: middle; }
#tab-companion .pill-warn:empty { display: none; }
</style>

<div class="panel-head">
  <h2>Companion</h2>
  <p class="panel-meta" data-meta></p>
</div>

<div class="grp" data-group="pair">
  <h3>Kopplung</h3>
  <div class="list">
    <div class="row">Status <span class="pill pill-warn" data-ha-error></span><span class="v" data-pair-status></span></div>
    <div class="row">
      <input type="text" data-code placeholder="XXXX-XXXX-XXXX" autocomplete="off" spellcheck="false" aria-label="Kopplungscode">
      <span class="end">
        <button type="button" class="btn btn-primary" data-act="pair">Koppeln</button>
        <button type="button" class="btn" data-act="pair_wrong">Falscher Code</button>
      </span>
    </div>
    <div class="row">Prüfstrom <span class="end">
      <button type="button" class="btn" data-act="drop_stream">Trennen</button>
      <button type="button" class="btn" data-act="open_stream">Öffnen</button>
    </span></div>
    <div class="row"><button type="button" class="btn btn-danger" data-act="unpair">Entkoppeln</button></div>
  </div>
  <p class="grp-err"></p>
</div>

<div class="grp" data-group="users">
  <h3>HA-Benutzer</h3>
  <div class="list">
    <div class="row">Prüfungen manuell beantworten
      <label class="switch"><input type="checkbox" data-manual aria-label="Prüfungen manuell beantworten"><span></span></label>
    </div>
    <div class="co-tablewrap">
      <table class="co-table">
        <thead><tr><th>Benutzername</th><th>Name</th><th>Passwort</th><th>Admin</th><th>aktiv</th><th></th></tr></thead>
        <tbody data-users></tbody>
      </table>
    </div>
    <div data-pending></div>
  </div>
  <p class="note">Mit manueller Beantwortung warten Prüfungen hier auf Ja, Nein oder Timeout.</p>
  <p class="grp-err"></p>
</div>

<div class="grp" data-group="history">
  <h3>Verlauf</h3>
  <div class="list">
    <div class="row">Am Austausch teilnehmen
      <span class="pill pill-warn" data-hstate-error></span>
      <label class="switch"><input type="checkbox" data-history aria-label="Am Austausch teilnehmen"><span></span></label>
    </div>
    <div class="row">Tage <input type="number" min="1" max="800" data-days aria-label="Tage"></div>
    <div data-factors></div>
    <div class="row">Verzögerung in ms <input type="number" min="0" max="60000" data-delay aria-label="Verzögerung in ms"></div>
    <div class="row">Nicht antworten
      <label class="switch"><input type="checkbox" data-silent aria-label="Nicht antworten"><span></span></label>
    </div>
    <div class="row">Nur ersten Block liefern
      <label class="switch"><input type="checkbox" data-partial aria-label="Nur ersten Block liefern"><span></span></label>
    </div>
    <div class="row"><button type="button" class="btn" data-act="history_offer">Neu anbieten</button></div>
  </div>
  <p class="note">Ein Faktor wirkt nur auf Zeiträume, die das Dashboard noch nicht abgeholt hat. Tage gelten ab der nächsten Anmeldung.</p>
  <p class="grp-err"></p>
</div>
</div>
<script>
(function () {
  const root = document.getElementById("tab-companion");
  if (!root) return;

  const ROLE_LABELS = {
    pv: "PV",
    load_total: "Verbrauch gesamt",
    grid_import: "Netzbezug",
    grid_export: "Einspeisung",
    battery_charge: "Batterie laden",
    battery_discharge: "Batterie entladen",
  };
  const $ = (sel) => root.querySelector(sel);
  const esc = (v) => window.konsole.esc(v);
  const setUnlessFocused = (el, value) => window.konsole.setUnlessFocused(el, value);

  let usersKey = null;
  let pendingKey = null;

  // Eine Tabelle mit Fokus in einem Eingabefeld wird nicht neu aufgebaut
  const editing = (container) => {
    const a = document.activeElement;
    return !!a && container.contains(a) && (a.tagName === "INPUT" || a.tagName === "SELECT");
  };

  function userRow(u) {
    return '<tr data-row="' + esc(u.ha_user_id) + '">' +
      '<td><input type="text" data-u="username" value="' + esc(u.username) + '" autocomplete="off"></td>' +
      '<td><input type="text" data-u="name" value="' + esc(u.name) + '" autocomplete="off"></td>' +
      '<td><input type="password" data-u="password" value="' + esc(u.password) + '" autocomplete="off"></td>' +
      '<td><input type="checkbox" data-u="admin"' + (u.admin ? " checked" : "") + '></td>' +
      '<td><input type="checkbox" data-u="active"' + (u.active ? " checked" : "") + '></td>' +
      '<td><button type="button" class="btn" data-act="user_save">Speichern</button> ' +
      '<button type="button" class="btn btn-danger" data-act="user_remove">Löschen</button></td></tr>';
  }

  function newRow() {
    return '<tr data-row="">' +
      '<td><input type="text" data-u="username" placeholder="Neu" autocomplete="off"></td>' +
      '<td><input type="text" data-u="name" autocomplete="off"></td>' +
      '<td><input type="password" data-u="password" autocomplete="off"></td>' +
      '<td><input type="checkbox" data-u="admin"></td>' +
      '<td><input type="checkbox" data-u="active" checked></td>' +
      '<td><button type="button" class="btn" data-act="user_save">Speichern</button></td></tr>';
  }

  function renderPair(ha) {
    let status = "nicht gekoppelt";
    if (ha.paired) status = ha.connected ? "gekoppelt, Strom verbunden" : "gekoppelt, Strom getrennt";
    $("[data-meta]").textContent = status;
    $("[data-pair-status]").textContent = status;
    $("[data-ha-error]").textContent = ha.error || "";
  }

  function renderUsers(ha) {
    const tbody = $("[data-users]");
    const key = JSON.stringify(ha.users);
    if (key !== usersKey && !editing(tbody)) {
      usersKey = key;
      tbody.innerHTML = ha.users.map(userRow).join("") + newRow();
    }
    setUnlessFocused($("[data-manual]"), ha.manual);

    const pending = $("[data-pending]");
    const pKey = JSON.stringify(ha.pending);
    if (pKey !== pendingKey) {
      pendingKey = pKey;
      pending.innerHTML = ha.pending.map((p) =>
        '<div class="row">Wartet: ' + esc(p.username) + '<span class="end">' +
        '<button type="button" class="btn" data-act="resolve" data-id="' + esc(p.id) + '" data-decision="yes">Ja</button> ' +
        '<button type="button" class="btn" data-act="resolve" data-id="' + esc(p.id) + '" data-decision="no">Nein</button> ' +
        '<button type="button" class="btn" data-act="resolve" data-id="' + esc(p.id) + '" data-decision="timeout">Timeout</button>' +
        '</span></div>'
      ).join("");
    }
  }

  function renderHistory(h) {
    $("[data-hstate-error]").textContent = h.error || "";
    setUnlessFocused($("[data-history]"), h.running);

    const grid = $("[data-factors]");
    if (!grid.children.length) {
      grid.innerHTML = Object.keys(h.scale).map((role) =>
        '<div class="row">' + esc(ROLE_LABELS[role] || role) +
        ' <input type="number" min="0" max="100" step="0.1" data-role="' + esc(role) + '" aria-label="' + esc(ROLE_LABELS[role] || role) + '"></div>'
      ).join("");
    }
    for (const input of grid.querySelectorAll("input")) {
      setUnlessFocused(input, h.scale[input.dataset.role]);
    }
    setUnlessFocused($("[data-days]"), h.days);
    setUnlessFocused($("[data-delay]"), h.faults.delay_ms);
    setUnlessFocused($("[data-silent]"), h.faults.silent);
    setUnlessFocused($("[data-partial]"), h.faults.partial);
  }

  function render(state) {
    renderPair(state.ha);
    renderUsers(state.ha);
    renderHistory(state.history);
  }

  function faultsBody() {
    return {
      delay_ms: Number($("[data-delay]").value || 0),
      silent: $("[data-silent]").checked,
      partial: $("[data-partial]").checked,
    };
  }

  function bodyFor(btn) {
    const act = btn.dataset.act;
    if (act === "pair") return { code: $("[data-code]").value };
    if (act === "history_start") return { days: Number($("[data-days]").value) };
    if (act === "history_faults") return faultsBody();
    if (act === "resolve") return { id: btn.dataset.id, decision: btn.dataset.decision };
    if (act === "user_save" || act === "user_remove") {
      const row = btn.closest("tr");
      const field = (key) => row.querySelector('[data-u="' + key + '"]');
      if (act === "user_remove") return { ha_user_id: row.dataset.row };
      return {
        ha_user_id: row.dataset.row,
        username: field("username").value,
        name: field("name").value,
        password: field("password").value,
        admin: field("admin").checked,
        active: field("active").checked,
      };
    }
    return {};
  }

  root.addEventListener("click", (event) => {
    const btn = event.target.closest("[data-act]");
    if (!btn || !root.contains(btn)) return;
    window.konsole.post("companion", btn.dataset.act, bodyFor(btn), btn);
  });

  root.addEventListener("change", (event) => {
    const el = event.target;
    if (el.matches("[data-manual]")) {
      window.konsole.post("companion", "manual", { on: el.checked }, el);
    } else if (el.matches("[data-history]")) {
      if (el.checked) window.konsole.post("companion", "history_start", { days: Number($("[data-days]").value) }, el);
      else window.konsole.post("companion", "history_stop", {}, el);
    } else if (el.matches("[data-role]")) {
      if (el.value === "") return;
      window.konsole.post("companion", "history_scale", { role: el.dataset.role, factor: Number(el.value) }, el);
    } else if (el.matches("[data-delay], [data-silent], [data-partial]")) {
      window.konsole.post("companion", "history_faults", faultsBody(), el);
    }
  });

  window.tabs = window.tabs || {};
  window.tabs.companion = { render: render };
})();
</script>
</div>
"""
