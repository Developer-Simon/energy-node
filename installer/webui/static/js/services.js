// Wie die Oberflaeche Schritte zu Diensten ordnet. Konfiguration, Vorschau,
// Ausfuehrung und Diagnose zeigen dieselbe Ordnung - sie entsteht hier und
// nur hier, aus manifest.steps[].kind (E7: ein neuer Dienst braucht keine
// Aenderung an der Oberflaeche).
(function () {
  'use strict';

  // Schritt 60 installiert das Dashboard-Binary; die MQTT-Bruecke laeuft darin.
  var CORE_STEP = '60';
  var DASHBOARD_UNIT = 'energy-node-dashboard.service';
  // Optionale Systemschritte, die in der Ausfuehrung keine eigene Station
  // bekommen, sondern unter der eines anderen laufen: 35 (Opt-in-Freigabe
  // des Shelly-Wake-Webhooks) ist eine weitere Firewall-Regel. In der
  // Konfiguration bleibt er ein eigener Schalter.
  var RUN_GROUP_OF = { '35': '30' };

  function optionalText(shell, key) {
    var text = shell.t(key);
    return text === key ? '' : text;
  }

  var Services = {
    CORE_STEP: CORE_STEP,
    DASHBOARD_UNIT: DASHBOARD_UNIT,
    // Die Bibliotheken, die scripts/build/lib/wheels.sh als eigene Wheels
    // baut (Vorschau und Diagnose). Ein Test haelt die Liste gegen das Skript.
    WHEEL_COMPONENTS: ['energy_node_common', 'battery_soc_core'],

    // isSelected: fuer Dienste ist ein fehlender Schluessel "aus", nie
    // "default" - sonst braechte ein Update einen neuen Dienst ungefragt
    // mit. Ein Systemschritt ohne Schluessel folgt dagegen der
    // Manifest-Vorgabe, wie step_selected und plan.sh (15 kam per Update).
    isSelected: function (step, selection) {
      if (!step.optional) {
        return true;
      }
      var steps = (selection && selection.steps) || null;
      if (steps && Object.prototype.hasOwnProperty.call(steps, step.id)) {
        return steps[step.id] === true;
      }
      return !step.service_id && !!steps && step.default === true;
    },

    // requiresMet: ein Schritt mit "requires" (manifest.json) zaehlt nur,
    // solange der benoetigte Schritt gewaehlt ist - 35 (Webhook-Port) nur
    // mit dem Shelly-Dienst 83. Fehlt der benoetigte Schritt im Manifest,
    // ist die Bedingung nicht erfuellt.
    requiresMet: function (manifest, step, selection) {
      if (!step.requires) {
        return true;
      }
      var needed = ((manifest && manifest.steps) || []).filter(function (s) { return s.id === step.requires; })[0];
      return !!needed && Services.isSelected(needed, selection);
    },

    // dropUnmet schaltet in der Auswahl jeden Schritt ab, dessen benoetigter
    // Schritt aus ist - sonst bliebe nach dem Abwaehlen von Shelly ein
    // unsichtbares Opt-in fuer den Webhook-Port gespeichert.
    dropUnmet: function (manifest, steps) {
      ((manifest && manifest.steps) || []).forEach(function (step) {
        if (step.requires && steps[step.id] === true && !Services.requiresMet(manifest, step, { steps: steps })) {
          steps[step.id] = false;
        }
      });
    },

    isKnown: function (step, selection) {
      return !!(selection && selection.steps && Object.prototype.hasOwnProperty.call(selection.steps, step.id));
    },

    serviceName: function (step, shell) {
      return optionalText(shell, 'service.' + step.service_id) || step.service_id;
    },

    stepName: function (id, shell) {
      return shell.t('step.' + id);
    },

    stepLabel: function (manifest, id, shell) {
      var step = ((manifest && manifest.steps) || []).filter(function (s) { return s.id === id; })[0];
      return step && step.service_id ? Services.serviceName(step, shell) : Services.stepName(id, shell);
    },

    // split: Dienste mit eigener Zeile, Geraete-Dienste, optionale
    // Systemschritte - jeweils in Manifest-Reihenfolge.
    split: function (manifest) {
      var out = { services: [], devices: [], system: [] };
      ((manifest && manifest.steps) || []).forEach(function (step) {
        if (step.service_id) {
          (step.kind === 'device' ? out.devices : out.services).push(step);
        } else if (step.optional) {
          out.system.push(step);
        }
      });
      return out;
    },

    toggles: function (manifest, selection, shell) {
      var parts = Services.split(manifest);
      var rows = parts.services.map(function (step) {
        return {
          key: 'service-' + step.id, kind: 'service', ids: [step.id],
          name: Services.serviceName(step, shell),
          hint: optionalText(shell, 'configure.hint.service.' + step.service_id),
          on: Services.isSelected(step, selection), known: Services.isKnown(step, selection), chips: null,
        };
      });
      if (parts.devices.length) {
        var chips = parts.devices.map(function (step) {
          return { id: step.id, name: Services.serviceName(step, shell), on: Services.isSelected(step, selection), known: Services.isKnown(step, selection) };
        });
        rows.push({
          key: 'devices', kind: 'devices', ids: parts.devices.map(function (step) { return step.id; }),
          name: shell.t('configure.devices.title'), hint: optionalText(shell, 'configure.hint.devices'),
          on: chips.some(function (chip) { return chip.on; }), known: true, chips: chips,
        });
      }
      parts.system.forEach(function (step) {
        // Erst sichtbar, wenn der benoetigte Schritt an ist (35 mit Shelly).
        if (!Services.requiresMet(manifest, step, selection)) {
          return;
        }
        rows.push({
          key: 'step-' + step.id, kind: 'system', ids: [step.id],
          name: Services.stepName(step.id, shell),
          hint: optionalText(shell, 'configure.hint.step.' + step.id),
          on: Services.isSelected(step, selection), known: Services.isKnown(step, selection), chips: null,
        });
      });
      return rows;
    },

    // runGroups: die Stationen der Ausfuehrung, eine je Schritt in
    // Manifest-Reihenfolge - so laufen sie auch, und die Leiste springt nie
    // zurueck. Dienste stehen wie Systemschritte in der Liste.
    runGroups: function (manifest, shell) {
      var steps = (manifest && manifest.steps) || [];
      var present = {};
      steps.forEach(function (step) { present[step.id] = true; });
      var groups = [];
      steps.forEach(function (step) {
        if (present[RUN_GROUP_OF[step.id]]) {
          return;
        }
        var ids = [step.id].concat(steps.filter(function (other) {
          return RUN_GROUP_OF[other.id] === step.id;
        }).map(function (other) { return other.id; }));
        groups.push({ key: 'step-' + step.id, label: Services.stepLabel(manifest, step.id, shell), ids: ids });
      });
      return groups;
    },

    // serviceUnits: die Dienste des Node mit ihrer Unit - MQTT-Bruecke und
    // Dashboard (Schritt 60) laufen immer, die uebrigen nach Auswahl.
    serviceUnits: function (manifest, selection, shell) {
      var parts = Services.split(manifest);
      return [{ key: 'bridge', name: shell.t('service.bridge'), on: true, unit: DASHBOARD_UNIT }]
        .concat(parts.services.concat(parts.devices).map(function (step) {
          return { key: step.id, name: Services.serviceName(step, shell), on: Services.isSelected(step, selection), unit: step.unit || '' };
        }))
        .concat([{ key: 'dashboard', name: shell.t('service.dashboard'), on: true, unit: DASHBOARD_UNIT }]);
    },

    // systemUpdatesText: die ausstehenden Systempakete (Schritt 15) mit dem
    // Stand der Paketlisten, "12 Updates · Stand 04.10.". Ohne Bericht
    // (apt-get fehlte oder scheiterte) leer, nie ein falsches "aktuell".
    systemUpdatesText: function (updates, shell) {
      if (!updates) {
        return '';
      }
      var text = updates.count ? shell.tn('system_updates.count', updates.count) : shell.t('system_updates.none');
      var day = window.Format.day(updates.checked_at, shell.lang);
      return day ? text + ' · ' + shell.t('system_updates.as_of', { day: day }) : text;
    },

    // systemPackagesText: "libssl3 3.0.11 → 3.0.13, openssl …" fuer die
    // Diagnose. Ein Paket ohne alte Version kaeme neu dazu.
    systemPackagesText: function (updates) {
      return ((updates && updates.packages) || []).map(function (pkg) {
        return pkg.name + ' ' + (pkg.from ? pkg.from + ' → ' : '') + pkg.to;
      }).join(', ');
    },

    groupOf: function (groups, stepId) {
      for (var i = 0; i < groups.length; i++) {
        if (groups[i].ids.indexOf(stepId) >= 0) {
          return { number: i + 1, group: groups[i] };
        }
      }
      return null;
    },
  };

  window.Services = Services;
})();
