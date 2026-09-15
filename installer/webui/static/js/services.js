// Wie die Oberflaeche Schritte zu Diensten ordnet. Konfiguration, Vorschau,
// Ausfuehrung und Diagnose zeigen dieselbe Ordnung - sie entsteht hier und
// nur hier, aus manifest.steps[].kind (E7: ein neuer Dienst braucht keine
// Aenderung an der Oberflaeche).
(function () {
  'use strict';

  // Schritt 60 installiert das Dashboard-Binary; die MQTT-Bruecke laeuft darin.
  var CORE_STEP = '60';
  var DASHBOARD_UNIT = 'energy-node-dashboard.service';

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
      ((manifest && manifest.steps) || []).forEach(function (step) {
        if (step.service_id) {
          return;
        }
        if (step.id === CORE_STEP) {
          groups.push(servicesGroup);
          placed = true;
          return;
        }
        groups.push({ key: 'step-' + step.id, label: Services.stepName(step.id, shell), ids: [step.id], subs: null });
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
