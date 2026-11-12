"""Oberflaeche des Reiters Speicher der Fake-Konsole (HTML-Fragment).

Gleiche Bausteine wie die anderen Reiter. Die Sticks kommen aus dem Katalog,
die Zustandszeilen aus dem Reiter. Das Skript setzt window.tabs.storage.
Aktionen laufen ueber window.konsole.post.
"""

import html

STORAGE_HTML = """<div id="tab-storage">
<div class="panel-head">
  <h2>Speicher</h2>
  <p class="panel-meta">Demo-Laufwerk am Einhängepunkt</p>
</div>

<div class="grp" data-group="sticks">
  <h3>Sticks</h3>
  <div class="list">
__STICK_ROWS__
  </div>
  <p class="note">Ein gezogener Stick behält seinen Inhalt. Steckt der eingehängte Stick ab, ist der Einhängepunkt leer, wie beim Dashboard ohne Laufwerk.</p>
  <p class="grp-err"></p>
</div>

<div class="grp" data-group="status">
  <h3>Zustand</h3>
  <div class="list">
    <div class="row"><span data-status-plugged>Eingesteckt</span></div>
    <div class="row"><span data-status-mounted>Nichts eingehängt</span></div>
  </div>
</div>

<div class="grp" data-group="faults">
  <h3>Fehler</h3>
  <div class="list">
    <div class="row">Formatieren
      <span class="end"><span class="seg" data-fault="format">
        <button type="button" data-act="fault" data-name="format" data-value="0" aria-pressed="false">Normal</button>
        <button type="button" data-act="fault" data-name="format" data-value="1" aria-pressed="false">Schlägt fehl</button>
      </span></span>
    </div>
    <div class="row">Einhängen
      <span class="end"><span class="seg" data-fault="mount">
        <button type="button" data-act="fault" data-name="mount" data-value="0" aria-pressed="false">Normal</button>
        <button type="button" data-act="fault" data-name="mount" data-value="1" aria-pressed="false">Schlägt fehl</button>
      </span></span>
    </div>
  </div>
  <p class="note">Schlägt Formatieren fehl, bleibt der Stick unverändert. Schlägt Einhängen fehl, bleibt der Einhängepunkt unverändert.</p>
  <p class="grp-err"></p>
</div>
<script>
(function () {
  const root = document.getElementById("tab-storage");
  if (!root) return;

  const $ = (sel) => root.querySelector(sel);

  function setPressed(container, on) {
    if (!container) return;
    container.querySelectorAll("button").forEach((btn) => {
      btn.setAttribute("aria-pressed", btn.dataset.value === (on ? "1" : "0") ? "true" : "false");
    });
  }

  function render(state) {
    state.sticks.forEach((stick) => {
      const row = root.querySelector('[data-stick="' + stick.kname + '"]');
      if (!row) return;
      const status = row.querySelector("[data-stick-status]");
      const btn = row.querySelector("[data-act='plug']");
      status.textContent = stick.plugged
        ? "eingesteckt" + (stick.active ? ", eingehängt" : "")
        : "gezogen";
      btn.textContent = stick.plugged ? "Ziehen" : "Einstecken";
      btn.dataset.next = stick.plugged ? "0" : "1";
    });
    setPressed($('[data-fault="format"]'), state.format_fails);
    setPressed($('[data-fault="mount"]'), state.mount_fails);
    $("[data-status-plugged]").textContent = state.plugged_text;
    $("[data-status-mounted]").textContent = state.mounted_text;
  }

  root.addEventListener("click", (event) => {
    const btn = event.target.closest("[data-act]");
    if (!btn || !root.contains(btn)) return;
    if (btn.dataset.act === "plug") {
      window.konsole.post("storage", "plug",
        { kname: btn.dataset.kname, plugged: btn.dataset.next === "1" }, btn);
    } else if (btn.dataset.act === "fault") {
      window.konsole.post("storage", "fault",
        { name: btn.dataset.name, on: btn.dataset.value === "1" }, btn);
    }
  });

  window.tabs = window.tabs || {};
  window.tabs.storage = { render: render };
})();
</script>
</div>
"""


def _stick_row(stick: dict) -> str:
    kname = html.escape(stick["kname"])
    name = html.escape(stick["name"])
    return (f'    <div class="row" data-stick="{kname}">{name} ({kname})'
            f'<span class="end"><span class="v" data-stick-status></span> '
            f'<button type="button" class="btn" data-act="plug" data-kname="{kname}" data-next="0">Ziehen</button>'
            f"</span></div>")


def storage_html(catalog: list) -> str:
    """Fragment fuer den Reiter: je Stick aus dem Katalog eine Zeile mit Einstecken oder Ziehen."""
    rows = "\n".join(_stick_row(stick) for stick in catalog)
    return STORAGE_HTML.replace("__STICK_ROWS__", rows)
