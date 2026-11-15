"""Oberflaeche des Reiters Leistung der Fake-Konsole (HTML-Fragment).

Gleiche Bausteine wie die anderen Reiter. performance_html() setzt die
Szenarien, die Einheiten und die Dienste ein. Das Skript setzt
window.tabs.performance. Aktionen laufen ueber window.konsole.post.
"""

PERFORMANCE_HTML = """<div id="tab-performance">
<style>
#tab-performance .perf-presets { display: flex; flex-wrap: wrap; gap: 6px; }
#tab-performance input[type="number"] { width: 6.5em; }
#tab-performance .perf-units { width: 100%; border-collapse: collapse; }
#tab-performance .perf-units th,
#tab-performance .perf-units td { padding: 4px 6px; text-align: left; font-variant-numeric: tabular-nums; }
#tab-performance .perf-units input[type="number"] { width: 5em; }
#tab-performance .btn[aria-pressed="true"] { outline: 2px solid var(--accent); outline-offset: 2px; }
</style>

<div class="panel-head">
  <h2>Leistung</h2>
  <p class="panel-meta">Szenario: <span data-current>Alles gut</span></p>
</div>

<div class="grp" data-group="presets">
  <h3>Szenario</h3>
  <div class="list">
    <div class="row perf-presets">
__PRESET_BUTTONS__
    </div>
  </div>
  <p class="note">Ein Szenario ersetzt alle Werte unten. Jede Änderung unten macht daraus eigene Werte.</p>
  <p class="grp-err"></p>
</div>

<div class="grp" data-group="values">
  <h3>Werte</h3>
  <div class="list">
    <div class="row">Messdauer
      <span class="end">
        <input type="number" min="0" step="1" data-hours aria-label="Stunden"> Std.
        <input type="number" min="0" max="59" step="1" data-minutes aria-label="Minuten"> Min.
        <button type="button" class="btn" data-act="hour">Messdauer um 1 Stunde erhöhen</button>
      </span>
    </div>
    <div class="row">Kerne <input type="number" min="1" max="64" step="1" data-global="cores" aria-label="Kerne"></div>
    <div class="row">CPU-Last (P95) in % <input type="number" min="0" max="100" step="0.1" data-global="cpu_pct" aria-label="CPU-Last in Prozent"></div>
    <div class="row">Temperatur in °C
      <input type="number" min="-20" max="120" step="0.1" data-global="temp_c" aria-label="Temperatur in °C">
      <label><input type="checkbox" data-temp-unknown aria-label="Temperatur unbekannt"> unbekannt</label>
    </div>
    <div class="row">Gedrosselt
      <span class="end"><label class="switch"><input type="checkbox" data-global="throttled" aria-label="Gedrosselt"><span></span></label></span>
    </div>
    <div class="row">RAM frei in % <input type="number" min="0" max="100" step="0.1" data-global="ram_free_pct" aria-label="RAM frei in Prozent"></div>
    <div class="row">Swap-Seiten pro s <input type="number" min="0" step="0.1" data-global="swap_pages_s" aria-label="Swap-Seiten pro Sekunde"></div>
    <div class="row">Browser-Clients <input type="number" min="1" step="1" data-global="clients_max" aria-label="Browser-Clients"></div>
    <div class="row">Fremdgerät
      <input type="text" data-external="name" placeholder="Name" autocomplete="off" spellcheck="false" aria-label="Name des Fremdgeräts">
      alle <input type="number" min="0" step="0.1" data-external="every_s" aria-label="Intervall in Sekunden"> s
      <label><input type="checkbox" data-external-none aria-label="Kein Fremdgerät"> keins</label>
    </div>
  </div>
  <p class="note">Die Messdauer zählt die Stunden und Minuten, die das Dashboard schon gemessen hat. Eine Temperatur ohne Messung zeigt das Dashboard als unbekannt.</p>
  <p class="grp-err"></p>
</div>

<div class="grp" data-group="units">
  <h3>Einheiten</h3>
  <div class="list">
    <table class="perf-units">
      <thead>
        <tr><th>Einheit</th><th>Aktiv</th><th>CPU %</th><th>RAM MB</th><th>Runde s</th><th>Nachrichten/min</th></tr>
      </thead>
      <tbody>
__UNIT_ROWS__
      </tbody>
    </table>
  </div>
  <p class="note">Eine Einheit ohne Aktiv fehlt im Dashboard. Die Dienste haben Rundenzeit und Nachrichten, die Systemdienste nur CPU und RAM.</p>
  <p class="grp-err"></p>
</div>
<script>
(function () {
  const root = document.getElementById("tab-performance");
  if (!root) return;

  const $ = (sel) => root.querySelector(sel);
  const setUnlessFocused = (el, value) => window.konsole.setUnlessFocused(el, value);
  const num = (el) => (el.value === "" ? null : Number(el.value));
  const fieldValue = (el) => (el.type === "checkbox" ? el.checked : num(el));
  const post = (action, body, el) => window.konsole.post("performance", action, body, el);

  function render(state) {
    const v = state.values;
    root.querySelectorAll('[data-act="preset"]').forEach((btn) => {
      btn.setAttribute("aria-pressed", String(btn.dataset.value === state.preset));
    });
    $("[data-current]").textContent = state.preset_label;

    const measured = Number(v.measured_s) || 0;
    setUnlessFocused($("[data-hours]"), Math.floor(measured / 3600));
    setUnlessFocused($("[data-minutes]"), Math.floor((measured % 3600) / 60));

    root.querySelectorAll("[data-global]").forEach((el) => {
      if (el.dataset.global !== "temp_c") setUnlessFocused(el, v[el.dataset.global]);
    });
    const unknown = v.temp_c === null;
    setUnlessFocused($("[data-temp-unknown]"), unknown);
    if (!unknown) setUnlessFocused($('[data-global="temp_c"]'), v.temp_c);

    const none = v.external === null;
    setUnlessFocused($("[data-external-none]"), none);
    ["name", "every_s"].forEach((key) => {
      const el = $('[data-external="' + key + '"]');
      el.disabled = none;
      if (!none) setUnlessFocused(el, v.external[key]);
    });

    state.units.forEach((u) => {
      const row = root.querySelector('[data-unit="' + u.id + '"]');
      if (!row) return;
      setUnlessFocused(row.querySelector('[data-field="present"]'), u.present);
      ["cpu_pct", "ram_mb", "round_s", "msg_per_min"].forEach((field) => {
        const input = row.querySelector('[data-field="' + field + '"]');
        if (input) setUnlessFocused(input, u[field]);
      });
    });
  }

  root.addEventListener("click", (event) => {
    const btn = event.target.closest('[data-act="preset"], [data-act="hour"]');
    if (!btn || !root.contains(btn)) return;
    if (btn.dataset.act === "preset") post("preset", { name: btn.dataset.value }, btn);
    else post("hour", {}, btn);
  });

  root.addEventListener("change", (event) => {
    const el = event.target;
    if (!root.contains(el)) return;
    if (el.dataset.global !== undefined) {
      post("global", { [el.dataset.global]: fieldValue(el) }, el);
      if (el.dataset.global === "temp_c") $("[data-temp-unknown]").checked = false;
    } else if (el.hasAttribute("data-temp-unknown")) {
      const temp = $('[data-global="temp_c"]');
      post("global", { temp_c: el.checked ? null : num(temp) }, el);
    } else if (el.hasAttribute("data-hours") || el.hasAttribute("data-minutes")) {
      const seconds = (num($("[data-hours]")) || 0) * 3600 + (num($("[data-minutes]")) || 0) * 60;
      post("global", { measured_s: seconds }, el);
    } else if (el.dataset.external !== undefined) {
      const value = el.dataset.external === "every_s" ? num(el) : el.value;
      post("external", { [el.dataset.external]: value }, el);
    } else if (el.hasAttribute("data-external-none")) {
      if (el.checked) {
        post("external", { none: true }, el);
      } else {
        post("external", {
          none: false,
          name: $('[data-external="name"]').value,
          every_s: num($('[data-external="every_s"]')),
        }, el);
      }
    } else if (el.dataset.field !== undefined) {
      const row = el.closest("[data-unit]");
      post("unit", { id: row.dataset.unit, [el.dataset.field]: fieldValue(el) }, el);
    }
  });

  window.tabs = window.tabs || {};
  window.tabs.performance = { render: render };
})();
</script>
</div>
"""


