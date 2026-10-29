"""Oberflaeche des Reiters Updates der Fake-Konsole (HTML-Fragment).

Gleiche Bausteine wie der MQTT-Reiter. updates_html() setzt den Port des
GitHub-Fakes ein und laesst den Installer-Abschnitt weg, wenn kein
Installer-Zustand eingerichtet ist. Das Skript setzt window.tabs.updates.
Aktionen laufen ueber window.konsole.post.
"""

INSTALLER_GROUP_HTML = """<div class="grp" data-group="installer">
  <h3>Installer-Zustand</h3>
  <div class="list">
    <div class="row">Installierte Version <span class="v" data-installed-text>keine</span>
      <input type="text" class="mono" data-installed style="width:8em" placeholder="v1.2.3" autocomplete="off" spellcheck="false" aria-label="Installierte Version">
      <span class="end"><button type="button" class="btn" data-act="installer_version">Setzen</button></span>
    </div>
    <div class="row"><button type="button" class="btn" data-act="installer_none">Keine Installation</button></div>
    <div class="row">Vorherige Version vorhanden
      <label class="switch"><input type="checkbox" data-previous aria-label="Vorherige Version vorhanden"><span></span></label>
    </div>
    <div class="row"><button type="button" class="btn btn-danger" data-act="installer_reset">Auf Fixture zurücksetzen</button></div>
  </div>
  <p class="note">Das Dashboard liest diese Dateien bei jedem Abruf neu.</p>
  <p class="grp-err"></p>
</div>
"""

UPDATES_HTML = """<div id="tab-updates" data-port="__PORT__">
<style>
#tab-updates input[type="range"] { flex: 1; min-width: 8em; }
#tab-updates [data-tag] { width: 8em; }
</style>

<div class="panel-head">
  <h2>Updates</h2>
  <p class="panel-meta" data-meta></p>
</div>

<div class="grp" data-group="release">
  <h3>Angebotenes Release</h3>
  <div class="list">
    <div class="row">Version
      <input type="text" class="mono" data-tag placeholder="v1.2.3" autocomplete="off" spellcheck="false" aria-label="Version">
      <span class="end"><button type="button" class="btn btn-primary" data-act="release">Übernehmen</button></span>
    </div>
    <div class="row">Antwort
      <span class="end"><span class="seg" data-mode>
        <button type="button" data-act="release" data-value="update" aria-pressed="false">Update</button>
        <button type="button" data-act="release" data-value="none" aria-pressed="false">Kein Update</button>
        <button type="button" data-act="release" data-value="error" aria-pressed="false">Fehler 500</button>
        <button type="button" data-act="release" data-value="ratelimit" aria-pressed="false">Rate-Limit</button>
      </span></span>
    </div>
  </div>
  <p class="note">Die Version ändert nur den Update-Check, das Paket zum Herunterladen bleibt dasselbe. Nach einer Änderung im Dashboard erneut nach Updates suchen.</p>
  <p class="grp-err"></p>
</div>

<div class="grp" data-group="download">
  <h3>Download</h3>
  <div class="list">
    <div class="row">Dauer
      <input type="range" min="0" max="120" step="1" data-seconds aria-label="Dauer in Sekunden">
      <span class="v" data-seconds-text>0 s</span>
    </div>
    <div class="row">Abbrechen
      <span class="end"><span class="seg" data-abort>
        <button type="button" data-act="download" data-value="never" aria-pressed="false">Nie</button>
        <button type="button" data-act="download" data-value="half" aria-pressed="false">Bei 50 %</button>
        <button type="button" data-act="download" data-value="start" aria-pressed="false">Sofort</button>
      </span></span>
    </div>
  </div>
  <p class="grp-err"></p>
</div>
__INSTALLER__</div>
<script>
(function () {
  const root = document.getElementById("tab-updates");
  if (!root) return;

  const $ = (sel) => root.querySelector(sel);
  const setUnlessFocused = (el, value) => window.konsole.setUnlessFocused(el, value);

  function setPressed(container, value) {
    container.querySelectorAll("button").forEach((btn) => {
      btn.setAttribute("aria-pressed", btn.dataset.value === value ? "true" : "false");
    });
  }

  function render(state) {
    const release = state.release;
    if (!state.reachable || !release) {
      $("[data-meta]").textContent = "GitHub-Fake nicht erreichbar";
      return;
    }
    const installer = state.installer;
    const fixture = !installer || installer.matches_fixture;
    $("[data-meta]").textContent = "GitHub-Fake auf Port " + root.dataset.port + ", Installer-Zustand " +
      (fixture ? "aus Fixture" : "verändert");

    setUnlessFocused($("[data-tag]"), release.tag);
    setPressed($("[data-mode]"), release.mode);
    setUnlessFocused($("[data-seconds]"), String(Math.round(release.seconds)));
    $("[data-seconds-text]").textContent = Math.round(release.seconds) + " s";
    setPressed($("[data-abort]"), release.abort);

    const group = $("[data-group=installer]");
    if (group && installer) {
      $("[data-installed-text]").textContent = installer.version || "keine";
      setUnlessFocused($("[data-previous]"), installer.previous);
    }
  }

  function bodyFor(btn) {
    switch (btn.dataset.act) {
      case "release":
        return btn.dataset.value ? { mode: btn.dataset.value } : { tag: $("[data-tag]").value };
      case "download":
        return { abort: btn.dataset.value };
      case "installer_version":
        return { version: $("[data-installed]").value };
      default:
        return {};
    }
  }

  root.addEventListener("click", (event) => {
    const btn = event.target.closest("[data-act]");
    if (!btn || !root.contains(btn)) return;
    window.konsole.post("updates", btn.dataset.act, bodyFor(btn), btn);
  });

  root.addEventListener("change", (event) => {
    const el = event.target;
    if (el.matches("[data-seconds]")) {
      window.konsole.post("updates", "download", { seconds: Number(el.value) }, el);
    } else if (el.matches("[data-previous]")) {
      window.konsole.post("updates", "installer_previous", { present: el.checked }, el);
    }
  });

  window.tabs = window.tabs || {};
  window.tabs.updates = { render: render };
})();
</script>
</div>
"""


def updates_html(port: int | None, installer: bool) -> str:
    """Fragment fuer den Reiter: Port des GitHub-Fakes, Installer-Abschnitt nur bei Bedarf."""
    html = UPDATES_HTML.replace("__PORT__", str(port))
    return html.replace("__INSTALLER__", INSTALLER_GROUP_HTML if installer else "")
