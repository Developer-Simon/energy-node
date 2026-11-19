"""Oberflaeche des Reiters Regelprüfung der Fake-Konsole (HTML-Fragment).

Eine Zeile je Szenario mit Titel, der Meldung, die es erzeugen soll, einem
Hinweis und einem Knopf zum Einfuegen oder Entfernen. Darunter die Knoepfe
fuer alle Szenarien. Das Skript setzt window.tabs.rulecheck, Aktionen laufen
ueber window.konsole.post.
"""

RULECHECK_HTML = """<div id="tab-rulecheck">
<style>
#tab-rulecheck .chk-row { align-items: flex-start; }
#tab-rulecheck .chk-text { flex: 1; min-width: 0; display: flex; flex-direction: column; gap: 2px; }
#tab-rulecheck .chk-expect { font-size: 0.92em; }
#tab-rulecheck .chk-note { font-size: 0.88em; color: var(--muted, inherit); }
</style>

<div class="panel-head">
  <h2>Regelprüfung</h2>
  <p class="panel-meta">Beispielregeln für jede Meldung der Regelprüfung</p>
</div>

<div class="grp" data-group="scenarios">
  <h3>Beispiele</h3>
  <div class="list" data-rows></div>
  <p class="note">Die Beispiele stehen in der Regeldatei der Demo. Die Seite Automationen danach neu laden, die Meldung erscheint mit der nächsten Aktualisierung der Prüfung.</p>
  <p class="grp-err"></p>
</div>

<div class="grp" data-group="all">
  <h3>Alle</h3>
  <div class="list">
    <div class="row">
      <span class="v">Alle Beispiele</span>
      <span class="end">
        <button type="button" class="btn btn-primary" data-act="insert_all">Alle einfügen</button>
        <button type="button" class="btn" data-act="remove_all">Alle entfernen</button>
      </span>
    </div>
  </div>
  <p class="grp-err"></p>
</div>

<script>
(function () {
  const root = document.getElementById("tab-rulecheck");
  if (!root) return;
  const rows = root.querySelector("[data-rows]");

  function scenarioRow(item) {
    const el = document.createElement("div");
    el.className = "row chk-row";

    const text = document.createElement("div");
    text.className = "chk-text";
    const title = document.createElement("strong");
    title.textContent = item.title;
    const expect = document.createElement("span");
    expect.className = "chk-expect";
    expect.textContent = "Meldung: " + item.expect;
    const note = document.createElement("span");
    note.className = "chk-note";
    note.textContent = item.note;
    text.append(title, expect, note);

    const end = document.createElement("span");
    end.className = "end";
    if (item.triggerable) {
      const trigger = document.createElement("button");
      trigger.type = "button";
      trigger.className = "btn";
      trigger.dataset.act = "trigger";
      trigger.dataset.scenario = item.id;
      trigger.textContent = "4× auslösen";
      end.append(trigger);
    }
    const btn = document.createElement("button");
    btn.type = "button";
    btn.className = item.inserted ? "btn" : "btn btn-primary";
    btn.dataset.act = item.inserted ? "remove" : "insert";
    btn.dataset.scenario = item.id;
    btn.textContent = item.inserted ? "Entfernen" : "Einfügen";
    end.append(btn);

    el.append(text, end);
    return el;
  }

  function render(state) {
    rows.replaceChildren(...state.map(scenarioRow));
  }

  root.addEventListener("click", (event) => {
    const btn = event.target.closest("[data-act]");
    if (!btn || !root.contains(btn)) return;
    const action = btn.dataset.act;
    if (action === "insert_all" || action === "remove_all") {
      window.konsole.post("rulecheck", action, {}, btn);
    } else {
      window.konsole.post("rulecheck", action, { scenario: btn.dataset.scenario }, btn);
    }
  });

  window.tabs = window.tabs || {};
  window.tabs.rulecheck = { render: render };
})();
</script>
</div>
"""
