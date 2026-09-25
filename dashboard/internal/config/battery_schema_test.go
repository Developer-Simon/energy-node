package config

import (
	"os"
	"path/filepath"
	"testing"
)

// The battery schema is the first one with conditional entries. These cases
// pin what must hold for real files: the shipped example and the Pi's file
// still validate, a DC-only system validates, fields of an inactive branch
// are tolerated but type-checked, and fields no branch declares are rejected.
func batterySchema(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "services", "battery_soc", "battery_soc_devices.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestBatterySchemaAcceptsTheShippedConfig(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "services", "battery_soc", "battery_soc_devices.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateDocument(data, batterySchema(t)); err != nil {
		t.Fatal(err)
	}
}

func TestBatterySchemaCases(t *testing.T) {
	cases := map[string]struct {
		doc  string
		want string
	}{
		"dc only single bank": {`[{"id":"b","name":"B","system_type":"dc_only","bank_b_enabled":false,
			"charger_dc_power_topic":"ina/i","charger_dc_power_unit":"A","inverter_dc_power_topic":"ina/i",
			"inverter_dc_power_unit":"A","inverter_dc_power_invert":true,"bank_a_voltage_topic":"ina/v",
			"bank_a_cell_count":5,"bank_a_capacity_ah":3.6}]`, ""},
		"series with stack sensor": {`[{"id":"b","name":"B","topology":"series","bank_a_voltage_topic":"a",
			"bank_b_voltage_topic":"b","bank_a_voltage_measures":"stack"}]`, ""},
		// The Pi's live file (2026-09-25): written by the old form, it keeps
		// bank_b_voltage_scale on a parallel pack.
		"pi file with series field on parallel": {`[{"id":"b","name":"B","topology":"parallel","bank_b_enabled":true,
			"charger_power_topic":"c","inverter_power_topic":"i","charger_dc_power_topic":"d",
			"bank_a_voltage_topic":"v","bank_a_voltage_scale":1,"bank_b_voltage_scale":1}]`, ""},
		"ac topic in dc only is left to the service": {`[{"id":"b","name":"B","system_type":"dc_only","charger_power_topic":"x"}]`, ""},
		"bank b field in single bank":                {`[{"id":"b","name":"B","bank_b_enabled":false,"bank_b_cell_count":8}]`, ""},
		"inactive field is type-checked": {`[{"id":"b","name":"B","bank_b_enabled":false,"bank_b_cell_count":"x"}]`,
			"$[0].bank_b_cell_count must be integer"},
		"current unit on ac": {`[{"id":"b","name":"B","charger_power_unit":"A"}]`,
			"$[0].charger_power_unit is not allowed"},
		"unknown unit": {`[{"id":"b","name":"B","charger_dc_power_unit":"kW"}]`,
			"$[0].charger_dc_power_unit has an unsupported value"},
	}
	for name, tc := range cases {
		err := ValidateDocument([]byte(tc.doc), batterySchema(t))
		got := ""
		if err != nil {
			got = err.Error()
		}
		if got != tc.want {
			t.Errorf("%s: got %q, want %q", name, got, tc.want)
		}
	}
}
