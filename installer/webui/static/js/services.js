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
    DASHBOARD_UNIT: DASHBOARD_UNIT,

    // isSelected: ein fehlender Schluessel ist "aus", nie "default". Sonst
    // braechte ein Update einen neuen Dienst ungefragt mit.
    isSelected: function (step, selection) {
      if (!step.optional) {
        return true;
      }
      return !!(selection && selection.steps && selection.steps[step.id] === true);
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

    runGroups: function (manifest, selection, shell) {
      var parts = Services.split(manifest);
      var serviceSteps = parts.services.concat(parts.devices);
      var subs = [{ key: 'bridge', name: shell.t('service.bridge'), on: true, unit: DASHBOARD_UNIT }]
        .concat(serviceSteps.map(function (step) {
          return { key: step.id, name: Services.serviceName(step, shell), on: Services.isSelected(step, selection), unit: step.unit || '' };
        }))
        .concat([{ key: 'dashboard', name: shell.t('service.dashboard'), on: true, unit: DASHBOARD_UNIT }]);
      var servicesGroup = {
        key: 'services', label: shell.t('run.group.services'),
        ids: [CORE_STEP].concat(serviceSteps.map(function (step) { return step.id; })), subs: subs,
      };

      var groups = [];
      var placed = false;
      var steps = (manifest && manifest.steps) || [];
      var present = {};
      steps.forEach(function (step) { present[step.id] = true; });
      steps.forEach(function (step) {
        if (step.service_id || present[RUN_GROUP_OF[step.id]]) {
          return;
        }
        if (step.id === CORE_STEP) {
          groups.push(servicesGroup);
          placed = true;
          return;
        }
        var ids = [step.id].concat(steps.filter(function (other) {
          return RUN_GROUP_OF[other.id] === step.id;
        }).map(function (other) { return other.id; }));
        groups.push({ key: 'step-' + step.id, label: Services.stepName(step.id, shell), ids: ids, subs: null });
      });
      if (!placed && serviceSteps.length) {
        servicesGroup.ids = servicesGroup.ids.slice(1);
        groups.push(servicesGroup);
      }
      return groups;
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