def _preset_buttons(preset_labels: dict) -> str:
    return "\n".join(
        f'      <button type="button" class="btn" data-act="preset" data-value="{name}" aria-pressed="false">{label}</button>'
        for name, label in preset_labels.items()
    )


def _unit_row(unit_id: str, service: bool) -> str:
    cells = [
        f"<td>{unit_id}</td>",
        '<td><label class="switch"><input type="checkbox" data-field="present" '
        f'aria-label="{unit_id} aktiv"><span></span></label></td>',
        '<td><input type="number" min="0" max="100" step="0.1" data-field="cpu_pct" '
        f'aria-label="{unit_id} CPU in Prozent"></td>',
        '<td><input type="number" min="0" step="0.1" data-field="ram_mb" '
        f'aria-label="{unit_id} RAM in MB"></td>',
    ]
    if service:
        cells.append('<td><input type="number" min="0" step="0.1" data-field="round_s" '
                     f'aria-label="{unit_id} Runde in Sekunden"></td>')
        cells.append('<td><input type="number" min="0" step="1" data-field="msg_per_min" '
                     f'aria-label="{unit_id} Nachrichten pro Minute"></td>')
    else:
        cells.append("<td>entfällt</td><td>entfällt</td>")
    return f'        <tr data-unit="{unit_id}">' + "".join(cells) + "</tr>"


def performance_html(preset_labels: dict, unit_ids, service_units) -> str:
    """Fragment fuer den Reiter: Szenario-Schaltflaechen, Werte und Tabelle der Einheiten."""
    rows = "\n".join(_unit_row(unit_id, unit_id in service_units) for unit_id in unit_ids)
    html = PERFORMANCE_HTML.replace("__PRESET_BUTTONS__", _preset_buttons(preset_labels))
    return html.replace("__UNIT_ROWS__", rows)
