"""Oberflaeche des Reiters Companion (HTML-Fragment fuer die Fake-Konsole).

Das Fragment bringt sein Stylesheet und sein Skript mit. Die Seite fuegt es
per innerHTML ein und startet das Skript danach neu. Das Skript setzt
window.tabs.companion = {render, actions}. render(state) ueberschreibt keine
Eingabefelder, die gerade den Fokus haben.
"""

COMPANION_HTML = """<div id="tab-companion" class="co-cards">
<style>
#tab-companion .co-cards { display: grid; gap: 16px; max-width: 760px; }
#tab-companion .card { background: var(--card); border: 1px solid var(--border); border-radius: 8px; padding: 12px 16px; }
#tab-companion h2 { margin: 0 0 8px; font-size: 1.05rem; }
#tab-companion h3 { margin: 12px 0 4px; font-size: 0.95rem; }
#tab-companion .co-row { display: flex; flex-wrap: wrap; gap: 8px; align-items: center; margin: 8px 0; }
#tab-companion .co-field { display: inline-flex; gap: 6px; align-items: center; }
#tab-companion .co-check { display: inline-flex; gap: 6px; align-items: center; margin-right: 12px; }
#tab-companion input[type=text], #tab-companion input[type=password], #tab-companion input[type=number] { padding: 4px 6px; border: 1px solid var(--border); border-radius: 4px; background: var(--bg); color: var(--fg); min-width: 0; max-width: 100%; }
#tab-companion input[type=number] { width: 7em; }
#tab-companion .co-table input[type=text], #tab-companion .co-table input[type=password] { width: 9em; }
#tab-companion button { padding: 5px 10px; border: 1px solid var(--border); border-radius: 6px; background: var(--card); color: var(--fg); cursor: pointer; }
#tab-companion .co-err, #tab-companion .co-state-err { color: #b3261e; margin: 6px 0 0; min-height: 1.2em; }
#tab-companion .co-muted, #tab-companion .co-hint { color: var(--muted); }
#tab-companion .co-hint { font-size: 0.9rem; }
#tab-companion .co-tablewrap { overflow-x: auto; }
#tab-companion table { border-collapse: collapse; }
#tab-companion th, #tab-companion td { text-align: left; padding: 4px 6px; border-bottom: 1px solid var(--border); vertical-align: middle; }
#tab-companion ul { padding-left: 18px; margin: 6px 0; }
</style>

<div class="card" data-card="pair">
  <h2>Kopplung</h2>
  <p class="co-status" data-pair-status></p>
  <p class="co-state-err" data-pair-error></p>
  <div class="co-row">
    <label class="co-field">Code <input type="text" data-code placeholder="XXXX-XXXX-XXXX" autocomplete="off" spellcheck="false"></label>
    <button type="button" data-act="pair">Koppeln</button>
  </div>
  <div class="co-row">
    <button type="button" data-act="unpair">Entkoppeln</button>
    <button type="button" data-act="pair_wrong">Falscher Code</button>
    <button type="button" data-act="drop_stream">Strom trennen</button>
    <button type="button" data-act="open_stream">Strom öffnen</button>
  </div>
  <p class="co-err"></p>
</div>

<div class="card" data-card="users">
  <h2>HA-Benutzer</h2>
  <div class="co-tablewrap">
    <table class="co-table">
      <thead><tr><th>Benutzername</th><th>Name</th><th>Passwort</th><th>Admin</th><th>aktiv</th><th></th></tr></thead>
      <tbody data-users></tbody>
    </table>
  </div>
  <div class="co-row">
    <label class="co-check"><input type="checkbox" data-manual> Prüfungen manuell beantworten</label>
  </div>
  <ul data-pending></ul>
  <p class="co-err"></p>
</div>

<div class="card" data-card="history">
  <h2>Verlauf</h2>
  <p class="co-status" data-hstatus></p>
  <p class="co-state-err" data-hstate-error></p>
  <div class="co-row">
    <label class="co-field">Tage <input type="number" min="1" max="800" data-days></label>
    <button type="button" data-act="history_start">Anmelden</button>
    <button type="button" data-act="history_stop">Abmelden</button>
  </div>
  <div class="co-row" data-factors></div>
  <div class="co-row">
    <button type="button" data-act="history_offer">Neu anbieten</button>
  </div>
  <h3>Fehler</h3>
  <div class="co-row">
    <label class="co-field">Verzögerung in ms <input type="number" min="0" max="60000" data-delay></label>
    <label class="co-check"><input type="checkbox" data-silent> Nicht antworten</label>
    <label class="co-check"><input type="checkbox" data-partial> Nur ersten Block liefern</label>
  </div>
  <p class="co-hint">Eine Skalierung wirkt nur auf Zeiträume, die das Dashboard noch nicht abgeholt hat.</p>
  <p class="co-err"></p>
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
  const ESC = { "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" };
  const esc = (v) => String(v == null ? "" : v).replace(/[&<>"']/g, (c) => ESC[c]);
  const $ = (sel) => root.querySelector(sel);

  let usersKey = null;
  let pendingKey = null;

  // Ein Feld mit Fokus wird nicht ueberschrieben
  const setUnlessFocused = (el, value) => {
    if (el && el !== document.activeElement) {
      if (el.type === "checkbox") el.checked = !!value;
      else el.value = value;
    }
  };
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
      '<td><button type="button" data-act="user_save">Speichern</button> ' +
      '<button type="button" data-act="user_remove">Löschen</button></td></tr>';
  }

  function newRow() {
    return '<tr data-row="">' +
      '<td><input type="text" data-u="username" placeholder="Neu" autocomplete="off"></td>' +
      '<td><input type="text" data-u="name" autocomplete="off"></td>' +
      '<td><input type="password" data-u="password" autocomplete="off"></td>' +
      '<td><input type="checkbox" data-u="admin"></td>' +
      '<td><input type="checkbox" data-u="active" checked></td>' +
      '<td><button type="button" data-act="user_save">Speichern</button></td></tr>';
  }

  function renderPair(ha) {
    let status = "nicht gekoppelt";
    if (ha.paired) status = ha.connected ? "gekoppelt, Strom verbunden" : "gekoppelt, Strom getrennt";
    $("[data-pair-status]").textContent = status;
    $("[data-pair-error]").textContent = ha.error || "";
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
      pending.innerHTML = ha.pending.length
        ? ha.pending.map((p) =>
            "<li>" + esc(p.username) + " " +
            '<button type="button" data-act="resolve" data-id="' + esc(p.id) + '" data-decision="yes">Ja</button> ' +
            '<button type="button" data-act="resolve" data-id="' + esc(p.id) + '" data-decision="no">Nein</button> ' +
            '<button type="button" data-act="resolve" data-id="' + esc(p.id) + '" data-decision="timeout">Timeout</button></li>'
          ).join("")
        : '<li class="co-muted">Keine offenen Prüfungen</li>';
    }
  }

  function renderHistory(h) {
    let status = "abgemeldet";
    if (h.running) status = h.peer_id ? "angemeldet, Peer " + h.peer_id : "verbindet";
    $("[data-hstatus]").textContent = status;
    $("[data-hstate-error]").textContent = h.error || "";

    const grid = $("[data-factors]");
    if (!grid.children.length) {
      grid.innerHTML = Object.keys(h.scale).map((role) =>
        '<label class="co-field">' + esc(ROLE_LABELS[role] || role) +
        ' <input type="number" min="0" max="100" step="0.1" data-role="' + esc(role) + '"></label>'
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

  async function send(card, action, body) {
    const err = card ? card.querySelector(".co-err") : null;
    if (err) err.textContent = "";
    let resp;
    let data;
    try {
      resp = await fetch("/api/companion/" + action, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(body),
      });
      data = await resp.json();
    } catch (e) {
      if (err) err.textContent = "Keine Verbindung zur Konsole.";
      return null;
    }
    if (!resp.ok) {
      if (err) err.textContent = data.error || "Aktion fehlgeschlagen.";
      return null;
    }
    render(data);
    return data;
  }

  root.addEventListener("click", (event) => {
    const btn = event.target.closest("[data-act]");
    if (!btn || !root.contains(btn)) return;
    send(btn.closest(".card"), btn.dataset.act, bodyFor(btn));
  });

  root.addEventListener("change", (event) => {
    const el = event.target;
    const card = el.closest(".card");
    if (el.matches("[data-manual]")) {
      send(card, "manual", { on: el.checked });
    } else if (el.matches("[data-role]")) {
      if (el.value === "") return;
      send(card, "history_scale", { role: el.dataset.role, factor: Number(el.value) });
    } else if (el.matches("[data-delay], [data-silent], [data-partial]")) {
      send(card, "history_faults", faultsBody());
    }
  });

  window.tabs = window.tabs || {};
  window.tabs.companion = { render: render, actions: { send: send } };
})();
</script>
</div>
"""
