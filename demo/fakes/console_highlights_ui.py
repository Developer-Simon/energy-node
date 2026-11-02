"""Oberflaeche des Reiters Highlights der Fake-Konsole (HTML-Fragment).

Eine Eingabe fuer die Startversion, ein Knopf zum Rendern und darunter die
gerenderte Seite in einem iframe. Das Skript setzt window.tabs.highlights,
Aktionen laufen ueber window.konsole.post.
"""

HIGHLIGHTS_HTML = """<div id="tab-highlights">
<style>
#tab-highlights input[data-since] { width: 9em; }
#tab-highlights iframe { width: 100%; height: 70vh; border: 1px solid var(--sep); border-radius: 10px; background: #fff; }
#tab-highlights iframe[hidden] { display: none; }
</style>

<div class="panel-head">
  <h2>Highlights</h2>
  <p class="panel-meta">Highlights der Changelog-Abschnitte mit Details und Bildern</p>
</div>

<div class="grp" data-group="render">
  <h3>Vorschau</h3>
  <div class="list">
    <div class="row">Abschnitte neuer als
      <input type="text" data-since placeholder="v0.10.0" autocomplete="off" spellcheck="false" aria-label="Startversion">
      <span class="end"><a class="btn" data-explore target="_blank" rel="noopener" hidden>Neues erkunden öffnen</a> <button type="button" class="btn btn-primary" data-act="render">Rendern</button></span>
    </div>
  </div>
  <p class="note">Liest dashboard/CHANGELOG.md, changelog/highlights und docs/images frisch aus dem Repo. Mit Installer-Zustand steht das Ergebnis danach auch im Dashboard unter Neues erkunden. Die Seite lädt es beim Öffnen, ein offener Dashboard-Tab braucht einen Reload.</p>
  <p class="grp-err"></p>
</div>

<iframe data-frame title="Highlights" hidden></iframe>
<script>
(function () {
  const root = document.getElementById("tab-highlights");
  if (!root) return;
  const $ = (sel) => root.querySelector(sel);
  let shown = 0;

  function render(state) {
    window.konsole.setUnlessFocused($("[data-since]"), state.since);
    const frame = $("[data-frame]");
    if (state.rendered && state.count !== shown) {
      shown = state.count;
      frame.src = "/highlights/?r=" + state.count;
    }
    frame.hidden = !state.rendered;
    const link = $("[data-explore]");
    link.hidden = !(state.rendered && state.explore_url);
    if (state.explore_url) link.href = state.explore_url;
  }

  root.addEventListener("click", (event) => {
    const btn = event.target.closest("[data-act]");
    if (!btn || !root.contains(btn)) return;
    if (btn.dataset.act === "render") {
      window.konsole.post("highlights", "render", { since: $("[data-since]").value.trim() }, btn);
    }
  });

  window.tabs = window.tabs || {};
  window.tabs.highlights = { render: render };
})();
</script>
</div>
"""
