package config

import (
	"os"
	"path/filepath"
	"testing"
)

// The battery schema is the first one with conditional entries. These cases
// pin the three things that must hold for real files: the shipped example
// (and thus the Pi's file) still validates, a DC-only system validates, and
// fields of an inactive branch are rejected.
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
		"ac topic in dc only": {`[{"id":"b","name":"B","system_type":"dc_only","charger_power_topic":"x"}]`,
			"$[0].charger_power_topic is not allowed"},
		"bank b field in single bank": {`[{"id":"b","name":"B","bank_b_enabled":false,"bank_b_cell_count":8}]`,
			"$[0].bank_b_cell_count is not allowed"},
		"series field in parallel": {`[{"id":"b","name":"B","bank_b_voltage_topic":"x"}]`,
			"$[0].bank_b_voltage_topic is not allowed"},
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
