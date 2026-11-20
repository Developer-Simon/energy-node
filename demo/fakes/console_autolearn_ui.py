"""Oberflaeche des Reiters Auto-Lernfunktionen der Fake-Konsole (HTML-Fragment).

Zwei Abschnitte. Regelprüfung: eine Zeile je Szenario mit Titel, der Meldung,
die es erzeugen soll, einem Hinweis und einem Knopf zum Einfuegen oder
Entfernen, darunter die Knoepfe fuer alle Szenarien. Lerndaten: Tagesprofile
erzeugen und entfernen, Hinweis "neu aktiv" zuruecksetzen. Das Skript setzt
window.tabs.autolearn, Aktionen laufen ueber window.konsole.post.
"""

AUTOLEARN_HTML = """<div id="tab-autolearn">
<style>
#tab-autolearn .chk-row { align-items: flex-start; }
#tab-autolearn .chk-text { flex: 1; min-width: 0; display: flex; flex-direction: column; gap: 2px; }
#tab-autolearn .chk-expect { font-size: 0.92em; }
#tab-autolearn .chk-note { font-size: 0.88em; color: var(--muted, inherit); }
#tab-autolearn input[type="number"] { width: 5em; }
</style>

<div class="panel-head">
  <h2>Auto-Lernfunktionen</h2>
  <p class="panel-meta">Beispielregeln für die Regelprüfung und Lerndaten der Demo</p>
</div>

<div class="grp" data-group="scenarios">
  <h3>Regelprüfung</h3>
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

<div class="grp" data-group="learning">
  <h3>Lerndaten</h3>
  <div class="list">
    <div class="row">
      <span class="v">Vergangene Tage</span>
      <span class="end">
        <label>Tage <input type="number" min="1" max="56" step="1" value="21" data-days aria-label="Tage"></label>
        <button type="button" class="btn btn-primary" data-act="day_create">Tagesprofile erzeugen</button>
      </span>
    </div>
    <div class="row">
      <span class="v">Tagesprofile entfernen</span>
      <span class="end">
        <button type="button" class="btn" data-act="day_remove">Tagesprofile entfernen</button>
      </span>
    </div>
    <div class="row">
      <span class="v">Hinweis „neu aktiv“ zurücksetzen</span>
      <span class="end">
        <button type="button" class="btn" data-act="ack_reset">Hinweis „neu aktiv“ zurücksetzen</button>
      </span>
    </div>
  </div>
  <p class="note">Erzeugte Tagesprofile tragen eine Kennzeichnung. Entfernen löscht nur diese Dateien, die echten Tagesprofile bleiben. Die Profile lassen Vorschläge für Schaltzeit, vergessene Laufzeit, Kopplung und überstimmte Regel erscheinen. Dafür müssen die Schalter Muster lernen sowie Gewohnheiten und Zuverlässigkeit eingeschaltet sein. Die Erkenner laufen einmal am Tag um 03:30 Uhr, oder fünf Minuten nach dem Start, wenn der letzte Lauf mehr als einen Tag zurückliegt. Dann liest das Dashboard die Tagesprofile neu, ein Neustart ist dafür nicht nötig. Der Hinweis „neu aktiv“ erscheint erst nach einem Neustart des Dashboards wieder, es liest die Einstellungen im Speicher.</p>
  <p class="grp-err"></p>
</div>

<script>
(function () {
  const root = document.getElementById("tab-autolearn");
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
    if (action === "insert_all" || action === "remove_all" || action === "day_remove" || action === "ack_reset") {
      window.konsole.post("autolearn", action, {}, btn);
    } else if (action === "day_create") {
      const input = root.querySelector("[data-days]");
      window.konsole.post("autolearn", action, { days: Number(input.value) }, btn);
    } else {
      window.konsole.post("autolearn", action, { scenario: btn.dataset.scenario }, btn);
    }
  });

  window.tabs = window.tabs || {};
  window.tabs.autolearn = { render: render };
})();
</script>
</div>
"""
